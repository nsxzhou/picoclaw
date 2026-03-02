# PicoClaw 功能与使用指南

PicoClaw 是一个超轻量级个人 AI 助手，采用 Go 语言从零重构，其核心架构经历过 AI Agent 自身的“自举”优化。以下是该项目的主要功能及使用方法的详细说明。

## 一、 这个项目有哪些功能？

### 1. 极致轻量与跨架构运行

- **资源极简**：内存占用小于 10MB，可在 10 美元级别的低功耗硬件上直接运行。
- **闪电启动**：启动耗时 <1 秒（低频单核亦可）。
- **多平台通杀**：支持通过单一二进制文件运行在 RISC-V、ARM 和 x86 架构硬件上。老旧安卓手机也能通过 Termux 重获新生。

### 2. 标准 AI 助手工作流

- **代码与执行**：具备极强的开发与分析能力，可担任全栈工程师。
- **记忆管理**：具有持久化的对话记忆和长期记忆，并支持工作区自动记录与文件管理。
- **搜索与学习**：支持外部搜索（DuckDuckGo，以及 Brave、Tavily API接入），能够在后台自动检索并总结最新信息。

### 3. 多模型与 "零代码" Provider 接入

- **丰富的大模型支持**：内置支持 OpenAI, Anthropic, Qwen, DeepSeek, Google Gemini, 智谱 (Zhipu), 新平台 Cerebras 等主流大模型厂商，通过简单的配置文件组合即可接入。
- **高级模型路由特性**：提供“模型备选（Fallback）”、“API 负载均衡”、配置多 Agent 各自跑不同模型接口的能力。同样也兼容 Ollama 等本地模型的接入。

### 4. 高级任务调度与异步执行

- **心跳系统 (Heartbeat)**：以 30 分钟为基础节奏（可配），后台 Agent 唤醒并读取 `HEARTBEAT.md` 文件内容主动执行任务。
- **异步子进程 (Spawn Subagent)**：支持拆分繁重的任务（如长文总结、网页爬取），生成一个不需要历史对话上下文包袱的新 Agent 去异步处理，完成后单独提醒用户，不阻塞主任务。
- **Cron 定时提醒系统**：支持用户用自然语言发号施令（如“每两小时提醒我喝水”），系统会解析为内部 Cron 任务，独立触发。

### 5. 跨平台聊天软件通信连接

支持作为 Bot 接入众多国内外聊天、协同软件。用户通过这些软件发送指令，可直接将功能调起：

- Telegram（支持通过 Groq/Whisper 附魔实现语音转文字）、Discord、Slack、Line。
- 钉钉 (DingTalk)、企业微信 (WeCom)、飞书 (Feishu)。
- QQ（官方 API），以及集成 OneBot/MaixCam 等硬件及协议支持。

---

## 二、 如何使用这些功能？

### 1. 安装与构建

您有多种方式让 PicoClaw 运行起来：

- **快速下载**：去项目的 [GitHub Releases](https://github.com/sipeed/picoclaw/releases) 下载适合您操作系统的预编译二进制文件。
- **源码编译**：
  ```bash
  git clone https://github.com/sipeed/picoclaw.git
  cd picoclaw
  make deps
  make build
  ```
- **Docker Compose**：方便地搭建和隔离环境，克隆代码后直接 `docker compose --profile gateway up -d` 即可。

### 2. 初始化与配置

第一次使用时：

1. 运行初始化指令：
   ```bash
   picoclaw onboard
   ```
2. 编辑在用户目录下的配置文件（通常为 `~/.picoclaw/config.json`）：
   配置你的大模型 API 密钥以及想要启用的通信渠道 Token：
   ```json
   {
     "model_list": [
       {
         "model_name": "deepseek-chat",
         "model": "deepseek/deepseek-chat",
         "api_key": "YOUR_DEEPSEEK_API_KEY"
       }
     ],
     "agents": { "defaults": { "model": "deepseek-chat" } }
   }
   ```

### 3. 命令行交互示例

如果你只想在终端与其交互：

- **单次提问**：
  ```bash
  picoclaw agent -m "帮我写一首关于春天的诗？"
  ```
- **持续对话（类似聊天界面）**：
  ```bash
  picoclaw agent
  ```

### 4. 配置定时与后台系统

- **使用 Cron 提醒器**：
  ```bash
  picoclaw cron add "remind me to stand up every 1 hour"
  picoclaw cron list  # 查看正在执行的后台命令
  ```
- **后台心跳轮询**：
  在 `~/.picoclaw/workspace/HEARTBEAT.md` 写入周期性任务（例如：“总结最新的 AI 相关新闻给用户”），然后启动网关服务：
  ```bash
  picoclaw gateway
  ```

PicoClaw 会作为后台守护进行管理所有日程、网络搜索和即时通信的接口回复。
