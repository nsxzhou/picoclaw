package tools

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/cron"
)

func newTestCronTool(t *testing.T) (*CronTool, *cron.CronService, *bus.MessageBus) {
	t.Helper()

	cfg := config.DefaultConfig()
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")
	cs := cron.NewCronService(storePath, nil)
	msgBus := bus.NewMessageBus()
	t.Cleanup(msgBus.Close)

	tool, err := NewCronTool(cs, msgBus, tmpDir, true, 30*time.Second, cfg)
	if err != nil {
		t.Fatalf("NewCronTool failed: %v", err)
	}
	return tool, cs, msgBus
}

func TestCronToolExecuteJob_DeliverFalsePublishesInbound(t *testing.T) {
	tool, cs, msgBus := newTestCronTool(t)

	delegation, err := cs.SignDelegation("job-1", "feishu", "oc_123", "feishu:ou_456", time.Now())
	if err != nil {
		t.Fatalf("SignDelegation failed: %v", err)
	}

	job := &cron.CronJob{
		ID: "job-1",
		Payload: cron.CronPayload{
			Message:    "scheduled test message",
			Deliver:    false,
			Channel:    "feishu",
			To:         "oc_123",
			Delegation: delegation,
		},
	}

	if got := tool.ExecuteJob(context.Background(), job); got != "ok" {
		t.Fatalf("ExecuteJob() = %q, want ok", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	msg, ok := msgBus.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected inbound message to be published")
	}

	if msg.Channel != "feishu" {
		t.Fatalf("inbound channel = %q, want feishu", msg.Channel)
	}
	if msg.ChatID != "oc_123" {
		t.Fatalf("inbound chat_id = %q, want oc_123", msg.ChatID)
	}
	if msg.SenderID != "feishu:ou_456" {
		t.Fatalf("inbound sender_id = %q, want feishu:ou_456", msg.SenderID)
	}
	if msg.SessionKey != "cron-job-1" {
		t.Fatalf("inbound session_key = %q, want cron-job-1", msg.SessionKey)
	}
	if msg.Content != "scheduled test message" {
		t.Fatalf("inbound content = %q, want scheduled test message", msg.Content)
	}
}

func TestCronToolExecuteJob_DeliverFalseDelegationInvalid(t *testing.T) {
	tool, cs, _ := newTestCronTool(t)

	delegation, err := cs.SignDelegation("job-1", "feishu", "oc_123", "feishu:ou_456", time.Now())
	if err != nil {
		t.Fatalf("SignDelegation failed: %v", err)
	}
	job := &cron.CronJob{
		ID: "job-1",
		Payload: cron.CronPayload{
			Message:    "scheduled test message",
			Deliver:    false,
			Channel:    "feishu",
			To:         "oc_other", // tampered chat id should fail validation
			Delegation: delegation,
		},
	}

	got := tool.ExecuteJob(context.Background(), job)
	if !strings.HasPrefix(got, "Error:") {
		t.Fatalf("ExecuteJob() = %q, want Error:*", got)
	}
}

func TestCronToolExecuteJob_DeliverFalsePublishInboundFailure(t *testing.T) {
	tool, cs, msgBus := newTestCronTool(t)
	msgBus.Close()

	delegation, err := cs.SignDelegation("job-1", "feishu", "oc_123", "feishu:ou_456", time.Now())
	if err != nil {
		t.Fatalf("SignDelegation failed: %v", err)
	}

	job := &cron.CronJob{
		ID: "job-1",
		Payload: cron.CronPayload{
			Message:    "scheduled test message",
			Deliver:    false,
			Channel:    "feishu",
			To:         "oc_123",
			Delegation: delegation,
		},
	}

	got := tool.ExecuteJob(context.Background(), job)
	if !strings.Contains(got, "publish cron inbound failed") {
		t.Fatalf("ExecuteJob() = %q, want publish cron inbound failed", got)
	}
}
