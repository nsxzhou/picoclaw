package feishudoc

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/auth"
)

func TestNormalizeSenderIdentity(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "canonical feishu", input: "feishu:ou_123", expect: "ou_123"},
		{name: "raw sender", input: "ou_456", expect: "ou_456"},
		{name: "other platform canonical", input: "telegram:123", expect: "telegram:123"},
		{name: "empty", input: "", expect: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeSenderIdentity(tt.input); got != tt.expect {
				t.Fatalf("normalizeSenderIdentity(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestSenderMatchesBoundIdentity(t *testing.T) {
	cred := &auth.AuthCredential{
		UserID:  "u_123",
		OpenID:  "ou_456",
		UnionID: "on_789",
	}

	for _, senderID := range []string{"u_123", "ou_456", "on_789"} {
		if !senderMatchesBoundIdentity(senderID, cred) {
			t.Fatalf("expected sender %q to match bound identity", senderID)
		}
	}
	if senderMatchesBoundIdentity("other", cred) {
		t.Fatal("expected mismatched sender to be rejected")
	}
}

func TestSelectAuthContextFallsBackToAppWhenUnbound(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("PICOCLAW_HOME", tempHome)

	srv := &Server{appID: "app-id", appSecret: "app-secret"}
	authCtx, err := srv.selectAuthContext(newInvokeMeta("feishu", "chat-1", "feishu:ou_123"))
	if err != nil {
		t.Fatalf("selectAuthContext returned error: %v", err)
	}
	if authCtx.Mode != authModeApp {
		t.Fatalf("expected app mode, got %s", authCtx.Mode)
	}
	if len(authCtx.RequestOptions) != 0 {
		t.Fatal("expected no request options in app mode")
	}
}

func TestSelectAuthContextUsesUserModeOnBoundMatch(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("PICOCLAW_HOME", tempHome)

	if err := auth.SetCredential(auth.FeishuCredentialProvider, &auth.AuthCredential{
		Provider:    auth.FeishuCredentialProvider,
		AuthMethod:  "oauth",
		AccessToken: "user-token",
		OpenID:      "ou_bound",
		Scope:       auth.RequiredFeishuScopes(),
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	srv := &Server{appID: "app-id", appSecret: "app-secret"}
	authCtx, err := srv.selectAuthContext(newInvokeMeta("feishu", "chat-1", "feishu:ou_bound"))
	if err != nil {
		t.Fatalf("selectAuthContext returned error: %v", err)
	}
	if authCtx.Mode != authModeUser {
		t.Fatalf("expected user mode, got %s", authCtx.Mode)
	}
	if len(authCtx.RequestOptions) != 1 {
		t.Fatalf("expected one request option, got %d", len(authCtx.RequestOptions))
	}
	if authCtx.BoundIdentityMatch == nil || !*authCtx.BoundIdentityMatch {
		t.Fatal("expected bound identity match to be true")
	}
}

func TestSelectAuthContextRejectsMismatchedSender(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("PICOCLAW_HOME", tempHome)

	if err := auth.SetCredential(auth.FeishuCredentialProvider, &auth.AuthCredential{
		Provider:    auth.FeishuCredentialProvider,
		AuthMethod:  "oauth",
		AccessToken: "user-token",
		UserID:      "u_bound",
		Scope:       auth.RequiredFeishuScopes(),
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	srv := &Server{appID: "app-id", appSecret: "app-secret"}
	authCtx, err := srv.selectAuthContext(newInvokeMeta("feishu", "chat-1", "feishu:u_other"))
	if !errors.Is(err, errBoundToAnotherUser) {
		t.Fatalf("expected errBoundToAnotherUser, got %v", err)
	}
	if authCtx == nil {
		t.Fatal("expected auth context on mismatch")
	}
	if authCtx.Mode != authModeUser {
		t.Fatalf("expected user mode on mismatch, got %s", authCtx.Mode)
	}
	if authCtx.BoundIdentityMatch == nil || *authCtx.BoundIdentityMatch {
		t.Fatal("expected bound identity match to be false")
	}
}

func TestSelectAuthContextLoadsCredentialFromProviderKeyedStore(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("PICOCLAW_HOME", tempHome)

	if err := auth.SetCredential(auth.FeishuCredentialProvider, &auth.AuthCredential{
		Provider:    auth.FeishuCredentialProvider,
		AuthMethod:  "oauth",
		AccessToken: "user-token",
		UnionID:     "on_bound",
		Scope:       auth.RequiredFeishuScopes(),
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(tempHome, "auth.json"))
	if err != nil {
		t.Fatalf("read auth store: %v", err)
	}
	if len(raw) == 0 {
		t.Fatal("expected persisted auth store content")
	}

	srv := &Server{appID: "app-id", appSecret: "app-secret"}
	authCtx, err := srv.selectAuthContext(newInvokeMeta("feishu", "chat-1", "on_bound"))
	if err != nil {
		t.Fatalf("selectAuthContext returned error: %v", err)
	}
	if authCtx.Mode != authModeUser {
		t.Fatalf("expected user mode, got %s", authCtx.Mode)
	}
}

func TestSelectAuthContextRejectsBindingWithMissingScopes(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("PICOCLAW_HOME", tempHome)

	if err := auth.SetCredential(auth.FeishuCredentialProvider, &auth.AuthCredential{
		Provider:    auth.FeishuCredentialProvider,
		AuthMethod:  "oauth",
		AccessToken: "user-token",
		OpenID:      "ou_bound",
		Scope:       []string{"auth:user.id:read"},
	}); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	srv := &Server{appID: "app-id", appSecret: "app-secret"}
	authCtx, err := srv.selectAuthContext(newInvokeMeta("feishu", "chat-1", "feishu:ou_bound"))
	if err == nil {
		t.Fatal("expected missing scope error")
	}
	if authCtx == nil || authCtx.Mode != authModeUser {
		t.Fatalf("expected user-mode auth context, got %+v", authCtx)
	}
	if !strings.Contains(err.Error(), "缺少文档权限") {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(err.Error(), "docx:document") {
		t.Fatalf("expected missing scope details, got %v", err)
	}
	if !strings.Contains(err.Error(), "docx:document.block:convert") {
		t.Fatalf("expected convert scope details, got %v", err)
	}
}
