# PicoClaw Docker 部署到 OpenCloudOS 8 完整指南

本指南基于项目已有的 Docker 配置，详细描述从零开始将你的 fork 分支部署到 OpenCloudOS 8 服务器的全流程。

---

## 架构总览

```mermaid
graph LR
    A[本地 Mac<br/>拉取上游合并 & 开发] -->|git push| B[GitHub Fork<br/>nsxzhou/picoclaw]
    B -->|git pull| C[OpenCloudOS 8 服务器]
    C -->|docker compose build| D[Docker 镜像]
    D -->|docker compose up| E[picoclaw-gateway<br/>长驻运行]
```

项目提供两种 Docker 镜像：

| 镜像类型   | Dockerfile                                                                             | 基础镜像         | 特点                                    |
| ---------- | -------------------------------------------------------------------------------------- | ---------------- | --------------------------------------- |
| **精简版** | [docker/Dockerfile](file:///Users/zhouzirui/code/picoclaw/docker/Dockerfile)           | `alpine:3.23`    | 仅 picoclaw 二进制，体积小              |
| **完整版** | [docker/Dockerfile.full](file:///Users/zhouzirui/code/picoclaw/docker/Dockerfile.full) | `node:24-alpine` | 含 Node.js + Python + uv，支持 MCP 工具 |

> [!TIP]
> 如果你需要使用 MCP 工具（如 filesystem、github 等），选择**完整版**；否则选择**精简版**即可。

---

## 第一阶段：服务器环境准备

### 1.1 安装 Docker

OpenCloudOS 8 基于 CentOS Stream 8，可直接使用 CentOS 源安装 Docker：

```bash
# 安装依赖
sudo dnf install -y dnf-plugins-core

# 添加 Docker 官方仓库
sudo dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo

# 安装 Docker CE + Compose 插件
sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin

# 启动 Docker 并设置开机自启
sudo systemctl enable --now docker

# 验证安装
docker --version
docker compose version
```

### 1.2 配置 Docker（可选但推荐）

```bash
# 让当前用户无需 sudo 使用 docker
sudo usermod -aG docker $USER
newgrp docker   # 立即生效（或重新登录）

# 配置国内镜像加速（如果服务器在国内）
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json <<'EOF'
{
  "registry-mirrors": [
    "https://mirror.ccs.tencentyun.com",
    "https://docker.nju.edu.cn"
  ],
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  }
}
EOF
sudo systemctl restart docker
```

### 1.3 安装 Git

```bash
sudo dnf install -y git
```

---

## 第二阶段：拉取代码 & 配置

### 2.1 克隆你的 Fork 仓库

```bash
# 创建项目目录
sudo mkdir -p /opt/picoclaw
sudo chown $USER:$USER /opt/picoclaw

# 克隆你的 fork
git clone https://github.com/nsxzhou/picoclaw.git /opt/picoclaw
cd /opt/picoclaw
```

### 2.2 配置 config.json

```bash
# 复制示例配置
mkdir -p /opt/picoclaw/config
cp /opt/picoclaw/config/config.example.json /opt/picoclaw/config/config.json

# 编辑配置，填入你的 API Key 和频道设置
vim /opt/picoclaw/config/config.json
```

**必须修改的关键配置项**：

```json
{
  "model_list": [
    {
      "model_name": "你的模型名",
      "model": "openai/gpt-5.2",
      "api_key": "sk-你的真实key", // ← 填入真实 API Key
      "api_base": "https://api.openai.com/v1"
    }
  ],
  "channels": {
    "telegram": {
      "enabled": true, // ← 启用你需要的频道
      "token": "你的真实token", // ← 填入真实 Token
      "allow_from": ["你的用户ID"] // ← 限制允许的用户
    }
  },
  "gateway": {
    "host": "0.0.0.0", // ← 改为 0.0.0.0 以监听外部请求
    "port": 18790
  }
}
```

### 2.3 配置 .env （可选）

```bash
cp /opt/picoclaw/.env.example /opt/picoclaw/.env
vim /opt/picoclaw/.env
# 设置时区等环境变量
```

### 2.4 飞书用户态文档操作的额外准备（可选）

如果你计划在 Docker 部署里使用飞书文档 MCP，并启用这次新增的“单用户绑定 + CLI 授权”能力，需要额外完成以下准备。

#### 2.4.1 在配置文件中填入飞书应用凭据

编辑 `/opt/picoclaw/config/config.json`，确保至少包含：

```json
{
  "channels": {
    "feishu": {
      "enabled": true,
      "app_id": "cli_xxx",
      "app_secret": "xxx"
    }
  },
  "tools": {
    "mcp": {
      "enabled": true,
      "servers": {
        "feishu-doc": {
          "enabled": true,
          "command": "picoclaw",
          "args": ["mcp-feishu-doc", "serve"]
        }
      }
    }
  }
}
```

> [!TIP]
> 飞书文档 sidecar 需要使用 `channels.feishu.app_id` 和 `channels.feishu.app_secret`。如果你只使用飞书聊天，不使用文档工具，可以不启用 `feishu-doc` MCP。

#### 2.4.2 在飞书开放平台配置重定向 URL

在飞书开放平台应用配置页的“重定向 URL”中添加：

```text
http://127.0.0.1:1456/auth/callback
```

这是 Feishu 用户态 OAuth 登录固定使用的回调地址。即使你最终在服务器 Docker 场景下走“手动粘贴回调 URL / code”的兜底流程，这个地址也必须先在开放平台中配置，否则无法获取授权码。

#### 2.4.3 开通权限

至少确保应用已经开通：

- `docs:doc`
- `drive:drive`

这两个权限基本覆盖：

- 文档搜索 / 浏览根空间 / 浏览文件夹
- `docx` 读取
- `docx` 创建 / 更新 / 评论
- 分享设置
- 成员权限管理
- 删除文件

如果你的机器人本身也运行在飞书频道中，还需要保留飞书消息收发相关权限（如 `im:message` 等）。

---

## 第三阶段：构建 & 启动

### 3.1 精简版部署（推荐大多数场景）

```bash
cd /opt/picoclaw

# 构建镜像（首次较慢，后续会利用缓存）
docker compose -f docker/docker-compose.yml build

# 启动 gateway 服务（后台运行）
docker compose -f docker/docker-compose.yml --profile gateway up -d
```

> [!NOTE]
> 精简版使用 [docker/Dockerfile](file:///Users/zhouzirui/code/picoclaw/docker/Dockerfile)，基于 `alpine:3.23`，最终镜像约 30-50MB。
> 但精简版 [docker-compose.yml](file:///Users/zhouzirui/code/picoclaw/docker/docker-compose.yml) 默认使用预构建镜像 `docker.io/sipeed/picoclaw:latest`。
> 你需要修改它以使用本地构建（见下方 3.3 节）。

### 3.2 完整版部署（需要 MCP 支持）

```bash
cd /opt/picoclaw

# 构建完整版镜像
docker compose -f docker/docker-compose.full.yml build

# 启动
docker compose -f docker/docker-compose.full.yml --profile gateway up -d
```

> [!IMPORTANT]
> 当前仓库里的 [docker/docker-compose.full.yml](file:///Users/zhouzirui/code/picoclaw/docker/docker-compose.full.yml) 只持久化了 `workspace`，**不会自动持久化 `~/.picoclaw/auth.json`**。如果你要使用 Feishu 用户态绑定，必须额外持久化 `auth.json`，否则容器重建后登录状态会丢失。

#### 3.2.1 为 Feishu 用户态登录持久化 `auth.json`

先在宿主机创建认证文件：

```bash
mkdir -p /opt/picoclaw/data
touch /opt/picoclaw/data/auth.json
chmod 600 /opt/picoclaw/data/auth.json
```

然后修改 `docker/docker-compose.full.yml`，为 `picoclaw-agent` 和 `picoclaw-gateway` 都增加 `auth.json` 挂载：

```diff
 services:
   picoclaw-agent:
     volumes:
       - ../config/config.json:/root/.picoclaw/config.json:ro
+      - ../data/auth.json:/root/.picoclaw/auth.json
       - picoclaw-workspace:/root/.picoclaw/workspace
       - picoclaw-npm-cache:/root/.npm
 
   picoclaw-gateway:
     volumes:
       - ../config/config.json:/root/.picoclaw/config.json:ro
+      - ../data/auth.json:/root/.picoclaw/auth.json
       - picoclaw-workspace:/root/.picoclaw/workspace
       - picoclaw-npm-cache:/root/.npm
```

> [!NOTE]
> `config.json` 继续保持只读挂载，`auth.json` 单独持久化即可。这样既能保住登录态，也不会把认证数据提交进 git。

#### 3.2.2 在服务器 Docker 中完成 Feishu 绑定

Feishu 登录在服务器 Docker 场景下**推荐使用 CLI + 手动粘贴回调 URL / code**，不要依赖自动 localhost 回调。

原因是 OAuth 回调地址固定为：

```text
http://127.0.0.1:1456/auth/callback
```

当 PicoClaw 运行在服务器容器中，而你在本地电脑浏览器完成授权时，浏览器跳转到的 `127.0.0.1` 指向的是**你本地电脑**，不是服务器容器，因此自动回调通常不会成功。

正确流程如下：

```bash
cd /opt/picoclaw

# 确保容器已启动
docker compose -f docker/docker-compose.full.yml --profile gateway up -d

# 进入容器
docker exec -it picoclaw-gateway-full sh

# 在容器内执行登录
picoclaw auth login --provider feishu
```

执行后，终端会打印一个飞书授权 URL。接下来：

1. 将授权 URL 复制到你本地浏览器打开。
2. 在浏览器中完成飞书授权。
3. 浏览器最终会跳转到一个类似下面的地址：

```text
http://127.0.0.1:1456/auth/callback?code=xxx&state=yyy
```

4. 这一步在本地浏览器中可能显示无法打开，这是正常现象。
5. 将浏览器地址栏中的**完整回调 URL**复制出来，粘贴回容器终端。
6. 也可以只粘贴 `code`，但推荐粘贴完整 URL。

登录成功后检查：

```bash
picoclaw auth status
```

确认输出中已经出现 Feishu 绑定信息以及对应的 `scope`。

#### 3.2.3 运行时行为说明

Feishu 文档工具启用后，Docker 中的行为与本地运行一致：

- 没有 `feishu-user` 绑定时，继续走 app-mode
- 已绑定且当前飞书发送者命中绑定身份时，走 user-mode
- 已绑定但当前发送者不是绑定用户时，直接拒绝，不会回退到 app-mode

这是 v1 的“单用户绑定实例”约束。

### 3.3 修改 docker-compose.yml 以支持本地构建

项目自带的 [docker/docker-compose.yml](file:///Users/zhouzirui/code/picoclaw/docker/docker-compose.yml) 默认拉取远程镜像 `docker.io/sipeed/picoclaw:latest`，这是**上游发布的镜像**，不包含你的修改。你需要改为本地构建：

```diff
 services:
   picoclaw-gateway:
-    image: docker.io/sipeed/picoclaw:latest
+    build:
+      context: ..
+      dockerfile: docker/Dockerfile
     container_name: picoclaw-gateway
     restart: on-failure
     profiles:
       - gateway
     volumes:
-      - ./data:/root/.picoclaw
+      - ../config/config.json:/root/.picoclaw/config.json:ro
+      - picoclaw-data:/root/.picoclaw/workspace
+
+volumes:
+  picoclaw-data:
```

> [!IMPORTANT]
> 或者你可以直接使用完整版 [docker-compose.full.yml](file:///Users/zhouzirui/code/picoclaw/docker/docker-compose.full.yml)，它已经配置好了本地构建，推荐直接使用。

### 3.4 验证服务状态

```bash
# 查看容器运行状态
docker compose -f docker/docker-compose.full.yml ps

# 查看日志
docker compose -f docker/docker-compose.full.yml logs -f picoclaw-gateway

# 健康检查（精简版 Dockerfile 已内置）
curl -s http://localhost:18790/health
```

---

## 第四阶段：日常更新流程

这是你**最常用的操作**——在本地合并上游更新后，在服务器上重新部署。

**这种“本地合并，服务器只发布”的模式是最佳实践，可以避免在服务器上处理合并冲突。**

### 4.1 在本地电脑（Mac）合并更新并推送

在你的 Mac 上执行：

```bash
# 进入你的本地仓库
cd /path/to/your/local/picoclaw

# 如果还没有添加上游仓库，只需执行一次
git remote add upstream https://github.com/sipeed/picoclaw.git

# 拉取上游最新代码
git fetch upstream

# 合并上游 main 分支到你当前的分支
git merge upstream/main

# 如果有冲突，在本地使用你熟悉的 IDE（如 VSCode）解决冲突
# 解决完毕后提交并推送到你的 GitHub fork
git push origin main
```

### 4.2 在服务器上拉取并部署

在 OpenCloudOS 8 服务器上执行：

```bash
cd /opt/picoclaw

# 拉取你的最新代码
git pull origin main
```

### 4.2 重新构建 & 重启

```bash
# 重新构建镜像（Docker 会利用缓存，只重新编译变更部分）
docker compose -f docker/docker-compose.full.yml build

# 重启服务（零停机方式）
docker compose -f docker/docker-compose.full.yml --profile gateway up -d --build --force-recreate
```

### 4.3 一键更新脚本

创建 `/opt/picoclaw/deploy.sh`：

```bash
#!/bin/bash
set -e

COMPOSE_FILE="docker/docker-compose.full.yml"
cd /opt/picoclaw

echo "=== 拉取最新代码 ==="
git pull origin main

echo "=== 重新构建镜像 ==="
docker compose -f $COMPOSE_FILE build

echo "=== 重启服务 ==="
docker compose -f $COMPOSE_FILE --profile gateway up -d --force-recreate

echo "=== 等待健康检查 ==="
sleep 5
docker compose -f $COMPOSE_FILE ps

echo "✅ 部署完成！"
```

```bash
chmod +x /opt/picoclaw/deploy.sh

# 以后更新只需：
./deploy.sh
```

---

## 第五阶段：运维管理

### 5.1 用 systemd 管理 Docker Compose 服务

创建 `/etc/systemd/system/picoclaw.service`：

```ini
[Unit]
Description=PicoClaw Gateway (Docker Compose)
Requires=docker.service
After=docker.service

[Service]
Type=oneshot
RemainAfterExit=yes
WorkingDirectory=/opt/picoclaw
ExecStart=/usr/bin/docker compose -f docker/docker-compose.full.yml --profile gateway up -d
ExecStop=/usr/bin/docker compose -f docker/docker-compose.full.yml --profile gateway down
TimeoutStartSec=0

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable picoclaw
sudo systemctl start picoclaw

# 服务器重启后自动启动
```

### 5.2 日志管理

```bash
# 实时查看日志
docker compose -f docker/docker-compose.full.yml logs -f

# 查看最近 100 行
docker compose -f docker/docker-compose.full.yml logs --tail=100

# Docker 日志自动轮转（已在 daemon.json 中配置）
```

### 5.3 数据备份

```bash
# 备份配置
cp /opt/picoclaw/config/config.json /opt/picoclaw/config/config.json.bak

# 备份 Feishu / OAuth 登录态（如果使用了 auth login）
cp /opt/picoclaw/data/auth.json /opt/picoclaw/data/auth.json.bak

# 备份 Docker volumes 数据
docker run --rm \
  -v picoclaw-workspace:/data \
  -v /opt/backup:/backup \
  alpine tar czf /backup/picoclaw-workspace-$(date +%Y%m%d).tar.gz -C /data .
```

### 5.4 回滚

```bash
cd /opt/picoclaw

# 查看 git 历史
git log --oneline -10

# 回滚到指定版本
git checkout <commit-hash>

# 重新构建 & 重启
docker compose -f docker/docker-compose.full.yml build
docker compose -f docker/docker-compose.full.yml --profile gateway up -d --force-recreate
```

### 5.5 清理旧镜像

```bash
# 清理悬空镜像（每次 build 后产生的旧层）
docker image prune -f

# 更激进的清理
docker system prune -f
```

---

## 防火墙配置

OpenCloudOS 8 默认使用 `firewalld`：

```bash
# 如果 gateway 需要外部访问（如 webhook 回调）
sudo firewall-cmd --permanent --add-port=18790/tcp
sudo firewall-cmd --reload

# 如果不需要外部直接访问，保持默认即可
```

---

## 常见问题

### Q: 构建时 `go mod download` 很慢？

设置 Go 代理（在 Dockerfile 中添加）：

```dockerfile
ENV GOPROXY=https://goproxy.cn,direct
```

### Q: Docker Compose 找不到 `--profile` 参数？

确保 Docker Compose V2（`docker compose`），不是 V1（`docker-compose`）。

### Q: 端口 18790 被占用？

```bash
sudo ss -tlnp | grep 18790
# 修改 config.json 中的 gateway.port
```

### Q: 如何在不同环境使用不同配置？

将 `config.json` 放在 Docker volume 外部挂载，不提交到 git：

```bash
# config.json 已被 .gitignore 忽略（config/ 在 .dockerignore 中）
```
