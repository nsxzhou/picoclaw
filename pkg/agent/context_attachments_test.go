package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestBuildMessages_IncludesAttachmentContext(t *testing.T) {
	cb := NewContextBuilder(t.TempDir())

	attachments := []bus.Attachment{
		{
			Name:        "report.txt",
			MediaType:   "text/plain",
			SizeBytes:   11,
			TextContent: "hello world",
		},
	}

	attachmentErrors := []bus.AttachmentError{
		{
			Name:        "large.pdf",
			Code:        "file_too_large",
			UserMessage: "Attachment \"large.pdf\" is too large to parse. Please upload a smaller file.",
		},
	}

	messages := cb.BuildMessages(
		context.Background(),
		nil,
		"",
		"please check files",
		nil,
		attachments,
		attachmentErrors,
		nil,
		"cli",
		"chat1",
	)
	if len(messages) == 0 {
		t.Fatal("BuildMessages returned empty messages")
	}

	userMsg := messages[len(messages)-1]
	if userMsg.Role != "user" {
		t.Fatalf("last message role = %q, want %q", userMsg.Role, "user")
	}
	if !strings.Contains(userMsg.Content, "please check files") {
		t.Fatalf("user message does not contain original text: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "BEGIN_ATTACHMENT_DATA") ||
		!strings.Contains(userMsg.Content, "END_ATTACHMENT_DATA") {
		t.Fatalf("user message does not contain attachment data boundaries: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "untrusted user-provided file data") {
		t.Fatalf("user message does not contain untrusted data guardrail: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "report.txt") || !strings.Contains(userMsg.Content, "hello world") {
		t.Fatalf("user message does not contain attachment context: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "BEGIN_ATTACHMENT_ERRORS") ||
		!strings.Contains(userMsg.Content, "END_ATTACHMENT_ERRORS") {
		t.Fatalf("user message does not contain attachment error boundaries: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "- large.pdf:") {
		t.Fatalf("user message does not contain attachment error: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "Do NOT attempt to find, read, or access these") {
		t.Fatalf("user message does not contain NOTE preventing tool usage: %q", userMsg.Content)
	}
}

type mockFileRefResolver struct {
	resolveFn func(ref *bus.FileRef) (string, string, error)
}

func (m *mockFileRefResolver) Resolve(_ context.Context, ref *bus.FileRef) (string, string, error) {
	if m.resolveFn == nil {
		return "", "", fmt.Errorf("resolver not configured")
	}
	return m.resolveFn(ref)
}

func TestBuildMessages_ResolvesHistoryFileRefs(t *testing.T) {
	cb := NewContextBuilder(t.TempDir())
	cb.SetFileRefResolver(&mockFileRefResolver{
		resolveFn: func(ref *bus.FileRef) (string, string, error) {
			if ref.FeishuFileKey == "doc_001" {
				return "application/pdf", "cGRmZGF0YQ==", nil
			}
			return "", "", fmt.Errorf("unexpected ref")
		},
	})

	history := []providers.Message{
		{
			Role:    "user",
			Content: "请参考上次文件",
			FileRefs: []providers.FileRefMeta{
				{
					Name:            "report.pdf",
					MediaType:       "application/pdf",
					Kind:            string(bus.AttachmentKindDocument),
					Source:          string(bus.FileRefSourceFeishu),
					FeishuMessageID: "om_001",
					FeishuFileKey:   "doc_001",
					FeishuResType:   "file",
				},
			},
		},
	}

	messages := cb.BuildMessages(context.Background(), history, "", "", nil, nil, nil, nil, "feishu", "chat1")
	if len(messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2 (system + history)", len(messages))
	}

	historyMsg := messages[1]
	if historyMsg.Role != "user" {
		t.Fatalf("history message role = %q, want user", historyMsg.Role)
	}
	if len(historyMsg.Files) != 1 {
		t.Fatalf("len(historyMsg.Files) = %d, want 1", len(historyMsg.Files))
	}
	if historyMsg.Files[0].MediaType != "application/pdf" {
		t.Fatalf("file media type = %q, want application/pdf", historyMsg.Files[0].MediaType)
	}
	if historyMsg.Files[0].Data != "cGRmZGF0YQ==" {
		t.Fatalf("file data not resolved as expected: %q", historyMsg.Files[0].Data)
	}
}

func TestBuildMessages_MergesFileRefsWithLegacyAttachments(t *testing.T) {
	cb := NewContextBuilder(t.TempDir())
	cb.SetFileRefResolver(&mockFileRefResolver{
		resolveFn: func(ref *bus.FileRef) (string, string, error) {
			switch ref.FeishuFileKey {
			case "img_001":
				return "image/jpeg", "aW1hZ2UtZnJvbS1yZWY=", nil
			case "doc_001":
				return "application/pdf", "ZG9jLWZyb20tcmVm", nil
			default:
				return "", "", fmt.Errorf("unexpected ref")
			}
		},
	})

	images := []bus.EncodedImage{
		{MediaType: "image/png", Data: "bGVnYWN5LWltZw=="},
	}
	attachments := []bus.Attachment{
		{
			Name:        "notes.txt",
			MediaType:   "text/plain",
			SizeBytes:   12,
			TextContent: "legacy attachment text",
		},
	}
	attachmentErrors := []bus.AttachmentError{
		{
			Name:        "broken.pdf",
			Code:        "parse_failed",
			UserMessage: "Attachment \"broken.pdf\" was received but could not be parsed.",
		},
	}
	fileRefs := []bus.FileRef{
		{
			Name:            "ref-image.jpg",
			Kind:            bus.AttachmentKindImage,
			Source:          bus.FileRefSourceFeishu,
			FeishuMessageID: "om_001",
			FeishuFileKey:   "img_001",
			FeishuResType:   "image",
		},
		{
			Name:            "ref-doc.pdf",
			Kind:            bus.AttachmentKindDocument,
			Source:          bus.FileRefSourceFeishu,
			FeishuMessageID: "om_001",
			FeishuFileKey:   "doc_001",
			FeishuResType:   "file",
		},
	}

	messages := cb.BuildMessages(
		context.Background(),
		nil,
		"",
		"please review all files",
		images,
		attachments,
		attachmentErrors,
		fileRefs,
		"feishu",
		"chat1",
	)
	if len(messages) == 0 {
		t.Fatal("BuildMessages returned empty messages")
	}

	userMsg := messages[len(messages)-1]
	if userMsg.Role != "user" {
		t.Fatalf("last message role = %q, want user", userMsg.Role)
	}
	if !strings.Contains(userMsg.Content, "please review all files") {
		t.Fatalf("user message missing original content: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "BEGIN_ATTACHMENT_DATA") ||
		!strings.Contains(userMsg.Content, "legacy attachment text") {
		t.Fatalf("user message missing legacy attachment context: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "BEGIN_ATTACHMENT_ERRORS") ||
		!strings.Contains(userMsg.Content, "- broken.pdf:") {
		t.Fatalf("user message missing attachment errors: %q", userMsg.Content)
	}
	if len(userMsg.Images) != 2 {
		t.Fatalf("len(userMsg.Images) = %d, want 2 (legacy + fileRef)", len(userMsg.Images))
	}
	if userMsg.Images[0].MediaType != "image/png" {
		t.Fatalf("legacy image media type = %q, want image/png", userMsg.Images[0].MediaType)
	}
	if userMsg.Images[1].MediaType != "image/jpeg" {
		t.Fatalf("fileRef image media type = %q, want image/jpeg", userMsg.Images[1].MediaType)
	}
	if len(userMsg.Files) != 1 {
		t.Fatalf("len(userMsg.Files) = %d, want 1", len(userMsg.Files))
	}
	if userMsg.Files[0].Name != "ref-doc.pdf" {
		t.Fatalf("fileRef doc name = %q, want ref-doc.pdf", userMsg.Files[0].Name)
	}
}

func TestBuildMessages_FileRefsWithoutResolverKeepsLegacyInput(t *testing.T) {
	cb := NewContextBuilder(t.TempDir())

	images := []bus.EncodedImage{
		{MediaType: "image/png", Data: "bGVnYWN5LWltZw=="},
	}
	attachments := []bus.Attachment{
		{
			Name:        "notes.txt",
			MediaType:   "text/plain",
			SizeBytes:   12,
			TextContent: "legacy attachment text",
		},
	}
	fileRefs := []bus.FileRef{
		{
			Name:            "ref-doc.pdf",
			Kind:            bus.AttachmentKindDocument,
			Source:          bus.FileRefSourceFeishu,
			FeishuMessageID: "om_001",
			FeishuFileKey:   "doc_001",
			FeishuResType:   "file",
		},
	}

	messages := cb.BuildMessages(
		context.Background(),
		nil,
		"",
		"",
		images,
		attachments,
		nil,
		fileRefs,
		"feishu",
		"chat1",
	)
	if len(messages) == 0 {
		t.Fatal("BuildMessages returned empty messages")
	}

	userMsg := messages[len(messages)-1]
	if len(userMsg.Images) != 1 {
		t.Fatalf("len(userMsg.Images) = %d, want 1 legacy image", len(userMsg.Images))
	}
	if len(userMsg.Files) != 0 {
		t.Fatalf("len(userMsg.Files) = %d, want 0 when resolver missing", len(userMsg.Files))
	}
	if !strings.Contains(userMsg.Content, "BEGIN_ATTACHMENT_DATA") {
		t.Fatalf("user message missing legacy attachment context: %q", userMsg.Content)
	}
	if !strings.Contains(userMsg.Content, "no resolver configured") {
		t.Fatalf("user message missing resolver error hint: %q", userMsg.Content)
	}
}
