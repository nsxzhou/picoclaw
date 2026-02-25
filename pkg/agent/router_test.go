package agent

import (
	"context"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

// mockClassifyProvider 模拟分类用的 LLM provider
type mockClassifyProvider struct {
	response string // 返回 "simple" 或 "complex"
}

func (m *mockClassifyProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{Content: m.response}, nil
}

func (m *mockClassifyProvider) GetDefaultModel() string {
	return "mock-classify"
}

func TestRouteModel_Disabled(t *testing.T) {
	provider := &mockClassifyProvider{response: "complex"}

	// routing 为 nil
	result := RouteModel(context.Background(), provider, "写一段代码", nil)
	if result != "" {
		t.Errorf("Expected empty string when routing is nil, got %q", result)
	}

	// routing 未启用
	routing := &config.ModelRoutingConfig{
		Enabled:      false,
		SimpleModel:  "simple-model",
		ComplexModel: "complex-model",
	}
	result = RouteModel(context.Background(), provider, "写一段代码", routing)
	if result != "" {
		t.Errorf("Expected empty string when routing is disabled, got %q", result)
	}
}

func TestRouteModel_EmptyMessage(t *testing.T) {
	provider := &mockClassifyProvider{response: "simple"}
	routing := &config.ModelRoutingConfig{
		Enabled:      true,
		SimpleModel:  "flash",
		ComplexModel: "gpt5",
	}

	result := RouteModel(context.Background(), provider, "  ", routing)
	if result != "flash" {
		t.Errorf("Expected 'flash' for empty message, got %q", result)
	}
}

func TestRouteModel_SimpleTask(t *testing.T) {
	provider := &mockClassifyProvider{response: "simple"}
	routing := &config.ModelRoutingConfig{
		Enabled:      true,
		SimpleModel:  "gemini-flash",
		ComplexModel: "gpt-5.2-high",
	}

	result := RouteModel(context.Background(), provider, "你好，今天天气怎么样", routing)
	if result != "gemini-flash" {
		t.Errorf("Expected 'gemini-flash', got %q", result)
	}
}

func TestRouteModel_ComplexTask(t *testing.T) {
	provider := &mockClassifyProvider{response: "complex"}
	routing := &config.ModelRoutingConfig{
		Enabled:      true,
		SimpleModel:  "gemini-flash",
		ComplexModel: "gpt-5.2-high",
	}

	result := RouteModel(context.Background(), provider, "帮我用Python实现一个快速排序算法", routing)
	if result != "gpt-5.2-high" {
		t.Errorf("Expected 'gpt-5.2-high', got %q", result)
	}
}

func TestRouteModel_ComplexWithWhitespace(t *testing.T) {
	provider := &mockClassifyProvider{response: "  Complex  \n"}
	routing := &config.ModelRoutingConfig{
		Enabled:      true,
		SimpleModel:  "flash",
		ComplexModel: "big-model",
	}

	result := RouteModel(context.Background(), provider, "分析代码", routing)
	if result != "big-model" {
		t.Errorf("Expected 'big-model' for response with whitespace, got %q", result)
	}
}

// mockErrorProvider 模拟分类调用失败的 provider
type mockErrorProvider struct{}

func (m *mockErrorProvider) Chat(
	ctx context.Context,
	messages []providers.Message,
	tools []providers.ToolDefinition,
	model string,
	opts map[string]any,
) (*providers.LLMResponse, error) {
	return nil, context.DeadlineExceeded
}

func (m *mockErrorProvider) GetDefaultModel() string {
	return "mock-error"
}

func TestRouteModel_FallbackOnError(t *testing.T) {
	provider := &mockErrorProvider{}
	routing := &config.ModelRoutingConfig{
		Enabled:      true,
		SimpleModel:  "flash",
		ComplexModel: "big-model",
	}

	// 分类失败应降级到 SimpleModel
	result := RouteModel(context.Background(), provider, "任何消息", routing)
	if result != "flash" {
		t.Errorf("Expected fallback to 'flash' on error, got %q", result)
	}
}
