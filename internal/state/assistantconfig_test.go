package state

// 翻译测试：上游 frontend/test/studio/assistantConfig.spec.ts（DR-009）——
// 每个 it 用例逐条翻译（DR-G002 纪律，用例名注释保留 spec 对应关系）。

import (
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
)

// spec: const cfg = (model) => ({ baseUrl: 'https://api.example.com/v1', apiKey: '****abcd', model, preset: ” })
func assistantCfg(model string) *llm.LlmEndpointConfig {
	return &llm.LlmEndpointConfig{BaseURL: "https://api.example.com/v1", APIKey: "****abcd", Model: model}
}

// spec: const EMPTY = { baseUrl: ”, apiKey: ”, model: ”, preset: ” }
var assistantEmpty = &llm.LlmEndpointConfig{}

func ptrCfg(c *llm.LlmEndpointConfig) *llm.LlmEndpointConfig { return c }

// spec: '助手槽全空 + 黑方有配置 → 借用黑方（authSlot 随黑槽）'
func TestResolveAssistantConfigBorrowBlack(t *testing.T) {
	r := ResolveAssistantConfig(assistantEmpty, ptrCfg(assistantCfg("glm-4-flash")), nil)
	if r.Source != AssistantSourceBlack {
		t.Fatalf("source = %q, want black", r.Source)
	}
	if r.Config == nil || r.Config.Model != "glm-4-flash" {
		t.Fatalf("config = %+v, want glm-4-flash", r.Config)
	}
	if r.AuthSlot != AssistantSlotBlack {
		t.Fatalf("authSlot = %q, want %q", r.AuthSlot, AssistantSlotBlack)
	}
}

// spec: '助手槽全空 + 黑空红有 → 借用红方'
func TestResolveAssistantConfigBorrowRed(t *testing.T) {
	r := ResolveAssistantConfig(assistantEmpty, nil, ptrCfg(assistantCfg("deepseek-chat")))
	if r.Source != AssistantSourceRed {
		t.Fatalf("source = %q, want red", r.Source)
	}
	if r.Config == nil || r.Config.Model != "deepseek-chat" {
		t.Fatalf("config = %+v, want deepseek-chat", r.Config)
	}
	if r.AuthSlot != AssistantSlotRed {
		t.Fatalf("authSlot = %q, want %q", r.AuthSlot, AssistantSlotRed)
	}
}

// spec: '黑红都有 → 黑方优先'
func TestResolveAssistantConfigBlackPriority(t *testing.T) {
	r := ResolveAssistantConfig(nil, ptrCfg(assistantCfg("black-model")), ptrCfg(assistantCfg("red-model")))
	if r.Source != AssistantSourceBlack {
		t.Fatalf("source = %q, want black", r.Source)
	}
	if r.Config == nil || r.Config.Model != "black-model" {
		t.Fatalf("config = %+v, want black-model", r.Config)
	}
}

// spec: '三槽全空 → null（维持"请先配置"提示，不产生 authSlot）'
func TestResolveAssistantConfigAllEmpty(t *testing.T) {
	r := ResolveAssistantConfig(assistantEmpty, assistantEmpty, assistantEmpty)
	if r.Source != "" {
		t.Fatalf("source = %q, want empty", r.Source)
	}
	if r.Config != nil {
		t.Fatalf("config = %+v, want nil", r.Config)
	}
	if r.AuthSlot != "" {
		t.Fatalf("authSlot = %q, want empty", r.AuthSlot)
	}
}

// spec: '三槽全 null（secure.get 未配置语义）→ null'
func TestResolveAssistantConfigAllNil(t *testing.T) {
	r := ResolveAssistantConfig(nil, nil, nil)
	if r.Source != "" || r.Config != nil {
		t.Fatalf("got %+v, want zero value", r)
	}
}

// spec: '助手槽部分填写（如只有 model）→ 独立配置不借用（DR-012 边界）'
func TestResolveAssistantConfigPartialAssistant(t *testing.T) {
	partial := &llm.LlmEndpointConfig{Model: "glm-4-flash"}
	r := ResolveAssistantConfig(partial, ptrCfg(assistantCfg("black-model")), ptrCfg(assistantCfg("red-model")))
	if r.Source != AssistantSourceAssistant {
		t.Fatalf("source = %q, want assistant", r.Source)
	}
	if r.Config != partial {
		t.Fatalf("config must be the assistant config itself (pointer identity)")
	}
	if r.AuthSlot != AssistantSlotAssistant {
		t.Fatalf("authSlot = %q, want %q", r.AuthSlot, AssistantSlotAssistant)
	}
}

// spec: '助手槽完整 → 原样返回自身（优先于任何对战配置）'
func TestResolveAssistantConfigFullAssistant(t *testing.T) {
	assistant := assistantCfg("qwen-vl-max")
	r := ResolveAssistantConfig(ptrCfg(assistant), ptrCfg(assistantCfg("black-model")), ptrCfg(assistantCfg("red-model")))
	if r.Source != AssistantSourceAssistant {
		t.Fatalf("source = %q, want assistant", r.Source)
	}
	if r.Config == nil || r.Config.Model != "qwen-vl-max" {
		t.Fatalf("config = %+v, want qwen-vl-max", r.Config)
	}
	if r.AuthSlot != AssistantSlotAssistant {
		t.Fatalf("authSlot = %q, want %q", r.AuthSlot, AssistantSlotAssistant)
	}
}

// spec: assistantSourceLabel——'黑/红显示名；assistant/null 空串'
func TestAssistantSourceLabel(t *testing.T) {
	if got := AssistantSourceLabel(AssistantSourceBlack); got != "黑方" {
		t.Fatalf("black label = %q", got)
	}
	if got := AssistantSourceLabel(AssistantSourceRed); got != "红方" {
		t.Fatalf("red label = %q", got)
	}
	if got := AssistantSourceLabel(AssistantSourceAssistant); got != "" {
		t.Fatalf("assistant label = %q", got)
	}
	if got := AssistantSourceLabel(""); got != "" {
		t.Fatalf("null label = %q", got)
	}
}

// 借用语义补充锚定（05 §6/§7："永不写入助手槽"）：ResolveAssistantConfig 为纯内存
// 解析——连续调用不产生状态，无法"落盘"；此处以幂等性锚定无副作用。
func TestResolveAssistantConfigPure(t *testing.T) {
	first := ResolveAssistantConfig(assistantEmpty, ptrCfg(assistantCfg("m1")), nil)
	second := ResolveAssistantConfig(assistantEmpty, ptrCfg(assistantCfg("m2")), nil)
	if first.Source != second.Source {
		t.Fatalf("resolve must be stateless")
	}
	if second.Config.Model != "m2" {
		t.Fatalf("second resolve must see latest black config")
	}
}
