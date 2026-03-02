package protocoltypes

type ToolCall struct {
	ID               string         `json:"id"`
	Type             string         `json:"type,omitempty"`
	Function         *FunctionCall  `json:"function,omitempty"`
	Name             string         `json:"-"`
	Arguments        map[string]any `json:"-"`
	ThoughtSignature string         `json:"-"` // Internal use only
	ExtraContent     *ExtraContent  `json:"extra_content,omitempty"`
}

type ExtraContent struct {
	Google *GoogleExtra `json:"google,omitempty"`
}

type GoogleExtra struct {
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

type FunctionCall struct {
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	ThoughtSignature string `json:"thought_signature,omitempty"`
}

type LLMResponse struct {
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	FinishReason     string     `json:"finish_reason"`
	Usage            *UsageInfo `json:"usage,omitempty"`
}

type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// CacheControl marks a content block for LLM-side prefix caching.
// Currently only "ephemeral" is supported (used by Anthropic).
type CacheControl struct {
	Type string `json:"type"` // "ephemeral"
}

// ContentBlock represents a structured segment of a system message.
// Adapters that understand SystemParts can use these blocks to set
// per-block cache control (e.g. Anthropic's cache_control: ephemeral).
type ContentBlock struct {
	Type         string        `json:"type"` // "text"
	Text         string        `json:"text"`
	CacheControl *CacheControl `json:"cache_control,omitempty"`
}

// ImageBlock carries a base64-encoded image for multimodal LLM requests.
type ImageBlock struct {
	MediaType string `json:"media_type"` // e.g. "image/jpeg", "image/png"
	Data      string `json:"data"`       // base64-encoded image data
}

// FileBlock carries a base64-encoded file for multimodal LLM requests.
// Used for documents (PDF, DOCX, etc.) that the model can process natively.
type FileBlock struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"` // e.g. "application/pdf"
	Data      string `json:"data"`       // base64-encoded file data
}

// FileRefMeta is the serializable metadata of a FileRef, stored in session history.
// It contains enough information to reconstruct a bus.FileRef for re-resolution.
type FileRefMeta struct {
	Name            string `json:"name"`
	MediaType       string `json:"media_type"`
	Kind            string `json:"kind"`
	Source          string `json:"source"`
	FeishuMessageID string `json:"feishu_message_id,omitempty"`
	FeishuFileKey   string `json:"feishu_file_key,omitempty"`
	FeishuResType   string `json:"feishu_res_type,omitempty"`
}

type Message struct {
	Role             string         `json:"role"`
	Content          string         `json:"content"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	SystemParts      []ContentBlock `json:"system_parts,omitempty"` // structured system blocks for cache-aware adapters
	Images           []ImageBlock   `json:"images,omitempty"`       // multimodal image attachments
	Files            []FileBlock    `json:"files,omitempty"`        // multimodal file attachments (PDF, DOCX, etc.)
	FileRefs         []FileRefMeta  `json:"file_refs,omitempty"`    // lazy file references for session persistence
	ToolCalls        []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type ToolDefinition struct {
	Type     string                 `json:"type"`
	Function ToolFunctionDefinition `json:"function"`
}

type ToolFunctionDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}
