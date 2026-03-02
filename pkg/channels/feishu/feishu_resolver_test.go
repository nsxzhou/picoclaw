//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package feishu

import (
	"bytes"
	"strings"
	"testing"
)

func TestReadAllWithLimit(t *testing.T) {
	data := []byte(strings.Repeat("a", 1024))

	got, err := readAllWithLimit(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("readAllWithLimit() unexpected error: %v", err)
	}
	if len(got) != len(data) {
		t.Fatalf("len(got) = %d, want %d", len(got), len(data))
	}

	if _, err := readAllWithLimit(bytes.NewReader(data), 128); err == nil {
		t.Fatal("expected size limit error, got nil")
	}
}

func TestDetectFeishuMediaType(t *testing.T) {
	tests := []struct {
		name     string
		data     []byte
		fallback string
		want     string
	}{
		{
			name: "webp magic header",
			data: func() []byte {
				payload := make([]byte, 100)
				buf := append([]byte("RIFF"), byte(len(payload)+4), 0, 0, 0)
				buf = append(buf, []byte("WEBP")...)
				buf = append(buf, payload...)
				return buf
			}(),
			fallback: "application/octet-stream",
			want:     "image/webp",
		},
		{
			name:     "png",
			data:     append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 32)...),
			fallback: "application/octet-stream",
			want:     "image/png",
		},
		{
			name:     "fallback used for unknown payload",
			data:     []byte{0x00, 0x01, 0x02, 0x03},
			fallback: "application/pdf",
			want:     "application/pdf",
		},
		{
			name:     "openxml fallback overrides zip sniff result",
			data:     append([]byte{'P', 'K', 0x03, 0x04}, make([]byte, 32)...),
			fallback: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
			want:     "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		},
		{
			name:     "default fallback",
			data:     []byte{0x00, 0x01, 0x02, 0x03},
			fallback: "",
			want:     "application/octet-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectFeishuMediaType(tt.data, tt.fallback)
			if got != tt.want {
				t.Fatalf("detectFeishuMediaType() = %q, want %q", got, tt.want)
			}
		})
	}
}
