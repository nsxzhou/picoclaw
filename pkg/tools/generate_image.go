package tools

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/media"
)

// GenerateImageTool 通过 AI 生图 API 生成图片。
// 支持两种 API 格式：
//   - images_generations: 标准 OpenAI /v1/images/generations 接口
//   - chat_completions:   通过 /v1/chat/completions 生图，图片以 markdown 链接返回
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

	// 根据 api_type 选择调用方式
	apiType := strings.TrimSpace(t.imgCfg.APIType)
	if apiType == "chat_completions" {
		return t.executeViaChatCompletions(ctx, prompt)
	}
	// 默认使用 images_generations
	return t.executeViaImagesGenerations(ctx, prompt, size)
}

// executeViaImagesGenerations 使用标准 OpenAI /v1/images/generations 接口生图。
func (t *GenerateImageTool) executeViaImagesGenerations(ctx context.Context, prompt, size string) *ToolResult {
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

	var imgResp imageGenResponse
	if err := json.Unmarshal(respBody, &imgResp); err != nil {
		return ErrorResult(fmt.Sprintf("failed to parse response: %v", err))
	}

	if len(imgResp.Data) == 0 {
		return ErrorResult("API returned no images")
	}

	imgData := imgResp.Data[0]

	// 确定图片 URL 或 base64 数据
	if imgData.B64JSON != "" {
		decoded, err := base64.StdEncoding.DecodeString(imgData.B64JSON)
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to decode base64 image: %v", err))
		}
		return t.saveAndReturn(ctx, decoded, prompt)
	}

	if imgData.URL != "" {
		return t.downloadAndReturn(ctx, imgData.URL, prompt)
	}

	return ErrorResult("API response contains no image data (neither b64_json nor url)")
}

// executeViaChatCompletions 使用 /v1/chat/completions 接口生图。
// 该格式通过 SSE 流返回，图片以 markdown 链接（![...](url)）嵌入在 content 中。
func (t *GenerateImageTool) executeViaChatCompletions(ctx context.Context, prompt string) *ToolResult {
	reqBody := chatCompletionsRequest{
		Model: t.imgCfg.Model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
		Stream: true,
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to marshal request: %v", err))
	}

	apiURL := strings.TrimRight(t.imgCfg.APIBase, "/") + "/v1/chat/completions"

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

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return ErrorResult(fmt.Sprintf("API returned status %d: %s", resp.StatusCode, string(respBody)))
	}

	// 解析 SSE 流，提取完整 content
	content := t.parseSSEContent(resp.Body)
	if content == "" {
		return ErrorResult("API returned no content in chat completions response")
	}

	// 从 content 中提取 markdown 图片 URL
	imageURL := extractMarkdownImageURL(content)
	if imageURL == "" {
		// 没有图片链接，可能 content 本身就是有用的文本
		return SilentResult(fmt.Sprintf("Image generation response: %s", content))
	}

	return t.downloadAndReturn(ctx, imageURL, prompt)
}

// parseSSEContent 从 SSE 流中提取所有 content 片段并拼接。
func (t *GenerateImageTool) parseSSEContent(body io.Reader) string {
	var contentBuilder strings.Builder
	scanner := bufio.NewScanner(body)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")
		if data == "[DONE]" {
			break
		}

		var chunk chatCompletionsChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}

		if len(chunk.Choices) > 0 {
			contentBuilder.WriteString(chunk.Choices[0].Delta.Content)
		}
	}

	return contentBuilder.String()
}

// mdImageRegex 匹配 markdown 图片语法 ![alt](url)
var mdImageRegex = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)

// extractMarkdownImageURL 从文本中提取第一个 markdown 图片的 URL。
func extractMarkdownImageURL(text string) string {
	matches := mdImageRegex.FindStringSubmatch(text)
	if len(matches) >= 3 {
		return matches[2]
	}
	return ""
}

// saveAndReturn 保存图片字节到文件并返回结果。
func (t *GenerateImageTool) saveAndReturn(ctx context.Context, imgBytes []byte, prompt string) *ToolResult {
	imgDir := filepath.Join(t.workspace, "generated_images")
	os.MkdirAll(imgDir, 0o755)

	filename := fmt.Sprintf("img_%d.png", time.Now().UnixMilli())
	imgPath := filepath.Join(imgDir, filename)

	if err := os.WriteFile(imgPath, imgBytes, 0o644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to save image: %v", err))
	}

	return t.buildResult(ctx, imgPath, filename, prompt)
}

// downloadAndReturn 从 URL 下载图片并返回结果。
func (t *GenerateImageTool) downloadAndReturn(ctx context.Context, imageURL, prompt string) *ToolResult {
	imgDir := filepath.Join(t.workspace, "generated_images")
	os.MkdirAll(imgDir, 0o755)

	// 根据 URL 推断扩展名，默认 .png
	ext := ".png"
	if strings.Contains(imageURL, ".jpg") || strings.Contains(imageURL, ".jpeg") {
		ext = ".jpg"
	} else if strings.Contains(imageURL, ".webp") {
		ext = ".webp"
	}
	filename := fmt.Sprintf("img_%d%s", time.Now().UnixMilli(), ext)
	imgPath := filepath.Join(imgDir, filename)

	if err := t.downloadImage(ctx, imageURL, imgPath); err != nil {
		return ErrorResult(fmt.Sprintf("failed to download image: %v", err))
	}

	return t.buildResult(ctx, imgPath, filename, prompt)
}

// buildResult 构建最终返回结果（通过 MediaStore 发送或返回文件路径）。
func (t *GenerateImageTool) buildResult(ctx context.Context, imgPath, filename, prompt string) *ToolResult {
	// 检测实际文件类型以设置正确的 Content-Type
	contentType := "image/png"
	if strings.HasSuffix(filename, ".jpg") || strings.HasSuffix(filename, ".jpeg") {
		contentType = "image/jpeg"
	} else if strings.HasSuffix(filename, ".webp") {
		contentType = "image/webp"
	}

	channel := ToolChannel(ctx)
	chatID := ToolChatID(ctx)
	if t.mediaStore != nil && channel != "" && chatID != "" {
		scope := fmt.Sprintf("tool:generate_image:%s:%s", channel, chatID)
		ref, err := t.mediaStore.Store(imgPath, media.MediaMeta{
			Filename:    filename,
			ContentType: contentType,
			Source:      "tool:generate_image",
		}, scope)
		if err != nil {
			return SilentResult(fmt.Sprintf("Image generated and saved to: %s (media store error: %v)", imgPath, err))
		}
		return MediaResult(
			fmt.Sprintf("Image generated successfully from prompt: %q, saved to %s", prompt, imgPath),
			[]string{ref},
		)
	}

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

// ===== 请求/响应结构 =====

// images_generations 格式
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

// chat_completions 格式
type chatCompletionsRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatCompletionsChunk struct {
	Choices []chatCompletionsChoice `json:"choices"`
}

type chatCompletionsChoice struct {
	Delta struct {
		Content string `json:"content"`
	} `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}
