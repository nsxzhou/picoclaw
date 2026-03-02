package channels

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sipeed/picoclaw/pkg/attachments"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/logger"
)

type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, msg bus.OutboundMessage) error
	IsRunning() bool
	IsAllowed(senderID string) bool
}

// dedupeCleanThreshold is the number of cached message IDs that triggers a
// lazy cleanup pass inside HandleMessage.
const dedupeCleanThreshold = 500

// dedupeExpiry is how long a message ID is kept in the dedup cache.
const dedupeExpiry = 10 * time.Minute

type BaseChannel struct {
	config       any
	bus          *bus.MessageBus
	running      bool
	name         string
	allowList    []string
	recentMsgIDs sync.Map // message_id -> time.Time
	dedupeCount  atomic.Int64
}

func NewBaseChannel(name string, config any, bus *bus.MessageBus, allowList []string) *BaseChannel {
	return &BaseChannel{
		config:    config,
		bus:       bus,
		name:      name,
		allowList: allowList,
		running:   false,
	}
}

func (c *BaseChannel) Name() string {
	return c.name
}

func (c *BaseChannel) IsRunning() bool {
	return c.running
}

func (c *BaseChannel) IsAllowed(senderID string) bool {
	if len(c.allowList) == 0 {
		return true
	}

	// Extract parts from compound senderID like "123456|username"
	idPart := senderID
	userPart := ""
	if idx := strings.Index(senderID, "|"); idx > 0 {
		idPart = senderID[:idx]
		userPart = senderID[idx+1:]
	}

	for _, allowed := range c.allowList {
		// Strip leading "@" from allowed value for username matching
		trimmed := strings.TrimPrefix(allowed, "@")
		allowedID := trimmed
		allowedUser := ""
		if idx := strings.Index(trimmed, "|"); idx > 0 {
			allowedID = trimmed[:idx]
			allowedUser = trimmed[idx+1:]
		}

		// Support either side using "id|username" compound form.
		// This keeps backward compatibility with legacy Telegram allowlist entries.
		if senderID == allowed ||
			idPart == allowed ||
			senderID == trimmed ||
			idPart == trimmed ||
			idPart == allowedID ||
			(allowedUser != "" && senderID == allowedUser) ||
			(userPart != "" && (userPart == allowed || userPart == trimmed || userPart == allowedUser)) {
			return true
		}
	}

	return false
}

func (c *BaseChannel) HandleMessage(senderID, chatID, content string, media []string, metadata map[string]string) {
	if !c.IsAllowed(senderID) {
		return
	}

	if c.shouldSkipDuplicate(metadata) {
		return
	}

	// Encode images eagerly while temp files still exist on disk.
	// Channel handlers typically defer os.Remove on media files, and
	// PublishInbound writes to a buffered channel — by the time the
	// consumer reads the message the files may already be gone.
	encodedImages := encodeImageMedia(media)
	parsedAttachments, attachmentErrors := attachments.Process(media)
	attachmentErrors = filterAttachmentErrorsByContent(content, attachmentErrors)

	msg := bus.InboundMessage{
		Channel:          c.name,
		SenderID:         senderID,
		ChatID:           chatID,
		Content:          content,
		Media:            media,
		EncodedImages:    encodedImages,
		Attachments:      parsedAttachments,
		AttachmentErrors: attachmentErrors,
		Metadata:         metadata,
	}

	c.bus.PublishInbound(msg)
}

// HandleMessageWithFileRefs is used by channels that support lazy file references
// (e.g. Feishu). When fileRefs is non-empty, the eager encode/parse pipeline is
// skipped — files will be resolved on demand in the provider layer.
func (c *BaseChannel) HandleMessageWithFileRefs(senderID, chatID, content string, media []string, fileRefs []bus.FileRef, metadata map[string]string) {
	if !c.IsAllowed(senderID) {
		return
	}

	if c.shouldSkipDuplicate(metadata) {
		return
	}

	msg := bus.InboundMessage{
		Channel:  c.name,
		SenderID: senderID,
		ChatID:   chatID,
		Content:  content,
		FileRefs: fileRefs,
		Metadata: metadata,
	}

	// If media paths are also provided (hybrid case), process them the old way.
	if len(media) > 0 {
		msg.Media = media
		msg.EncodedImages = encodeImageMedia(media)
		parsedAttachments, attachmentErrors := attachments.Process(media)
		msg.Attachments = parsedAttachments
		msg.AttachmentErrors = filterAttachmentErrorsByContent(content, attachmentErrors)
	}

	c.bus.PublishInbound(msg)
}

func filterAttachmentErrorsByContent(content string, errs []bus.AttachmentError) []bus.AttachmentError {
	if len(errs) == 0 {
		return nil
	}

	lowered := strings.ToLower(content)
	hasAudioTranscription := strings.Contains(lowered, "audio transcription:") ||
		strings.Contains(lowered, "voice transcription:")
	if !hasAudioTranscription {
		return errs
	}

	filtered := make([]bus.AttachmentError, 0, len(errs))
	for _, errItem := range errs {
		if errItem.Code == "audio_not_supported" {
			continue
		}
		filtered = append(filtered, errItem)
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

func (c *BaseChannel) setRunning(running bool) {
	c.running = running
}

// shouldSkipDuplicate deduplicates inbound messages by message_id.
// 返回 true 表示该消息应被跳过（重复消息）。
func (c *BaseChannel) shouldSkipDuplicate(metadata map[string]string) bool {
	if len(metadata) == 0 {
		return false
	}

	msgID := metadata["message_id"]
	if msgID == "" {
		return false
	}

	if _, loaded := c.recentMsgIDs.LoadOrStore(msgID, time.Now()); loaded {
		logger.DebugCF(c.name, "Duplicate message skipped", map[string]any{"message_id": msgID})
		return true
	}

	if c.dedupeCount.Add(1) >= int64(dedupeCleanThreshold) {
		c.cleanExpiredDedupeEntries()
	}
	return false
}

// cleanExpiredDedupeEntries removes message IDs older than dedupeExpiry and
// resets the approximate counter.
func (c *BaseChannel) cleanExpiredDedupeEntries() {
	cutoff := time.Now().Add(-dedupeExpiry)
	c.recentMsgIDs.Range(func(key, value any) bool {
		if ts, ok := value.(time.Time); ok && ts.Before(cutoff) {
			c.recentMsgIDs.Delete(key)
		}
		return true
	})
	c.dedupeCount.Store(0)
}
