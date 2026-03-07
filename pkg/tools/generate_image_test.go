package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

// canListen 检测当前环境是否允许绑定端口（某些沙箱环境可能禁止）。
func canListen() bool {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return false
	}
	l.Close()
	return true
}

func TestGenerateImageTool_Name(t *testing.T) {
	tool := NewGenerateImageTool("/tmp/ws", config.ImageToolConfig{}, nil)
	if tool.Name() != "generate_image" {
		t.Errorf("expected name 'generate_image', got %q", tool.Name())
	}
}

func TestGenerateImageTool_Parameters(t *testing.T) {
	tool := NewGenerateImageTool("/tmp/ws", config.ImageToolConfig{}, nil)
	params := tool.Parameters()
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	if _, ok := props["prompt"]; !ok {
		t.Error("expected 'prompt' in parameters")
	}
	required, ok := params["required"].([]string)
	if !ok {
		t.Fatal("expected required list")
	}
	if len(required) != 1 || required[0] != "prompt" {
		t.Errorf("expected required=['prompt'], got %v", required)
	}
}

func TestGenerateImageTool_EmptyPrompt(t *testing.T) {
	tool := NewGenerateImageTool("/tmp/ws", config.ImageToolConfig{
		ToolConfig: config.ToolConfig{Enabled: true},
		APIKey:     "test-key",
		APIBase:    "http://localhost",
		Model:      "test-model",
	}, nil)

	result := tool.Execute(context.Background(), map[string]any{})
	if !result.IsError {
		t.Error("expected error for empty prompt")
	}
	if !strings.Contains(result.ForLLM, "prompt is required") {
		t.Errorf("expected 'prompt is required' error, got %q", result.ForLLM)
	}
}

func TestGenerateImageTool_NotConfigured(t *testing.T) {
	tool := NewGenerateImageTool("/tmp/ws", config.ImageToolConfig{}, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"prompt": "a cute cat",
	})
	if !result.IsError {
		t.Error("expected error for unconfigured tool")
	}
	if !strings.Contains(result.ForLLM, "not configured") {
		t.Errorf("expected 'not configured' error, got %q", result.ForLLM)
	}
}

func TestGenerateImageTool_APIError(t *testing.T) {
	if !canListen() {
		t.Skip("skipping: cannot bind port in this environment")
	}
	// 模拟返回错误的 API 服务
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	tool := NewGenerateImageTool("/tmp/ws", config.ImageToolConfig{
		ToolConfig: config.ToolConfig{Enabled: true},
		APIKey:     "test-key",
		APIBase:    server.URL,
		Model:      "test-model",
	}, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"prompt": "a cute cat",
	})
	if !result.IsError {
		t.Error("expected error for API failure")
	}
	if !strings.Contains(result.ForLLM, "500") {
		t.Errorf("expected status 500 in error, got %q", result.ForLLM)
	}
}

func TestGenerateImageTool_SuccessB64(t *testing.T) {
	if !canListen() {
		t.Skip("skipping: cannot bind port in this environment")
	}
	// 创建临时 workspace
	tmpDir := t.TempDir()

	// 1x1 白色 PNG（最小有效 PNG）
	pngBytes := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, // PNG signature
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53,
		0xde, 0x00, 0x00, 0x00, 0x0c, 0x49, 0x44, 0x41, // IDAT chunk
		0x54, 0x08, 0xd7, 0x63, 0xf8, 0xcf, 0xc0, 0x00,
		0x00, 0x00, 0x02, 0x00, 0x01, 0xe2, 0x21, 0xbc,
		0x33, 0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, // IEND chunk
		0x44, 0xae, 0x42, 0x60, 0x82,
	}
	b64Data := base64.StdEncoding.EncodeToString(pngBytes)

	// 模拟成功的 API 服务
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/v1/images/generations") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}

		// 验证请求体
		var reqBody imageGenRequest
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if reqBody.Prompt != "a cute cat" {
			t.Errorf("unexpected prompt: %s", reqBody.Prompt)
		}
		if reqBody.Model != "test-model" {
			t.Errorf("unexpected model: %s", reqBody.Model)
		}

		resp := imageGenResponse{
			Created: 1234567890,
			Data: []imageGenData{
				{B64JSON: b64Data},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tool := NewGenerateImageTool(tmpDir, config.ImageToolConfig{
		ToolConfig: config.ToolConfig{Enabled: true},
		APIKey:     "test-key",
		APIBase:    server.URL,
		Model:      "test-model",
	}, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"prompt": "a cute cat",
	})
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if !strings.Contains(result.ForLLM, "saved to") {
		t.Errorf("expected 'saved to' in result, got %q", result.ForLLM)
	}

	// 验证图片已保存
	imgDir := filepath.Join(tmpDir, "generated_images")
	entries, err := os.ReadDir(imgDir)
	if err != nil {
		t.Fatalf("failed to read image dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 image file, got %d", len(entries))
	}
	if !strings.HasSuffix(entries[0].Name(), ".png") {
		t.Errorf("expected .png file, got %s", entries[0].Name())
	}
}

func TestGenerateImageTool_EmptyData(t *testing.T) {
	if !canListen() {
		t.Skip("skipping: cannot bind port in this environment")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := imageGenResponse{
			Created: 1234567890,
			Data:    []imageGenData{},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	tool := NewGenerateImageTool("/tmp/ws", config.ImageToolConfig{
		ToolConfig: config.ToolConfig{Enabled: true},
		APIKey:     "test-key",
		APIBase:    server.URL,
		Model:      "test-model",
	}, nil)

	result := tool.Execute(context.Background(), map[string]any{
		"prompt": "a cute cat",
	})
	if !result.IsError {
		t.Error("expected error for empty data")
	}
	if !strings.Contains(result.ForLLM, "no images") {
		t.Errorf("expected 'no images' error, got %q", result.ForLLM)
	}
}
