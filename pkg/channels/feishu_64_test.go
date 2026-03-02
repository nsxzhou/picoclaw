//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package channels

import (
	"testing"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

func strPtr(s string) *string {
	return &s
}

func TestExtractFeishuTextContent(t *testing.T) {
	tests := []struct {
		name    string
		message *larkim.EventMessage
		want    string
	}{
		{
			name:    "nil message",
			message: nil,
			want:    "",
		},
		{
			name:    "empty content",
			message: &larkim.EventMessage{Content: strPtr("")},
			want:    "",
		},
		{
			name:    "valid text payload",
			message: &larkim.EventMessage{Content: strPtr(`{"text":"你好"}`)},
			want:    "你好",
		},
		{
			name:    "invalid json falls back to raw",
			message: &larkim.EventMessage{Content: strPtr("plain text")},
			want:    "plain text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFeishuTextContent(tt.message)
			if got != tt.want {
				t.Fatalf("extractFeishuTextContent() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractFeishuImageKey(t *testing.T) {
	tests := []struct {
		name    string
		message *larkim.EventMessage
		want    string
	}{
		{
			name:    "nil message",
			message: nil,
			want:    "",
		},
		{
			name:    "valid payload",
			message: &larkim.EventMessage{Content: strPtr(`{"image_key":"img_123"}`)},
			want:    "img_123",
		},
		{
			name:    "invalid payload",
			message: &larkim.EventMessage{Content: strPtr(`{"text":"no image key"}`)},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractFeishuImageKey(tt.message)
			if got != tt.want {
				t.Fatalf("extractFeishuImageKey() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractFeishuFileInfo(t *testing.T) {
	tests := []struct {
		name     string
		message  *larkim.EventMessage
		wantKey  string
		wantName string
	}{
		{
			name:     "nil message",
			message:  nil,
			wantKey:  "",
			wantName: "",
		},
		{
			name:     "valid payload",
			message:  &larkim.EventMessage{Content: strPtr(`{"file_key":"file_123","file_name":"a.txt"}`)},
			wantKey:  "file_123",
			wantName: "a.txt",
		},
		{
			name:     "invalid payload",
			message:  &larkim.EventMessage{Content: strPtr(`{"text":"no file info"}`)},
			wantKey:  "",
			wantName: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotKey, gotName := extractFeishuFileInfo(tt.message)
			if gotKey != tt.wantKey || gotName != tt.wantName {
				t.Fatalf("extractFeishuFileInfo() = (%q, %q), want (%q, %q)", gotKey, gotName, tt.wantKey, tt.wantName)
			}
		})
	}
}
