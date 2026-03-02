# PicoClaw 架构限制：多模态（图片）识别缺陷与幻觉分析

## 1. 现象描述

在成功为 Feishu（飞书）渠道接入图片和文件接收功能后，发现一个严重的识别错误现象：
当用户向 Bot 发送一张无关的图片（如卡通表情包贴纸）时，LLM 会一本正经地回复：“这张图片展示的是 Sipeed LicheeRV Nano 开发板...”，完全偏离了真实的图片内容。

## 2. 根因分析：幻觉的完美闭环

经过对底层代码链路的追踪与排查（从接收消息到构建上下文再到调用大模型），发现该问题并非由于具体渠道（如飞书）的实现错误，而是源于 **PicoClaw 核心架构目前尚未实现多模态（视觉）支持**。

具体原因可归结为以下三个层面：

### 2.1 媒体文件被静默丢弃 (Agent Loop 层)

虽然各渠道（如 `telegram.go`, `feishu_64.go`）正确地将下载的本地文件路径存入了 `bus.InboundMessage.Media` 数组，但在 `pkg/agent/loop.go` 的 `processMessage` 阶段，实际传给上下文构建器的 `media` 参数被硬编码为了 `nil`：

```go
// pkg/agent/loop.go
messages := agent.ContextBuilder.BuildMessages(
    history,
    summary,
    opts.UserMessage,
    nil, // <--- 关键点：所有频道的 media (附件) 在这里被直接丢弃
    opts.Channel,
    opts.ChatID,
)
```

### 2.2 缺乏多模态数据结构支持 (Provider 层)

追踪底层 LLM Provider 的实现（如 `pkg/providers/protocoltypes/types.go` 和 `openai_compat/provider.go`），目前传递给各类接口的 `Message` 结构体中，`Content` 字段被直接定义为了单纯的 `string`。

各大模型厂商（如 OpenAI Vision, Gemini, Anthropic）对于包含图片的请求，通常要求 `Content` 是一个复杂的对象数组（包含 `type: "text"` 和 `type: "image_url"`），并将图片的 Base64 编码或公开 URL 嵌入其中。
目前的文本字符串架构无法承载或传递真实的像素/Base64 数据。

### 2.3 系统上下文引发的深度幻觉 (Context 层)

由于 LLM 端完全没有收到图片的实际像素信息，它看到的只是一行干瘪的占位文本：`[image: photo]`。
然而，`pkg/agent/context.go` 中构建的 System Prompt（以及项目默认的 `AGENTS.md`、`IDENTITY.md` 及相关 Skills 文档）为大模型植入了极其强烈的上下文预设（诸如 Sipeed, LicheeRV Nano 等硬件信息）。
为了完成“分析图片”的用户指令，大模型基于这些上下文进行了强烈的脑补（Hallucination），从而非常自信地给出了荒谬的结论。

## 3. 路线图印证

此限制已经记录在项目的官方路线图中：

> `ROADMAP.md`: [attachment](https://github.com/sipeed/picoclaw/issues/348): Native handling of images, audio, and video attachments.
> 这表明系统设计之初就已知晓该架构鸿沟。

## 4. 后续重构建议（多模态升级路线）

要让 PicoClaw 真正具备“看图”能力，需要进行一次自下而上的底层重构：

1. **协议层 (`pkg/providers/protocoltypes`)**：
   将 `Message.Content` 的类型从 `string` 重构为支持多态结构的复杂类型（或者新增 `Parts` 数组字段），以同时容纳文本块和图像块。

2. **适配器层 (`pkg/providers/*`)**：
   修改各个厂商的 API 适配器（如 `openai_compat`, `anthropic`, `gemini`），识别并提取消息中的图像模块，将其转换为 Base64 字符串插入请求 Payload。

3. **引擎层 (`pkg/agent/context.go` & `loop.go`)**：
   - 修改 `ContextBuilder`，使其真正接收并处理传入的 `media []string` 路径列表。
   - 读取这些本地图片文件，转换为 Base64 格式，并正确拼接为多模态的 Message 结构返回给 `AgentLoop`。

只有完成上述核心链路的改造，前端各个 Channel（不只是飞书，同样适用于 Telegram、Discord 等）传递进来的图片才能真正送达多模态大模型的“眼中”。
