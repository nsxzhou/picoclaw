//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/sipeed/picoclaw/pkg/attachments"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/channels"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/identity"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type FeishuChannel struct {
	*channels.BaseChannel
	config   config.FeishuConfig
	client   *lark.Client
	wsClient *larkws.Client

	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewFeishuChannel(cfg config.FeishuConfig, bus *bus.MessageBus) (*FeishuChannel, error) {
	base := channels.NewBaseChannel("feishu", cfg, bus, cfg.AllowFrom,
		channels.WithGroupTrigger(cfg.GroupTrigger),
		channels.WithReasoningChannelID(cfg.ReasoningChannelID),
	)

	return &FeishuChannel{
		BaseChannel: base,
		config:      cfg,
		client:      lark.NewClient(cfg.AppID, cfg.AppSecret),
	}, nil
}

// NewFileRefResolver returns a resolver that downloads Feishu files on demand.
func (c *FeishuChannel) NewFileRefResolver() *FeishuFileRefResolver {
	return NewFeishuFileRefResolver(c.client)
}

func (c *FeishuChannel) Start(ctx context.Context) error {
	if c.config.AppID == "" || c.config.AppSecret == "" {
		return fmt.Errorf("feishu app_id or app_secret is empty")
	}

	dispatcher := larkdispatcher.NewEventDispatcher(c.config.VerificationToken, c.config.EncryptKey).
		OnP2MessageReceiveV1(c.handleMessageReceive)

	runCtx, cancel := context.WithCancel(ctx)

	c.mu.Lock()
	c.cancel = cancel
	c.wsClient = larkws.NewClient(
		c.config.AppID,
		c.config.AppSecret,
		larkws.WithEventHandler(dispatcher),
	)
	wsClient := c.wsClient
	c.mu.Unlock()

	c.SetRunning(true)
	logger.InfoC("feishu", "Feishu channel started (websocket mode)")

	go func() {
		if err := wsClient.Start(runCtx); err != nil {
			logger.ErrorCF("feishu", "Feishu websocket stopped with error", map[string]any{
				"error": err.Error(),
			})
		}
	}()

	return nil
}

func (c *FeishuChannel) Stop(ctx context.Context) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.wsClient = nil
	c.mu.Unlock()

	c.SetRunning(false)
	logger.InfoC("feishu", "Feishu channel stopped")
	return nil
}

func (c *FeishuChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return channels.ErrNotRunning
	}

	if msg.ChatID == "" {
		return fmt.Errorf("chat ID is empty")
	}

	payload, err := json.Marshal(map[string]string{"text": msg.Content})
	if err != nil {
		return fmt.Errorf("failed to marshal feishu content: %w", err)
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(msg.ChatID).
			MsgType(larkim.MsgTypeText).
			Content(string(payload)).
			Uuid(fmt.Sprintf("picoclaw-%d", time.Now().UnixNano())).
			Build()).
		Build()

	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu send: %w", channels.ErrTemporary)
	}

	if !resp.Success() {
		return fmt.Errorf("feishu api error (code=%d msg=%s): %w", resp.Code, resp.Msg, channels.ErrTemporary)
	}

	logger.DebugCF("feishu", "Feishu message sent", map[string]any{
		"chat_id": msg.ChatID,
	})

	return nil
}

func (c *FeishuChannel) handleMessageReceive(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event == nil || event.Event == nil || event.Event.Message == nil {
		return nil
	}

	message := event.Event.Message
	sender := event.Event.Sender

	chatID := stringValue(message.ChatId)
	if chatID == "" {
		return nil
	}

	senderID := extractFeishuSenderID(sender)
	if senderID == "" {
		senderID = "unknown"
	}

	msgType := stringValue(message.MessageType)
	content := ""
	var fileRefs []bus.FileRef

	messageID := stringValue(message.MessageId)

	switch msgType {
	case "text":
		content = extractFeishuTextContent(message)
	case "image":
		imageKey := extractFeishuImageKey(message)
		if imageKey != "" && messageID != "" {
			fileRefs = append(fileRefs, bus.FileRef{
				Name:            "feishu_image.jpg",
				MediaType:       "application/octet-stream",
				Kind:            bus.AttachmentKindImage,
				Source:          bus.FileRefSourceFeishu,
				FeishuMessageID: messageID,
				FeishuFileKey:   imageKey,
				FeishuResType:   "image",
			})
			content = "[image: photo]"
		} else {
			content = "[image: missing key or message_id]"
		}
	case "file":
		fileKey, fileName := extractFeishuFileInfo(message)
		if fileName == "" {
			fileName = "feishu_file"
		}
		if fileKey != "" && messageID != "" {
			fileRefs = append(fileRefs, bus.FileRef{
				Name:            fileName,
				MediaType:       attachments.InferMediaTypeFromName(fileName),
				Kind:            attachments.InferAttachmentKindFromName(fileName),
				Source:          bus.FileRefSourceFeishu,
				FeishuMessageID: messageID,
				FeishuFileKey:   fileKey,
				FeishuResType:   "file",
			})
			content = fmt.Sprintf("[file: %s]", fileName)
		} else {
			content = "[file: missing key or message_id]"
		}
	default:
		content = extractFeishuTextContent(message)
		if strings.TrimSpace(content) == "" {
			content = fmt.Sprintf("[unsupported message type: %s]", msgType)
		}
	}

	if content == "" {
		content = "[empty message]"
	}

	metadata := map[string]string{}
	if msgType != "" {
		metadata["message_type"] = msgType
	}
	if chatType := stringValue(message.ChatType); chatType != "" {
		metadata["chat_type"] = chatType
	}
	if sender != nil && sender.TenantKey != nil {
		metadata["tenant_key"] = *sender.TenantKey
	}

	chatType := stringValue(message.ChatType)
	var peer bus.Peer
	if chatType == "p2p" {
		peer = bus.Peer{Kind: "direct", ID: senderID}
	} else {
		peer = bus.Peer{Kind: "group", ID: chatID}
		// In group chats, apply unified group trigger filtering
		respond, cleaned := c.ShouldRespondInGroup(false, content)
		if !respond {
			return nil
		}
		content = cleaned
	}

	logger.InfoCF("feishu", "Feishu message received", map[string]any{
		"sender_id":    senderID,
		"chat_id":      chatID,
		"message_type": msgType,
		"preview":      utils.Truncate(content, 80),
	})

	senderInfo := bus.SenderInfo{
		Platform:    "feishu",
		PlatformID:  senderID,
		CanonicalID: identity.BuildCanonicalID("feishu", senderID),
	}

	if !c.IsAllowedSender(senderInfo) {
		return nil
	}

	if len(fileRefs) > 0 {
		c.HandleMessageWithFileRefs(ctx, peer, messageID, senderID, chatID, content, nil, fileRefs, metadata, senderInfo)
	} else {
		c.HandleMessage(ctx, peer, messageID, senderID, chatID, content, nil, metadata, senderInfo)
	}

	return nil
}

func extractFeishuSenderID(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil {
		return ""
	}

	if sender.SenderId.UserId != nil && *sender.SenderId.UserId != "" {
		return *sender.SenderId.UserId
	}
	if sender.SenderId.OpenId != nil && *sender.SenderId.OpenId != "" {
		return *sender.SenderId.OpenId
	}
	if sender.SenderId.UnionId != nil && *sender.SenderId.UnionId != "" {
		return *sender.SenderId.UnionId
	}

	return ""
}

func extractFeishuTextContent(message *larkim.EventMessage) string {
	if message == nil || message.Content == nil || *message.Content == "" {
		return ""
	}

	var textPayload struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(*message.Content), &textPayload); err == nil {
		return textPayload.Text
	}

	return *message.Content
}

func extractFeishuImageKey(message *larkim.EventMessage) string {
	if message == nil || message.Content == nil || *message.Content == "" {
		return ""
	}

	var imagePayload struct {
		ImageKey string `json:"image_key"`
	}
	if err := json.Unmarshal([]byte(*message.Content), &imagePayload); err == nil {
		return imagePayload.ImageKey
	}
	return ""
}

func extractFeishuFileInfo(message *larkim.EventMessage) (fileKey, fileName string) {
	if message == nil || message.Content == nil || *message.Content == "" {
		return "", ""
	}

	var filePayload struct {
		FileKey  string `json:"file_key"`
		FileName string `json:"file_name"`
	}
	if err := json.Unmarshal([]byte(*message.Content), &filePayload); err == nil {
		return filePayload.FileKey, filePayload.FileName
	}

	return "", ""
}
