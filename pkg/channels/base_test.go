package channels

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
)

func TestBaseChannelIsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowList []string
		senderID  string
		want      bool
	}{
		{
			name:      "empty allowlist allows all",
			allowList: nil,
			senderID:  "anyone",
			want:      true,
		},
		{
			name:      "compound sender matches numeric allowlist",
			allowList: []string{"123456"},
			senderID:  "123456|alice",
			want:      true,
		},
		{
			name:      "compound sender matches username allowlist",
			allowList: []string{"@alice"},
			senderID:  "123456|alice",
			want:      true,
		},
		{
			name:      "numeric sender matches legacy compound allowlist",
			allowList: []string{"123456|alice"},
			senderID:  "123456",
			want:      true,
		},
		{
			name:      "non matching sender is denied",
			allowList: []string{"123456"},
			senderID:  "654321|bob",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewBaseChannel("test", nil, nil, tt.allowList)
			if got := ch.IsAllowed(tt.senderID); got != tt.want {
				t.Fatalf("IsAllowed(%q) = %v, want %v", tt.senderID, got, tt.want)
			}
		})
	}
}

func TestFilterAttachmentErrorsByContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		errs    []bus.AttachmentError
		wantLen int
		wantHas string
	}{
		{
			name:    "keep audio not supported without transcription",
			content: "[audio: sample.wav]",
			errs: []bus.AttachmentError{
				{Code: "audio_not_supported", Name: "sample.wav"},
			},
			wantLen: 1,
			wantHas: "audio_not_supported",
		},
		{
			name:    "drop audio not supported with transcription",
			content: "[voice transcription: hello world]",
			errs: []bus.AttachmentError{
				{Code: "audio_not_supported", Name: "sample.wav"},
			},
			wantLen: 0,
		},
		{
			name:    "keep non-audio errors with transcription",
			content: "[audio transcription: hello world]",
			errs: []bus.AttachmentError{
				{Code: "audio_not_supported", Name: "sample.wav"},
				{Code: "file_too_large", Name: "big.pdf"},
			},
			wantLen: 1,
			wantHas: "file_too_large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterAttachmentErrorsByContent(tt.content, tt.errs)
			if len(got) != tt.wantLen {
				t.Fatalf("len(got) = %d, want %d", len(got), tt.wantLen)
			}
			if tt.wantHas != "" {
				found := false
				for _, errItem := range got {
					if errItem.Code == tt.wantHas {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected error code %q in result, got %+v", tt.wantHas, got)
				}
			}
		})
	}
}

func drainInbound(mb *bus.MessageBus, maxCount int) int {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	received := 0
	for i := 0; i < maxCount; i++ {
		if _, ok := mb.ConsumeInbound(ctx); ok {
			received++
		} else {
			break
		}
	}
	return received
}

func TestHandleMessageDeduplication(t *testing.T) {
	t.Run("duplicate message_id is skipped", func(t *testing.T) {
		mb := bus.NewMessageBus()
		defer mb.Close()
		ch := NewBaseChannel("test", nil, mb, nil)

		meta := map[string]string{"message_id": "msg_001"}
		ch.HandleMessage("user1", "chat1", "hello", nil, meta)
		ch.HandleMessage("user1", "chat1", "hello", nil, meta)

		got := drainInbound(mb, 10)
		if got != 1 {
			t.Fatalf("expected 1 inbound message, got %d", got)
		}
	})

	t.Run("different message_ids are both published", func(t *testing.T) {
		mb := bus.NewMessageBus()
		defer mb.Close()
		ch := NewBaseChannel("test", nil, mb, nil)

		ch.HandleMessage("user1", "chat1", "hello", nil, map[string]string{"message_id": "msg_001"})
		ch.HandleMessage("user1", "chat1", "world", nil, map[string]string{"message_id": "msg_002"})

		got := drainInbound(mb, 10)
		if got != 2 {
			t.Fatalf("expected 2 inbound messages, got %d", got)
		}
	})

	t.Run("no message_id skips dedup", func(t *testing.T) {
		mb := bus.NewMessageBus()
		defer mb.Close()
		ch := NewBaseChannel("test", nil, mb, nil)

		ch.HandleMessage("user1", "chat1", "hello", nil, map[string]string{})
		ch.HandleMessage("user1", "chat1", "hello", nil, map[string]string{})

		got := drainInbound(mb, 10)
		if got != 2 {
			t.Fatalf("expected 2 inbound messages (no dedup without message_id), got %d", got)
		}
	})

	t.Run("nil metadata skips dedup", func(t *testing.T) {
		mb := bus.NewMessageBus()
		defer mb.Close()
		ch := NewBaseChannel("test", nil, mb, nil)

		ch.HandleMessage("user1", "chat1", "hello", nil, nil)
		ch.HandleMessage("user1", "chat1", "hello", nil, nil)

		got := drainInbound(mb, 10)
		if got != 2 {
			t.Fatalf("expected 2 inbound messages (nil metadata), got %d", got)
		}
	})

	t.Run("concurrent duplicate message_id is skipped", func(t *testing.T) {
		mb := bus.NewMessageBus()
		defer mb.Close()
		ch := NewBaseChannel("test", nil, mb, nil)

		meta := map[string]string{"message_id": "msg_concurrent"}
		const workers = 64

		var wg sync.WaitGroup
		wg.Add(workers)
		for i := 0; i < workers; i++ {
			go func() {
				defer wg.Done()
				ch.HandleMessage("user1", "chat1", "hello", nil, meta)
			}()
		}
		wg.Wait()

		got := drainInbound(mb, 10)
		if got != 1 {
			t.Fatalf("expected 1 inbound message under concurrency, got %d", got)
		}
	})
}
