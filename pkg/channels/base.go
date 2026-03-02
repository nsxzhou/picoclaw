package channels

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sipeed/picoclaw/pkg/attachments"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/media"
)

var (
	uniqueIDCounter uint64
	uniqueIDPrefix  string
)

// dedupeCleanThreshold is the number of cached message IDs that triggers a
// lazy cleanup pass inside HandleMessage.
const dedupeCleanThreshold = 500

// dedupeExpiry is how long a message ID is kept in the dedup cache.
const dedupeExpiry = 10 * time.Minute

func init() {
	// One-time read from crypto/rand for a unique prefix (single syscall).
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// fallback to time-based prefix
		binary.BigEndian.PutUint64(b[:], uint64(time.Now().UnixNano()))
	}
	uniqueIDPrefix = hex.EncodeToString(b[:])
}

// uniqueID generates a process-unique ID using a random prefix and an atomic counter.
// This ID is intended for internal correlation (e.g. media scope keys) and is NOT
// cryptographically secure — it must not be used in contexts where unpredictability matters.
func uniqueID() string {
	n := atomic.AddUint64(&uniqueIDCounter, 1)
	return uniqueIDPrefix + strconv.FormatUint(n, 16)
}

type Channel interface {
	Name() string
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	Send(ctx context.Context, msg bus.OutboundMessage) error
	IsRunning() bool
	IsAllowed(senderID string) bool
	IsAllowedSender(sender bus.SenderInfo) bool
	ReasoningChannelID() string
}

// BaseChannelOption is a functional option for configuring a BaseChannel.
type BaseChannelOption func(*BaseChannel)

// WithMaxMessageLength sets the maximum message length (in runes) for a channel.
// Messages exceeding this limit will be automatically split by the Manager.
// A value of 0 means no limit.
func WithMaxMessageLength(n int) BaseChannelOption {
	return func(c *BaseChannel) { c.maxMessageLength = n }
}

// WithGroupTrigger sets the group trigger configuration for a channel.
func WithGroupTrigger(gt config.GroupTriggerConfig) BaseChannelOption {
	return func(c *BaseChannel) { c.groupTrigger = gt }
}

// WithReasoningChannelID sets the reasoning channel ID where thoughts should be sent.
func WithReasoningChannelID(id string) BaseChannelOption {
	return func(c *BaseChannel) { c.reasoningChannelID = id }
}

// MessageLengthProvider is an opt-in interface that channels implement
// to advertise their maximum message length. The Manager uses this via
// type assertion to decide whether to split outbound messages.
type MessageLengthProvider interface {
	MaxMessageLength() int
}

type BaseChannel struct {
	config              any
	bus                 *bus.MessageBus
	running             atomic.Bool
	name                string
	allowList           []string
	maxMessageLength    int
	groupTrigger        config.GroupTriggerConfig
	mediaStore          media.MediaStore
	placeholderRecorder PlaceholderRecorder
	owner               Channel // the concrete channel that embeds this BaseChannel
	reasoningChannelID  string
	recentMsgIDs        sync.Map // message_id -> time.Time
	dedupeCount         atomic.Int64
}

func NewBaseChannel(
	name string,
	config any,
	bus *bus.MessageBus,
	allowList []string,
	opts ...BaseChannelOption,
) *BaseChannel {
	bc := &BaseChannel{
		config:    config,
		bus:       bus,
		name:      name,
		allowList: allowList,
	}
	for _, opt := range opts {
		opt(bc)
	}
	return bc
}

// MaxMessageLength returns the maximum message length (in runes) for this channel.
// A value of 0 means no limit.
func (c *BaseChannel) MaxMessageLength() int {
	return c.maxMessageLength
}

// ShouldRespondInGroup determines whether the bot should respond in a group chat.
// Each channel is responsible for:
//  1. Detecting isMentioned (platform-specific)
//  2. Stripping bot mention from content (platform-specific)
//  3. Calling this method to get the group response decision
//
// Logic:
//   - If isMentioned → always respond
//   - If mention_only configured and not mentioned → ignore
//   - If prefixes configured → respond if content starts with any prefix (strip it)
//   - If prefixes configured but no match and not mentioned → ignore
//   - Otherwise (no group_trigger configured) → respond to all (permissive default)
func (c *BaseChannel) ShouldRespondInGroup(isMentioned bool, content string) (bool, string) {
	gt := c.groupTrigger

	// Mentioned → always respond
	if isMentioned {
		return true, strings.TrimSpace(content)
	}

	// mention_only → require mention
	if gt.MentionOnly {
		return false, content
	}

	// Prefix matching
	if len(gt.Prefixes) > 0 {
		for _, prefix := range gt.Prefixes {
			if prefix != "" && strings.HasPrefix(content, prefix) {
				return true, strings.TrimSpace(strings.TrimPrefix(content, prefix))
			}
		}
		// Prefixes configured but none matched and not mentioned → ignore
		return false, content
	}

	// No group_trigger configured → permissive (respond to all)
	return true, strings.TrimSpace(content)
}

func (c *BaseChannel) Name() string {
	return c.name
}

func (c *BaseChannel) ReasoningChannelID() string {
	return c.reasoningChannelID
}

func (c *BaseChannel) IsRunning() bool {
	return c.running.Load()
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

// IsAllowedSender checks whether a structured SenderInfo is permitted by the allow-list.
// It delegates to identity.MatchAllowed for each entry, providing unified matching
// across all legacy formats and the new canonical "platform:id" format.
func (c *BaseChannel) IsAllowedSender(sender bus.SenderInfo) bool {
	if len(c.allowList) == 0 {
		return true
	}

	for _, allowed := range c.allowList {
		if identity.MatchAllowed(sender, allowed) {
			return true
		}
	}

	return false
}

func (c *BaseChannel) HandleMessage(
	ctx context.Context,
	peer bus.Peer,
	messageID, senderID, chatID, content string,
	media []string,
	metadata map[string]string,
	senderOpts ...bus.SenderInfo,
) {
	// Use SenderInfo-based allow check when available, else fall back to string
	var sender bus.SenderInfo
	if len(senderOpts) > 0 {
		sender = senderOpts[0]
	}
	if sender.CanonicalID != "" || sender.PlatformID != "" {
		if !c.IsAllowedSender(sender) {
			return
		}
	} else {
		if !c.IsAllowed(senderID) {
			return
		}
	}

	if c.shouldSkipDuplicate(messageID, metadata) {
		return
	}

	// Set SenderID to canonical if available, otherwise keep the raw senderID
	resolvedSenderID := senderID
	if sender.CanonicalID != "" {
		resolvedSenderID = sender.CanonicalID
	}

	scope := BuildMediaScope(c.name, chatID, messageID)

	processableMediaPaths := c.resolveProcessableMediaPaths(media)
	encodedImages := encodeImageMedia(processableMediaPaths)
	parsedAttachments, attachmentErrors := attachments.Process(processableMediaPaths)
	attachmentErrors = filterAttachmentErrorsByContent(content, attachmentErrors)

	msg := bus.InboundMessage{
		Channel:          c.name,
		SenderID:         resolvedSenderID,
		Sender:           sender,
		ChatID:           chatID,
		Content:          content,
		Media:            media,
		EncodedImages:    encodedImages,
		Attachments:      parsedAttachments,
		AttachmentErrors: attachmentErrors,
		Peer:             peer,
		MessageID:        messageID,
		MediaScope:       scope,
		Metadata:         metadata,
	}

	// Auto-trigger typing indicator, message reaction, and placeholder before publishing.
	// Each capability is independent — all three may fire for the same message.
	if c.owner != nil && c.placeholderRecorder != nil {
		// Typing — independent pipeline
		if tc, ok := c.owner.(TypingCapable); ok {
			if stop, err := tc.StartTyping(ctx, chatID); err == nil {
				c.placeholderRecorder.RecordTypingStop(c.name, chatID, stop)
			}
		}
		// Reaction — independent pipeline
		if rc, ok := c.owner.(ReactionCapable); ok && messageID != "" {
			if undo, err := rc.ReactToMessage(ctx, chatID, messageID); err == nil {
				c.placeholderRecorder.RecordReactionUndo(c.name, chatID, undo)
			}
		}
		// Placeholder — independent pipeline
		if pc, ok := c.owner.(PlaceholderCapable); ok {
			if phID, err := pc.SendPlaceholder(ctx, chatID); err == nil && phID != "" {
				c.placeholderRecorder.RecordPlaceholder(c.name, chatID, phID)
			}
		}
	}

	if err := c.bus.PublishInbound(ctx, msg); err != nil {
		logger.ErrorCF("channels", "Failed to publish inbound message", map[string]any{
			"channel": c.name,
			"chat_id": chatID,
			"error":   err.Error(),
		})
	}
}

// HandleMessageWithFileRefs is used by channels that support lazy file references
// (e.g. Feishu). When fileRefs is non-empty, files will be resolved on demand in
// the provider layer, while legacy media payloads continue to be processed eagerly.
func (c *BaseChannel) HandleMessageWithFileRefs(
	ctx context.Context,
	peer bus.Peer,
	messageID, senderID, chatID, content string,
	media []string,
	fileRefs []bus.FileRef,
	metadata map[string]string,
	senderOpts ...bus.SenderInfo,
) {
	// Use SenderInfo-based allow check when available, else fall back to string
	var sender bus.SenderInfo
	if len(senderOpts) > 0 {
		sender = senderOpts[0]
	}
	if sender.CanonicalID != "" || sender.PlatformID != "" {
		if !c.IsAllowedSender(sender) {
			return
		}
	} else {
		if !c.IsAllowed(senderID) {
			return
		}
	}

	if c.shouldSkipDuplicate(messageID, metadata) {
		return
	}

	resolvedSenderID := senderID
	if sender.CanonicalID != "" {
		resolvedSenderID = sender.CanonicalID
	}

	scope := BuildMediaScope(c.name, chatID, messageID)

	processableMediaPaths := c.resolveProcessableMediaPaths(media)
	encodedImages := encodeImageMedia(processableMediaPaths)
	parsedAttachments, attachmentErrors := attachments.Process(processableMediaPaths)
	attachmentErrors = filterAttachmentErrorsByContent(content, attachmentErrors)

	msg := bus.InboundMessage{
		Channel:          c.name,
		SenderID:         resolvedSenderID,
		Sender:           sender,
		ChatID:           chatID,
		Content:          content,
		Media:            media,
		EncodedImages:    encodedImages,
		Attachments:      parsedAttachments,
		AttachmentErrors: attachmentErrors,
		FileRefs:         fileRefs,
		Peer:             peer,
		MessageID:        messageID,
		MediaScope:       scope,
		Metadata:         metadata,
	}

	if err := c.bus.PublishInbound(ctx, msg); err != nil {
		logger.ErrorCF("channels", "Failed to publish inbound message", map[string]any{
			"channel": c.name,
			"chat_id": chatID,
			"error":   err.Error(),
		})
	}
}

func (c *BaseChannel) resolveProcessableMediaPaths(media []string) []string {
	if len(media) == 0 {
		return nil
	}

	out := make([]string, 0, len(media))
	for _, item := range media {
		path := strings.TrimSpace(item)
		if path == "" {
			continue
		}

		if strings.HasPrefix(path, "media://") {
			if c.mediaStore == nil {
				continue
			}
			resolved, err := c.mediaStore.Resolve(path)
			if err != nil {
				logger.DebugCF("channels", "Skip unresolved media ref", map[string]any{
					"ref":   path,
					"error": err.Error(),
				})
				continue
			}
			path = resolved
		}

		if _, err := os.Stat(path); err != nil {
			continue
		}

		out = append(out, path)
	}

	if len(out) == 0 {
		return nil
	}
	return out
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

func (c *BaseChannel) SetRunning(running bool) {
	c.running.Store(running)
}

// SetMediaStore injects a MediaStore into the channel.
func (c *BaseChannel) SetMediaStore(s media.MediaStore) { c.mediaStore = s }

// GetMediaStore returns the injected MediaStore (may be nil).
func (c *BaseChannel) GetMediaStore() media.MediaStore { return c.mediaStore }

// SetPlaceholderRecorder injects a PlaceholderRecorder into the channel.
func (c *BaseChannel) SetPlaceholderRecorder(r PlaceholderRecorder) {
	c.placeholderRecorder = r
}

// GetPlaceholderRecorder returns the injected PlaceholderRecorder (may be nil).
func (c *BaseChannel) GetPlaceholderRecorder() PlaceholderRecorder {
	return c.placeholderRecorder
}

// SetOwner injects the concrete channel that embeds this BaseChannel.
// This allows HandleMessage to auto-trigger TypingCapable / ReactionCapable / PlaceholderCapable.
func (c *BaseChannel) SetOwner(ch Channel) {
	c.owner = ch
}

// BuildMediaScope constructs a scope key for media lifecycle tracking.
func BuildMediaScope(channel, chatID, messageID string) string {
	id := messageID
	if id == "" {
		id = uniqueID()
	}
	return channel + ":" + chatID + ":" + id
}

// shouldSkipDuplicate deduplicates inbound messages by message_id.
// 返回 true 表示该消息应被跳过（重复消息）。
func (c *BaseChannel) shouldSkipDuplicate(messageID string, metadata map[string]string) bool {
	msgID := strings.TrimSpace(messageID)
	if msgID == "" && len(metadata) > 0 {
		msgID = strings.TrimSpace(metadata["message_id"])
	}
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
