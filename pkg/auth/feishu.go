package auth

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
)

const (
	FeishuCredentialProvider = "feishu-user"
	FeishuAuthorizeURL       = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	FeishuTokenURL           = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	FeishuCallbackPort       = 1456
	FeishuRedirectURI        = "http://127.0.0.1:1456/auth/callback"
)

var feishuRequiredScopes = []string{
	"auth:user.id:read",
	"docs:doc",
	"docx:document",
	"drive:drive",
	"offline_access",
}

// RequiredFeishuScopes 返回 PicoClaw Feishu 文档能力所需的固定授权范围。
func RequiredFeishuScopes() []string {
	return append([]string(nil), feishuRequiredScopes...)
}

// MissingFeishuScopes 返回当前授权结果里缺失的必需 scope。
func MissingFeishuScopes(granted []string) []string {
	grantedSet := make(map[string]struct{}, len(granted))
	for _, scope := range granted {
		scope = strings.TrimSpace(scope)
		if scope != "" {
			grantedSet[scope] = struct{}{}
		}
	}

	missing := make([]string, 0, len(feishuRequiredScopes))
	for _, scope := range feishuRequiredScopes {
		if _, ok := grantedSet[scope]; !ok {
			missing = append(missing, scope)
		}
	}
	return missing
}

// BuildFeishuAuthorizeURL 构造飞书用户态授权地址。
func BuildFeishuAuthorizeURL(appID string, pkce PKCECodes, state, redirectURI string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {strings.TrimSpace(appID)},
		"redirect_uri":          {strings.TrimSpace(redirectURI)},
		"scope":                 {strings.Join(feishuRequiredScopes, " ")},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return FeishuAuthorizeURL + "?" + params.Encode()
}

// LoginFeishuBrowser 复用现有 localhost 回调和手动粘贴兜底，完成飞书用户绑定。
func LoginFeishuBrowser(appID, appSecret string) (*AuthCredential, error) {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if appID == "" || appSecret == "" {
		return nil, fmt.Errorf("feishu app_id and app_secret are required")
	}

	pkce, err := GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("generating PKCE: %w", err)
	}
	state, err := GenerateState()
	if err != nil {
		return nil, fmt.Errorf("generating state: %w", err)
	}

	authURL := BuildFeishuAuthorizeURL(appID, pkce, state, FeishuRedirectURI)
	resultCh := make(chan callbackResult, 1)

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			resultCh <- callbackResult{err: fmt.Errorf("state mismatch")}
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}

		if errMsg := strings.TrimSpace(r.URL.Query().Get("error")); errMsg != "" {
			resultCh <- callbackResult{err: fmt.Errorf("authorization failed: %s", errMsg)}
			http.Error(w, "Authorization failed", http.StatusBadRequest)
			return
		}

		code := strings.TrimSpace(r.URL.Query().Get("code"))
		if code == "" {
			resultCh <- callbackResult{err: fmt.Errorf("no authorization code received")}
			http.Error(w, "No authorization code received", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html><body><h2>Authentication successful!</h2><p>You can close this window.</p></body></html>")
		resultCh <- callbackResult{code: code}
	})

	listener, err := net.Listen("tcp", feishuCallbackAddr())
	if err != nil {
		return nil, fmt.Errorf("starting callback server on port %d: %w", FeishuCallbackPort, err)
	}

	server := &http.Server{Handler: mux}
	go server.Serve(listener)
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	}()

	fmt.Printf("Open this URL to authenticate:\n\n%s\n\n", authURL)
	if err := OpenBrowser(authURL); err != nil {
		fmt.Printf("Could not open browser automatically.\nPlease open this URL manually:\n\n%s\n\n", authURL)
	}

	fmt.Printf(
		"Wait! If you are in a headless environment and cannot reach localhost:%d,\n",
		FeishuCallbackPort,
	)
	fmt.Println("please complete the login in your local browser and then PASTE the final redirect URL (or just the code) here.")
	fmt.Println("Waiting for authentication (browser or manual paste)...")

	manualCh := make(chan string)
	go func() {
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		manualCh <- strings.TrimSpace(input)
	}()

	select {
	case result := <-resultCh:
		if result.err != nil {
			return nil, result.err
		}
		return ExchangeFeishuCode(appID, appSecret, result.code, pkce.CodeVerifier, FeishuRedirectURI)
	case manualInput := <-manualCh:
		code, parseErr := parseFeishuManualInput(manualInput, state)
		if parseErr != nil {
			return nil, parseErr
		}
		return ExchangeFeishuCode(appID, appSecret, code, pkce.CodeVerifier, FeishuRedirectURI)
	case <-time.After(5 * time.Minute):
		return nil, fmt.Errorf("authentication timed out after 5 minutes")
	}
}

// ExchangeFeishuCode 用授权码换取 user_access_token，并补齐绑定身份。
func ExchangeFeishuCode(
	appID string,
	appSecret string,
	code string,
	codeVerifier string,
	redirectURI string,
) (*AuthCredential, error) {
	payload := map[string]any{
		"grant_type":    "authorization_code",
		"code":          strings.TrimSpace(code),
		"client_id":     strings.TrimSpace(appID),
		"client_secret": strings.TrimSpace(appSecret),
		"redirect_uri":  strings.TrimSpace(redirectURI),
		"code_verifier": strings.TrimSpace(codeVerifier),
	}

	root, err := doFeishuJSONRequest(http.MethodPost, FeishuTokenURL, payload, "")
	if err != nil {
		return nil, err
	}
	cred, err := feishuCredentialFromTokenPayload(root)
	if err != nil {
		return nil, err
	}

	if err := PopulateFeishuUserInfo(cred, appID, appSecret); err != nil {
		return nil, err
	}
	return cred, nil
}

// RefreshFeishuAccessToken 在调用前刷新快过期的用户 token。
func RefreshFeishuAccessToken(cred *AuthCredential, appID, appSecret string) (*AuthCredential, error) {
	if cred == nil {
		return nil, fmt.Errorf("credential is nil")
	}
	if strings.TrimSpace(cred.RefreshToken) == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	payload := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": cred.RefreshToken,
		"client_id":     strings.TrimSpace(appID),
		"client_secret": strings.TrimSpace(appSecret),
	}

	root, err := doFeishuJSONRequest(http.MethodPost, FeishuTokenURL, payload, "")
	if err != nil {
		return nil, err
	}
	refreshed, err := feishuCredentialFromTokenPayload(root)
	if err != nil {
		return nil, err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = cred.RefreshToken
	}

	// 中文注释：刷新接口不保证返回全部绑定字段，这里主动保留并重新拉取用户信息。
	refreshed.Provider = FeishuCredentialProvider
	refreshed.AuthMethod = "oauth"
	refreshed.AccountID = cred.AccountID
	if err := PopulateFeishuUserInfo(refreshed, appID, appSecret); err != nil {
		if refreshed.Email == "" {
			refreshed.Email = cred.Email
		}
		if len(refreshed.Scope) == 0 {
			refreshed.Scope = append([]string(nil), cred.Scope...)
		}
		if refreshed.TenantKey == "" {
			refreshed.TenantKey = cred.TenantKey
		}
		if refreshed.UserID == "" {
			refreshed.UserID = cred.UserID
		}
		if refreshed.OpenID == "" {
			refreshed.OpenID = cred.OpenID
		}
		if refreshed.UnionID == "" {
			refreshed.UnionID = cred.UnionID
		}
		if refreshed.DisplayName == "" {
			refreshed.DisplayName = cred.DisplayName
		}
		return refreshed, fmt.Errorf("refresh token succeeded but fetching user info failed: %w", err)
	}
	return refreshed, nil
}

// PopulateFeishuUserInfo 使用 user_access_token 拉取绑定身份元信息。
func PopulateFeishuUserInfo(cred *AuthCredential, appID, appSecret string) error {
	if cred == nil {
		return fmt.Errorf("credential is nil")
	}
	if strings.TrimSpace(cred.AccessToken) == "" {
		return fmt.Errorf("missing access token")
	}

	client := lark.NewClient(strings.TrimSpace(appID), strings.TrimSpace(appSecret))
	resp, err := client.Authen.V1.UserInfo.Get(
		context.Background(),
		larkcore.WithUserAccessToken(cred.AccessToken),
	)
	if err != nil {
		return fmt.Errorf("fetch user info failed: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("fetch user info failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil {
		return fmt.Errorf("fetch user info failed: empty response")
	}

	cred.Provider = FeishuCredentialProvider
	cred.AuthMethod = "oauth"
	cred.Email = firstNonEmptyString(authStr(resp.Data.Email), authStr(resp.Data.EnterpriseEmail), cred.Email)
	cred.DisplayName = firstNonEmptyString(authStr(resp.Data.Name), authStr(resp.Data.EnName), cred.DisplayName)
	cred.TenantKey = firstNonEmptyString(authStr(resp.Data.TenantKey), cred.TenantKey)
	cred.UserID = firstNonEmptyString(authStr(resp.Data.UserId), cred.UserID)
	cred.OpenID = firstNonEmptyString(authStr(resp.Data.OpenId), cred.OpenID)
	cred.UnionID = firstNonEmptyString(authStr(resp.Data.UnionId), cred.UnionID)
	return nil
}

func parseFeishuManualInput(input string, expectedState string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("manual input canceled")
	}
	if !strings.Contains(input, "?") {
		return input, nil
	}

	u, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("parse callback URL: %w", err)
	}
	if expectedState != "" {
		if state := strings.TrimSpace(u.Query().Get("state")); state != "" && state != expectedState {
			return "", fmt.Errorf("state mismatch")
		}
	}
	code := strings.TrimSpace(u.Query().Get("code"))
	if code == "" {
		return "", fmt.Errorf("could not find authorization code in input")
	}
	return code, nil
}

func feishuCredentialFromTokenPayload(root map[string]any) (*AuthCredential, error) {
	body := nestedFeishuPayload(root)
	accessToken := stringValue(body["access_token"])
	if accessToken == "" {
		return nil, fmt.Errorf("token exchange failed: missing access_token")
	}

	cred := &AuthCredential{
		AccessToken:  accessToken,
		RefreshToken: stringValue(body["refresh_token"]),
		Provider:     FeishuCredentialProvider,
		AuthMethod:   "oauth",
		Scope:        normalizeFeishuScope(body["scope"]),
	}
	if expiresIn := intValue(body["expires_in"]); expiresIn > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}
	return cred, nil
}

func doFeishuJSONRequest(method, endpoint string, payload any, accessToken string) (map[string]any, error) {
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(raw))
	}

	req, err := http.NewRequest(method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := json.Unmarshal(rawBody, &data); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s", firstNonEmptyString(stringValue(data["msg"]), string(rawBody)))
	}
	if code := intValue(data["code"]); code != 0 && code != 200 {
		return nil, fmt.Errorf("%s", firstNonEmptyString(stringValue(data["msg"]), "unexpected API error"))
	}
	return data, nil
}

func nestedFeishuPayload(root map[string]any) map[string]any {
	if root == nil {
		return map[string]any{}
	}
	if nested, ok := root["data"].(map[string]any); ok {
		return nested
	}
	return root
}

func normalizeFeishuScope(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		parts := strings.FieldsFunc(value, func(r rune) bool {
			return r == ',' || r == ' '
		})
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
		return out
	default:
		return nil
	}
}

func stringValue(raw any) string {
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		if raw == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(raw))
	}
}

func intValue(raw any) int {
	switch value := raw.(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	case string:
		n, _ := json.Number(strings.TrimSpace(value)).Int64()
		return int(n)
	default:
		return 0
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func feishuCallbackAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", FeishuCallbackPort)
}

func authStr(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}
