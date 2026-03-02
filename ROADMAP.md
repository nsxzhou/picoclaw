
# 🦐 PicoClaw 路线图

> **愿景**：打造终极的轻量级、安全、完全自治 AI Agent 基础设施——自动化琐事，释放你的创造力

---

## 🚀 1. 核心优化：极致轻量

*这是我们的标志性特征。我们持续对抗软件臃肿，确保 PicoClaw 能在最小型嵌入式设备上流畅运行。*

* [**内存占用缩减**](https://github.com/sipeed/picoclaw/issues/346)
  * **目标**：在 64MB RAM 的嵌入式开发板（例如低端 RISC-V SBC）上流畅运行，核心进程内存占用 < 20MB。
  * **背景**：在边缘设备上，RAM 昂贵且稀缺。内存优化优先级高于存储体积优化。
  * **行动**：分析版本间内存增长，移除冗余依赖，并优化数据结构。

## 🛡️ 2. 安全加固：纵深防御

*偿还早期技术债。我们邀请安全专家共同打造“默认安全（Secure-by-Default）”的 Agent。*

* **输入防护与权限控制**
  * **Prompt Injection 防御**：强化 JSON 提取逻辑，防止 LLM 被操纵。
  * **工具滥用防护**：严格校验参数，确保生成命令始终处于安全边界内。
  * **SSRF 防护**：为网络工具内置拦截列表，防止访问内网 IP（LAN/元数据服务）。

* **沙箱与隔离**
  * **文件系统沙箱**：将文件读写限制在指定目录内。
  * **上下文隔离**：防止不同用户会话或频道之间的数据泄露。
  * **隐私脱敏**：自动从日志和标准输出中脱敏敏感信息（API Key、PII）。

* **认证与密钥**
  * **加密升级**：采用 `ChaCha20-Poly1305` 等现代算法存储密钥。
  * **OAuth 2.0 流程**：在 CLI 中弃用硬编码 API Key，迁移到安全的 OAuth 流程。

## 🔌 3. 连接能力：协议优先架构

*连接每一个模型，触达每一个平台。*

* **Provider**
  * [**架构升级**](https://github.com/sipeed/picoclaw/issues/283)：将分类方式从“按厂商（Vendor-based）”重构为“按协议（Protocol-based）”（例如 OpenAI-compatible、Ollama-compatible）。*(状态：由 @Daming 推进中，预计 5 天)*
  * **本地模型**：深度集成 **Ollama**、**vLLM**、**LM Studio**、**Mistral**（本地推理）。
  * **在线模型**：持续支持前沿闭源模型。

* **Channel**
  * **IM 矩阵**：QQ、微信（企业微信）、钉钉、飞书（Lark）、Telegram、Discord、WhatsApp、LINE、Slack、Email、KOOK、Signal、...
  * **标准协议**：支持 **OneBot** 协议。
  * [**附件**](https://github.com/sipeed/picoclaw/issues/348)：原生处理图片、音频和视频附件。

* **Skill Marketplace**
  * [**技能发现**](https://github.com/sipeed/picoclaw/issues/287)：实现 `find_skill`，可从 [GitHub Skills Repo] 或其他注册中心自动发现并安装技能。

## 🧠 4. 高级能力：从 Chatbot 到 Agentic AI

*不止对话——更聚焦于行动与协作。*

* **操作能力**
  * [**MCP 支持**](https://github.com/sipeed/picoclaw/issues/290)：原生支持 **Model Context Protocol (MCP)**。
  * [**浏览器自动化**](https://github.com/sipeed/picoclaw/issues/293)：通过 CDP（Chrome DevTools Protocol）或 ActionBook 控制无头浏览器。
  * [**移动端操作**](https://github.com/sipeed/picoclaw/issues/292)：支持 Android 设备控制（类似 BotDrop）。

* **多 Agent 协作**
  * [**基础多 Agent**](https://github.com/sipeed/picoclaw/issues/294)：实现基础多 Agent 能力。
  * [**模型路由**](https://github.com/sipeed/picoclaw/issues/295)：“Smart Routing”——将简单任务分发到小型/本地模型（快/便宜），复杂任务分发到 SOTA 模型（更智能）。
  * [**Swarm 模式**](https://github.com/sipeed/picoclaw/issues/284)：支持同一网络内多个 PicoClaw 实例协作。
  * [**AIEOS**](https://github.com/sipeed/picoclaw/issues/296)：探索 AI-Native 操作系统交互范式。

## 📚 5. 开发者体验（DevEx）与文档

*降低上手门槛，让任何人都能在几分钟内部署。*

* [**QuickGuide（零配置启动）**](https://github.com/sipeed/picoclaw/issues/350)
  * 交互式 CLI 向导：若启动时无配置，自动检测环境并逐步引导完成 Token/网络设置。

* **完整文档体系**
  * **平台指南**：为 Windows、macOS、Linux、Android 提供专门指南。
  * **分步教程**：提供“保姆级” Provider 与 Channel 配置教程。
  * **AI 辅助文档**：使用 AI 自动生成 API 参考与代码注释（人工复核以防幻觉）。

## 🤖 6. 工程体系：AI 驱动的开源协作

*源于 Vibe Coding，我们将继续使用 AI 加速开发。*

* **AI 增强 CI/CD**
  * 集成 AI 用于自动 Code Review、Lint 与 PR Labeling。
  * **Bot 噪声治理**：优化机器人交互，保持 PR 时间线清晰。
  * **Issue 分诊**：使用 AI Agent 分析新 Issue 并给出初步修复建议。

## 🎨 7. 品牌与社区

* [**Logo 设计**](https://github.com/sipeed/picoclaw/issues/297)：我们正在征集 **虾蛄（Mantis Shrimp / Stomatopoda）** Logo 设计！
  * *概念*：体现“体积虽小，力量强大（Small but Mighty）”与“闪电式出击（Lightning Fast Strikes）”。

---

### 🤝 贡献邀请

欢迎社区为本路线图中的任意事项贡献力量！请在对应 Issue 下留言或直接提交 PR。一起打造最好的 Edge AI Agent！
