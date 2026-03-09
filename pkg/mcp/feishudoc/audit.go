package feishudoc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type auditRecord struct {
	Timestamp          string         `json:"timestamp"`
	Tool               string         `json:"tool"`
	ActionID           string         `json:"action_id,omitempty"`
	Channel            string         `json:"channel"`
	ChatID             string         `json:"chat_id"`
	SenderID           string         `json:"sender_id,omitempty"`
	Target             string         `json:"target,omitempty"`
	Status             string         `json:"status"`
	AuthMode           string         `json:"auth_mode,omitempty"`
	BoundIdentityMatch *bool          `json:"bound_identity_match,omitempty"`
	Details            map[string]any `json:"details,omitempty"`
	Error              string         `json:"error,omitempty"`
}

type auditLogger struct {
	mu   sync.Mutex
	path string
}

func newAuditLogger(workspace string) *auditLogger {
	return &auditLogger{
		path: filepath.Join(workspace, "state", "feishu_doc_audit.jsonl"),
	}
}

func (l *auditLogger) Write(rec auditRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().UTC().Format(time.RFC3339Nano)
	}

	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return fmt.Errorf("create audit log directory: %w", err)
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal audit record: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open audit log: %w", err)
	}
	defer f.Close()

	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}

	return nil
}
