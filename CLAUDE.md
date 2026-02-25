# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

PicoClaw 是一个用 Go 语言编写的超轻量级 AI 助手,内存占用 <10MB,启动时间 <1s。项目从 Python 的 nanobot 重构而来,通过 AI 自举过程完成架构迁移。

**核心特性:**
- 单一二进制文件,跨平台支持 (RISC-V, ARM, x86)
- 多 LLM 提供商支持 (OpenAI, Anthropic, Zhipu, Gemini, Groq 等)
- 多聊天平台集成 (Telegram, Discord, QQ, DingTalk, LINE, WeCom)
- 工具系统 (文件操作、命令执行、网络搜索、子代理生成)
- 技能系统 (可扩展的自定义技能)
- 沙箱安全机制

## 构建与测试命令

### 构建
```bash
# 安装依赖
make deps

# 构建当前平台
make build
# 输出: build/picoclaw-{platform}-{arch}

# 构建所有平台
make build-all

# 构建并安装到 ~/.local/bin
make install
```

### 测试
```bash
# 运行所有测试
make test

# 运行单个包的测试
go test ./pkg/agent/...

# 运行单个测试函数
go test -run TestFunctionName ./pkg/path/to/package

# 带详细输出
go test -v ./pkg/...

# 带覆盖率
go test -cover ./pkg/...
```

### 代码质量
```bash
# 静态分析
make vet

# 格式化代码
make fmt

# 运行 linter
make lint

# 自动修复 lint 问题
make fix

# 完整检查 (deps + fmt + vet + test)
make check
```

### 运行
```bash
# 初始化配置
./build/picoclaw onboard

# 单次对话
./build/picoclaw agent -m "你的问题"

# 交互模式
./build/picoclaw agent

# 启动网关 (多平台集成)
./build/picoclaw gateway

# 查看状态
./build/picoclaw status

# 定时任务管理
./build/picoclaw cron list
./build/picoclaw cron add "0 9 * * *" "提醒我开会"
```

## 核心架构

### 目录结构
```
cmd/picoclaw/          # CLI 入口和命令定义
pkg/
  ├── agent/           # Agent 核心逻辑 (实例、循环、上下文、内存)
  ├── providers/       # LLM 提供商适配器 (统一接口)
  ├── tools/           # 工具系统 (文件、命令、网络、消息、子代理)
  ├── channels/        # 聊天平台集成 (Telegram, Discord 等)
  ├── config/          # 配置管理和迁移
  ├── session/         # 会话管理和历史记录
  ├── skills/          # 技能系统 (加载、注册、搜索)
  ├── routing/         # 消息路由 (agent ID, session key)
  ├── bus/             # 消息总线 (事件驱动架构)
  ├── auth/            # OAuth 认证 (Anthropic, OpenAI)
  ├── cron/            # 定时任务服务
  ├── heartbeat/       # 心跳任务 (周期性执行)
  └── state/           # 持久化状态管理
```

### 关键组件

**1. Agent 系统 (`pkg/agent/`)**
- `AgentInstance`: 完整配置的 agent 实例,包含 workspace、session、tools、provider
- `AgentLoop`: 主事件循环,处理消息、工具调用、会话管理
- `AgentRegistry`: 管理多个 agent 实例 (支持多 agent 配置)
- `ContextBuilder`: 构建 LLM 上下文 (系统提示、工具定义、会话历史)
- `Memory`: 长期记忆管理 (MEMORY.md)

**2. Provider 系统 (`pkg/providers/`)**
- 统一的 `LLMProvider` 接口
- 支持 OpenAI 兼容协议 (`openai_compat/`)
- 支持 Anthropic 原生协议 (`anthropic/`)
- 特殊提供商: Claude CLI, Codex CLI, GitHub Copilot
- `FallbackChain`: 自动故障转移和重试机制
- `ModelConfig`: 模型配置和候选者解析

**3. Tools 系统 (`pkg/tools/`)**
- `Tool` 接口: `Name()`, `Description()`, `Parameters()`, `Execute()`
- `ToolRegistry`: 工具注册和执行管理
- 内置工具:
  - 文件操作: `read_file`, `write_file`, `edit_file`, `append_file`, `list_dir`
  - 命令执行: `exec` (带沙箱保护)
  - 网络搜索: `web_search` (Brave, Tavily, DuckDuckGo)
  - 消息发送: `message` (跨平台消息)
  - 子代理: `spawn` (创建独立的子 agent)
  - 技能管理: `skills_search`, `skills_install`
  - 定时任务: `cron_add`, `cron_list`, `cron_remove`

**4. 沙箱安全 (`restrict_to_workspace`)**
- 默认启用,限制文件和命令访问在 workspace 内
- 危险命令黑名单 (`rm -rf`, `format`, `shutdown` 等)
- 路径验证和规范化

**5. 配置系统 (`pkg/config/`)**
- 配置文件: `~/.picoclaw/config.json`
- 环境变量覆盖: `PICOCLAW_*` 前缀
- 自动迁移旧配置格式
- `model_list`: 新的模型配置方式 (零代码添加提供商)

### 数据流

```
用户消息 → Channel → MessageBus → AgentLoop
  ↓
AgentLoop.processMessage()
  ↓
1. 加载 session 历史
2. 构建上下文 (系统提示 + 工具定义 + 历史)
3. 调用 LLM Provider
  ↓
LLM 响应 (文本 / 工具调用)
  ↓
4. 执行工具 (ToolRegistry)
5. 将工具结果添加到历史
6. 继续循环 (最多 MaxIterations 次)
  ↓
7. 返回最终响应
8. 保存 session
9. 触发摘要 (如果需要)
```

### Provider 选择逻辑

1. 优先使用 `model_list` 配置 (新格式)
2. 回退到 `providers` 配置 (旧格式,已弃用)
3. 根据模型名称推断提供商:
   - `openai/*`, `gpt*` → OpenAI
   - `anthropic/*`, `claude*` → Anthropic
   - `zhipu/*`, `glm*` → Zhipu
   - `gemini/*`, `google/*` → Gemini
   - `groq/*` → Groq
   - `ollama/*` → Ollama (本地)
4. 默认回退到 OpenRouter

## 开发注意事项

### 添加新工具
1. 在 `pkg/tools/` 创建新文件 (如 `my_tool.go`)
2. 实现 `Tool` 接口:
   ```go
   type MyTool struct {}
   func (t *MyTool) Name() string { return "my_tool" }
   func (t *MyTool) Description() string { return "..." }
   func (t *MyTool) Parameters() map[string]any { return map[string]any{...} }
   func (t *MyTool) Execute(ctx context.Context, args map[string]any) *ToolResult { ... }
   ```
3. 在 `agent/instance.go` 的 `NewAgentInstance()` 中注册:
   ```go
   toolsRegistry.Register(tools.NewMyTool())
   ```

### 添加新 Provider
1. 如果是 OpenAI 兼容协议:
   - 只需在 `config.json` 的 `model_list` 中添加配置
   - 无需修改代码
2. 如果是自定义协议:
   - 在 `pkg/providers/` 创建新文件
   - 实现 `LLMProvider` 接口
   - 在 `factory.go` 中添加选择逻辑

### 添加新 Channel
1. 在 `pkg/channels/` 创建新文件 (如 `mychannel.go`)
2. 实现 `Channel` 接口:
   ```go
   type MyChannel struct { *BaseChannel }
   func (c *MyChannel) Start(ctx context.Context) error { ... }
   func (c *MyChannel) Stop() error { ... }
   func (c *MyChannel) SendMessage(chatID, text string) error { ... }
   ```
3. 在 `channels/manager.go` 的 `NewManager()` 中注册

### 测试约定
- 测试文件命名: `*_test.go`
- 集成测试: `*_integration_test.go` (需要外部依赖)
- Mock 对象: `mock_*_test.go`
- 使用 `testify/assert` 进行断言
- 使用 table-driven tests 处理多个测试用例

### 构建标签
- `stdjson`: 使用标准库 JSON (默认)
- 如需使用 sonic (高性能 JSON): 移除 `-tags stdjson`

### 日志规范
使用 `pkg/logger` 包:
```go
logger.Info("message")
logger.InfoCF("category", "message", map[string]any{"key": "value"})
logger.Error("error message")
logger.ErrorCF("category", "error", map[string]any{"error": err})
```

### 配置优先级
1. 环境变量 (`PICOCLAW_*`)
2. 配置文件 (`~/.picoclaw/config.json`)
3. 代码默认值

### Workspace 布局
```
~/.picoclaw/workspace/
├── sessions/          # 会话历史
├── memory/            # 长期记忆 (MEMORY.md)
├── state/             # 持久化状态
├── cron/              # 定时任务数据库
├── skills/            # 自定义技能
├── AGENTS.md          # Agent 行为指南
├── HEARTBEAT.md       # 周期性任务提示
├── IDENTITY.md        # Agent 身份
├── SOUL.md            # Agent 灵魂
├── TOOLS.md           # 工具描述
└── USER.md            # 用户偏好
```

## 常见任务

### 调试 LLM 调用
在 `pkg/agent/loop.go` 的 `callLLM()` 中添加日志:
```go
logger.DebugCF("llm", "Request", map[string]any{"messages": messages})
logger.DebugCF("llm", "Response", map[string]any{"response": resp})
```

### 调试工具执行
工具执行日志自动记录在 `pkg/tools/registry.go` 的 `ExecuteWithContext()` 中。

### 添加新的配置选项
1. 在 `pkg/config/config.go` 中添加字段
2. 在 `pkg/config/defaults.go` 中设置默认值
3. 更新 `config/config.example.json`
4. 如需环境变量支持,添加 `env:` 标签

### 处理配置迁移
在 `pkg/config/migration.go` 中添加迁移逻辑,在 `Load()` 时自动执行。

## 性能优化原则

1. **内存优化**: 避免大对象复制,使用指针传递
2. **并发安全**: 使用 `sync.RWMutex` 保护共享状态
3. **上下文传递**: 所有长时间操作接受 `context.Context`
4. **资源清理**: 使用 `defer` 确保资源释放
5. **JSON 性能**: 默认使用 `stdjson` 标签 (标准库),可选 sonic

## 安全注意事项

1. **沙箱限制**: 默认启用 `restrict_to_workspace`,生产环境不要禁用
2. **命令黑名单**: 在 `pkg/tools/shell.go` 中维护危险命令列表
3. **路径验证**: 所有文件操作前验证路径在 workspace 内
4. **API 密钥**: 不要在代码中硬编码,使用配置文件或环境变量
5. **用户输入**: 所有用户输入都经过 LLM 处理,不直接执行

## 依赖管理

- Go 版本: 1.25.7+
- 主要依赖:
  - `anthropic-sdk-go`: Anthropic API 客户端
  - `openai-go`: OpenAI API 客户端
  - `telego`: Telegram Bot API
  - `discordgo`: Discord Bot API
  - `gronx`: Cron 表达式解析
  - `oauth2`: OAuth 认证

更新依赖:
```bash
make update-deps
```
