package feishudoc

import (
	"testing"
	"time"
)

func TestConfirmationManagerCreateValidateAndConsume(t *testing.T) {
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	manager := newConfirmationManager(2 * time.Minute)

	payload := map[string]any{
		"doc_token": "doccn123",
		"strategy":  "replace",
	}

	action, err := manager.Create("doc_update", "feishu:chat-1", "user-1", payload, "preview", now)
	if err != nil {
		t.Fatalf("create action failed: %v", err)
	}
	if action.ID == "" {
		t.Fatal("expected non-empty action id")
	}

	if _, err := manager.Validate(action.ID, "doc_update", "feishu:chat-1", payload, now.Add(time.Minute)); err != nil {
		t.Fatalf("validate action failed: %v", err)
	}

	manager.Consume(action.ID)
	if _, err := manager.Validate(action.ID, "doc_update", "feishu:chat-1", payload, now.Add(time.Minute)); err == nil {
		t.Fatal("expected consumed action to be invalid")
	}
}

func TestConfirmationManagerValidatePayloadMismatch(t *testing.T) {
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	manager := newConfirmationManager(2 * time.Minute)

	payloadA := map[string]any{"content": "A"}
	action, err := manager.Create("doc_update", "feishu:chat-1", "user-1", payloadA, "preview", now)
	if err != nil {
		t.Fatalf("create action failed: %v", err)
	}

	payloadB := map[string]any{"content": "B"}
	if _, err := manager.Validate(
		action.ID,
		"doc_update",
		"feishu:chat-1",
		payloadB,
		now.Add(time.Minute),
	); err == nil {
		t.Fatal("expected payload mismatch validation error")
	}
}

func TestConfirmationManagerValidateContextMismatch(t *testing.T) {
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	manager := newConfirmationManager(2 * time.Minute)

	payload := map[string]any{"doc_token": "doccnX"}
	action, err := manager.Create("doc_delete", "feishu:chat-1", "user-1", payload, "preview", now)
	if err != nil {
		t.Fatalf("create action failed: %v", err)
	}

	if _, err := manager.Validate(
		action.ID,
		"doc_delete",
		"feishu:chat-2",
		payload,
		now.Add(time.Minute),
	); err == nil {
		t.Fatal("expected context mismatch validation error")
	}
}

func TestConfirmationManagerExpiredAction(t *testing.T) {
	now := time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC)
	manager := newConfirmationManager(30 * time.Second)

	payload := map[string]any{"doc_token": "doccnX"}
	action, err := manager.Create("doc_share", "feishu:chat-1", "user-1", payload, "preview", now)
	if err != nil {
		t.Fatalf("create action failed: %v", err)
	}

	if _, err := manager.Validate(
		action.ID,
		"doc_share",
		"feishu:chat-1",
		payload,
		now.Add(31*time.Second),
	); err == nil {
		t.Fatal("expected expired action validation error")
	}
}
