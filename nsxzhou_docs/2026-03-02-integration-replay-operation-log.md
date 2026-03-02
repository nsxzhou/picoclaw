# 2026-03-02 集成重放与冲突处理全过程记录

## 1. 背景与目标

- 背景：本地分支先提交了附件/多模态相关改动，随后拉取上游更新后出现冲突与报错。
- 目标：
  - 保留本地 5 个功能提交（attachments/feishu/agent/providers/i18n）。
  - 兼容上游最新架构调整（尤其是 channels 子包化、tools sandbox 变更）。
  - 以最佳实践完成集成，避免“硬改冲突”导致后续不可维护。
  - 最终在原工作分支 `main` 上得到可编译、可测试通过的结果。

## 2. 初始状态

- 仓库路径：`/Users/zhouzirui/code/picoclaw`
- 涉及主要分支：
  - 原工作分支：`main`
  - 备份分支：`backup/merge-conflict-20260302-121623`
  - 集成分支：`integration/replay-attachments`
  - 上游分支：`upstream/main`
- 上游关键新增提交（本次集成期间发现）：
  - `d5370c9` `fix(tools): allow /dev/null redirection and add read/write sandbox split (#967)`

## 3. 问题根因

## 3.1 直接拉上游后报错的根因

- 本地功能改动与上游同一时期对核心路径均有大改，冲突集中在：
  - `pkg/channels/*`（架构扁平文件改为子包）
  - `pkg/agent/*`（消息处理、上下文与媒体链路）
  - `pkg/providers/*`（多模态消息序列化）
  - `pkg/config/*`、`pkg/tools/*`（配置与沙箱行为）
- 冲突后若仅“按文件覆盖”，容易出现：
  - API/签名不一致
  - 旧文件残留（路径迁移后的遗留文件继续参与编译）
  - `go.sum` 校验项缺失，导致依赖解析失败

## 3.2 本次出现的具体报错类型

- `go.sum` 缺少附件解析相关依赖 checksum（`godocx/pdf/excelize` 等）。
- 全量测试阶段出现 `onboard` 用例失败：
  - 期望 `AGENTS.md`，但本地嵌入目录仍有遗留 `AGENT.md`。
- 在 `main` 回放时出现符号缺失：
  - `extractFeishuTextContent` / `extractFeishuImageKey` / `extractFeishuFileInfo`
  - `config.ModelRoutingConfig`
  - 原因是“新旧路径文件混入同一编译单元”。

## 4. 执行策略（最佳实践）

采用“先在干净基线上重放并验证，再回并原分支”的两阶段策略：

1. 阶段 A：在 `upstream/main` 基础建立集成分支，逐个重放本地功能提交并解冲突。
2. 阶段 B：在集成分支完成依赖修复与测试，通过后再合入 `main`。

核心原则：

- 以上游当前架构为主干（不回退上游结构演进）。
- 本地功能按“最小侵入、功能等价”重放。
- 每个关键节点做可执行验证（至少关键模块测试，最终全量测试）。

## 5. 阶段 A：在集成分支重放与修复

## 5.1 分支与提交重放

- 新建备份：`backup/merge-conflict-20260302-121623`
- 新建集成分支：`integration/replay-attachments`（基于 `upstream/main`）
- 将原 5 个提交重放为新提交（避免直接带入旧冲突上下文）：

| 原提交 | 新提交 | 说明 |
|---|---|---|
| `054f520` | `a8c8825` | attachments |
| `2a5e090` | `c1ba4e0` | feishu |
| `a628b39` | `d20dcc8` | agent |
| `709aec9` | `0b55ed2` | providers |
| `97c6ec4` | `e99a9c4` | i18n/docs |

## 5.2 冲突处理要点

- 保留上游 channels 子包架构，迁移本地功能到新路径。
- `pkg/channels/base.go`：保留上游消息入口签名，同时并入附件解析与 file-ref 处理能力。
- `pkg/channels/media.go`：保留上游接口，补齐图片编码与媒体处理辅助逻辑。
- Feishu 相关迁移到 `pkg/channels/feishu/` 子包，新增 resolver 及对应测试。
- `pkg/agent/loop.go`：合并上游流程与本地附件上下文/文件引用持久化能力。

## 5.3 上游新增提交并入

- 识别到分支与上游差异中存在新提交：`d5370c9`。
- 在集成分支先 cherry-pick，再执行 `rebase upstream/main`。
- rebase 时自动跳过等价 cherry-pick（`2d8bac5` 被识别为已应用），最终集成分支达到 `ahead 6, behind 0`。

## 5.4 依赖修复

- 执行：`go mod tidy`
- 生成依赖整理提交：`1a64790` `chore(deps): tidy module sums for attachment parsers`
- 关键补齐依赖项包括：
  - `github.com/gomutex/godocx`
  - `github.com/ledongthuc/pdf`
  - `github.com/xuri/excelize/v2`
  - 以及其传递依赖（`xuri/*`, `richardlehane/*`, `go-deepcopy` 等）

## 5.5 验证

- 关键模块测试通过：
  - `go test ./pkg/channels/... ./pkg/agent/... ./pkg/providers/...`
  - `go test ./pkg/tools ./pkg/config`
- 全量测试最初失败点：
  - `cmd/picoclaw/internal/onboard` 的 `AGENTS.md` 检查
  - 原因：本地未跟踪嵌入目录仍是 `AGENT.md` 历史文件
- 对齐后（本地目录修正）全量通过：
  - `go test ./...`

## 6. 阶段 B：将结果应用回原分支 `main`

## 6.1 首次尝试（失败）

- 方案：直接在 `main` 批量 cherry-pick 6 个新提交。
- 结果：冲突爆发。
- 原因：`main` 已有同主题旧提交（`054f520/2a5e090/a628b39/709aec9/97c6ec4`），重复回放导致“同功能不同历史”冲突。

处理：

- `git cherry-pick --abort`

## 6.2 第二次尝试

- `git merge --ff-only integration/replay-attachments`
- 结果：失败（历史分叉，无法快进）。

## 6.3 最终方案（成功）

- 在 `main` 执行普通 merge，并将冲突对齐到集成分支最终稳定结果。
- 对遗留旧路径文件进行清理，避免与新子包文件重复编译。
- 最终形成 merge 提交：
  - `3eae226` `merge: integrate upstream replay branch with attachment fixes`

## 6.4 关键修正动作

- 发现并移除不应存在于最终树中的遗留文件（导致符号缺失）：
  - `pkg/agent/router.go`
  - `pkg/agent/router_test.go`
  - `pkg/channels/feishu_64_test.go`
  - `pkg/channels/feishu_resolver.go`
  - `pkg/channels/feishu_resolver_test.go`
- 保持 `.ace-tool/` 为未跟踪，不纳入提交。

## 7. 关键时间线（北京时间 +0800）

- `12:16:37` 切到 `integration/replay-attachments`
- `12:27:57` 生成 `59dce6e`（attachments 重放早期哈希）
- `12:35:33` 生成 `f89732f`（feishu 重放早期哈希）
- `12:42:23` 生成 `baa4d64`（agent 重放早期哈希）
- `12:44:19` 生成 `f398288`（providers 重放早期哈希）
- `12:45:15` 生成 `ba1c638`（i18n 重放早期哈希）
- `12:53:24` cherry-pick 上游 `d5370c9`（早期等价哈希 `2d8bac5`）
- `12:54:46` 提交依赖整理（早期哈希 `d455d49`）
- `12:55:22` rebase 到上游后定型：
  - `a8c8825` / `c1ba4e0` / `d20dcc8` / `0b55ed2` / `e99a9c4` / `1a64790`
- `13:05:16` 在 `main` 完成最终 merge 提交：`3eae226`

## 8. 最终状态

- 当前分支：`main`
- 关键提交链：
  - `3eae226`（merge）
  - `1a64790`（deps tidy）
  - `e99a9c4`（i18n）
  - `0b55ed2`（providers）
  - `d20dcc8`（agent）
  - `c1ba4e0`（feishu）
  - `a8c8825`（attachments）
  - `d5370c9`（上游 tools 修复）
- 验证结果：
  - `go test ./...` 通过
- 工作区说明：
  - `.ace-tool/` 保持未跟踪（未提交）

## 9. 经验与后续建议

- 对“本地已有提交 + 上游同路径大改”的场景，优先采用：
  - 备份分支
  - 基于上游新基线重放（cherry-pick/rebase）
  - 最后再回并原分支
- 避免直接在原分支上叠加冲突修修补补，容易遗留旧文件与签名不一致问题。
- 冲突后务必执行两类验证：
  - 关键模块测试（快速反馈）
  - 全量测试（防止边角回归）
- 对路径迁移（如 `pkg/channels/*` 子包化）要特别检查“旧文件是否残留参与编译”。
