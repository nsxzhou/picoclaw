.PHONY: all build install uninstall clean help test

# 编译相关变量
BINARY_NAME=picoclaw
BUILD_DIR=build
CMD_DIR=cmd/$(BINARY_NAME)
MAIN_GO=$(CMD_DIR)/main.go

# 版本信息
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
GIT_COMMIT=$(shell git rev-parse --short=8 HEAD 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date +%FT%T%z)
GO_VERSION=$(shell $(GO) version | awk '{print $$3}')
INTERNAL=github.com/sipeed/picoclaw/cmd/picoclaw/internal
LDFLAGS=-ldflags "-X $(INTERNAL).version=$(VERSION) -X $(INTERNAL).gitCommit=$(GIT_COMMIT) -X $(INTERNAL).buildTime=$(BUILD_TIME) -X $(INTERNAL).goVersion=$(GO_VERSION) -s -w"

# Go 语言编译参数
GO?=CGO_ENABLED=0 go
GOFLAGS?=-v -tags stdjson

# Golangci-lint (代码检查工具)
GOLANGCI_LINT?=golangci-lint

# 安装路径配置
INSTALL_PREFIX?=$(HOME)/.local
INSTALL_BIN_DIR=$(INSTALL_PREFIX)/bin
INSTALL_MAN_DIR=$(INSTALL_PREFIX)/share/man/man1
INSTALL_TMP_SUFFIX=.new

# 工作区和技能目录
PICOCLAW_HOME?=$(HOME)/.picoclaw
WORKSPACE_DIR?=$(PICOCLAW_HOME)/workspace
WORKSPACE_SKILLS_DIR=$(WORKSPACE_DIR)/skills
BUILTIN_SKILLS_DIR=$(CURDIR)/skills

# 操作系统架构检测
UNAME_S:=$(shell uname -s)
UNAME_M:=$(shell uname -m)

# 针对不同平台的设置 (Platform-specific settings)
ifeq ($(UNAME_S),Linux)
	PLATFORM=linux
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),aarch64)
		ARCH=arm64
	else ifeq ($(UNAME_M),loongarch64)
		ARCH=loong64
	else ifeq ($(UNAME_M),riscv64)
		ARCH=riscv64
	else
		ARCH=$(UNAME_M)
	endif
else ifeq ($(UNAME_S),Darwin)
	PLATFORM=darwin
	ifeq ($(UNAME_M),x86_64)
		ARCH=amd64
	else ifeq ($(UNAME_M),arm64)
		ARCH=arm64
	else
		ARCH=$(UNAME_M)
	endif
else
	PLATFORM=$(UNAME_S)
	ARCH=$(UNAME_M)
endif

BINARY_PATH=$(BUILD_DIR)/$(BINARY_NAME)-$(PLATFORM)-$(ARCH)

# 默认执行目标
all: build

## generate: 运行 go generate (自动生成代码)
generate:
	@echo "正在运行 generate..."
	@rm -r ./$(CMD_DIR)/workspace 2>/dev/null || true
	@$(GO) generate ./...
	@echo "generate 运行完成"

## build: 构建适用于当前操作系统的 picoclaw 二进制文件
build: generate
	@echo "正在为 $(PLATFORM)/$(ARCH) 构建 $(BINARY_NAME) ..."
	@mkdir -p $(BUILD_DIR)
	@$(GO) build $(GOFLAGS) $(LDFLAGS) -o $(BINARY_PATH) ./$(CMD_DIR)
	@echo "构建完成: $(BINARY_PATH)"
	@ln -sf $(BINARY_NAME)-$(PLATFORM)-$(ARCH) $(BUILD_DIR)/$(BINARY_NAME)

## build-all: 交叉编译适用于多个操作系统的 picoclaw
build-all: generate
	@echo "正在为多个不同平台构建 (交叉编译)..."
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./$(CMD_DIR)
	GOOS=linux GOARCH=loong64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-loong64 ./$(CMD_DIR)
	GOOS=linux GOARCH=riscv64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-riscv64 ./$(CMD_DIR)
	GOOS=linux GOARCH=arm GOARM=7 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-armv7 ./$(CMD_DIR)
	GOOS=darwin GOARCH=arm64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./$(CMD_DIR)
	GOOS=windows GOARCH=amd64 $(GO) build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./$(CMD_DIR)
	@echo "所有平台构建完成"

## install: 安装 picoclaw 到系统目录并拷贝内置技能
install: build
	@echo "正在安装 $(BINARY_NAME)..."
	@mkdir -p $(INSTALL_BIN_DIR)
	# 使用临时后缀文件进行原子更新，避免文件被占用
	@cp $(BUILD_DIR)/$(BINARY_NAME) $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX)
	@chmod +x $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX)
	@mv -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)$(INSTALL_TMP_SUFFIX) $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "可执行文件已安装到: $(INSTALL_BIN_DIR)/$(BINARY_NAME)"
	@echo "安装完成！"

## uninstall: 从系统中安全移除 picoclaw 可执行文件
uninstall:
	@echo "正在卸载 $(BINARY_NAME)..."
	@rm -f $(INSTALL_BIN_DIR)/$(BINARY_NAME)
	@echo "已从 $(INSTALL_BIN_DIR)/$(BINARY_NAME) 移除二进制文件。"
	@echo "注意：仅移除了可执行文件。"
	@echo "如果您需要同时删除全部配置数据 (config.json, workspace 等), 请运行 'make uninstall-all'"

## uninstall-all: 移除 picoclaw 的可执行文件和所有目录数据
uninstall-all:
	@echo "正在移除工作区和技能数据..."
	@rm -rf $(PICOCLAW_HOME)
	@echo "已清理完毕工作区: $(PICOCLAW_HOME)"
	@echo "完全卸载已完成！"

## clean: 清理构建残留 (build 目录)
clean:
	@echo "正在清理编译残留产物..."
	@rm -rf $(BUILD_DIR)
	@echo "清理完成"

## vet: 运行 go vet 进行代码静态分析
vet:
	@$(GO) vet ./...

## test: 运行 Go 测试
test:
	@$(GO) test ./...

## fmt: 格式化所有 Go 代码
fmt:
	@$(GOLANGCI_LINT) fmt

## lint: 运行 golangci-lint 进行代码质量检查
lint:
	@$(GOLANGCI_LINT) run

## fix: 自动修复可通过 lint 工具修复的代码格式问题
fix:
	@$(GOLANGCI_LINT) run --fix

## deps: 下载项目所需的所有依赖包
deps:
	@$(GO) mod download
	@$(GO) mod verify

## update-deps: 更新所有的项目依赖项
update-deps:
	@$(GO) get -u ./...
	@$(GO) mod tidy

## check: 执行预检查环节: 包括安装依赖、格式化、静态分析以及测试
check: deps fmt vet test

## run: 立即构建并直接运行 picoclaw
run: build
	@$(BUILD_DIR)/$(BINARY_NAME) $(ARGS)

## help: 显示所有可用命令说明的帮助信息
help:
	@echo "picoclaw Makefile 帮助向导"
	@echo ""
	@echo "使用方法:"
	@echo "  make [指令目标]"
	@echo ""
	@echo "可用指令目标 (Targets):"
	@grep -E '^## ' $(MAKEFILE_LIST) | sort | awk -F': ' '{printf "  %-16s %s\n", substr($$1, 4), $$2}'
	@echo ""
	@echo "常用示例:"
	@echo "  make build              # 为当前平台构建可执行文件"
	@echo "  make install            # 安装可执行文件到 ~/.local/bin"
	@echo "  make uninstall          # 从系统中卸载移除可执行文件"
	@echo "  make check              # 运行全面的本地代码校验和测试"
	@echo "  make deps               # 同步下载代码依赖库缓存"
	@echo ""
	@echo "环境变量 (Environment Variables):"
	@echo "  INSTALL_PREFIX          # 安装默认前缀目录 (默认: ~/.local)"
	@echo "  WORKSPACE_DIR           # 工作区目录路径 (默认: ~/.picoclaw/workspace)"
	@echo "  VERSION                 # 自定义构建版本号 (默认: 根据当前 git describe获取)"
	@echo ""
	@echo "当前配置状态 (Current Configuration):"
	@echo "  系统平台 (Platform): $(PLATFORM)/$(ARCH)"
	@echo "  输出文件 (Binary): $(BINARY_PATH)"
	@echo "  安装路径 (Install Prefix): $(INSTALL_PREFIX)"
	@echo "  工作区 (Workspace): $(WORKSPACE_DIR)"
