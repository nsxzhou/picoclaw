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

func TestLooksLikeFeishuFileToken(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "file url", input: "https://feishu.cn/file/boxcn123", want: true},
		{name: "file prefix", input: "file/boxcn123", want: true},
		{name: "docx url", input: "https://feishu.cn/docx/doccn123", want: false},
		{name: "plain token", input: "boxcn123", want: false},
		{name: "empty", input: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := looksLikeFeishuFileToken(tt.input)
			if got != tt.want {
				t.Fatalf("looksLikeFeishuFileToken(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsFeishuNotFoundError(t *testing.T) {
	tests := []struct {
		name    string
		code    int
		msg     string
		callErr error
		want    bool
	}{
		{name: "explicit code", code: 1770002, msg: "", callErr: nil, want: true},
		{name: "english msg", code: 0, msg: "not found", callErr: nil, want: true},
		{name: "chinese msg", code: 0, msg: "资源不存在", callErr: nil, want: true},
		{name: "error text", code: 0, msg: "", callErr: errString("api error: code=1770002"), want: true},
		{name: "other", code: 999, msg: "permission denied", callErr: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isFeishuNotFoundError(tt.code, tt.msg, tt.callErr)
			if got != tt.want {
				t.Fatalf("isFeishuNotFoundError(%d, %q, %v) = %v, want %v", tt.code, tt.msg, tt.callErr, got, tt.want)
			}
		})
	}
}

func TestShouldFallbackDocxToFile(t *testing.T) {
	t.Run("unresolved docx meta with not found", func(t *testing.T) {
		meta := &fileMeta{Type: feishuDocType, Resolved: false}
		if !shouldFallbackDocxToFile("boxcn123", meta, 1770002, "not found", nil) {
			t.Fatal("expected fallback for unresolved docx meta")
		}
	})

	t.Run("resolved docx meta + explicit file token + not found", func(t *testing.T) {
		meta := &fileMeta{Type: feishuDocType, Resolved: true}
		if !shouldFallbackDocxToFile("file/boxcn123", meta, 1770002, "not found", nil) {
			t.Fatal("expected fallback when token clearly points to file")
		}
	})

	t.Run("resolved docx meta + plain token + not found", func(t *testing.T) {
		meta := &fileMeta{Type: feishuDocType, Resolved: true}
		if shouldFallbackDocxToFile("doccn123", meta, 1770002, "not found", nil) {
			t.Fatal("did not expect fallback for resolved docx token")
		}
	})

	t.Run("file type should not use docx fallback gate", func(t *testing.T) {
		meta := &fileMeta{Type: feishuFileType, Resolved: true}
		if shouldFallbackDocxToFile("file/boxcn123", meta, 1770002, "not found", nil) {
			t.Fatal("did not expect fallback gate to run for non-docx meta type")
		}
	})
}

type errString string

func (e errString) Error() string {
	return string(e)
}

func strPtr(v string) *string {
	return &v
}
