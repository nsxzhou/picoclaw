package channels

import (
	"encoding/base64"
	"net/http"
	"os"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
)

// maxImageSize is the upper bound (20 MB) for a single image file.
// Larger files are silently skipped to protect memory and API limits.
const maxImageSize = 20 * 1024 * 1024

// supportedImageTypes lists MIME types accepted by vision-capable LLMs.
var supportedImageTypes = map[string]bool{
	"image/jpeg": true,
	"image/png":  true,
	"image/gif":  true,
	"image/webp": true,
}

// encodeImageMedia reads image files from disk, detects their MIME type,
// and returns base64-encoded representations. Non-image files, oversized
// files, and unreadable paths are silently skipped.
//
// This MUST be called while the temp files still exist (before defer cleanup).
func encodeImageMedia(mediaPaths []string) []bus.EncodedImage {
	if len(mediaPaths) == 0 {
		return nil
	}

	var images []bus.EncodedImage
	for _, path := range mediaPaths {
		img := encodeOneImage(path)
		if img != nil {
			images = append(images, *img)
		}
	}
	return images
}

// encodeOneImage encodes a single image file. Returns nil if the file
// is not a supported image, exceeds size limit, or cannot be read.
func encodeOneImage(path string) *bus.EncodedImage {
	info, err := os.Stat(path)
	if err != nil {
		logger.DebugCF("media", "Skipping unreadable media file", map[string]any{
			"path":  path,
			"error": err.Error(),
		})
		return nil
	}

	if info.Size() > maxImageSize {
		logger.WarnCF("media", "Skipping oversized media file", map[string]any{
			"path":     path,
			"size_mb":  info.Size() / (1024 * 1024),
			"limit_mb": maxImageSize / (1024 * 1024),
		})
		return nil
	}

	mediaType := detectImageType(path)
	if mediaType == "" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		logger.WarnCF("media", "Failed to read media file", map[string]any{
			"path":  path,
			"error": err.Error(),
		})
		return nil
	}

	encoded := base64.StdEncoding.EncodeToString(data)
	logger.InfoCF("media", "Encoded image for LLM", map[string]any{
		"path":       path,
		"media_type": mediaType,
		"size_bytes": len(data),
	})

	return &bus.EncodedImage{
		MediaType: mediaType,
		Data:      encoded,
	}
}

// detectImageType sniffs the file content to determine its MIME type.
// Returns empty string for non-image or unsupported types.
func detectImageType(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	// http.DetectContentType needs at most 512 bytes
	buf := make([]byte, 512)
	n, err := f.Read(buf)
	if err != nil || n == 0 {
		return ""
	}

	// http.DetectContentType doesn't recognize WebP; check manually.
	// WebP files start with "RIFF" (4 bytes) + size (4 bytes) + "WEBP".
	if n >= 12 && string(buf[:4]) == "RIFF" && string(buf[8:12]) == "WEBP" {
		return "image/webp"
	}

	contentType := http.DetectContentType(buf[:n])
	// DetectContentType may return params like "image/jpeg; charset=..."
	if idx := strings.Index(contentType, ";"); idx > 0 {
		contentType = strings.TrimSpace(contentType[:idx])
	}

	if supportedImageTypes[contentType] {
		return contentType
	}
	return ""
}
