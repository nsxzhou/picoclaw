package feishudoc

import (
	"testing"

	larkdrive "github.com/larksuite/oapi-sdk-go/v3/service/drive/v1"
)

func TestNormalizeFolderToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain token", input: "fldAbc123", want: "fldAbc123"},
		{name: "drive folder url", input: "https://my.feishu.cn/drive/folder/fldAbc123", want: "fldAbc123"},
		{
			name:  "drive folder url with query",
			input: "https://my.feishu.cn/drive/folder/fldAbc123?from=explorer",
			want:  "fldAbc123",
		},
		{name: "folder token query", input: "folder_token=fldXYZ987", want: "fldXYZ987"},
		{
			name:  "folder url with folder token query",
			input: "https://my.feishu.cn/drive/folder/?folder_token=fldXYZ987&from=nav",
			want:  "fldXYZ987",
		},
		{name: "raw token", input: "abc123", want: "abc123"},
		{name: "folder url with non-fld token", input: "https://my.feishu.cn/drive/folder/abc123", want: "abc123"},
		{name: "empty", input: "", want: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeFolderToken(tc.input)
			if got != tc.want {
				t.Fatalf("normalizeFolderToken(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestFilterDriveItemsByQuery(t *testing.T) {
	items := []map[string]any{
		{"name": "AI 日报", "token": "fld1", "type": "folder"},
		{"name": "AI行业日报_2026-03-10.md", "token": "file1", "type": "file"},
		{"name": "随机文档", "token": "doc1", "type": "docx"},
	}

	got := filterDriveItemsByQuery(items, "ai")
	if len(got) != 2 {
		t.Fatalf("expected 2 filtered items, got %d", len(got))
	}
	if got[0]["token"] != "fld1" || got[1]["token"] != "file1" {
		t.Fatalf("unexpected filtered items: %#v", got)
	}
}

func TestSortByNameAndToken(t *testing.T) {
	items := []map[string]any{
		{"name": "beta", "token": "token-2"},
		{"name": "Alpha", "token": "token-3"},
		{"name": "alpha", "token": "token-1"},
	}

	sortByNameAndToken(items)
	if items[0]["token"] != "token-1" {
		t.Fatalf("first token = %v, want token-1", items[0]["token"])
	}
	if items[1]["token"] != "token-3" {
		t.Fatalf("second token = %v, want token-3", items[1]["token"])
	}
	if items[2]["token"] != "token-2" {
		t.Fatalf("third token = %v, want token-2", items[2]["token"])
	}
}

func TestDriveFileToMap(t *testing.T) {
	file := &larkdrive.File{
		Token:       strPtr("tok-1"),
		Name:        strPtr("日报"),
		Type:        strPtr("FOLDER"),
		Url:         strPtr("https://example"),
		ParentToken: strPtr("fld-parent"),
		OwnerId:     strPtr("ou_123"),
	}

	got := driveFileToMap(file)
	if got["doc_token"] != "tok-1" {
		t.Fatalf("doc_token = %v, want tok-1", got["doc_token"])
	}
	if got["type"] != "folder" {
		t.Fatalf("type = %v, want folder", got["type"])
	}
	if got["name"] != "日报" {
		t.Fatalf("name = %v, want 日报", got["name"])
	}
}

func TestNormalizeUploadMIMEType(t *testing.T) {
	if got := normalizeUploadMIMEType(""); got != "text/markdown; charset=utf-8" {
		t.Fatalf("default mime type = %q", got)
	}
	if got := normalizeUploadMIMEType("text/plain"); got != "text/plain" {
		t.Fatalf("custom mime type = %q", got)
	}
}

func strPtr(v string) *string {
	return &v
}
