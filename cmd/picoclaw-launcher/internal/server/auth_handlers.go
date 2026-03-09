package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

const (
	feishuCredentialProvider = "feishu-user"
	feishuAuthorizeURL       = "https://accounts.feishu.cn/open-apis/authen/v1/authorize"
	feishuTokenURL           = "https://open.feishu.cn/open-apis/authen/v2/oauth/token"
	feishuUserInfoURL        = "https://open.feishu.cn/open-apis/authen/v1/user_info"
	feishuRedirectURI        = "http://127.0.0.1:1456/auth/callback"
	feishuCallbackAddr       = "127.0.0.1:1456"
	feishuLoginTimeout       = 5 * time.Minute
)

// oauthSession stores in-flight OAuth state for browser-based flows.
type oauthSession struct {
	Provider    string
	PKCE        auth.PKCECodes
	State       string
	RedirectURI string
	OAuthCfg    auth.OAuthProviderConfig
	ConfigPath  string
	AuthURL     string
	ReturnURL   string
	Status      string
	Error       string
	Done        bool
	CallbackSrv *http.Server
}

// deviceCodeSession stores in-flight device code flow state.
type deviceCodeSession struct {
	mu         sync.Mutex
	Provider   string
	Info       *auth.DeviceCodeInfo
	OAuthCfg   auth.OAuthProviderConfig
	ConfigPath string
	Status     string // "pending", "success", "error"
	Error      string
	Done       bool
}

var (
	oauthSessions   = map[string]*oauthSession{} // keyed by state
	oauthSessionsMu sync.Mutex

	activeDeviceSession   *deviceCodeSession
	activeDeviceSessionMu sync.Mutex

	activeFeishuOAuthSession   *oauthSession
	activeFeishuOAuthSessionMu sync.Mutex
)

// handleOpenAILogin starts the OpenAI device code flow and returns device code info to the frontend.
func handleOpenAILogin(w http.ResponseWriter, configPath string) {
	// Check if there's already a pending device code session
	activeDeviceSessionMu.Lock()
	if activeDeviceSession != nil {
		activeDeviceSession.mu.Lock()
		if !activeDeviceSession.Done {
			resp := map[string]any{
				"status":     "pending",
				"device_url": activeDeviceSession.Info.VerifyURL,
				"user_code":  activeDeviceSession.Info.UserCode,
				"message":    "Device code flow already in progress. Enter the code in your browser.",
			}
			activeDeviceSession.mu.Unlock()
			activeDeviceSessionMu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		activeDeviceSession.mu.Unlock()
	}
	activeDeviceSessionMu.Unlock()

	// Request a device code
	oauthCfg := auth.OpenAIOAuthConfig()
	info, err := auth.RequestDeviceCode(oauthCfg)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to request device code: %v", err), http.StatusInternalServerError)
		return
	}

	session := &deviceCodeSession{
		Provider:   "openai",
		Info:       info,
		OAuthCfg:   oauthCfg,
		ConfigPath: configPath,
		Status:     "pending",
	}

	activeDeviceSessionMu.Lock()
	activeDeviceSession = session
	activeDeviceSessionMu.Unlock()

	// Start background polling
	go func() {
		deadline := time.After(15 * time.Minute)
		ticker := time.NewTicker(time.Duration(info.Interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-deadline:
				session.mu.Lock()
				session.Status = "error"
				session.Error = "Authentication timed out after 15 minutes"
				session.Done = true
				session.mu.Unlock()
				return
			case <-ticker.C:
				cred, err := auth.PollDeviceCodeOnce(oauthCfg, info.DeviceAuthID, info.UserCode)
				if err != nil {
					continue // Still pending
				}
				if cred != nil {
					if saveErr := auth.SetCredential("openai", cred); saveErr != nil {
						session.mu.Lock()
						session.Status = "error"
						session.Error = saveErr.Error()
						session.Done = true
						session.mu.Unlock()
						return
					}
					updateConfigAfterLogin(configPath, "openai", cred)
					session.mu.Lock()
					session.Status = "success"
					session.Done = true
					session.mu.Unlock()
					log.Printf("OpenAI device code login successful (account: %s)", cred.AccountID)
					return
				}
			}
		}
	}()

	// Return device code info to frontend
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":     "pending",
		"device_url": info.VerifyURL,
		"user_code":  info.UserCode,
		"message":    "Open the URL and enter the code to authenticate.",
	})
}

// handleAnthropicLogin saves a pasted API token for Anthropic.
func handleAnthropicLogin(w http.ResponseWriter, token, configPath string) {
	if token == "" {
		http.Error(w, "Token is required for Anthropic login", http.StatusBadRequest)
		return
	}

	cred := &auth.AuthCredential{
		AccessToken: token,
		Provider:    "anthropic",
		AuthMethod:  "token",
	}

	if err := auth.SetCredential("anthropic", cred); err != nil {
		http.Error(w, fmt.Sprintf("Failed to save credentials: %v", err), http.StatusInternalServerError)
		return
	}

	updateConfigAfterLogin(configPath, "anthropic", cred)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Anthropic token saved",
	})
}

// handleGoogleAntigravityLogin generates a PKCE + auth URL and returns it to the frontend.
func handleGoogleAntigravityLogin(w http.ResponseWriter, r *http.Request, configPath string) {
	oauthCfg := auth.GoogleAntigravityOAuthConfig()

	pkce, err := auth.GeneratePKCE()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate PKCE: %v", err), http.StatusInternalServerError)
		return
	}

	state, err := auth.GenerateState()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate state: %v", err), http.StatusInternalServerError)
		return
	}

	// Build redirect URI pointing to picoclaw-launcher's own callback
	scheme := "http"
	redirectURI := fmt.Sprintf("%s://%s/auth/callback", scheme, r.Host)

	authURL := auth.BuildAuthorizeURL(oauthCfg, pkce, state, redirectURI)

	// Store session for callback
	oauthSessionsMu.Lock()
	oauthSessions[state] = &oauthSession{
		Provider:    "google-antigravity",
		PKCE:        pkce,
		State:       state,
		RedirectURI: redirectURI,
		OAuthCfg:    oauthCfg,
		ConfigPath:  configPath,
	}
	oauthSessionsMu.Unlock()

	// Clean up stale sessions after 10 minutes
	go func() {
		time.Sleep(10 * time.Minute)
		oauthSessionsMu.Lock()
		delete(oauthSessions, state)
		oauthSessionsMu.Unlock()
	}()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "redirect",
		"auth_url": authURL,
		"message":  "Open the URL to authenticate with Google.",
	})
}

// handleFeishuLogin generates a browser auth URL and completes the OAuth callback on localhost:1456.
func handleFeishuLogin(w http.ResponseWriter, r *http.Request, configPath string) {
	appCfg, err := config.LoadConfig(configPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to load config: %v", err), http.StatusInternalServerError)
		return
	}
	appID := strings.TrimSpace(appCfg.Channels.Feishu.AppID)
	appSecret := strings.TrimSpace(appCfg.Channels.Feishu.AppSecret)
	if appID == "" || appSecret == "" {
		http.Error(w, "Feishu app_id and app_secret are required in channels.feishu before login", http.StatusBadRequest)
		return
	}

	activeFeishuOAuthSessionMu.Lock()
	if activeFeishuOAuthSession != nil && !activeFeishuOAuthSession.Done {
		authURL := activeFeishuOAuthSession.AuthURL
		activeFeishuOAuthSessionMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"status":   "redirect",
			"auth_url": authURL,
			"message":  "Feishu browser login is already in progress.",
		})
		return
	}
	activeFeishuOAuthSessionMu.Unlock()

	pkce, err := auth.GeneratePKCE()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate PKCE: %v", err), http.StatusInternalServerError)
		return
	}

	state, err := auth.GenerateState()
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to generate state: %v", err), http.StatusInternalServerError)
		return
	}

	session := &oauthSession{
		Provider:    feishuCredentialProvider,
		PKCE:        pkce,
		State:       state,
		RedirectURI: feishuRedirectURI,
		ConfigPath:  configPath,
		ReturnURL:   fmt.Sprintf("http://%s/#auth", r.Host),
		Status:      "pending",
	}
	session.AuthURL = buildFeishuAuthorizeURL(appID, pkce, state, feishuRedirectURI)

	if err := startFeishuCallbackServer(session, appID, appSecret); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start localhost callback server: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "redirect",
		"auth_url": session.AuthURL,
		"message":  "Open the URL to authenticate with Feishu.",
	})
}

// handleOAuthCallback processes the OAuth callback from Google Antigravity.
func handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	oauthSessionsMu.Lock()
	session, ok := oauthSessions[state]
	if ok {
		delete(oauthSessions, state)
	}
	oauthSessionsMu.Unlock()

	if !ok {
		http.Error(w, "Invalid or expired OAuth state", http.StatusBadRequest)
		return
	}

	if code == "" {
		errMsg := r.URL.Query().Get("error")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(
			w,
			`<html><body><h2>Authentication failed</h2><p>%s</p><p>You can close this window.</p></body></html>`,
			errMsg,
		)
		return
	}

	cred, err := auth.ExchangeCodeForTokens(session.OAuthCfg, code, session.PKCE.CodeVerifier, session.RedirectURI)
	if err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(
			w,
			`<html><body><h2>Authentication failed</h2><p>%s</p><p>You can close this window.</p></body></html>`,
			err.Error(),
		)
		return
	}

	cred.Provider = session.Provider

	// Fetch user info for Google Antigravity
	if session.Provider == "google-antigravity" {
		if email, err := fetchGoogleUserEmail(cred.AccessToken); err == nil {
			cred.Email = email
		}
		if projectID, err := providers.FetchAntigravityProjectID(cred.AccessToken); err == nil {
			cred.ProjectID = projectID
		}
	}

	if err := auth.SetCredential(session.Provider, cred); err != nil {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><body><h2>Failed to save credentials</h2><p>%s</p></body></html>`, err.Error())
		return
	}

	updateConfigAfterLogin(session.ConfigPath, session.Provider, cred)

	// Redirect back to picoclaw-launcher UI
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<html><body>
		<h2>Authentication successful!</h2>
		<p>Redirecting back to Config Editor...</p>
		<script>setTimeout(function(){ window.location.href = '/#auth'; }, 1000);</script>
	</body></html>`)
}

func startFeishuCallbackServer(session *oauthSession, appID, appSecret string) error {
	activeFeishuOAuthSessionMu.Lock()
	defer activeFeishuOAuthSessionMu.Unlock()

	listener, err := net.Listen("tcp", feishuCallbackAddr)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		handleFeishuCallback(w, r, session, appID, appSecret)
	})

	server := &http.Server{Handler: mux}
	session.CallbackSrv = server
	activeFeishuOAuthSession = session

	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("Feishu localhost callback server stopped with error: %v", err)
		}
	}()

	go func() {
		time.Sleep(feishuLoginTimeout)
		activeFeishuOAuthSessionMu.Lock()
		defer activeFeishuOAuthSessionMu.Unlock()
		if activeFeishuOAuthSession == session && !session.Done {
			session.Status = "error"
			session.Error = "Authentication timed out"
			session.Done = true
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
			activeFeishuOAuthSession = nil
		}
	}()

	return nil
}

func handleFeishuCallback(w http.ResponseWriter, r *http.Request, session *oauthSession, appID, appSecret string) {
	defer func() {
		if session.CallbackSrv != nil {
			go func(server *http.Server) {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = server.Shutdown(ctx)
			}(session.CallbackSrv)
		}
		activeFeishuOAuthSessionMu.Lock()
		if activeFeishuOAuthSession == session {
			activeFeishuOAuthSession = nil
		}
		activeFeishuOAuthSessionMu.Unlock()
	}()

	if r.URL.Query().Get("state") != session.State {
		session.Status = "error"
		session.Error = "Invalid or expired OAuth state"
		session.Done = true
		http.Error(w, session.Error, http.StatusBadRequest)
		return
	}

	if errMsg := strings.TrimSpace(r.URL.Query().Get("error")); errMsg != "" {
		session.Status = "error"
		session.Error = errMsg
		session.Done = true
		renderLauncherRedirect(w, "Authentication failed", errMsg, "")
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		session.Status = "error"
		session.Error = "No authorization code received"
		session.Done = true
		renderLauncherRedirect(w, "Authentication failed", session.Error, "")
		return
	}

	cred, err := exchangeFeishuCode(code, session.PKCE.CodeVerifier, session.RedirectURI, appID, appSecret)
	if err != nil {
		session.Status = "error"
		session.Error = err.Error()
		session.Done = true
		renderLauncherRedirect(w, "Authentication failed", err.Error(), "")
		return
	}

	if err := auth.SetCredential(feishuCredentialProvider, cred); err != nil {
		session.Status = "error"
		session.Error = err.Error()
		session.Done = true
		renderLauncherRedirect(w, "Failed to save credentials", err.Error(), "")
		return
	}

	session.Status = "success"
	session.Done = true

	renderLauncherRedirect(w, "Authentication successful!", "Redirecting back to Config Editor...", session.ReturnURL)
}

func buildFeishuAuthorizeURL(appID string, pkce auth.PKCECodes, state, redirectURI string) string {
	params := url.Values{
		"response_type":         {"code"},
		"client_id":             {appID},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {pkce.CodeChallenge},
		"code_challenge_method": {"S256"},
		"state":                 {state},
	}
	return feishuAuthorizeURL + "?" + params.Encode()
}

func exchangeFeishuCode(
	code string,
	codeVerifier string,
	redirectURI string,
	appID string,
	appSecret string,
) (*auth.AuthCredential, error) {
	payload := map[string]any{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     appID,
		"client_secret": appSecret,
		"redirect_uri":  redirectURI,
		"code_verifier": codeVerifier,
	}

	root, err := doJSONRequest(feishuTokenURL, payload, "")
	if err != nil {
		return nil, fmt.Errorf("token exchange failed: %w", err)
	}
	body := nestedPayload(root)

	accessToken := mapString(body, "access_token")
	if accessToken == "" {
		return nil, fmt.Errorf("token exchange failed: missing access_token")
	}

	cred := &auth.AuthCredential{
		AccessToken:  accessToken,
		RefreshToken: mapString(body, "refresh_token"),
		Provider:     feishuCredentialProvider,
		AuthMethod:   "oauth",
	}

	if expiresIn := mapInt(body, "expires_in"); expiresIn > 0 {
		cred.ExpiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	}

	userInfoRoot, err := doJSONRequest(feishuUserInfoURL, nil, accessToken)
	if err != nil {
		return nil, fmt.Errorf("fetch user info failed: %w", err)
	}
	userInfo := nestedPayload(userInfoRoot)

	cred.Email = mapString(userInfo, "email")
	setOptionalCredentialString(cred, "DisplayName", firstNonEmpty(mapString(userInfo, "name"), mapString(userInfo, "en_name")))
	setOptionalCredentialString(cred, "TenantKey", mapString(userInfo, "tenant_key"))
	setOptionalCredentialString(cred, "UserID", mapString(userInfo, "user_id"))
	setOptionalCredentialString(cred, "OpenID", mapString(userInfo, "open_id"))
	setOptionalCredentialString(cred, "UnionID", mapString(userInfo, "union_id"))
	setOptionalCredentialScope(cred, mapStringSlice(userInfo["scope"]))
	return cred, nil
}

func doJSONRequest(endpoint string, payload map[string]any, accessToken string) (map[string]any, error) {
	var bodyReader io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		bodyReader = strings.NewReader(string(raw))
	}

	method := http.MethodGet
	if payload != nil {
		method = http.MethodPost
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
		return nil, fmt.Errorf("%s", firstNonEmpty(mapString(data, "msg"), string(rawBody)))
	}
	if code := mapInt(data, "code"); code != 0 && code != 200 {
		return nil, fmt.Errorf("%s", firstNonEmpty(mapString(data, "msg"), "unexpected API error"))
	}
	return data, nil
}

func nestedPayload(root map[string]any) map[string]any {
	if root == nil {
		return map[string]any{}
	}
	if nested, ok := root["data"].(map[string]any); ok {
		return nested
	}
	return root
}

func mapString(root map[string]any, key string) string {
	if root == nil {
		return ""
	}
	raw, ok := root[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func mapInt(root map[string]any, key string) int {
	if root == nil {
		return 0
	}
	raw, ok := root[key]
	if !ok || raw == nil {
		return 0
	}
	switch value := raw.(type) {
	case float64:
		return int(value)
	case int:
		return value
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

func mapStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		return value
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			s := strings.TrimSpace(fmt.Sprint(item))
			if s != "" {
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

func setOptionalCredentialString(cred *auth.AuthCredential, fieldName, value string) {
	if cred == nil || strings.TrimSpace(value) == "" {
		return
	}
	structValue := reflect.ValueOf(cred)
	if structValue.Kind() == reflect.Pointer {
		structValue = structValue.Elem()
	}
	if !structValue.IsValid() || structValue.Kind() != reflect.Struct {
		return
	}
	field := structValue.FieldByName(fieldName)
	if !field.IsValid() || !field.CanSet() || field.Kind() != reflect.String {
		return
	}
	field.SetString(value)
}

func setOptionalCredentialScope(cred *auth.AuthCredential, scope []string) {
	if cred == nil || len(scope) == 0 {
		return
	}
	structValue := reflect.ValueOf(cred)
	if structValue.Kind() == reflect.Pointer {
		structValue = structValue.Elem()
	}
	if !structValue.IsValid() || structValue.Kind() != reflect.Struct {
		return
	}
	field := structValue.FieldByName("Scope")
	if !field.IsValid() || !field.CanSet() {
		return
	}
	switch field.Kind() {
	case reflect.String:
		field.SetString(strings.Join(scope, ", "))
	case reflect.Slice:
		if field.Type().Elem().Kind() != reflect.String {
			return
		}
		values := reflect.MakeSlice(field.Type(), 0, len(scope))
		for _, item := range scope {
			values = reflect.Append(values, reflect.ValueOf(item))
		}
		field.Set(values)
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func renderLauncherRedirect(w http.ResponseWriter, title string, message string, redirectURL string) {
	w.Header().Set("Content-Type", "text/html")
	if redirectURL == "" {
		fmt.Fprintf(
			w,
			`<html><body><h2>%s</h2><p>%s</p><p>You can close this window.</p></body></html>`,
			title,
			message,
		)
		return
	}
	fmt.Fprintf(
		w,
		`<html><body><h2>%s</h2><p>%s</p><script>setTimeout(function(){ window.location.href = %q; }, 1000);</script></body></html>`,
		title,
		message,
		redirectURL,
	)
}

// fetchGoogleUserEmail retrieves the user's email from Google's userinfo endpoint.
func fetchGoogleUserEmail(accessToken string) (string, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading userinfo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("userinfo request failed: %s", string(body))
	}

	var userInfo struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return "", err
	}
	return userInfo.Email, nil
}
