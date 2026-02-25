// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT

package agent

import (
	"context"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// classifySystemPrompt 是分类用的系统提示，引导 LLM 只返回 simple/complex
const classifySystemPrompt = `你是一个任务难度分类器。根据用户的消息，判断这是一个"简单"还是"复杂"的任务。
简单任务：问候、闲聊、简单问答、翻译短句、提醒、常识问题、日常对话
复杂任务：编程/代码编写、数学推理、多步骤分析、架构设计、调试、数据分析、长篇创作、复杂逻辑推理
只回复一个词：simple 或 complex`

// RouteModel 使用简单模型分类任务难度，返回应使用的模型名。
// 如果分类调用失败，降级返回 SimpleModel（保守策略）。
func RouteModel(
	ctx context.Context,
	provider providers.LLMProvider,
	userMessage string,
	routing *config.ModelRoutingConfig,
) string {
	if routing == nil || !routing.Enabled {
		return ""
	}

	// 空消息直接走简单模型
	if strings.TrimSpace(userMessage) == "" {
		return routing.SimpleModel
	}

	// 构建分类请求：系统提示 + 用户消息
	messages := []providers.Message{
		{Role: "system", Content: classifySystemPrompt},
		{Role: "user", Content: userMessage},
	}

	// 使用较短的超时，避免分类调用阻塞太久
	classifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// 调用简单模型进行分类（不需要工具、低 token）
	resp, err := provider.Chat(classifyCtx, messages, nil, routing.SimpleModel, map[string]any{
		"max_tokens":  20,
		"temperature": 0.0,
	})

	if err != nil {
		logger.WarnCF("router", "分类调用失败，降级到简单模型", map[string]any{
			"error": err.Error(),
		})
		return routing.SimpleModel
	}

	// 解析分类结果
	result := strings.TrimSpace(strings.ToLower(resp.Content))
	if strings.Contains(result, "complex") {
		logger.InfoCF("router", "任务分类: complex → "+routing.ComplexModel, nil)
		return routing.ComplexModel
	}

	logger.InfoCF("router", "任务分类: simple → "+routing.SimpleModel, nil)
	return routing.SimpleModel
}
