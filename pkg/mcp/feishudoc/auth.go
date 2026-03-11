package feishudoc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
	larksearch "github.com/larksuite/oapi-sdk-go/v3/service/search/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/identity"
)

const (
	authModeApp  = "app"
	authModeUser = "user"

	capabilityBoundaryText = "不支持飞书客户端最近访问视图，只支持搜索/浏览根空间/浏览文件夹"
	authModeUserLabel      = "当前为用户态操作"
	authModeAppLabel       = "当前为应用态操作"
)

var (
	errBoundToAnotherUser          = errors.New("该实例已绑定到另一位飞书用户")
	errSystemSenderNeedsDelegation = errors.New("当前请求来自系统定时/异步触发，缺少用户委托身份。请在用户会话中重新创建定时任务后重试")
	supportedSearchTypes           = []string{"docx", "doc", "sheet", "slides", "bitable", "wiki", "mindnote"}
)

type callAuthContext struct {
	Mode               string
	RequestOptions     []larkcore.RequestOptionFunc
	BoundIdentityMatch *bool
	SenderRawID        string
}

type fileMeta struct {
	Token            string
	Type             string
	Title            string
	URL              string
	OwnerID          string
	CreateTime       string
	LatestModifyTime string
	Resolved         bool
}

func (s *Server) selectAuthContext(meta invokeMeta) (*callAuthContext, error) {
	ctx := &callAuthContext{
		Mode:        authModeApp,
		SenderRawID: normalizeSenderIdentity(meta.SenderID),
	}

	cred, err := auth.GetCredential(auth.FeishuCredentialProvider)
	if err != nil {
		return ctx, fmt.Errorf("load feishu binding failed: %w", err)
	}
	if cred == nil {
		return ctx, nil
	}

	if cred.NeedsRefresh() && strings.TrimSpace(cred.RefreshToken) != "" {
		refreshed, refreshErr := auth.RefreshFeishuAccessToken(cred, s.appID, s.appSecret)
		if refreshErr != nil {
			return &callAuthContext{
				Mode:               authModeUser,
				BoundIdentityMatch: boolPtr(false),
				SenderRawID:        ctx.SenderRawID,
			}, refreshErr
		}
		cred = refreshed
		if saveErr := auth.SetCredential(auth.FeishuCredentialProvider, cred); saveErr != nil {
			return &callAuthContext{
				Mode:               authModeUser,
				BoundIdentityMatch: boolPtr(false),
				SenderRawID:        ctx.SenderRawID,
			}, fmt.Errorf("persist refreshed feishu token failed: %w", saveErr)
		}
	}

	match := senderMatchesBoundIdentity(ctx.SenderRawID, cred)
	if !match {
		if isSystemSenderIdentity(ctx.SenderRawID) {
			return &callAuthContext{
				Mode:               authModeUser,
				BoundIdentityMatch: boolPtr(false),
				SenderRawID:        ctx.SenderRawID,
			}, errSystemSenderNeedsDelegation
		}
		return &callAuthContext{
			Mode:               authModeUser,
			BoundIdentityMatch: boolPtr(false),
			SenderRawID:        ctx.SenderRawID,
		}, errBoundToAnotherUser
	}
	if strings.TrimSpace(cred.AccessToken) == "" {
		return &callAuthContext{
			Mode:               authModeUser,
			BoundIdentityMatch: boolPtr(true),
			SenderRawID:        ctx.SenderRawID,
		}, fmt.Errorf("feishu binding is missing access token")
	}
	if missing := auth.MissingFeishuScopes(cred.Scope); len(missing) > 0 {
		return &callAuthContext{
				Mode:               authModeUser,
				BoundIdentityMatch: boolPtr(true),
				SenderRawID:        ctx.SenderRawID,
			}, fmt.Errorf(
				"当前飞书绑定缺少文档权限（缺失: %s）。请先在飞书开放平台开通对应权限，然后重新执行 picoclaw auth login --provider feishu 或在 launcher 中重新登录",
				strings.Join(missing, ", "),
			)
	}

	return &callAuthContext{
		Mode:               authModeUser,
		RequestOptions:     []larkcore.RequestOptionFunc{larkcore.WithUserAccessToken(cred.AccessToken)},
		BoundIdentityMatch: boolPtr(true),
		SenderRawID:        ctx.SenderRawID,
	}, nil
}

func normalizeSenderIdentity(senderID string) string {
	senderID = strings.TrimSpace(senderID)
	if senderID == "" {
		return ""
	}
	if platform, rawID, ok := identity.ParseCanonicalID(senderID); ok && strings.EqualFold(platform, "feishu") {
		return strings.TrimSpace(rawID)
	}
	return senderID
}

func senderMatchesBoundIdentity(senderID string, cred *auth.AuthCredential) bool {
	senderID = strings.TrimSpace(senderID)
	if senderID == "" || cred == nil {
		return false
	}
	for _, candidate := range []string{cred.UserID, cred.OpenID, cred.UnionID} {
		if strings.TrimSpace(candidate) != "" && senderID == strings.TrimSpace(candidate) {
			return true
		}
	}
	return false
}

func isSystemSenderIdentity(senderID string) bool {
	senderID = strings.TrimSpace(senderID)
	if senderID == "" {
		return true
	}

	lowered := strings.ToLower(senderID)
	if lowered == "cron" || lowered == "system" {
		return true
	}
	if strings.HasPrefix(lowered, "async:") {
		return true
	}
	return false
}

func authLabel(mode string) string {
	if mode == authModeUser {
		return authModeUserLabel
	}
	return authModeAppLabel
}

func attachCommonResultFields(payload map[string]any, authCtx *callAuthContext) map[string]any {
	if payload == nil {
		payload = make(map[string]any)
	}
	mode := authModeApp
	if authCtx != nil && authCtx.Mode != "" {
		mode = authCtx.Mode
	}
	payload["auth_mode"] = mode
	payload["auth_mode_label"] = authLabel(mode)
	payload["capability_boundary"] = capabilityBoundaryText
	return payload
}

func errorTextResult(err error, authCtx *callAuthContext) *mcp.CallToolResult {
	result := textResult(attachCommonResultFields(map[string]any{
		"status": "error",
		"error":  err.Error(),
	}, authCtx))
	result.IsError = true
	return result
}

func docSearchResultToMap(item *larksearch.DocResUnit) map[string]any {
	if item == nil || item.ResultMeta == nil {
		return map[string]any{}
	}
	meta := item.ResultMeta
	fileType := strings.ToLower(strings.TrimSpace(strVal(meta.DocTypes)))
	title := strings.TrimSpace(stripHighlight(strVal(item.TitleHighlighted)))
	if title == "" {
		title = strings.TrimSpace(strVal(meta.Token))
	}
	return map[string]any{
		"doc_token":       strVal(meta.Token),
		"token":           strVal(meta.Token),
		"name":            title,
		"title":           title,
		"type":            fileType,
		"url":             strVal(meta.Url),
		"owner_id":        strVal(meta.OwnerId),
		"owner_name":      strVal(meta.OwnerName),
		"created_time":    intString(meta.CreateTime),
		"modified_time":   intString(meta.UpdateTime),
		"summary":         stripHighlight(strVal(item.SummaryHighlighted)),
		"entity_type":     strVal(item.EntityType),
		"title_highlight": strVal(item.TitleHighlighted),
	}
}

func stripHighlight(raw string) string {
	replacer := strings.NewReplacer("<h>", "", "</h>", "")
	return strings.TrimSpace(replacer.Replace(raw))
}

func intString(v *int) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%d", *v)
}

func normalizeFileToken(raw string) string {
	token := strings.TrimSpace(raw)
	if token == "" {
		return ""
	}

	patterns := []string{
		"/docx/",
		"/file/",
		"/docs/",
		"/sheet/",
		"/sheets/",
		"/slides/",
		"/base/",
		"/bitable/",
		"/wiki/",
		"/mindnotes/",
		"/mindnote/",
	}
	for _, pattern := range patterns {
		if idx := strings.Index(token, pattern); idx >= 0 {
			token = token[idx+len(pattern):]
			break
		}
	}

	prefixes := []string{
		"docx/",
		"file/",
		"docs/",
		"doc/",
		"sheet/",
		"sheets/",
		"slides/",
		"base/",
		"bitable/",
		"wiki/",
		"mindnotes/",
		"mindnote/",
	}
	for _, prefix := range prefixes {
		token = strings.TrimPrefix(token, prefix)
	}
	if cut := strings.IndexAny(token, "?#/"); cut >= 0 {
		token = token[:cut]
	}
	return strings.TrimSpace(token)
}

func (s *Server) resolveFileMeta(ctx context.Context, token string, authCtx *callAuthContext) (*fileMeta, error) {
	token = normalizeFileToken(token)
	if token == "" {
		return nil, errors.New("doc_token is required")
	}

	// 中文注释：先尝试 docx，再尝试 file，其它类型按搜索支持列表兜底。
	candidates := make([]string, 0, 2+len(supportedSearchTypes))
	seen := make(map[string]struct{}, 2+len(supportedSearchTypes))
	for _, candidate := range append([]string{feishuDocType, feishuFileType}, supportedSearchTypes...) {
		candidate = strings.ToLower(strings.TrimSpace(candidate))
		if candidate == "" {
			continue
		}
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		candidates = append(candidates, candidate)
	}

	for _, candidate := range candidates {
		reqDoc := larkdrive.NewRequestDocBuilder().DocToken(token).DocType(candidate).Build()
		metaReq := larkdrive.NewBatchQueryMetaReqBuilder().
			MetaRequest(larkdrive.NewMetaRequestBuilder().
				RequestDocs([]*larkdrive.RequestDoc{reqDoc}).
				WithUrl(true).
				Build()).
			Build()
		resp, err := s.client.Drive.V1.Meta.BatchQuery(ctx, metaReq, authCtx.RequestOptions...)
		if err != nil || !resp.Success() || resp.Data == nil || len(resp.Data.Metas) == 0 {
			continue
		}

		meta := resp.Data.Metas[0]
		fileType := strings.ToLower(strings.TrimSpace(strVal(meta.DocType)))
		if fileType == "" {
			fileType = candidate
		}
		return &fileMeta{
			Token:            strVal(meta.DocToken),
			Type:             fileType,
			Title:            strVal(meta.Title),
			URL:              strVal(meta.Url),
			OwnerID:          strVal(meta.OwnerId),
			CreateTime:       strVal(meta.CreateTime),
			LatestModifyTime: strVal(meta.LatestModifyTime),
			Resolved:         true,
		}, nil
	}

	return &fileMeta{Token: token, Type: feishuDocType, Resolved: false}, nil
}
