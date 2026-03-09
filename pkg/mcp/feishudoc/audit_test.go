package feishudoc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditLoggerWrite(t *testing.T) {
	workspace := t.TempDir()
	logger := newAuditLogger(workspace)

	record := auditRecord{
		Tool:     "doc_update",
		ActionID: "action-1",
		Channel:  "feishu",
		ChatID:   "chat-1",
		SenderID: "user-1",
		Target:   "doccn123",
		Status:   "success",
		Details: map[string]any{
			"strategy": "replace",
		},
	}

	if err := logger.Write(record); err != nil {
		t.Fatalf("write audit record failed: %v", err)
	}

	logPath := filepath.Join(workspace, "state", "feishu_doc_audit.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var got auditRecord
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal audit line failed: %v", err)
	}
	if got.Timestamp == "" {
		t.Fatal("expected timestamp to be set")
	}
	if got.Tool != "doc_update" {
		t.Fatalf("unexpected tool: %s", got.Tool)
	}
	if got.Channel != "feishu" || got.ChatID != "chat-1" {
		t.Fatalf("unexpected channel/chat: %s/%s", got.Channel, got.ChatID)
	}
}

func TestAuditLoggerWriteAppend(t *testing.T) {
	workspace := t.TempDir()
	logger := newAuditLogger(workspace)

	if err := logger.Write(auditRecord{
		Tool:    "doc_read",
		Channel: "feishu",
		ChatID:  "chat-1",
		Status:  "success",
	}); err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if err := logger.Write(auditRecord{
		Tool:    "doc_delete",
		Channel: "feishu",
		ChatID:  "chat-1",
		Status:  "error",
		Error:   "denied",
	}); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	logPath := filepath.Join(workspace, "state", "feishu_doc_audit.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read audit log failed: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
}
