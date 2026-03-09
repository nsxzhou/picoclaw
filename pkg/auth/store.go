package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/fileutil"
)

type AuthCredential struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	AccountID    string    `json:"account_id,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Provider     string    `json:"provider"`
	AuthMethod   string    `json:"auth_method"`
	Email        string    `json:"email,omitempty"`
	ProjectID    string    `json:"project_id,omitempty"`
	Scope        []string  `json:"scope,omitempty"`
	TenantKey    string    `json:"tenant_key,omitempty"`
	UserID       string    `json:"user_id,omitempty"`
	OpenID       string    `json:"open_id,omitempty"`
	UnionID      string    `json:"union_id,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
}

type AuthStore struct {
	Credentials map[string]*AuthCredential `json:"credentials"`
}

func (c *AuthCredential) UnmarshalJSON(data []byte) error {
	type rawCredential struct {
		AccessToken  string          `json:"access_token"`
		RefreshToken string          `json:"refresh_token,omitempty"`
		AccountID    string          `json:"account_id,omitempty"`
		ExpiresAt    time.Time       `json:"expires_at,omitempty"`
		Provider     string          `json:"provider"`
		AuthMethod   string          `json:"auth_method"`
		Email        string          `json:"email,omitempty"`
		ProjectID    string          `json:"project_id,omitempty"`
		Scope        json.RawMessage `json:"scope,omitempty"`
		TenantKey    string          `json:"tenant_key,omitempty"`
		UserID       string          `json:"user_id,omitempty"`
		OpenID       string          `json:"open_id,omitempty"`
		UnionID      string          `json:"union_id,omitempty"`
		DisplayName  string          `json:"display_name,omitempty"`
	}

	var decoded rawCredential
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	*c = AuthCredential{
		AccessToken:  decoded.AccessToken,
		RefreshToken: decoded.RefreshToken,
		AccountID:    decoded.AccountID,
		ExpiresAt:    decoded.ExpiresAt,
		Provider:     decoded.Provider,
		AuthMethod:   decoded.AuthMethod,
		Email:        decoded.Email,
		ProjectID:    decoded.ProjectID,
		TenantKey:    decoded.TenantKey,
		UserID:       decoded.UserID,
		OpenID:       decoded.OpenID,
		UnionID:      decoded.UnionID,
		DisplayName:  decoded.DisplayName,
	}

	scopeRaw := decoded.Scope
	if len(scopeRaw) == 0 || string(scopeRaw) == "null" {
		return nil
	}

	var scopeList []string
	if err := json.Unmarshal(scopeRaw, &scopeList); err == nil {
		c.Scope = compactScope(scopeList)
		return nil
	}

	var scopeText string
	if err := json.Unmarshal(scopeRaw, &scopeText); err == nil {
		c.Scope = parseScopeString(scopeText)
		return nil
	}

	var scopeItems []any
	if err := json.Unmarshal(scopeRaw, &scopeItems); err == nil {
		scopeList = make([]string, 0, len(scopeItems))
		for _, item := range scopeItems {
			if text := strings.TrimSpace(toString(item)); text != "" {
				scopeList = append(scopeList, text)
			}
		}
		c.Scope = compactScope(scopeList)
	}

	return nil
}

func (c *AuthCredential) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(c.ExpiresAt)
}

func (c *AuthCredential) NeedsRefresh() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().Add(5 * time.Minute).After(c.ExpiresAt)
}

func authFilePath() string {
	if home := os.Getenv("PICOCLAW_HOME"); home != "" {
		return filepath.Join(home, "auth.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".picoclaw", "auth.json")
}

func LoadStore() (*AuthStore, error) {
	path := authFilePath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &AuthStore{Credentials: make(map[string]*AuthCredential)}, nil
		}
		return nil, err
	}

	var store AuthStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	if store.Credentials == nil {
		store.Credentials = make(map[string]*AuthCredential)
	}
	return &store, nil
}

func SaveStore(store *AuthStore) error {
	path := authFilePath()
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}

	// Use unified atomic write utility with explicit sync for flash storage reliability.
	return fileutil.WriteFileAtomic(path, data, 0o600)
}

func GetCredential(provider string) (*AuthCredential, error) {
	store, err := LoadStore()
	if err != nil {
		return nil, err
	}
	cred, ok := store.Credentials[provider]
	if !ok {
		return nil, nil
	}
	return cred, nil
}

func SetCredential(provider string, cred *AuthCredential) error {
	store, err := LoadStore()
	if err != nil {
		return err
	}
	store.Credentials[provider] = cred
	return SaveStore(store)
}

func DeleteCredential(provider string) error {
	store, err := LoadStore()
	if err != nil {
		return err
	}
	delete(store.Credentials, provider)
	return SaveStore(store)
}

func DeleteAllCredentials() error {
	path := authFilePath()
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func parseScopeString(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' '
	})
	return compactScope(parts)
}

func compactScope(scope []string) []string {
	if len(scope) == 0 {
		return nil
	}
	out := make([]string, 0, len(scope))
	for _, item := range scope {
		item = strings.TrimSpace(item)
		if item != "" {
			out = append(out, item)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func toString(v any) string {
	switch value := v.(type) {
	case string:
		return value
	default:
		if v == nil {
			return ""
		}
		return strings.TrimSpace(fmt.Sprint(value))
	}
}
