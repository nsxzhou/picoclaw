package fileutil

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestWriteFileAtomicRenameBusyFallsBackToDirectWrite(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "auth.json")
	if err := os.WriteFile(target, []byte(`{"old":true}`), 0o600); err != nil {
		t.Fatalf("seed target file: %v", err)
	}

	originalRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		return syscall.EBUSY
	}
	defer func() {
		renameFile = originalRename
	}()

	want := []byte(`{"credentials":{"feishu-user":{"access_token":"token"}}}`)
	if err := WriteFileAtomic(target, want, 0o600); err != nil {
		t.Fatalf("WriteFileAtomic() error: %v", err)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target file: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("target content = %q, want %q", string(got), string(want))
	}
}

func TestWriteFileAtomicRenameUnexpectedError(t *testing.T) {
	tmpDir := t.TempDir()
	target := filepath.Join(tmpDir, "auth.json")

	originalRename := renameFile
	renameFile = func(oldpath, newpath string) error {
		return errors.New("rename denied")
	}
	defer func() {
		renameFile = originalRename
	}()

	err := WriteFileAtomic(target, []byte(`{}`), 0o600)
	if err == nil {
		t.Fatal("expected error")
	}
}
