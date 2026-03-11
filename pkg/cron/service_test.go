package cron

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSaveStore_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")

	cs := NewCronService(storePath, nil)

	_, err := cs.AddJob("test", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", false, "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("cron store has permission %04o, want 0600", perm)
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}

func TestCronDelegationSignAndValidate(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")
	cs := NewCronService(storePath, nil)

	const (
		jobID    = "job-1"
		channel  = "feishu"
		chatID   = "oc_123"
		senderID = "feishu:ou_456"
	)

	delegation, err := cs.SignDelegation(jobID, channel, chatID, senderID, time.Unix(100, 0))
	if err != nil {
		t.Fatalf("SignDelegation failed: %v", err)
	}
	if delegation == nil {
		t.Fatal("expected delegation to be non-nil")
	}
	if delegation.SenderID != senderID {
		t.Fatalf("delegation sender_id = %q, want %q", delegation.SenderID, senderID)
	}

	if err := cs.ValidateDelegation(delegation, jobID, channel, chatID); err != nil {
		t.Fatalf("ValidateDelegation failed: %v", err)
	}
}

func TestCronDelegationValidateRejectsTamperedClaims(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")
	cs := NewCronService(storePath, nil)

	delegation, err := cs.SignDelegation("job-1", "feishu", "oc_123", "feishu:ou_456", time.Unix(100, 0))
	if err != nil {
		t.Fatalf("SignDelegation failed: %v", err)
	}

	if err := cs.ValidateDelegation(delegation, "job-1", "feishu", "oc_999"); err == nil {
		t.Fatal("expected ValidateDelegation to fail after claims tampering")
	}
}
