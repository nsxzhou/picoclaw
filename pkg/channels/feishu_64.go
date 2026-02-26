//go:build amd64 || arm64 || riscv64 || mips64 || ppc64

package channels

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkdispatcher "github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/utils"
)

type FeishuChannel struct {
	*BaseChannel
	config   config.FeishuConfig
	client   *lark.Client
	wsClient *larkws.Client

	mu     sync.Mutex
	cancel context.CancelFunc
}

func NewFeishuChannel(cfg config.FeishuConfig, bus *bus.MessageBus) (*FeishuChannel, error) {
	base := NewBaseChannel("feishu", cfg, bus, cfg.AllowFrom)

	return &FeishuChannel{
		BaseChannel: base,
		config:      cfg,
		client:      lark.NewClient(cfg.AppID, cfg.AppSecret),
	}, nil
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

	c.setRunning(true)
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

	c.setRunning(false)
	logger.InfoC("feishu", "Feishu channel stopped")
	return nil
}

func (c *FeishuChannel) Send(ctx context.Context, msg bus.OutboundMessage) error {
	if !c.IsRunning() {
		return fmt.Errorf("feishu channel not running")
	}

	if msg.ChatID == "" {
		return fmt.Errorf("chat ID is empty")
	}

	// 将 Markdown 转换为飞书 Post 富文本格式
	postContent := markdownToFeishuPost(msg.Content)
	payload, err := json.Marshal(postContent)
	if err != nil {
		return fmt.Errorf("failed to marshal feishu content: %w", err)
	}

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(msg.ChatID).
			MsgType(larkim.MsgTypePost).
			Content(string(payload)).
			Uuid(fmt.Sprintf("picoclaw-%d", time.Now().UnixNano())).
			Build()).
		Build()

	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to send feishu message: %w", err)
	}

	if !resp.Success() {
		return fmt.Errorf("feishu api error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	logger.DebugCF("feishu", "Feishu message sent", map[string]any{
		"chat_id": msg.ChatID,
	})

	return nil
}

func (c *FeishuChannel) handleMessageReceive(_ context.Context, event *larkim.P2MessageReceiveV1) error {
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

	content := extractFeishuMessageContent(message)
	if content == "" {
		content = "[empty message]"
	}

	metadata := map[string]string{}
	if messageID := stringValue(message.MessageId); messageID != "" {
		metadata["message_id"] = messageID
	}
	if messageType := stringValue(message.MessageType); messageType != "" {
		metadata["message_type"] = messageType
	}
	if chatType := stringValue(message.ChatType); chatType != "" {
		metadata["chat_type"] = chatType
	}
	if sender != nil && sender.TenantKey != nil {
		metadata["tenant_key"] = *sender.TenantKey
	}

	chatType := stringValue(message.ChatType)
	if chatType == "p2p" {
		metadata["peer_kind"] = "direct"
		metadata["peer_id"] = senderID
	} else {
		metadata["peer_kind"] = "group"
		metadata["peer_id"] = chatID
	}

	logger.InfoCF("feishu", "Feishu message received", map[string]any{
		"sender_id": senderID,
		"chat_id":   chatID,
		"preview":   utils.Truncate(content, 80),
	})

	c.HandleMessage(senderID, chatID, content, nil, metadata)
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

func extractFeishuMessageContent(message *larkim.EventMessage) string {
	if message == nil || message.Content == nil || *message.Content == "" {
		return ""
	}

	if message.MessageType != nil && *message.MessageType == larkim.MsgTypeText {
		var textPayload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(*message.Content), &textPayload); err == nil {
			return textPayload.Text
		}
	}

	return *message.Content
}

func stringValue(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// ===== Markdown → 飞书 Post 富文本转换 =====

// feishuPostElement 表示飞书 Post 消息中的一个元素
type feishuPostElement struct {
	Tag      string   `json:"tag"`
	Text     string   `json:"text,omitempty"`
	Href     string   `json:"href,omitempty"`
	Style    []string `json:"style,omitempty"`
	Language string   `json:"language,omitempty"`
}

// feishuPostContent 是飞书 Post 消息的完整结构
type feishuPostContent struct {
	ZhCN *feishuPostBody `json:"zh_cn"`
}

type feishuPostBody struct {
	Content [][]feishuPostElement `json:"content"`
}

// markdownToFeishuPost 将 Markdown 文本转换为飞书 Post 富文本结构
func markdownToFeishuPost(text string) feishuPostContent {
	if text == "" {
		return feishuPostContent{ZhCN: &feishuPostBody{Content: [][]feishuPostElement{}}}
	}

	var paragraphs [][]feishuPostElement

	// 提取代码块并用占位符替换
	var codeBlocks []struct {
		lang string
		code string
	}
	codeBlockRe := regexp.MustCompile("(?s)```(\\w*)\n?(.*?)```")
	cbIndex := 0
	text = codeBlockRe.ReplaceAllStringFunc(text, func(m string) string {
		matches := codeBlockRe.FindStringSubmatch(m)
		lang := ""
		code := ""
		if len(matches) >= 3 {
			lang = matches[1]
			code = matches[2]
		}
		// 去掉尾部多余换行
		code = strings.TrimRight(code, "\n")
		codeBlocks = append(codeBlocks, struct {
			lang string
			code string
		}{lang: lang, code: code})
		placeholder := fmt.Sprintf("\x00CODEBLOCK_%d\x00", cbIndex)
		cbIndex++
		return placeholder
	})

	lines := strings.Split(text, "\n")
	codeBlockPlaceholderRe := regexp.MustCompile(`\x00CODEBLOCK_(\d+)\x00`)

	for _, line := range lines {
		// 检查是否为代码块占位符
		if m := codeBlockPlaceholderRe.FindStringSubmatch(line); len(m) == 2 {
			var idx int
			fmt.Sscanf(m[1], "%d", &idx)
			if idx < len(codeBlocks) {
				cb := codeBlocks[idx]
				paragraphs = append(paragraphs, []feishuPostElement{{
					Tag:      "code_block",
					Language: cb.lang,
					Text:     cb.code,
				}})
			}
			continue
		}

		// 空行 → 空段落
		if strings.TrimSpace(line) == "" {
			paragraphs = append(paragraphs, []feishuPostElement{})
			continue
		}

		// 标题 → 加粗文本
		if m := regexp.MustCompile(`^#{1,6}\s+(.+)$`).FindStringSubmatch(line); len(m) == 2 {
			elements := parseFeishuInline(m[1])
			// 给所有元素追加 bold 样式
			for i := range elements {
				elements[i].Style = appendStyle(elements[i].Style, "bold")
			}
			paragraphs = append(paragraphs, elements)
			continue
		}

		// 引用块 → 加 ❝ 前缀
		if m := regexp.MustCompile(`^>\s*(.*)$`).FindStringSubmatch(line); len(m) == 2 {
			inner := parseFeishuInline(m[1])
			elements := []feishuPostElement{{Tag: "text", Text: "❝ "}}
			elements = append(elements, inner...)
			paragraphs = append(paragraphs, elements)
			continue
		}

		// 列表项 → • 前缀
		if m := regexp.MustCompile(`^(\s*)[-*]\s+(.+)$`).FindStringSubmatch(line); len(m) == 3 {
			indent := m[1]
			inner := parseFeishuInline(m[2])
			elements := []feishuPostElement{{Tag: "text", Text: indent + "• "}}
			elements = append(elements, inner...)
			paragraphs = append(paragraphs, elements)
			continue
		}

		// 有序列表
		if m := regexp.MustCompile(`^(\s*)\d+\.\s+(.+)$`).FindStringSubmatch(line); len(m) == 3 {
			indent := m[1]
			inner := parseFeishuInline(m[2])
			// 保留原始数字编号
			numMatch := regexp.MustCompile(`^(\s*)(\d+)\.\s+`).FindStringSubmatch(line)
			prefix := indent + numMatch[2] + ". "
			elements := []feishuPostElement{{Tag: "text", Text: prefix}}
			elements = append(elements, inner...)
			paragraphs = append(paragraphs, elements)
			continue
		}

		// 普通行 → 解析行内格式
		paragraphs = append(paragraphs, parseFeishuInline(line))
	}

	return feishuPostContent{
		ZhCN: &feishuPostBody{Content: paragraphs},
	}
}

// parseFeishuInline 解析一行文本中的行内 Markdown 格式
func parseFeishuInline(text string) []feishuPostElement {
	if text == "" {
		return []feishuPostElement{{Tag: "text", Text: ""}}
	}

	// 定义行内模式的正则（按优先级排列）
	// 匹配: 行内代码、链接、加粗、斜体、删除线
	pattern := regexp.MustCompile(
		"`([^`]+)`" + // 行内代码
			`|\[([^\]]+)\]\(([^)]+)\)` + // 链接
			`|\*\*(.+?)\*\*` + // 加粗 **
			`|__(.+?)__` + // 加粗 __
			`|\*([^*]+)\*` + // 斜体 *
			`|_([^_]+)_` + // 斜体 _
			`|~~(.+?)~~`, // 删除线
	)

	var elements []feishuPostElement
	lastIndex := 0

	for _, loc := range pattern.FindAllStringSubmatchIndex(text, -1) {
		// 匹配前的普通文本
		if loc[0] > lastIndex {
			elements = append(elements, feishuPostElement{
				Tag:  "text",
				Text: text[lastIndex:loc[0]],
			})
		}

		// 按 submatch 判断实际匹配的是哪种模式
		switch {
		case loc[2] >= 0 && loc[3] >= 0: // 行内代码 `code`
			elements = append(elements, feishuPostElement{
				Tag:   "text",
				Text:  text[loc[2]:loc[3]],
				Style: []string{"code_inline"},
			})
		case loc[4] >= 0 && loc[5] >= 0: // 链接 [text](url)
			elements = append(elements, feishuPostElement{
				Tag:  "a",
				Text: text[loc[4]:loc[5]],
				Href: text[loc[6]:loc[7]],
			})
		case loc[8] >= 0 && loc[9] >= 0: // 加粗 **text**
			elements = append(elements, feishuPostElement{
				Tag:   "text",
				Text:  text[loc[8]:loc[9]],
				Style: []string{"bold"},
			})
		case loc[10] >= 0 && loc[11] >= 0: // 加粗 __text__
			elements = append(elements, feishuPostElement{
				Tag:   "text",
				Text:  text[loc[10]:loc[11]],
				Style: []string{"bold"},
			})
		case loc[12] >= 0 && loc[13] >= 0: // 斜体 *text*
			elements = append(elements, feishuPostElement{
				Tag:   "text",
				Text:  text[loc[12]:loc[13]],
				Style: []string{"italic"},
			})
		case loc[14] >= 0 && loc[15] >= 0: // 斜体 _text_
			elements = append(elements, feishuPostElement{
				Tag:   "text",
				Text:  text[loc[14]:loc[15]],
				Style: []string{"italic"},
			})
		case loc[16] >= 0 && loc[17] >= 0: // 删除线 ~~text~~
			elements = append(elements, feishuPostElement{
				Tag:   "text",
				Text:  text[loc[16]:loc[17]],
				Style: []string{"strikethrough"},
			})
		}

		lastIndex = loc[1]
	}

	// 剩余的普通文本
	if lastIndex < len(text) {
		elements = append(elements, feishuPostElement{
			Tag:  "text",
			Text: text[lastIndex:],
		})
	}

	if len(elements) == 0 {
		return []feishuPostElement{{Tag: "text", Text: text}}
	}

	return elements
}

// appendStyle 追加样式（去重）
func appendStyle(styles []string, style string) []string {
	for _, s := range styles {
		if s == style {
			return styles
		}
	}
	return append(styles, style)
}
