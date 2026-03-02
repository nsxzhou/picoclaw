package channels

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectImageType(t *testing.T) {
	tests := []struct {
		name     string
		header   []byte // file magic bytes
		expected string
	}{
		{"jpeg", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F'}, "image/jpeg"},
		{"png", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"gif", []byte("GIF89a" + strings.Repeat("\x00", 100)), "image/gif"},
		// http.DetectContentType requires a valid RIFF/WEBP header with correct size field
		{"webp", func() []byte {
			payload := make([]byte, 100)
			data := append([]byte("RIFF"), byte(len(payload)+4), 0, 0, 0)
			data = append(data, []byte("WEBP")...)
			data = append(data, payload...)
			return data
		}(), "image/webp"},
		{"text_file", []byte("hello world, this is plain text"), ""},
		{"pdf", []byte("%PDF-1.4 some content here"), ""},
		{"empty", []byte{}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := filepath.Join(t.TempDir(), "testfile")
			if err := os.WriteFile(tmp, tt.header, 0644); err != nil {
				t.Fatal(err)
			}
			got := detectImageType(tmp)
			if got != tt.expected {
				t.Errorf("detectImageType() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestDetectImageType_NonexistentFile(t *testing.T) {
	got := detectImageType("/nonexistent/path/image.jpg")
	if got != "" {
		t.Errorf("expected empty string for nonexistent file, got %q", got)
	}
}

func TestEncodeImageMedia_JPEG(t *testing.T) {
	// Minimal valid JPEG: SOI + APP0 marker + padding
	jpegData := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 100)...)
	tmp := filepath.Join(t.TempDir(), "test.jpg")
	if err := os.WriteFile(tmp, jpegData, 0644); err != nil {
		t.Fatal(err)
	}

	images := encodeImageMedia([]string{tmp})
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MediaType != "image/jpeg" {
		t.Errorf("media_type = %q, want %q", images[0].MediaType, "image/jpeg")
	}

	decoded, err := base64.StdEncoding.DecodeString(images[0].Data)
	if err != nil {
		t.Fatalf("invalid base64: %v", err)
	}
	if len(decoded) != len(jpegData) {
		t.Errorf("decoded length = %d, want %d", len(decoded), len(jpegData))
	}
}

func TestEncodeImageMedia_PNG(t *testing.T) {
	pngData := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 50)...)
	tmp := filepath.Join(t.TempDir(), "test.png")
	if err := os.WriteFile(tmp, pngData, 0644); err != nil {
		t.Fatal(err)
	}

	images := encodeImageMedia([]string{tmp})
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].MediaType != "image/png" {
		t.Errorf("media_type = %q, want %q", images[0].MediaType, "image/png")
	}
}

func TestEncodeImageMedia_SkipsNonImage(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "readme.txt")
	if err := os.WriteFile(tmp, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	images := encodeImageMedia([]string{tmp})
	if len(images) != 0 {
		t.Errorf("expected 0 images for text file, got %d", len(images))
	}
}

func TestEncodeImageMedia_SkipsNonexistent(t *testing.T) {
	images := encodeImageMedia([]string{"/nonexistent/file.jpg"})
	if len(images) != 0 {
		t.Errorf("expected 0 images for nonexistent file, got %d", len(images))
	}
}

func TestEncodeImageMedia_EmptyInput(t *testing.T) {
	if images := encodeImageMedia(nil); images != nil {
		t.Errorf("expected nil for nil input, got %v", images)
	}
	if images := encodeImageMedia([]string{}); images != nil {
		t.Errorf("expected nil for empty input, got %v", images)
	}
}

func TestEncodeImageMedia_MixedFiles(t *testing.T) {
	dir := t.TempDir()

	jpegFile := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(jpegFile, append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, make([]byte, 50)...), 0644); err != nil {
		t.Fatal(err)
	}

	textFile := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textFile, []byte("some notes"), 0644); err != nil {
		t.Fatal(err)
	}

	pngFile := filepath.Join(dir, "icon.png")
	pngHeader := []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}
	pngData := append(pngHeader, make([]byte, 50)...)
	if err := os.WriteFile(pngFile, pngData, 0644); err != nil {
		t.Fatal(err)
	}

	images := encodeImageMedia([]string{jpegFile, textFile, pngFile, "/nonexistent"})
	if len(images) != 2 {
		t.Fatalf("expected 2 images (jpeg+png), got %d", len(images))
	}
	if images[0].MediaType != "image/jpeg" {
		t.Errorf("first image type = %q, want image/jpeg", images[0].MediaType)
	}
	if images[1].MediaType != "image/png" {
		t.Errorf("second image type = %q, want image/png", images[1].MediaType)
	}
}
