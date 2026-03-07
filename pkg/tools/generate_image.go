package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
)

// GenerateImageTool 通过 OpenAI 兼容的 /v1/images/generations 接口生成图片。
type GenerateImageTool struct {
	workspace  string
	imgCfg     config.ImageToolConfig
	mediaStore media.MediaStore
	httpClient *http.Client
}

// NewGenerateImageTool 创建生图工具实例。
func NewGenerateImageTool(workspace string, imgCfg config.ImageToolConfig, store media.MediaStore) *GenerateImageTool {
	return &GenerateImageTool{
		workspace:  workspace,
		imgCfg:     imgCfg,
		mediaStore: store,
		httpClient: &http.Client{Timeout: 120 * time.Second},
	}
}

func (t *GenerateImageTool) Name() string { return "generate_image" }

func (t *GenerateImageTool) Description() string {
	return "Generate an image from a text prompt using an AI image generation model. Returns the image file path or sends it to the user."
}

func (t *GenerateImageTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "A detailed text description of the image to generate.",
			},
			"size": map[string]any{
				"type":        "string",
				"description": "Image size. Options: 1024x1024, 1792x1024, 1024x1792. Default: 1024x1024.",
				"enum":        []string{"1024x1024", "1792x1024", "1024x1792"},
			},
		},
		"required": []string{"prompt"},
	}
}

// SetMediaStore 设置媒体存储（延迟注入，在 agent loop 创建后设置）。
func (t *GenerateImageTool) SetMediaStore(store media.MediaStore) {
	t.mediaStore = store
}

func (t *GenerateImageTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	prompt, _ := args["prompt"].(string)
	if strings.TrimSpace(prompt) == "" {
		return ErrorResult("prompt is required")
	}

	if t.imgCfg.APIKey == "" || t.imgCfg.APIBase == "" || t.imgCfg.Model == "" {
		return ErrorResult("generate_image tool is not configured: api_key, api_base, and model are required in tools.generate_image config")
	}

	size, _ := args["size"].(string)
	if size == "" {
		size = "1024x1024"
	}

	// 构建请求
	reqBody := imageGenRequest{
		Model:          t.imgCfg.Model,
		Prompt:         prompt,
		N:              1,
		Size:           size,
		ResponseFormat: "b64_json",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to marshal request: %v", err))
	}

	// 构造 API URL
	apiURL := strings.TrimRight(t.imgCfg.APIBase, "/") + "/v1/images/generations"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to create request: %v", err))
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.imgCfg.APIKey)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return ErrorResult(fmt.Sprintf("API request failed: %v", err))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to read response: %v", err))
	}

	if resp.StatusCode != http.StatusOK {
		return ErrorResult(fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(respBody)))
	}

	// 解析响应
	var imgResp imageGenResponse
	if err := json.Unmarshal(respBody, &imgResp); err != nil {
		return ErrorResult(fmt.Sprintf("failed to parse response: %v", err))
	}

	if len(imgResp.Data) == 0 {
		return ErrorResult("API returned no images")
	}

	imgData := imgResp.Data[0]

	// 保存图片到 workspace
	imgDir := filepath.Join(t.workspace, "generated_images")
	os.MkdirAll(imgDir, 0o755)

	filename := fmt.Sprintf("img_%d.png", time.Now().UnixMilli())
	imgPath := filepath.Join(imgDir, filename)

	if imgData.B64JSON != "" {
		// base64 解码并保存
		decoded, err := base64.StdEncoding.DecodeString(imgData.B64JSON)
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to decode base64 image: %v", err))
		}
		if err := os.WriteFile(imgPath, decoded, 0o644); err != nil {
			return ErrorResult(fmt.Sprintf("failed to save image: %v", err))
		}
	} else if imgData.URL != "" {
		// 从 URL 下载图片
		if err := t.downloadImage(ctx, imgData.URL, imgPath); err != nil {
			return ErrorResult(fmt.Sprintf("failed to download image: %v", err))
		}
	} else {
		return ErrorResult("API response contains no image data (neither b64_json nor url)")
	}

	// 如果有 MediaStore，通过媒体管道发送
	channel := ToolChannel(ctx)
	chatID := ToolChatID(ctx)
	if t.mediaStore != nil && channel != "" && chatID != "" {
		scope := fmt.Sprintf("tool:generate_image:%s:%s", channel, chatID)
		ref, err := t.mediaStore.Store(imgPath, media.MediaMeta{
			Filename:    filename,
			ContentType: "image/png",
			Source:      "tool:generate_image",
		}, scope)
		if err != nil {
			// 媒体存储失败，回退到返回文件路径
			return SilentResult(fmt.Sprintf("Image generated and saved to: %s (media store error: %v)", imgPath, err))
		}
		return MediaResult(
			fmt.Sprintf("Image generated successfully from prompt: %q, saved to %s", prompt, imgPath),
			[]string{ref},
		)
	}

	// 无 MediaStore（CLI 模式），返回文件路径
	return SilentResult(fmt.Sprintf("Image generated and saved to: %s", imgPath))
}

// downloadImage 从 URL 下载图片到本地路径。
func (t *GenerateImageTool) downloadImage(ctx context.Context, url, destPath string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	f, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = io.Copy(f, resp.Body)
	return err
}

// OpenAI-compatible /v1/images/generations 请求和响应结构

type imageGenRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              int    `json:"n"`
	Size           string `json:"size"`
	ResponseFormat string `json:"response_format"`
}

type imageGenResponse struct {
	Created int64          `json:"created"`
	Data    []imageGenData `json:"data"`
}

type imageGenData struct {
	URL     string `json:"url,omitempty"`
	B64JSON string `json:"b64_json,omitempty"`
}
