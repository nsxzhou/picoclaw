package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuildFeishuAuthorizeURL(t *testing.T) {
	pkce := PKCECodes{CodeVerifier: "verifier", CodeChallenge: "challenge"}
	got := BuildFeishuAuthorizeURL("cli_test", pkce, "state-1", FeishuRedirectURI)
	if !strings.HasPrefix(got, FeishuAuthorizeURL+"?") {
		t.Fatalf("unexpected prefix: %s", got)
	}
	if !strings.Contains(got, "client_id=cli_test") {
		t.Fatalf("missing client_id: %s", got)
	}
	if !strings.Contains(got, "code_challenge=challenge") {
		t.Fatalf("missing code_challenge: %s", got)
	}
	if !strings.Contains(got, "state=state-1") {
		t.Fatalf("missing state: %s", got)
	}
}

func TestParseFeishuManualInput(t *testing.T) {
	code, err := parseFeishuManualInput("http://127.0.0.1:1456/auth/callback?code=test-code&state=s1", "s1")
	if err != nil {
		t.Fatalf("parseFeishuManualInput() error: %v", err)
	}
	if code != "test-code" {
		t.Fatalf("code = %q, want test-code", code)
	}
}

func TestParseFeishuManualInputStateMismatch(t *testing.T) {
	_, err := parseFeishuManualInput("http://127.0.0.1:1456/auth/callback?code=test-code&state=s2", "s1")
	if err == nil {
		t.Fatal("expected state mismatch error")
	}
}

func TestFeishuCredentialFromTokenPayload(t *testing.T) {
	root := map[string]any{
		"code": 0,
		"data": map[string]any{
			"access_token":  "u-token",
			"refresh_token": "r-token",
			"expires_in":    7200,
			"scope":         "docs:doc drive:file",
		},
	}
	cred, err := feishuCredentialFromTokenPayload(root)
	if err != nil {
		t.Fatalf("feishuCredentialFromTokenPayload() error: %v", err)
	}
	if cred.AccessToken != "u-token" || cred.RefreshToken != "r-token" {
		t.Fatalf("unexpected credential: %+v", cred)
	}
	if len(cred.Scope) != 2 {
		t.Fatalf("scope len = %d, want 2", len(cred.Scope))
	}
}

func TestDoFeishuJSONRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["grant_type"] != "authorization_code" {
			t.Fatalf("grant_type = %v, want authorization_code", body["grant_type"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{"access_token": "token"},
		})
	}))
	defer server.Close()

	resp, err := doFeishuJSONRequest(http.MethodPost, server.URL, map[string]any{"grant_type": "authorization_code"}, "")
	if err != nil {
		t.Fatalf("doFeishuJSONRequest() error: %v", err)
	}
	if nestedFeishuPayload(resp)["access_token"] != "token" {
		t.Fatalf("unexpected resp: %+v", resp)
	}
}
