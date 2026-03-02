package bus

// EncodedImage holds a base64-encoded image ready for LLM consumption.
// Images are encoded eagerly in the channel layer to avoid temp-file race conditions.
type EncodedImage struct {
	MediaType string `json:"media_type"` // e.g. "image/jpeg", "image/png"
	Data      string `json:"data"`       // base64-encoded image data
}

// AttachmentKind classifies inbound media for downstream processing.
type AttachmentKind string

const (
	AttachmentKindImage    AttachmentKind = "image"
	AttachmentKindAudio    AttachmentKind = "audio"
	AttachmentKindVideo    AttachmentKind = "video"
	AttachmentKindDocument AttachmentKind = "document"
	AttachmentKindUnknown  AttachmentKind = "unknown"
)

// Attachment describes one inbound media file and optional extracted text.
type Attachment struct {
	Name        string         `json:"name"`
	MediaType   string         `json:"media_type"`
	SizeBytes   int64          `json:"size_bytes"`
	LocalPath   string         `json:"local_path,omitempty"`
	Kind        AttachmentKind `json:"kind"`
	TextContent string         `json:"text_content,omitempty"`
}

// FileRefSource identifies the origin platform of a file reference.
type FileRefSource string

const (
	FileRefSourceFeishu FileRefSource = "feishu" // 飞书资源引用（message_id + file_key）
)

// FileRef is a lazy file reference that can be resolved on demand.
// Instead of downloading and encoding files eagerly, channels that support
// permanent storage (e.g. Feishu) construct FileRefs. The provider layer
// resolves them just before sending the LLM request.
type FileRef struct {
	Name      string         `json:"name"`
	MediaType string         `json:"media_type"`
	SizeBytes int64          `json:"size_bytes,omitempty"`
	Kind      AttachmentKind `json:"kind"`
	Source    FileRefSource  `json:"source"`

	// 飞书资源标识
	FeishuMessageID string `json:"feishu_message_id,omitempty"`
	FeishuFileKey   string `json:"feishu_file_key,omitempty"`
	FeishuResType   string `json:"feishu_res_type,omitempty"` // "image" 或 "file"
}

// AttachmentError records a failed attachment parsing attempt.
type AttachmentError struct {
	Name        string `json:"name"`
	Code        string `json:"code"`
	Reason      string `json:"reason,omitempty"`
	UserMessage string `json:"user_message"`
}

type InboundMessage struct {
	Channel          string            `json:"channel"`
	SenderID         string            `json:"sender_id"`
	ChatID           string            `json:"chat_id"`
	Content          string            `json:"content"`
	Media            []string          `json:"media,omitempty"`
	EncodedImages    []EncodedImage    `json:"encoded_images,omitempty"`
	Attachments      []Attachment      `json:"attachments,omitempty"`
	AttachmentErrors []AttachmentError `json:"attachment_errors,omitempty"`
	FileRefs         []FileRef         `json:"file_refs,omitempty"` // lazy file references (Feishu etc.)
	SessionKey       string            `json:"session_key"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type OutboundMessage struct {
	Channel string `json:"channel"`
	ChatID  string `json:"chat_id"`
	Content string `json:"content"`
}

type MessageHandler func(InboundMessage) error
