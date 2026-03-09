package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/config"
)

// ── Config API tests ─────────────────────────────────────────────

type feishuAuthCredential struct {
	AccessToken  string `json:"access_token"`
	Provider     string `json:"provider"`
	AuthMethod   string `json:"auth_method"`
	DisplayName  string `json:"display_name"`
	TenantKey    string `json:"tenant_key"`
	UserID       string `json:"user_id"`
	OpenID       string `json:"open_id"`
	UnionID      string `json:"union_id"`
	Email        string `json:"email"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
}

func setupConfigMux(t *testing.T, cfg *config.Config) (*http.ServeMux, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	mux := http.NewServeMux()
	RegisterConfigAPI(mux, path)
	RegisterAuthAPI(mux, path)
	return mux, path
}

func setupAuthHome(t *testing.T) {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
}

func TestGetConfig(t *testing.T) {
	cfg := &config.Config{
		ModelList: []config.ModelConfig{
			{ModelName: "gpt-4o", Model: "openai/gpt-4o"},
		},
	}
	mux, path := setupConfigMux(t, cfg)

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/config: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Config config.Config `json:"config"`
		Path   string        `json:"path"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if resp.Path != path {
		t.Errorf("expected path %q, got %q", path, resp.Path)
	}
	if len(resp.Config.ModelList) != 1 {
		t.Errorf("expected 1 model, got %d", len(resp.Config.ModelList))
	}
}

func TestGetConfig_MissingFile_ReturnsDefault(t *testing.T) {
	mux := http.NewServeMux()
	RegisterConfigAPI(mux, "/tmp/nonexistent-picoclaw-launcher-test/config.json")

	req := httptest.NewRequest("GET", "/api/config", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// LoadConfig returns a default empty config when file is missing
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for missing file (default config), got %d", w.Code)
	}
}

func TestPutConfig(t *testing.T) {
	cfg := &config.Config{}
	mux, path := setupConfigMux(t, cfg)

	newCfg := config.Config{
		ModelList: []config.ModelConfig{
			{ModelName: "claude", Model: "anthropic/claude-sonnet-4.6", AuthMethod: "token"},
		},
	}
	body, _ := json.Marshal(newCfg)

	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("PUT /api/config: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	saved, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("load saved config: %v", err)
	}
	if len(saved.ModelList) != 1 {
		t.Fatalf("expected 1 model saved, got %d", len(saved.ModelList))
	}
	if saved.ModelList[0].Model != "anthropic/claude-sonnet-4.6" {
		t.Errorf("expected model anthropic/claude-sonnet-4.6, got %q", saved.ModelList[0].Model)
	}
}

func TestPutConfig_InvalidJSON(t *testing.T) {
	cfg := &config.Config{}
	mux, _ := setupConfigMux(t, cfg)

	req := httptest.NewRequest("PUT", "/api/config", strings.NewReader("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON, got %d", w.Code)
	}
}

// ── Auth API tests ───────────────────────────────────────────────

func TestAuthStatus(t *testing.T) {
	setupAuthHome(t)
	cfg := &config.Config{}
	mux, _ := setupConfigMux(t, cfg)

	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/status: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Providers     []providerStatus `json:"providers"`
		PendingDevice map[string]any   `json:"pending_device"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// providers should be a non-nil list (could be empty)
	if resp.Providers == nil {
		t.Error("providers should not be nil")
	}
}

func TestAuthStatus_FeishuProviderNormalized(t *testing.T) {
	setupAuthHome(t)

	storePath := filepath.Join(os.Getenv("HOME"), ".picoclaw", "auth.json")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	store := map[string]any{
		"credentials": map[string]any{
			"feishu-user": feishuAuthCredential{
				AccessToken: "token",
				Provider:    "feishu-user",
				AuthMethod:  "oauth",
				DisplayName: "测试用户",
				TenantKey:   "tenant-1",
				UserID:      "ou-user",
				OpenID:      "ou-open",
				UnionID:     "on-union",
				Email:       "user@example.com",
				Scope:       "docs:doc,drive:file",
			},
		},
	}
	raw, _ := json.Marshal(store)
	if err := os.WriteFile(storePath, raw, 0o600); err != nil {
		t.Fatalf("write auth store: %v", err)
	}

	mux, _ := setupConfigMux(t, &config.Config{})
	req := httptest.NewRequest("GET", "/api/auth/status", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/status: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Providers []providerStatus `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Providers) != 1 {
		t.Fatalf("expected 1 provider, got %d", len(resp.Providers))
	}
	got := resp.Providers[0]
	if got.Provider != "feishu" {
		t.Fatalf("provider = %q, want feishu", got.Provider)
	}

	credRuntimeType := reflect.TypeOf(auth.AuthCredential{})
	if _, ok := credRuntimeType.FieldByName("DisplayName"); ok {
		if got.DisplayName != "测试用户" || got.TenantKey != "tenant-1" || got.UserID != "ou-user" {
			t.Fatalf("unexpected feishu details: %+v", got)
		}
		if got.Scope != "docs:doc,drive:file" {
			t.Fatalf("scope = %q, want docs:doc,drive:file", got.Scope)
		}
	}
}

func TestAuthLogin_UnsupportedProvider(t *testing.T) {
	setupAuthHome(t)
	cfg := &config.Config{}
	mux, _ := setupConfigMux(t, cfg)

	body := `{"provider": "unsupported"}`
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for unsupported provider, got %d", w.Code)
	}
}

func TestAuthLogin_AnthropicNoToken(t *testing.T) {
	setupAuthHome(t)
	cfg := &config.Config{}
	mux, _ := setupConfigMux(t, cfg)

	body := `{"provider": "anthropic"}`
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for anthropic without token, got %d", w.Code)
	}
}

func TestAuthLogin_InvalidBody(t *testing.T) {
	setupAuthHome(t)
	cfg := &config.Config{}
	mux, _ := setupConfigMux(t, cfg)

	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid JSON body, got %d", w.Code)
	}
}

func TestAuthLogin_FeishuReturnsAuthURLWithRequiredScopes(t *testing.T) {
	setupAuthHome(t)
	cfg := &config.Config{}
	cfg.Channels.Feishu.AppID = "cli_test"
	cfg.Channels.Feishu.AppSecret = "secret"
	mux, _ := setupConfigMux(t, cfg)

	activeFeishuOAuthSessionMu.Lock()
	activeFeishuOAuthSession = nil
	activeFeishuOAuthSessionMu.Unlock()

	t.Cleanup(func() {
		activeFeishuOAuthSessionMu.Lock()
		session := activeFeishuOAuthSession
		activeFeishuOAuthSession = nil
		activeFeishuOAuthSessionMu.Unlock()
		if session != nil && session.CallbackSrv != nil {
			_ = session.CallbackSrv.Close()
		}
	})

	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"provider":"feishu"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Host = "127.0.0.1:8080"
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Status  string `json:"status"`
		AuthURL string `json:"auth_url"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Status != "redirect" {
		t.Fatalf("status = %q, want redirect", resp.Status)
	}
	authURL, err := url.Parse(resp.AuthURL)
	if err != nil {
		t.Fatalf("parse auth_url: %v", err)
	}
	if got := authURL.Query().Get("scope"); got != strings.Join(auth.RequiredFeishuScopes(), " ") {
		t.Fatalf("scope = %q, want %q", got, strings.Join(auth.RequiredFeishuScopes(), " "))
	}
}

func TestAuthLogout_InvalidBody(t *testing.T) {
	setupAuthHome(t)
	cfg := &config.Config{}
	mux, _ := setupConfigMux(t, cfg)

	req := httptest.NewRequest("POST", "/api/auth/logout", strings.NewReader("{bad"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid body, got %d", w.Code)
	}
}

func TestOAuthCallback_InvalidState(t *testing.T) {
	setupAuthHome(t)
	cfg := &config.Config{}
	mux, _ := setupConfigMux(t, cfg)

	req := httptest.NewRequest("GET", "/auth/callback?state=invalid&code=test", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid state, got %d", w.Code)
	}
}

func TestAuthLogout_FeishuDeletesFeishuUserCredential(t *testing.T) {
	setupAuthHome(t)

	storePath := filepath.Join(os.Getenv("HOME"), ".picoclaw", "auth.json")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir auth dir: %v", err)
	}
	store := map[string]any{
		"credentials": map[string]any{
			"feishu-user": feishuAuthCredential{
				AccessToken: "token",
				Provider:    "feishu-user",
				AuthMethod:  "oauth",
			},
		},
	}
	raw, _ := json.Marshal(store)
	if err := os.WriteFile(storePath, raw, 0o600); err != nil {
		t.Fatalf("write auth store: %v", err)
	}

	mux, _ := setupConfigMux(t, &config.Config{})
	req := httptest.NewRequest("POST", "/api/auth/logout", strings.NewReader(`{"provider":"feishu"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/logout: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read auth store: %v", err)
	}
	var got map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode auth store: %v", err)
	}
	if _, ok := got["credentials"]["feishu-user"]; ok {
		t.Fatal("expected feishu-user credential to be deleted")
	}
}

// ── Utility tests ────────────────────────────────────────────────

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	if path == "" {
		t.Error("defaultConfigPath should not return empty")
	}
	if !strings.HasSuffix(path, filepath.Join(".picoclaw", "config.json")) {
		t.Errorf("expected path ending with .picoclaw/config.json, got %q", path)
	}
}

func TestGetLocalIP(t *testing.T) {
	// Just ensure it doesn't panic; IP may or may not be available
	ip := GetLocalIP()
	if ip != "" {
		// If returned, should look like an IP
		if !strings.Contains(ip, ".") {
			t.Errorf("getLocalIP returned non-IPv4 looking string: %q", ip)
		}
	}
}
