package feishudoc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/adler32"
	"sort"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdocs "github.com/larksuite/oapi-sdk-go/v3/service/docs/v1"
	larkdocx "github.com/larksuite/oapi-sdk-go/v3/service/docx/v1"
	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
	larksearch "github.com/larksuite/oapi-sdk-go/v3/service/search/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
)

const (
	serverName    = "picoclaw-feishu-doc"
	serverVersion = "1.0.0"

	toolDocSearch        = "doc_search"
	toolDocRead          = "doc_read"
	toolDocFolderCreate  = "doc_folder_create"
	toolDocFileUpload    = "doc_file_upload"
	toolDocCreate        = "doc_create"
	toolDocUpdate        = "doc_update"
	toolDocComment       = "doc_comment"
	toolDocShare         = "doc_share"
	toolDocSetPermission = "doc_set_permission"
	toolDocDelete        = "doc_delete"

	feishuDocType            = "docx"
	feishuFolderType         = "folder"
	defaultConfirmTTL        = 5 * time.Minute
	defaultSearchPageSize    = 100
	maxSearchPageSize        = 200
	defaultReadPageSize      = 200
	maxReadPageSize          = 500
	defaultMaxReadContentLen = 12000
	maxReadContentLen        = 200000
	maxListChildrenPageCount = 20
)

type Server struct {
	client        *lark.Client
	confirmations *confirmationManager
	audit         *auditLogger
	now           func() time.Time
	appID         string
	appSecret     string
}

type Options struct {
	AppID           string
	AppSecret       string
	Workspace       string
	ConfirmationTTL time.Duration
}

type docSearchInput struct {
	Query     string `json:"query,omitempty"`
	Folder    string `json:"folder_token,omitempty"`
	PageToken string `json:"page_token,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
	Channel   string `json:"__channel,omitempty"`
	ChatID    string `json:"__chat_id,omitempty"`
	SenderID  string `json:"__sender_id,omitempty"`
}

type docReadInput struct {
	DocToken       string `json:"doc_token"`
	IncludeBlocks  bool   `json:"include_blocks,omitempty"`
	IncludeMD      *bool  `json:"include_markdown,omitempty"`
	PageSize       int    `json:"page_size,omitempty"`
	MaxContentChar int    `json:"max_content_chars,omitempty"`
	Channel        string `json:"__channel,omitempty"`
	ChatID         string `json:"__chat_id,omitempty"`
	SenderID       string `json:"__sender_id,omitempty"`
}

type docCreateInput struct {
	Title        string `json:"title"`
	FolderToken  string `json:"folder_token,omitempty"`
	Content      string `json:"content,omitempty"`
	ContentType  string `json:"content_type,omitempty"`
	ActionID     string `json:"action_id,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
	Channel      string `json:"__channel,omitempty"`
	ChatID       string `json:"__chat_id,omitempty"`
	SenderID     string `json:"__sender_id,omitempty"`
}

type docFolderCreateInput struct {
	Name         string `json:"name"`
	FolderToken  string `json:"folder_token,omitempty"`
	ActionID     string `json:"action_id,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
	Channel      string `json:"__channel,omitempty"`
	ChatID       string `json:"__chat_id,omitempty"`
	SenderID     string `json:"__sender_id,omitempty"`
}

type docFileUploadInput struct {
	FolderToken  string `json:"folder_token"`
	FileName     string `json:"file_name"`
	Content      string `json:"content"`
	MIMEType     string `json:"mime_type,omitempty"`
	ActionID     string `json:"action_id,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
	Channel      string `json:"__channel,omitempty"`
	ChatID       string `json:"__chat_id,omitempty"`
	SenderID     string `json:"__sender_id,omitempty"`
}

type docUpdateInput struct {
	DocToken     string `json:"doc_token"`
	Content      string `json:"content"`
	ContentType  string `json:"content_type,omitempty"`
	Strategy     string `json:"strategy,omitempty"` // replace | append
	ActionID     string `json:"action_id,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
	Channel      string `json:"__channel,omitempty"`
	ChatID       string `json:"__chat_id,omitempty"`
	SenderID     string `json:"__sender_id,omitempty"`
}

type docCommentInput struct {
	DocToken     string `json:"doc_token"`
	Comment      string `json:"comment"`
	CommentID    string `json:"comment_id,omitempty"`
	IsWhole      *bool  `json:"is_whole,omitempty"`
	ActionID     string `json:"action_id,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
	Channel      string `json:"__channel,omitempty"`
	ChatID       string `json:"__chat_id,omitempty"`
	SenderID     string `json:"__sender_id,omitempty"`
}

type docShareInput struct {
	DocToken        string `json:"doc_token"`
	LinkShareEntity string `json:"link_share_entity,omitempty"`
	ExternalAccess  *bool  `json:"external_access,omitempty"`
	SecurityEntity  string `json:"security_entity,omitempty"`
	CommentEntity   string `json:"comment_entity,omitempty"`
	ShareEntity     string `json:"share_entity,omitempty"`
	InviteExternal  *bool  `json:"invite_external,omitempty"`
	ActionID        string `json:"action_id,omitempty"`
	Confirmation    string `json:"confirmation,omitempty"`
	Channel         string `json:"__channel,omitempty"`
	ChatID          string `json:"__chat_id,omitempty"`
	SenderID        string `json:"__sender_id,omitempty"`
}

type docSetPermissionInput struct {
	DocToken         string `json:"doc_token"`
	Operation        string `json:"operation"` // list | grant | revoke
	MemberID         string `json:"member_id,omitempty"`
	MemberType       string `json:"member_type,omitempty"`
	Perm             string `json:"perm,omitempty"`
	PermType         string `json:"perm_type,omitempty"`
	CollaboratorType string `json:"collaborator_type,omitempty"`
	NeedNotification *bool  `json:"need_notification,omitempty"`
	ActionID         string `json:"action_id,omitempty"`
	Confirmation     string `json:"confirmation,omitempty"`
	Channel          string `json:"__channel,omitempty"`
	ChatID           string `json:"__chat_id,omitempty"`
	SenderID         string `json:"__sender_id,omitempty"`
}

type docDeleteInput struct {
	DocToken     string `json:"doc_token"`
	ActionID     string `json:"action_id,omitempty"`
	Confirmation string `json:"confirmation,omitempty"`
	Channel      string `json:"__channel,omitempty"`
	ChatID       string `json:"__chat_id,omitempty"`
	SenderID     string `json:"__sender_id,omitempty"`
}

type invokeMeta struct {
	Channel    string
	ChatID     string
	SenderID   string
	ContextKey string
}

func NewFromConfig(cfg *config.Config) (*Server, error) {
	if cfg == nil {
		return nil, errors.New("config is nil")
	}

	return New(Options{
		AppID:     cfg.Channels.Feishu.AppID,
		AppSecret: cfg.Channels.Feishu.AppSecret,
		Workspace: cfg.WorkspacePath(),
	})
}

func New(opts Options) (*Server, error) {
	if strings.TrimSpace(opts.AppID) == "" {
		return nil, errors.New("feishu app_id is required")
	}
	if strings.TrimSpace(opts.AppSecret) == "" {
		return nil, errors.New("feishu app_secret is required")
	}
	if strings.TrimSpace(opts.Workspace) == "" {
		return nil, errors.New("workspace is required")
	}

	ttl := opts.ConfirmationTTL
	if ttl <= 0 {
		ttl = defaultConfirmTTL
	}

	return &Server{
		client:        lark.NewClient(opts.AppID, opts.AppSecret),
		confirmations: newConfirmationManager(ttl),
		audit:         newAuditLogger(opts.Workspace),
		now:           time.Now,
		appID:         strings.TrimSpace(opts.AppID),
		appSecret:     strings.TrimSpace(opts.AppSecret),
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	if s == nil {
		return errors.New("server is nil")
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    serverName,
		Version: serverVersion,
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocSearch,
		Description: "搜索或浏览飞书云文档。query 非空时搜索 docx/doc/sheet/slides/bitable/wiki/mindnote；query 为空时只浏览根空间或文件夹。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, s.handleDocSearch)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocRead,
		Description: "读取飞书文档内容。当前仅对 docx 提供完整内容读取；非 docx 返回元信息与能力说明。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
	}, s.handleDocRead)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocFolderCreate,
		Description: "在飞书云空间创建文件夹。首次调用会返回 action_id，需二次确认后执行。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true)},
	}, s.handleDocFolderCreate)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocFileUpload,
		Description: "上传文件到飞书云空间文件夹（常用于 Markdown 归档回退）。首次调用会返回 action_id，需二次确认后执行。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true)},
	}, s.handleDocFileUpload)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocCreate,
		Description: "创建飞书 docx 文档，可选写入初始内容。首次调用会返回 action_id，需二次确认后执行。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true)},
	}, s.handleDocCreate)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocUpdate,
		Description: "更新飞书 docx 文档内容，支持 replace 或 append。首次调用会返回 action_id，需二次确认后执行。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true)},
	}, s.handleDocUpdate)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocComment,
		Description: "为飞书 docx 文档创建评论或回复评论。首次调用会返回 action_id，需二次确认后执行。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true)},
	}, s.handleDocComment)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocShare,
		Description: "更新飞书文档的公开分享设置。首次调用会返回 action_id，需二次确认后执行。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true)},
	}, s.handleDocShare)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocSetPermission,
		Description: "管理飞书文档成员权限（list/grant/revoke）。grant/revoke 首次调用会返回 action_id，需二次确认后执行。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true)},
	}, s.handleDocSetPermission)

	mcp.AddTool(server, &mcp.Tool{
		Name:        toolDocDelete,
		Description: "删除飞书文档。首次调用会返回 action_id，需二次确认后执行。",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: boolPtr(true)},
	}, s.handleDocDelete)

	return server.Run(ctx, &mcp.StdioTransport{})
}

func (s *Server) handleDocSearch(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docSearchInput,
) (*mcp.CallToolResult, any, error) {
	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocSearch,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}

	pageSize := clampInt(in.PageSize, defaultSearchPageSize, 1, maxSearchPageSize)
	folderToken := normalizeFolderToken(in.Folder)
	if strings.TrimSpace(in.Folder) != "" && folderToken == "" {
		return errorTextResult(errors.New("folder_token is invalid: provide folder token or drive folder URL"), authCtx), nil, nil
	}
	query := strings.TrimSpace(in.Query)
	if query != "" {
		items, hasMore, nextPageToken, searchMode, searchErr := s.searchDocsWithFallback(
			ctx,
			query,
			folderToken,
			strings.TrimSpace(in.PageToken),
			pageSize,
			authCtx,
		)
		if searchErr != nil {
			s.auditSafe(auditRecord{
				Tool:               toolDocSearch,
				Channel:            meta.Channel,
				ChatID:             meta.ChatID,
				SenderID:           meta.SenderID,
				Status:             "error",
				AuthMode:           authCtx.Mode,
				BoundIdentityMatch: authCtx.BoundIdentityMatch,
				Error:              searchErr.Error(),
			})
			return errorTextResult(searchErr, authCtx), nil, nil
		}

		result := attachCommonResultFields(map[string]any{
			"query":           query,
			"search_mode":     searchMode,
			"returned_count":  len(items),
			"items":           items,
			"has_more":        hasMore,
			"next_page_token": nextPageToken,
			"folder_token":    folderToken,
		}, authCtx)
		s.auditSafe(auditRecord{
			Tool:               toolDocSearch,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Status:             "success",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Details: map[string]any{
				"query":          query,
				"search_mode":    searchMode,
				"returned_count": len(items),
				"folder_token":   folderToken,
			},
		})
		return textResult(result), nil, nil
	}

	items, hasMore, nextPageToken, err := s.listDriveItems(
		ctx,
		folderToken,
		strings.TrimSpace(in.PageToken),
		pageSize,
		authCtx,
	)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocSearch,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}

	searchMode := "browse_root"
	if folderToken != "" {
		searchMode = "browse_folder"
	}

	result := attachCommonResultFields(map[string]any{
		"query":           "",
		"search_mode":     searchMode,
		"returned_count":  len(items),
		"items":           items,
		"has_more":        hasMore,
		"next_page_token": nextPageToken,
		"folder_token":    folderToken,
	}, authCtx)

	s.auditSafe(auditRecord{
		Tool:               toolDocSearch,
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details: map[string]any{
			"query":          "",
			"search_mode":    searchMode,
			"returned_count": len(items),
			"folder_token":   folderToken,
		},
	})

	return textResult(result), nil, nil
}

func (s *Server) handleDocRead(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docReadInput,
) (*mcp.CallToolResult, any, error) {
	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocRead,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}

	docToken := normalizeFileToken(in.DocToken)
	if docToken == "" {
		return errorTextResult(errors.New("doc_token is required"), authCtx), nil, nil
	}

	metaInfo, metaErr := s.resolveFileMeta(ctx, docToken, authCtx)
	if metaErr != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocRead,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              metaErr.Error(),
		})
		return errorTextResult(metaErr, authCtx), nil, nil
	}

	if metaInfo.Type != feishuDocType {
		out := attachCommonResultFields(map[string]any{
			"doc_token":      metaInfo.Token,
			"token":          metaInfo.Token,
			"title":          metaInfo.Title,
			"type":           metaInfo.Type,
			"url":            metaInfo.URL,
			"owner_id":       metaInfo.OwnerID,
			"created_time":   metaInfo.CreateTime,
			"modified_time":  metaInfo.LatestModifyTime,
			"content_status": "metadata_only",
			"message":        "当前仅对 docx 提供完整内容读取，其他类型先返回元信息与能力说明。",
		}, authCtx)
		s.auditSafe(auditRecord{
			Tool:               toolDocRead,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             metaInfo.Token,
			Status:             "success",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Details:            map[string]any{"type": metaInfo.Type, "content_status": "metadata_only"},
		})
		return textResult(out), nil, nil
	}

	metaResp, err := s.client.Docx.V1.Document.Get(ctx, larkdocx.NewGetDocumentReqBuilder().
		DocumentId(docToken).
		Build(), authCtx.RequestOptions...)
	if err != nil {
		return errorTextResult(fmt.Errorf("get document failed: %w", err), authCtx), nil, nil
	}
	if !metaResp.Success() {
		return errorTextResult(fmt.Errorf("get document api error: code=%d msg=%s", metaResp.Code, metaResp.Msg), authCtx), nil, nil
	}

	rawResp, err := s.client.Docx.V1.Document.RawContent(ctx, larkdocx.NewRawContentDocumentReqBuilder().
		DocumentId(docToken).
		Lang(larkdocx.LangZH).
		Build(), authCtx.RequestOptions...)
	if err != nil {
		return errorTextResult(fmt.Errorf("get raw content failed: %w", err), authCtx), nil, nil
	}
	if !rawResp.Success() {
		return errorTextResult(fmt.Errorf("raw content api error: code=%d msg=%s", rawResp.Code, rawResp.Msg), authCtx), nil, nil
	}

	maxContent := clampInt(in.MaxContentChar, defaultMaxReadContentLen, 256, maxReadContentLen)
	rawContent, rawTruncated := truncateText(strVal(rawResp.Data.Content), maxContent)

	includeMarkdown := true
	if in.IncludeMD != nil {
		includeMarkdown = *in.IncludeMD
	}

	markdownContent := ""
	mdTruncated := false
	mdError := ""
	if includeMarkdown {
		mdResp, mdErr := s.client.Docs.V1.Content.Get(ctx, larkdocs.NewGetContentReqBuilder().
			DocToken(docToken).
			DocType(feishuDocType).
			ContentType(larkdocx.ContentTypeMarkdown).
			Lang("zh").
			Build(), authCtx.RequestOptions...)
		if mdErr != nil {
			mdError = mdErr.Error()
		} else if !mdResp.Success() {
			mdError = fmt.Sprintf("code=%d msg=%s", mdResp.Code, mdResp.Msg)
		} else {
			markdownContent, mdTruncated = truncateText(strVal(mdResp.Data.Content), maxContent)
		}
	}

	out := attachCommonResultFields(map[string]any{
		"doc_token":             docToken,
		"token":                 docToken,
		"title":                 strVal(metaResp.Data.Document.Title),
		"type":                  feishuDocType,
		"url":                   metaInfo.URL,
		"revision_id":           intVal(metaResp.Data.Document.RevisionId),
		"raw_content":           rawContent,
		"raw_content_truncated": rawTruncated,
	}, authCtx)

	if includeMarkdown {
		out["markdown_content"] = markdownContent
		out["markdown_truncated"] = mdTruncated
		if mdError != "" {
			out["markdown_error"] = mdError
		}
	}

	if in.IncludeBlocks {
		pageSize := clampInt(in.PageSize, defaultReadPageSize, 1, maxReadPageSize)
		blockItems, hasMore, nextPageToken, blockErr := s.listBlockSummaries(ctx, docToken, pageSize, authCtx)
		if blockErr != nil {
			out["blocks_error"] = blockErr.Error()
		} else {
			out["blocks"] = blockItems
			out["blocks_has_more"] = hasMore
			out["blocks_next_page_token"] = nextPageToken
		}
	}

	s.auditSafe(auditRecord{
		Tool:               toolDocRead,
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Target:             docToken,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details:            map[string]any{"include_blocks": in.IncludeBlocks, "type": feishuDocType},
	})

	return textResult(out), nil, nil
}

func (s *Server) handleDocFolderCreate(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docFolderCreateInput,
) (*mcp.CallToolResult, any, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return errorTextResult(errors.New("name is required"), nil), nil, nil
	}

	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocFolderCreate,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}

	parentFolderToken := normalizeFolderToken(in.FolderToken)
	if strings.TrimSpace(in.FolderToken) != "" && parentFolderToken == "" {
		return errorTextResult(errors.New("folder_token is invalid: provide folder token or drive folder URL"), authCtx), nil, nil
	}

	payload := map[string]any{
		"name":         name,
		"folder_token": parentFolderToken,
	}
	action, pendingResult := s.prepareWriteAction(
		toolDocFolderCreate,
		meta,
		authCtx,
		in.ActionID,
		in.Confirmation,
		payload,
		fmt.Sprintf("创建文件夹《%s》", name),
		parentFolderToken,
	)
	if pendingResult != nil {
		return pendingResult, nil, nil
	}

	reqBody := larkdrive.NewCreateFolderFileReqBodyBuilder().Name(name)
	if parentFolderToken != "" {
		reqBody.FolderToken(parentFolderToken)
	}

	resp, createErr := s.client.Drive.V1.File.CreateFolder(ctx, larkdrive.NewCreateFolderFileReqBuilder().
		Body(reqBody.Build()).
		Build(), authCtx.RequestOptions...)
	if createErr != nil {
		return s.writeErrorWithAudit(toolDocFolderCreate, meta, authCtx, action, createErr), nil, nil
	}
	if !resp.Success() {
		return s.writeErrorWithAudit(
			toolDocFolderCreate,
			meta,
			authCtx,
			action,
			fmt.Errorf("create folder api error: code=%d msg=%s", resp.Code, resp.Msg),
		), nil, nil
	}

	folderToken := strVal(resp.Data.Token)
	if folderToken == "" {
		return s.writeErrorWithAudit(
			toolDocFolderCreate,
			meta,
			authCtx,
			action,
			errors.New("empty folder token returned by create folder api"),
		), nil, nil
	}

	if action != nil {
		s.confirmations.Consume(action.ID)
	}

	result := attachCommonResultFields(map[string]any{
		"status":              "ok",
		"folder_token":        folderToken,
		"token":               folderToken,
		"type":                feishuFolderType,
		"name":                name,
		"title":               name,
		"url":                 strVal(resp.Data.Url),
		"parent_folder_token": parentFolderToken,
		"action_id":           actionIDOrEmpty(action),
	}, authCtx)

	s.auditSafe(auditRecord{
		Tool:               toolDocFolderCreate,
		ActionID:           actionIDOrEmpty(action),
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Target:             folderToken,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details: map[string]any{
			"name":                name,
			"parent_folder_token": parentFolderToken,
		},
	})

	return textResult(result), nil, nil
}

func (s *Server) handleDocFileUpload(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docFileUploadInput,
) (*mcp.CallToolResult, any, error) {
	rawFolderToken := strings.TrimSpace(in.FolderToken)
	folderToken := normalizeFolderToken(in.FolderToken)
	if rawFolderToken != "" && folderToken == "" {
		return errorTextResult(errors.New("folder_token is invalid: provide folder token or drive folder URL"), nil), nil, nil
	}
	if folderToken == "" {
		return errorTextResult(errors.New("folder_token is required"), nil), nil, nil
	}
	fileName := strings.TrimSpace(in.FileName)
	if fileName == "" {
		return errorTextResult(errors.New("file_name is required"), nil), nil, nil
	}
	if strings.TrimSpace(in.Content) == "" {
		return errorTextResult(errors.New("content is required"), nil), nil, nil
	}
	mimeType := normalizeUploadMIMEType(in.MIMEType)

	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocFileUpload,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             folderToken,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}

	payload := map[string]any{
		"folder_token": folderToken,
		"file_name":    fileName,
		"content":      in.Content,
		"mime_type":    mimeType,
	}
	action, pendingResult := s.prepareWriteAction(
		toolDocFileUpload,
		meta,
		authCtx,
		in.ActionID,
		in.Confirmation,
		payload,
		fmt.Sprintf("上传文件《%s》到文件夹 %s", fileName, folderToken),
		folderToken,
	)
	if pendingResult != nil {
		return pendingResult, nil, nil
	}

	contentBytes := []byte(in.Content)
	checksum := fmt.Sprintf("%d", adler32.Checksum(contentBytes))
	uploadReqBody := larkdrive.NewUploadAllFileReqBodyBuilder().
		FileName(fileName).
		ParentType(larkdrive.ParentTypeExplorer).
		ParentNode(folderToken).
		Size(len(contentBytes)).
		Checksum(checksum).
		File(bytes.NewReader(contentBytes))

	uploadResp, uploadErr := s.client.Drive.V1.File.UploadAll(ctx, larkdrive.NewUploadAllFileReqBuilder().
		Body(uploadReqBody.Build()).
		Build(), authCtx.RequestOptions...)
	if uploadErr != nil {
		return s.writeErrorWithAudit(toolDocFileUpload, meta, authCtx, action, uploadErr), nil, nil
	}
	if !uploadResp.Success() {
		return s.writeErrorWithAudit(
			toolDocFileUpload,
			meta,
			authCtx,
			action,
			fmt.Errorf("upload file api error: code=%d msg=%s", uploadResp.Code, uploadResp.Msg),
		), nil, nil
	}

	fileToken := strVal(uploadResp.Data.FileToken)
	if fileToken == "" {
		return s.writeErrorWithAudit(
			toolDocFileUpload,
			meta,
			authCtx,
			action,
			errors.New("empty file token returned by upload api"),
		), nil, nil
	}

	if action != nil {
		s.confirmations.Consume(action.ID)
	}

	result := attachCommonResultFields(map[string]any{
		"status":       "ok",
		"file_token":   fileToken,
		"token":        fileToken,
		"type":         "file",
		"file_name":    fileName,
		"folder_token": folderToken,
		"mime_type":    mimeType,
		"size":         len(contentBytes),
		"checksum":     checksum,
		"action_id":    actionIDOrEmpty(action),
	}, authCtx)

	s.auditSafe(auditRecord{
		Tool:               toolDocFileUpload,
		ActionID:           actionIDOrEmpty(action),
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Target:             fileToken,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details: map[string]any{
			"file_name":    fileName,
			"folder_token": folderToken,
			"size":         len(contentBytes),
		},
	})

	return textResult(result), nil, nil
}

func (s *Server) handleDocCreate(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docCreateInput,
) (*mcp.CallToolResult, any, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return errorTextResult(errors.New("title is required"), nil), nil, nil
	}

	contentType, err := normalizeContentType(in.ContentType)
	if err != nil {
		return errorTextResult(err, nil), nil, nil
	}

	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocCreate,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}
	folderToken := normalizeFolderToken(in.FolderToken)
	if strings.TrimSpace(in.FolderToken) != "" && folderToken == "" {
		return errorTextResult(errors.New("folder_token is invalid: provide folder token or drive folder URL"), authCtx), nil, nil
	}
	payload := map[string]any{
		"title":        title,
		"folder_token": folderToken,
		"content":      in.Content,
		"content_type": contentType,
	}

	preview := fmt.Sprintf("创建文档《%s》", title)
	if strings.TrimSpace(in.Content) != "" {
		preview += "并写入初始内容"
	}

	action, pendingResult := s.prepareWriteAction(
		toolDocCreate,
		meta,
		authCtx,
		in.ActionID,
		in.Confirmation,
		payload,
		preview,
		"",
	)
	if pendingResult != nil {
		return pendingResult, nil, nil
	}

	createReqBody := larkdocx.NewCreateDocumentReqBodyBuilder().Title(title)
	if folderToken != "" {
		createReqBody.FolderToken(folderToken)
	}
	createResp, createErr := s.client.Docx.V1.Document.Create(ctx, larkdocx.NewCreateDocumentReqBuilder().
		Body(createReqBody.Build()).
		Build(), authCtx.RequestOptions...)
	if createErr != nil {
		return s.writeErrorWithAudit(toolDocCreate, meta, authCtx, action, createErr), nil, nil
	}
	if !createResp.Success() {
		return s.writeErrorWithAudit(
			toolDocCreate,
			meta,
			authCtx,
			action,
			fmt.Errorf("create doc api error: code=%d msg=%s", createResp.Code, createResp.Msg),
		), nil, nil
	}

	docToken := strVal(createResp.Data.Document.DocumentId)
	if docToken == "" {
		err := errors.New("empty doc token returned by create api")
		return s.writeErrorWithAudit(toolDocCreate, meta, authCtx, action, err), nil, nil
	}

	if strings.TrimSpace(in.Content) != "" {
		if replaceErr := s.replaceDocumentContent(ctx, docToken, in.Content, contentType, authCtx); replaceErr != nil {
			return s.writeErrorWithAudit(toolDocCreate, meta, authCtx, action, replaceErr), nil, nil
		}
	}

	if action != nil {
		s.confirmations.Consume(action.ID)
	}

	result := attachCommonResultFields(map[string]any{
		"status":      "ok",
		"doc_token":   docToken,
		"token":       docToken,
		"type":        feishuDocType,
		"title":       title,
		"doc_url":     fmt.Sprintf("https://feishu.cn/docx/%s", docToken),
		"action_id":   actionIDOrEmpty(action),
		"content_set": strings.TrimSpace(in.Content) != "",
	}, authCtx)
	s.auditSafe(auditRecord{
		Tool:               toolDocCreate,
		ActionID:           actionIDOrEmpty(action),
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Target:             docToken,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details: map[string]any{
			"title":       title,
			"content_set": strings.TrimSpace(in.Content) != "",
		},
	})

	return textResult(result), nil, nil
}

func (s *Server) handleDocUpdate(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docUpdateInput,
) (*mcp.CallToolResult, any, error) {
	docToken := normalizeDocToken(in.DocToken)
	if docToken == "" {
		return errorTextResult(errors.New("doc_token is required"), nil), nil, nil
	}
	if strings.TrimSpace(in.Content) == "" {
		return errorTextResult(errors.New("content is required"), nil), nil, nil
	}

	contentType, err := normalizeContentType(in.ContentType)
	if err != nil {
		return errorTextResult(err, nil), nil, nil
	}

	strategy := strings.ToLower(strings.TrimSpace(in.Strategy))
	if strategy == "" {
		strategy = "replace"
	}
	if strategy != "replace" && strategy != "append" {
		return errorTextResult(errors.New("strategy must be one of: replace, append"), nil), nil, nil
	}

	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocUpdate,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}
	payload := map[string]any{
		"doc_token":    docToken,
		"content":      in.Content,
		"content_type": contentType,
		"strategy":     strategy,
	}

	action, pendingResult := s.prepareWriteAction(
		toolDocUpdate,
		meta,
		authCtx,
		in.ActionID,
		in.Confirmation,
		payload,
		fmt.Sprintf("更新文档 %s（%s）", docToken, strategy),
		docToken,
	)
	if pendingResult != nil {
		return pendingResult, nil, nil
	}

	if strategy == "replace" {
		err = s.replaceDocumentContent(ctx, docToken, in.Content, contentType, authCtx)
	} else {
		err = s.appendDocumentContent(ctx, docToken, in.Content, contentType, authCtx)
	}
	if err != nil {
		return s.writeErrorWithAudit(toolDocUpdate, meta, authCtx, action, err), nil, nil
	}

	if action != nil {
		s.confirmations.Consume(action.ID)
	}

	result := attachCommonResultFields(map[string]any{
		"status":      "ok",
		"doc_token":   docToken,
		"token":       docToken,
		"type":        feishuDocType,
		"strategy":    strategy,
		"action_id":   actionIDOrEmpty(action),
		"content_len": len(in.Content),
	}, authCtx)
	s.auditSafe(auditRecord{
		Tool:               toolDocUpdate,
		ActionID:           actionIDOrEmpty(action),
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Target:             docToken,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details: map[string]any{
			"strategy":    strategy,
			"content_len": len(in.Content),
		},
	})

	return textResult(result), nil, nil
}

func (s *Server) handleDocComment(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docCommentInput,
) (*mcp.CallToolResult, any, error) {
	docToken := normalizeDocToken(in.DocToken)
	if docToken == "" {
		return errorTextResult(errors.New("doc_token is required"), nil), nil, nil
	}
	comment := strings.TrimSpace(in.Comment)
	if comment == "" {
		return errorTextResult(errors.New("comment is required"), nil), nil, nil
	}

	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocComment,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}
	payload := map[string]any{
		"doc_token":  docToken,
		"comment":    comment,
		"comment_id": strings.TrimSpace(in.CommentID),
		"is_whole":   boolPtrValue(in.IsWhole, true),
	}
	action, pendingResult := s.prepareWriteAction(
		toolDocComment,
		meta,
		authCtx,
		in.ActionID,
		in.Confirmation,
		payload,
		fmt.Sprintf("为文档 %s 添加评论", docToken),
		docToken,
	)
	if pendingResult != nil {
		return pendingResult, nil, nil
	}

	replyElem := larkdrive.NewReplyElementBuilder().
		Type("text_run").
		TextRun(larkdrive.NewTextRunBuilder().Text(comment).Build()).
		Build()
	reply := larkdrive.NewFileCommentReplyBuilder().
		Content(larkdrive.NewReplyContentBuilder().Elements([]*larkdrive.ReplyElement{replyElem}).Build()).
		Build()
	replyList := larkdrive.NewReplyListBuilder().Replies([]*larkdrive.FileCommentReply{reply}).Build()

	commentBuilder := larkdrive.NewFileCommentBuilder().ReplyList(replyList).IsWhole(boolPtrValue(in.IsWhole, true))
	if strings.TrimSpace(in.CommentID) != "" {
		commentBuilder.CommentId(strings.TrimSpace(in.CommentID))
	}

	commentResp, commentErr := s.client.Drive.V1.FileComment.Create(ctx, larkdrive.NewCreateFileCommentReqBuilder().
		FileToken(docToken).
		FileType(feishuDocType).
		FileComment(commentBuilder.Build()).
		Build(), authCtx.RequestOptions...)
	if commentErr != nil {
		return s.writeErrorWithAudit(toolDocComment, meta, authCtx, action, commentErr), nil, nil
	}
	if !commentResp.Success() {
		return s.writeErrorWithAudit(
			toolDocComment,
			meta,
			authCtx,
			action,
			fmt.Errorf("create comment api error: code=%d msg=%s", commentResp.Code, commentResp.Msg),
		), nil, nil
	}

	if action != nil {
		s.confirmations.Consume(action.ID)
	}

	result := attachCommonResultFields(map[string]any{
		"status":      "ok",
		"doc_token":   docToken,
		"token":       docToken,
		"type":        feishuDocType,
		"comment_id":  strVal(commentResp.Data.CommentId),
		"is_whole":    boolVal(commentResp.Data.IsWhole),
		"action_id":   actionIDOrEmpty(action),
		"create_time": intVal(commentResp.Data.CreateTime),
	}, authCtx)
	s.auditSafe(auditRecord{
		Tool:               toolDocComment,
		ActionID:           actionIDOrEmpty(action),
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Target:             docToken,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details: map[string]any{
			"comment_id": strVal(commentResp.Data.CommentId),
			"is_reply":   strings.TrimSpace(in.CommentID) != "",
		},
	})
	return textResult(result), nil, nil
}

func (s *Server) handleDocShare(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docShareInput,
) (*mcp.CallToolResult, any, error) {
	docToken := normalizeFileToken(in.DocToken)
	if docToken == "" {
		return errorTextResult(errors.New("doc_token is required"), nil), nil, nil
	}

	reqBody := &larkdrive.PermissionPublicRequest{}
	changed := make(map[string]any)

	if strings.TrimSpace(in.LinkShareEntity) != "" {
		value := strings.TrimSpace(in.LinkShareEntity)
		reqBody.LinkShareEntity = &value
		changed["link_share_entity"] = value
	}
	if in.ExternalAccess != nil {
		reqBody.ExternalAccess = in.ExternalAccess
		changed["external_access"] = *in.ExternalAccess
	}
	if strings.TrimSpace(in.SecurityEntity) != "" {
		value := strings.TrimSpace(in.SecurityEntity)
		reqBody.SecurityEntity = &value
		changed["security_entity"] = value
	}
	if strings.TrimSpace(in.CommentEntity) != "" {
		value := strings.TrimSpace(in.CommentEntity)
		reqBody.CommentEntity = &value
		changed["comment_entity"] = value
	}
	if strings.TrimSpace(in.ShareEntity) != "" {
		value := strings.TrimSpace(in.ShareEntity)
		reqBody.ShareEntity = &value
		changed["share_entity"] = value
	}
	if in.InviteExternal != nil {
		reqBody.InviteExternal = in.InviteExternal
		changed["invite_external"] = *in.InviteExternal
	}

	if len(changed) == 0 {
		return errorTextResult(errors.New("at least one share setting must be provided"), nil), nil, nil
	}

	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocShare,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}
	metaInfo, metaErr := s.resolveFileMeta(ctx, docToken, authCtx)
	if metaErr != nil {
		return errorTextResult(metaErr, authCtx), nil, nil
	}
	action, pendingResult := s.prepareWriteAction(
		toolDocShare,
		meta,
		authCtx,
		in.ActionID,
		in.Confirmation,
		map[string]any{
			"doc_token": docToken,
			"changes":   changed,
		},
		fmt.Sprintf("更新文档 %s 的公开分享设置", docToken),
		docToken,
	)
	if pendingResult != nil {
		return pendingResult, nil, nil
	}

	shareResp, shareErr := s.client.Drive.V1.PermissionPublic.Patch(ctx, larkdrive.NewPatchPermissionPublicReqBuilder().
		Token(docToken).
		Type(metaInfo.Type).
		PermissionPublicRequest(reqBody).
		Build(), authCtx.RequestOptions...)
	if shareErr != nil {
		return s.writeErrorWithAudit(toolDocShare, meta, authCtx, action, shareErr), nil, nil
	}
	if !shareResp.Success() {
		return s.writeErrorWithAudit(
			toolDocShare,
			meta,
			authCtx,
			action,
			fmt.Errorf("share api error: code=%d msg=%s", shareResp.Code, shareResp.Msg),
		), nil, nil
	}

	if action != nil {
		s.confirmations.Consume(action.ID)
	}

	result := attachCommonResultFields(map[string]any{
		"status":            "ok",
		"doc_token":         docToken,
		"token":             docToken,
		"type":              metaInfo.Type,
		"action_id":         actionIDOrEmpty(action),
		"applied_changes":   changed,
		"permission_public": permissionPublicMap(shareResp.Data.PermissionPublic),
	}, authCtx)
	s.auditSafe(auditRecord{
		Tool:               toolDocShare,
		ActionID:           actionIDOrEmpty(action),
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Target:             docToken,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details:            changed,
	})

	return textResult(result), nil, nil
}

func (s *Server) handleDocSetPermission(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docSetPermissionInput,
) (*mcp.CallToolResult, any, error) {
	docToken := normalizeFileToken(in.DocToken)
	if docToken == "" {
		return errorTextResult(errors.New("doc_token is required"), nil), nil, nil
	}

	operation := strings.ToLower(strings.TrimSpace(in.Operation))
	if operation == "" {
		return errorTextResult(errors.New("operation is required: list, grant, revoke"), nil), nil, nil
	}

	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocSetPermission,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}
	metaInfo, metaErr := s.resolveFileMeta(ctx, docToken, authCtx)
	if metaErr != nil {
		return errorTextResult(metaErr, authCtx), nil, nil
	}
	memberType := trimDefault(strings.ToLower(strings.TrimSpace(in.MemberType)), "openid")
	collaboratorType := trimDefault(strings.ToLower(strings.TrimSpace(in.CollaboratorType)), "user")
	perm := trimDefault(strings.ToLower(strings.TrimSpace(in.Perm)), "view")
	permType := trimDefault(strings.ToLower(strings.TrimSpace(in.PermType)), "container")

	switch operation {
	case "list":
		listBuilder := larkdrive.NewListPermissionMemberReqBuilder().
			Token(docToken).
			Type(metaInfo.Type).
			Fields("*")
		if permType != "" {
			listBuilder.PermType(permType)
		}

		resp, err := s.client.Drive.V1.PermissionMember.List(ctx, listBuilder.Build(), authCtx.RequestOptions...)
		if err != nil {
			return errorTextResult(fmt.Errorf("list permissions failed: %w", err), authCtx), nil, nil
		}
		if !resp.Success() {
			return errorTextResult(fmt.Errorf("list permissions api error: code=%d msg=%s", resp.Code, resp.Msg), authCtx), nil, nil
		}

		items := make([]map[string]any, 0, len(resp.Data.Items))
		for _, member := range resp.Data.Items {
			items = append(items, map[string]any{
				"member_id":   strVal(member.MemberId),
				"member_type": strVal(member.MemberType),
				"name":        strVal(member.Name),
				"type":        strVal(member.Type),
				"perm":        strVal(member.Perm),
				"perm_type":   strVal(member.PermType),
			})
		}

		sort.Slice(items, func(i, j int) bool {
			left := strMap(items[i], "member_id")
			right := strMap(items[j], "member_id")
			return left < right
		})

		result := attachCommonResultFields(map[string]any{
			"status":      "ok",
			"operation":   operation,
			"doc_token":   docToken,
			"token":       docToken,
			"type":        metaInfo.Type,
			"items":       items,
			"items_count": len(items),
		}, authCtx)
		s.auditSafe(auditRecord{
			Tool:               toolDocSetPermission,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "success",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Details:            map[string]any{"operation": operation, "items_count": len(items), "type": metaInfo.Type},
		})
		return textResult(result), nil, nil
	case "grant":
		memberID := strings.TrimSpace(in.MemberID)
		if memberID == "" {
			return errorTextResult(errors.New("member_id is required for grant operation"), authCtx), nil, nil
		}

		payload := map[string]any{
			"operation":         operation,
			"doc_token":         docToken,
			"member_id":         memberID,
			"member_type":       memberType,
			"collaborator_type": collaboratorType,
			"perm":              perm,
			"perm_type":         permType,
			"need_notification": boolPtrValue(in.NeedNotification, false),
		}
		action, pendingResult := s.prepareWriteAction(
			toolDocSetPermission,
			meta,
			authCtx,
			in.ActionID,
			in.Confirmation,
			payload,
			fmt.Sprintf("授予成员 %s 文档 %s 权限 %s", memberID, docToken, perm),
			docToken,
		)
		if pendingResult != nil {
			return pendingResult, nil, nil
		}

		member := larkdrive.NewBaseMemberBuilder().
			MemberId(memberID).
			MemberType(memberType).
			Type(collaboratorType).
			Perm(perm).
			PermType(permType).
			Build()

		builder := larkdrive.NewCreatePermissionMemberReqBuilder().
			Token(docToken).
			Type(metaInfo.Type).
			BaseMember(member)
		if in.NeedNotification != nil {
			builder.NeedNotification(*in.NeedNotification)
		}

		resp, err := s.client.Drive.V1.PermissionMember.Create(ctx, builder.Build(), authCtx.RequestOptions...)
		if err != nil {
			return s.writeErrorWithAudit(toolDocSetPermission, meta, authCtx, action, err), nil, nil
		}
		if !resp.Success() {
			return s.writeErrorWithAudit(
				toolDocSetPermission,
				meta,
				authCtx,
				action,
				fmt.Errorf("grant permission api error: code=%d msg=%s", resp.Code, resp.Msg),
			), nil, nil
		}

		if action != nil {
			s.confirmations.Consume(action.ID)
		}

		result := attachCommonResultFields(map[string]any{
			"status":      "ok",
			"operation":   operation,
			"doc_token":   docToken,
			"token":       docToken,
			"type":        metaInfo.Type,
			"action_id":   actionIDOrEmpty(action),
			"member_id":   memberID,
			"member_type": memberType,
			"perm":        perm,
			"perm_type":   permType,
		}, authCtx)
		s.auditSafe(auditRecord{
			Tool:               toolDocSetPermission,
			ActionID:           actionIDOrEmpty(action),
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "success",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Details: map[string]any{
				"operation": operation,
				"member_id": memberID,
				"perm":      perm,
				"perm_type": permType,
				"type":      metaInfo.Type,
			},
		})
		return textResult(result), nil, nil
	case "revoke":
		memberID := strings.TrimSpace(in.MemberID)
		if memberID == "" {
			return errorTextResult(errors.New("member_id is required for revoke operation"), authCtx), nil, nil
		}

		payload := map[string]any{
			"operation":         operation,
			"doc_token":         docToken,
			"member_id":         memberID,
			"member_type":       memberType,
			"collaborator_type": collaboratorType,
			"perm_type":         permType,
		}
		action, pendingResult := s.prepareWriteAction(
			toolDocSetPermission,
			meta,
			authCtx,
			in.ActionID,
			in.Confirmation,
			payload,
			fmt.Sprintf("移除成员 %s 对文档 %s 的权限", memberID, docToken),
			docToken,
		)
		if pendingResult != nil {
			return pendingResult, nil, nil
		}

		body := larkdrive.NewDeletePermissionMemberReqBodyBuilder().
			Type(collaboratorType).
			PermType(permType).
			Build()
		resp, err := s.client.Drive.V1.PermissionMember.Delete(ctx, larkdrive.NewDeletePermissionMemberReqBuilder().
			Token(docToken).
			Type(metaInfo.Type).
			MemberId(memberID).
			MemberType(memberType).
			Body(body).
			Build(), authCtx.RequestOptions...)
		if err != nil {
			return s.writeErrorWithAudit(toolDocSetPermission, meta, authCtx, action, err), nil, nil
		}
		if !resp.Success() {
			return s.writeErrorWithAudit(
				toolDocSetPermission,
				meta,
				authCtx,
				action,
				fmt.Errorf("revoke permission api error: code=%d msg=%s", resp.Code, resp.Msg),
			), nil, nil
		}

		if action != nil {
			s.confirmations.Consume(action.ID)
		}

		result := attachCommonResultFields(map[string]any{
			"status":      "ok",
			"operation":   operation,
			"doc_token":   docToken,
			"token":       docToken,
			"type":        metaInfo.Type,
			"action_id":   actionIDOrEmpty(action),
			"member_id":   memberID,
			"member_type": memberType,
		}, authCtx)
		s.auditSafe(auditRecord{
			Tool:               toolDocSetPermission,
			ActionID:           actionIDOrEmpty(action),
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "success",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Details: map[string]any{
				"operation": operation,
				"member_id": memberID,
				"type":      metaInfo.Type,
			},
		})
		return textResult(result), nil, nil
	default:
		return errorTextResult(errors.New("operation must be one of: list, grant, revoke"), authCtx), nil, nil
	}
}

func (s *Server) handleDocDelete(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	in docDeleteInput,
) (*mcp.CallToolResult, any, error) {
	docToken := normalizeFileToken(in.DocToken)
	if docToken == "" {
		return errorTextResult(errors.New("doc_token is required"), nil), nil, nil
	}

	meta := newInvokeMeta(in.Channel, in.ChatID, in.SenderID)
	authCtx, err := s.selectAuthContext(meta)
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               toolDocDelete,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             docToken,
			Status:             "error",
			AuthMode:           authCtx.Mode,
			BoundIdentityMatch: authCtx.BoundIdentityMatch,
			Error:              err.Error(),
		})
		return errorTextResult(err, authCtx), nil, nil
	}
	metaInfo, metaErr := s.resolveFileMeta(ctx, docToken, authCtx)
	if metaErr != nil {
		return errorTextResult(metaErr, authCtx), nil, nil
	}
	action, pendingResult := s.prepareWriteAction(
		toolDocDelete,
		meta,
		authCtx,
		in.ActionID,
		in.Confirmation,
		map[string]any{"doc_token": docToken},
		fmt.Sprintf("删除文档 %s", docToken),
		docToken,
	)
	if pendingResult != nil {
		return pendingResult, nil, nil
	}

	resp, err := s.client.Drive.V1.File.Delete(ctx, larkdrive.NewDeleteFileReqBuilder().
		FileToken(docToken).
		Type(metaInfo.Type).
		Build(), authCtx.RequestOptions...)
	if err != nil {
		return s.writeErrorWithAudit(toolDocDelete, meta, authCtx, action, err), nil, nil
	}
	if !resp.Success() {
		return s.writeErrorWithAudit(
			toolDocDelete,
			meta,
			authCtx,
			action,
			fmt.Errorf("delete doc api error: code=%d msg=%s", resp.Code, resp.Msg),
		), nil, nil
	}

	if action != nil {
		s.confirmations.Consume(action.ID)
	}

	result := attachCommonResultFields(map[string]any{
		"status":    "ok",
		"doc_token": docToken,
		"token":     docToken,
		"type":      metaInfo.Type,
		"deleted":   true,
		"action_id": actionIDOrEmpty(action),
	}, authCtx)
	s.auditSafe(auditRecord{
		Tool:               toolDocDelete,
		ActionID:           actionIDOrEmpty(action),
		Channel:            meta.Channel,
		ChatID:             meta.ChatID,
		SenderID:           meta.SenderID,
		Target:             docToken,
		Status:             "success",
		AuthMode:           authCtx.Mode,
		BoundIdentityMatch: authCtx.BoundIdentityMatch,
		Details:            map[string]any{"deleted": true, "type": metaInfo.Type},
	})

	return textResult(result), nil, nil
}

func (s *Server) searchDocsWithFallback(
	ctx context.Context,
	query string,
	folderToken string,
	pageToken string,
	pageSize int,
	authCtx *callAuthContext,
) ([]map[string]any, bool, string, string, error) {
	docFilterBuilder := larksearch.NewDocFilterBuilder().DocTypes(supportedSearchTypes)
	if folderToken != "" {
		docFilterBuilder.FolderTokens([]string{folderToken})
	}
	docFilter := docFilterBuilder.Build()
	wikiFilter := larksearch.NewWikiFilterBuilder().DocTypes(supportedSearchTypes).Build()
	searchReq := larksearch.NewSearchDocWikiReqBuilder().
		Body(larksearch.NewSearchDocWikiReqBodyBuilder().
			Query(query).
			DocFilter(docFilter).
			WikiFilter(wikiFilter).
			PageSize(pageSize).
			PageToken(pageToken).
			Build()).
		Build()

	searchResp, searchErr := s.client.Search.V2.DocWiki.Search(ctx, searchReq, authCtx.RequestOptions...)
	if searchErr == nil && searchResp.Success() {
		items := make([]map[string]any, 0)
		if searchResp.Data != nil {
			for _, item := range searchResp.Data.ResUnits {
				if mapped := docSearchResultToMap(item); len(mapped) > 0 {
					items = append(items, mapped)
				}
			}
		}
		sortByNameAndToken(items)
		hasMore := false
		nextPageToken := ""
		if searchResp.Data != nil {
			hasMore = boolVal(searchResp.Data.HasMore)
			nextPageToken = strVal(searchResp.Data.PageToken)
		}
		return items, hasMore, nextPageToken, "search", nil
	}

	var primaryErr error
	if searchErr != nil {
		primaryErr = fmt.Errorf("search docs failed: %w", searchErr)
	} else {
		primaryErr = fmt.Errorf("search docs api error: code=%d msg=%s", searchResp.Code, searchResp.Msg)
	}

	// 中文注释：搜索接口在应用态会失败，这里回退到 Drive 列表并做本地关键词过滤，保证可用性。
	listItems, hasMore, nextPageToken, listErr := s.listDriveItems(ctx, folderToken, pageToken, pageSize, authCtx)
	if listErr != nil {
		return nil, false, "", "", fmt.Errorf("%v; drive fallback failed: %w", primaryErr, listErr)
	}

	return filterDriveItemsByQuery(listItems, query), hasMore, nextPageToken, "search_drive_fallback", nil
}

func (s *Server) listDriveItems(
	ctx context.Context,
	folderToken string,
	pageToken string,
	pageSize int,
	authCtx *callAuthContext,
) ([]map[string]any, bool, string, error) {
	req := larkdrive.NewListFileReqBuilder().PageSize(pageSize)
	if pageToken != "" {
		req.PageToken(pageToken)
	}
	if folderToken != "" {
		req.FolderToken(folderToken)
	}

	resp, err := s.client.Drive.V1.File.List(ctx, req.Build(), authCtx.RequestOptions...)
	if err != nil {
		return nil, false, "", fmt.Errorf("browse docs failed: %w", err)
	}
	if !resp.Success() {
		return nil, false, "", fmt.Errorf("browse docs api error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	items := make([]map[string]any, 0)
	for _, f := range safeDriveFiles(resp.Data) {
		items = append(items, driveFileToMap(f))
	}
	sortByNameAndToken(items)
	return items, boolVal(resp.Data.HasMore), strVal(resp.Data.NextPageToken), nil
}

func driveFileToMap(f *larkdrive.File) map[string]any {
	if f == nil {
		return map[string]any{}
	}

	name := strVal(f.Name)
	token := strVal(f.Token)
	return map[string]any{
		"doc_token":     token,
		"token":         token,
		"name":          name,
		"title":         name,
		"type":          strings.ToLower(strVal(f.Type)),
		"url":           strVal(f.Url),
		"parent_token":  strVal(f.ParentToken),
		"owner_id":      strVal(f.OwnerId),
		"created_time":  strVal(f.CreatedTime),
		"modified_time": strVal(f.ModifiedTime),
	}
}

func filterDriveItemsByQuery(items []map[string]any, query string) []map[string]any {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" || len(items) == 0 {
		return items
	}

	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		candidates := []string{
			strings.ToLower(strMap(item, "name")),
			strings.ToLower(strMap(item, "title")),
			strings.ToLower(strMap(item, "token")),
			strings.ToLower(strMap(item, "doc_token")),
			strings.ToLower(strMap(item, "type")),
		}
		for _, candidate := range candidates {
			if candidate != "" && strings.Contains(candidate, query) {
				out = append(out, item)
				break
			}
		}
	}
	return out
}

func sortByNameAndToken(items []map[string]any) {
	sort.Slice(items, func(i, j int) bool {
		left := strings.ToLower(strMap(items[i], "name"))
		right := strings.ToLower(strMap(items[j], "name"))
		if left == right {
			return strMap(items[i], "token") < strMap(items[j], "token")
		}
		return left < right
	})
}

func (s *Server) listBlockSummaries(
	ctx context.Context,
	docToken string,
	pageSize int,
	authCtx *callAuthContext,
) ([]map[string]any, bool, string, error) {
	resp, err := s.client.Docx.V1.DocumentBlock.List(ctx, larkdocx.NewListDocumentBlockReqBuilder().
		DocumentId(docToken).
		DocumentRevisionId(-1).
		PageSize(pageSize).
		Build(), authCtx.RequestOptions...)
	if err != nil {
		return nil, false, "", err
	}
	if !resp.Success() {
		return nil, false, "", fmt.Errorf("list blocks api error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	items := make([]map[string]any, 0, len(resp.Data.Items))
	for _, block := range resp.Data.Items {
		items = append(items, blockSummary(block))
	}

	return items, boolVal(resp.Data.HasMore), strVal(resp.Data.PageToken), nil
}

func (s *Server) listRootChildren(ctx context.Context, docToken string, authCtx *callAuthContext) ([]*larkdocx.Block, error) {
	pageToken := ""
	children := make([]*larkdocx.Block, 0)

	for page := 0; page < maxListChildrenPageCount; page++ {
		builder := larkdocx.NewGetDocumentBlockChildrenReqBuilder().
			DocumentId(docToken).
			BlockId(docToken).
			DocumentRevisionId(-1).
			WithDescendants(false).
			PageSize(maxReadPageSize)
		if pageToken != "" {
			builder.PageToken(pageToken)
		}

		resp, err := s.client.Docx.V1.DocumentBlockChildren.Get(ctx, builder.Build(), authCtx.RequestOptions...)
		if err != nil {
			return nil, err
		}
		if !resp.Success() {
			return nil, fmt.Errorf("get children api error: code=%d msg=%s", resp.Code, resp.Msg)
		}

		children = append(children, resp.Data.Items...)

		if !boolVal(resp.Data.HasMore) {
			return children, nil
		}
		pageToken = strVal(resp.Data.PageToken)
		if pageToken == "" {
			return children, nil
		}
	}

	return children, nil
}

func (s *Server) replaceDocumentContent(
	ctx context.Context,
	docToken string,
	content string,
	contentType string,
	authCtx *callAuthContext,
) error {
	children, err := s.listRootChildren(ctx, docToken, authCtx)
	if err != nil {
		return err
	}

	if len(children) > 0 {
		if err := s.deleteRootChildren(ctx, docToken, len(children), authCtx); err != nil {
			return err
		}
	}

	if strings.TrimSpace(content) == "" {
		return nil
	}

	return s.insertDocumentContent(ctx, docToken, content, contentType, 0, authCtx)
}

func (s *Server) appendDocumentContent(
	ctx context.Context,
	docToken string,
	content string,
	contentType string,
	authCtx *callAuthContext,
) error {
	children, err := s.listRootChildren(ctx, docToken, authCtx)
	if err != nil {
		return err
	}

	return s.insertDocumentContent(ctx, docToken, content, contentType, len(children), authCtx)
}

func (s *Server) deleteRootChildren(ctx context.Context, docToken string, childCount int, authCtx *callAuthContext) error {
	if childCount <= 0 {
		return nil
	}

	body := larkdocx.NewBatchDeleteDocumentBlockChildrenReqBodyBuilder().
		StartIndex(0).
		EndIndex(childCount).
		Build()
	req := larkdocx.NewBatchDeleteDocumentBlockChildrenReqBuilder().
		DocumentId(docToken).
		BlockId(docToken).
		DocumentRevisionId(-1).
		Body(body).
		Build()

	resp, err := s.client.Docx.V1.DocumentBlockChildren.BatchDelete(ctx, req, authCtx.RequestOptions...)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("delete children api error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	return nil
}

func (s *Server) insertDocumentContent(
	ctx context.Context,
	docToken string,
	content string,
	contentType string,
	index int,
	authCtx *callAuthContext,
) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}

	convertResp, err := s.client.Docx.V1.Document.Convert(ctx, larkdocx.NewConvertDocumentReqBuilder().
		Body(larkdocx.NewConvertDocumentReqBodyBuilder().
			ContentType(contentType).
			Content(content).
			Build()).
		Build(), authCtx.RequestOptions...)
	if err != nil {
		return err
	}
	if !convertResp.Success() {
		return fmt.Errorf("convert content api error: code=%d msg=%s", convertResp.Code, convertResp.Msg)
	}
	if convertResp.Data == nil || len(convertResp.Data.Blocks) == 0 || len(convertResp.Data.FirstLevelBlockIds) == 0 {
		return nil
	}

	bodyBuilder := larkdocx.NewCreateDocumentBlockDescendantReqBodyBuilder().
		ChildrenId(convertResp.Data.FirstLevelBlockIds).
		Descendants(convertResp.Data.Blocks).
		Index(index)
	req := larkdocx.NewCreateDocumentBlockDescendantReqBuilder().
		DocumentId(docToken).
		BlockId(docToken).
		DocumentRevisionId(-1).
		Body(bodyBuilder.Build()).
		Build()

	createResp, createErr := s.client.Docx.V1.DocumentBlockDescendant.Create(ctx, req, authCtx.RequestOptions...)
	if createErr != nil {
		return createErr
	}
	if !createResp.Success() {
		return fmt.Errorf("insert content api error: code=%d msg=%s", createResp.Code, createResp.Msg)
	}

	return nil
}

func (s *Server) prepareWriteAction(
	tool string,
	meta invokeMeta,
	authCtx *callAuthContext,
	actionID string,
	confirmation string,
	payload any,
	preview string,
	target string,
) (*pendingAction, *mcp.CallToolResult) {
	confirmText := strings.TrimSpace(confirmation)
	authMode := authModeApp
	var boundIdentityMatch *bool
	if authCtx != nil {
		if strings.TrimSpace(authCtx.Mode) != "" {
			authMode = authCtx.Mode
		}
		boundIdentityMatch = authCtx.BoundIdentityMatch
	}
	if confirmText == "" {
		action, err := s.confirmations.Create(tool, meta.ContextKey, meta.SenderID, authMode, payload, preview, s.now())
		if err != nil {
			return nil, errorTextResult(err, authCtx)
		}

		s.auditSafe(auditRecord{
			Tool:               tool,
			ActionID:           action.ID,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             target,
			Status:             "pending_confirmation",
			AuthMode:           authMode,
			BoundIdentityMatch: boundIdentityMatch,
			Details: map[string]any{
				"preview":    preview,
				"expires_at": action.ExpiresAt.UTC().Format(time.RFC3339Nano),
			},
		})

		return nil, textResult(attachCommonResultFields(map[string]any{
			"status":     "pending_confirmation",
			"action_id":  action.ID,
			"tool":       tool,
			"target":     target,
			"preview":    preview,
			"expires_at": action.ExpiresAt.UTC().Format(time.RFC3339Nano),
			"next_step":  "请保持参数不变，携带 action_id 并在 confirmation 中填写任意确认语后重试。",
		}, authCtx))
	}

	if strings.TrimSpace(actionID) == "" {
		err := errors.New("action_id is required when confirmation is provided")
		s.auditSafe(auditRecord{
			Tool:               tool,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             target,
			Status:             "error",
			AuthMode:           authMode,
			BoundIdentityMatch: boundIdentityMatch,
			Error:              err.Error(),
		})
		return nil, errorTextResult(err, authCtx)
	}

	action, err := s.confirmations.Validate(actionID, tool, meta.ContextKey, authMode, payload, s.now())
	if err != nil {
		s.auditSafe(auditRecord{
			Tool:               tool,
			ActionID:           actionID,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             target,
			Status:             "error",
			AuthMode:           authMode,
			BoundIdentityMatch: boundIdentityMatch,
			Error:              err.Error(),
		})
		return nil, errorTextResult(err, authCtx)
	}

	if action.SenderID != "" && meta.SenderID != "" && action.SenderID != meta.SenderID {
		err := fmt.Errorf("action_id %q does not match current sender", actionID)
		s.auditSafe(auditRecord{
			Tool:               tool,
			ActionID:           actionID,
			Channel:            meta.Channel,
			ChatID:             meta.ChatID,
			SenderID:           meta.SenderID,
			Target:             target,
			Status:             "error",
			AuthMode:           authMode,
			BoundIdentityMatch: boundIdentityMatch,
			Error:              err.Error(),
		})
		return nil, errorTextResult(err, authCtx)
	}

	return action, nil
}

func (s *Server) writeErrorWithAudit(
	tool string,
	meta invokeMeta,
	authCtx *callAuthContext,
	action *pendingAction,
	err error,
) *mcp.CallToolResult {
	s.auditSafe(auditRecord{
		Tool:     tool,
		ActionID: actionIDOrEmpty(action),
		Channel:  meta.Channel,
		ChatID:   meta.ChatID,
		SenderID: meta.SenderID,
		Status:   "error",
		Error:    err.Error(),
		AuthMode: func() string {
			if authCtx == nil || strings.TrimSpace(authCtx.Mode) == "" {
				return ""
			}
			return authCtx.Mode
		}(),
		BoundIdentityMatch: func() *bool {
			if authCtx == nil {
				return nil
			}
			return authCtx.BoundIdentityMatch
		}(),
	})
	return errorTextResult(err, authCtx)
}

func (s *Server) auditSafe(rec auditRecord) {
	if s.audit == nil {
		return
	}
	if err := s.audit.Write(rec); err != nil {
		logger.WarnCF("feishu-doc", "failed to write audit record", map[string]any{
			"tool":  rec.Tool,
			"error": err.Error(),
		})
	}
}

func permissionPublicMap(p *larkdrive.PermissionPublic) map[string]any {
	if p == nil {
		return map[string]any{}
	}
	out := map[string]any{}
	if p.ExternalAccess != nil {
		out["external_access"] = *p.ExternalAccess
	}
	if p.SecurityEntity != nil {
		out["security_entity"] = *p.SecurityEntity
	}
	if p.CommentEntity != nil {
		out["comment_entity"] = *p.CommentEntity
	}
	if p.ShareEntity != nil {
		out["share_entity"] = *p.ShareEntity
	}
	if p.LinkShareEntity != nil {
		out["link_share_entity"] = *p.LinkShareEntity
	}
	if p.InviteExternal != nil {
		out["invite_external"] = *p.InviteExternal
	}
	if p.LockSwitch != nil {
		out["lock_switch"] = *p.LockSwitch
	}
	return out
}

func newInvokeMeta(channel, chatID, senderID string) invokeMeta {
	channel = strings.TrimSpace(channel)
	chatID = strings.TrimSpace(chatID)
	senderID = strings.TrimSpace(senderID)
	if channel == "" {
		channel = "unknown"
	}
	if chatID == "" {
		chatID = "unknown"
	}
	return invokeMeta{
		Channel:    channel,
		ChatID:     chatID,
		SenderID:   senderID,
		ContextKey: channel + ":" + chatID,
	}
}

func safeDriveFiles(data *larkdrive.ListFileRespData) []*larkdrive.File {
	if data == nil || len(data.Files) == 0 {
		return nil
	}
	return data.Files
}

func normalizeFolderToken(raw string) string {
	token := strings.TrimSpace(raw)
	if token == "" {
		return ""
	}

	patterns := []string{
		"/drive/folder/",
		"/folder/",
	}
	for _, pattern := range patterns {
		if idx := strings.Index(token, pattern); idx >= 0 {
			token = token[idx+len(pattern):]
			break
		}
	}

	// 支持从带 query 的分享链接里提取 folder_token=xxx。
	if idx := strings.Index(token, "folder_token="); idx >= 0 {
		token = token[idx+len("folder_token="):]
	}

	token = strings.TrimPrefix(token, "folder/")
	token = strings.TrimPrefix(token, "drive/folder/")
	if cut := strings.IndexAny(token, "?#/&"); cut >= 0 {
		token = token[:cut]
	}
	return strings.TrimSpace(token)
}

func normalizeDocToken(raw string) string {
	token := strings.TrimSpace(raw)
	if token == "" {
		return ""
	}

	if idx := strings.Index(token, "/docx/"); idx >= 0 {
		token = token[idx+len("/docx/"):]
	}
	token = strings.TrimPrefix(token, "docx/")

	if cut := strings.IndexAny(token, "?#/"); cut >= 0 {
		token = token[:cut]
	}

	return strings.TrimSpace(token)
}

func normalizeContentType(contentType string) (string, error) {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if ct == "" {
		return larkdocx.ContentTypeMarkdown, nil
	}
	if ct == larkdocx.ContentTypeMarkdown || ct == larkdocx.ContentTypeHTML {
		return ct, nil
	}
	return "", errors.New("content_type must be one of: markdown, html")
}

func normalizeUploadMIMEType(raw string) string {
	mimeType := strings.TrimSpace(raw)
	if mimeType == "" {
		return "text/markdown; charset=utf-8"
	}
	return mimeType
}

func textResult(payload any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		data, _ = json.Marshal(map[string]any{"status": "error", "error": err.Error()})
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(data)}},
	}
}

func errorResult(err error) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}
	result.SetError(err)
	return result
}

func boolPtr(v bool) *bool {
	return &v
}

func boolPtrValue(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func actionIDOrEmpty(action *pendingAction) string {
	if action == nil {
		return ""
	}
	return action.ID
}

func strVal(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func boolVal(v *bool) bool {
	return v != nil && *v
}

func intVal(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func clampInt(value, def, min, max int) int {
	if value <= 0 {
		value = def
	}
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func trimDefault(v, fallback string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return fallback
	}
	return v
}

func truncateText(raw string, maxLen int) (string, bool) {
	if maxLen <= 0 || len(raw) <= maxLen {
		return raw, false
	}
	return raw[:maxLen] + "\n...[truncated]", true
}

func blockSummary(block *larkdocx.Block) map[string]any {
	if block == nil {
		return map[string]any{}
	}

	out := map[string]any{
		"block_id":   strVal(block.BlockId),
		"parent_id":  strVal(block.ParentId),
		"block_type": intVal(block.BlockType),
	}

	if text := extractBlockText(block); text != "" {
		preview, _ := truncateText(text, 160)
		out["text_preview"] = preview
	}
	if len(block.Children) > 0 {
		out["children_count"] = len(block.Children)
	}
	return out
}

func extractBlockText(block *larkdocx.Block) string {
	if block == nil {
		return ""
	}

	textBlocks := []*larkdocx.Text{
		block.Text,
		block.Heading1,
		block.Heading2,
		block.Heading3,
		block.Heading4,
		block.Heading5,
		block.Heading6,
		block.Heading7,
		block.Heading8,
		block.Heading9,
		block.Bullet,
		block.Ordered,
		block.Quote,
		block.Code,
		block.Equation,
		block.Todo,
		block.Page,
	}

	for _, t := range textBlocks {
		if plain := textElementsToPlainText(t); plain != "" {
			return plain
		}
	}

	return ""
}

func textElementsToPlainText(text *larkdocx.Text) string {
	if text == nil || len(text.Elements) == 0 {
		return ""
	}

	parts := make([]string, 0, len(text.Elements))
	for _, el := range text.Elements {
		if el == nil {
			continue
		}
		switch {
		case el.TextRun != nil && el.TextRun.Content != nil:
			parts = append(parts, *el.TextRun.Content)
		case el.MentionUser != nil && el.MentionUser.UserId != nil:
			parts = append(parts, "@"+*el.MentionUser.UserId)
		case el.MentionDoc != nil && el.MentionDoc.Title != nil:
			parts = append(parts, "@"+*el.MentionDoc.Title)
		}
	}

	return strings.TrimSpace(strings.Join(parts, ""))
}

func strMap(m map[string]any, key string) string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return ""
	}
	value, ok := raw.(string)
	if !ok {
		return ""
	}
	return value
}
