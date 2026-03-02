//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package channels

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
)

const maxFeishuResolveBytes = int64(20 * 1024 * 1024)

// FeishuFileRefResolver resolves Feishu file references by downloading
// from the Feishu MessageResource API into memory.
type FeishuFileRefResolver struct {
	client *lark.Client
}

func NewFeishuFileRefResolver(client *lark.Client) *FeishuFileRefResolver {
	return &FeishuFileRefResolver{client: client}
}

func (r *FeishuFileRefResolver) Resolve(ctx context.Context, ref *bus.FileRef) (string, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if ref.Source != bus.FileRefSourceFeishu {
		return "", "", fmt.Errorf("unsupported file ref source: %s", ref.Source)
	}
	if ref.FeishuMessageID == "" || ref.FeishuFileKey == "" {
		return "", "", fmt.Errorf("missing feishu message_id or file_key")
	}
	resType := ref.FeishuResType
	if resType == "" {
		if ref.Kind == bus.AttachmentKindImage {
			resType = "image"
		} else {
			resType = "file"
		}
	}

	downloadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := r.client.Im.MessageResource.Get(downloadCtx,
		larkim.NewGetMessageResourceReqBuilder().
			MessageId(ref.FeishuMessageID).
			FileKey(ref.FeishuFileKey).
			Type(resType).
			Build())
	if err != nil {
		return "", "", fmt.Errorf("feishu resource download failed: %w", err)
	}
	if !resp.Success() {
		return "", "", fmt.Errorf("feishu resource API error: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.File == nil {
		return "", "", fmt.Errorf("feishu resource API returned empty file stream")
	}

	data, err := readAllWithLimit(resp.File, maxFeishuResolveBytes)
	if err != nil {
		return "", "", fmt.Errorf("failed to read feishu resource: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	mediaType := detectFeishuMediaType(data, ref.MediaType)

	logger.DebugCF("feishu", "FileRef resolved", map[string]any{
		"message_id": ref.FeishuMessageID,
		"file_key":   ref.FeishuFileKey,
		"res_type":   resType,
		"size_bytes": len(data),
	})

	return mediaType, encoded, nil
}

func readAllWithLimit(reader io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(reader, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("file too large to resolve (>%d bytes)", maxBytes)
	}
	return data, nil
}

func detectFeishuMediaType(data []byte, fallback string) string {
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}

	sniffSize := len(data)
	if sniffSize > 512 {
		sniffSize = 512
	}
	if sniffSize > 0 {
		contentType := http.DetectContentType(data[:sniffSize])
		if idx := strings.Index(contentType, ";"); idx > 0 {
			contentType = strings.TrimSpace(contentType[:idx])
		}
		if fallback != "" {
			// docx/xlsx/pptx are zip containers; keep extension-based MIME when available.
			if contentType == "application/zip" && strings.Contains(fallback, "openxmlformats") {
				return fallback
			}
			// Some binary formats may be mis-detected as text/plain with short payload.
			if contentType == "text/plain" && fallback != "text/plain" {
				return fallback
			}
		}
		if contentType != "" && contentType != "application/octet-stream" {
			return contentType
		}
	}

	if fallback != "" {
		return fallback
	}
	return "application/octet-stream"
}
