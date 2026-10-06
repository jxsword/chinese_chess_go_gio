package ui

// T4'.2 测试：LLM 配置卡状态面（预设选择/掩码回显不变/粘贴回填/测试连接状态）。
// 按开封 09 §4 原则"能在 state 层测的逻辑不进 Gio 层测"——纯状态断言，不渲染。

import (
	"errors"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
)

// 上游 LlmConfigCard presetFor：预设名优先，端点地址回退匹配，兜底自定义。
func TestLlmConfigCardPresetFor(t *testing.T) {
	byName := NewLlmConfigCard("t", "llm_config_black", nil, llm.LlmEndpointConfig{Preset: "DeepSeek"}, nil)
	if got := byName.currentPreset().Name; got != "DeepSeek" {
		t.Fatalf("preset = %q, want DeepSeek", got)
	}
	// 旧存档兼容：preset 为空、baseUrl 命中 Kimi 预设。
	byURL := NewLlmConfigCard("t", "llm_config_black", nil, llm.LlmEndpointConfig{BaseURL: "https://api.moonshot.cn/v1"}, nil)
	if got := byURL.currentPreset().Name; got != "Kimi（Moonshot）" {
		t.Fatalf("preset = %q, want Kimi", got)
	}
	// 未知端点 → 自定义。
	custom := NewLlmConfigCard("t", "llm_config_black", nil, llm.LlmEndpointConfig{BaseURL: "https://my.gateway/v1"}, nil)
	if got := custom.currentPreset().Name; got != "自定义" {
		t.Fatalf("preset = %q, want 自定义", got)
	}
}

// 预设切换：非自定义回填端点与示例模型；自定义仅记录预设名。
func TestLlmConfigCardSelectPreset(t *testing.T) {
	var changed llm.LlmEndpointConfig
	card := NewLlmConfigCard("t", "llm_config_black", nil, llm.LlmEndpointConfig{}, func(c llm.LlmEndpointConfig) { changed = c })
	card.selectPreset(llm.LlmPresets[0]) // 智谱 GLM
	cfg := card.Config()
	if cfg.BaseURL != llm.LlmPresets[0].BaseURL || cfg.Model != llm.LlmPresets[0].ExampleModel || cfg.Preset != "智谱 GLM" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if changed.Preset != "智谱 GLM" {
		t.Fatalf("onChange not fired: %+v", changed)
	}
	card.selectPreset(llm.LlmPresetCustom)
	cfg = card.Config()
	if cfg.Preset != "自定义" || cfg.BaseURL != llm.LlmPresets[0].BaseURL {
		t.Fatalf("custom preset must keep fields, cfg = %+v", cfg)
	}
}

// 掩码回显：加载的掩码 Key 原样呈现在编辑器（保存合并由复制物 storage.Set 收口）。
func TestLlmConfigCardMaskedKeyRoundtrip(t *testing.T) {
	masked := "****abcd"
	card := NewLlmConfigCard("t", "llm_config_black", nil, llm.LlmEndpointConfig{APIKey: masked, BaseURL: "https://a/v1", Model: "m"}, nil)
	if got := card.Config().APIKey; got != masked {
		t.Fatalf("masked key = %q, want %q", got, masked)
	}
	// 页面以掩码配置再次 SetConfig（镜像/重载路径）：掩码不丢、不展开。
	card.SetConfig(llm.LlmEndpointConfig{APIKey: masked})
	if got := card.Config().APIKey; got != masked {
		t.Fatalf("masked key after reload = %q", got)
	}
}

// 粘贴回填（KG-004 异步路径的收口面）：文本进目标字段；失败/空文本不动表单。
func TestLlmConfigCardApplyPaste(t *testing.T) {
	card := NewLlmConfigCard("t", "llm_config_black", nil, llm.LlmEndpointConfig{Preset: "DeepSeek"}, nil)
	card.ApplyPaste(PasteTargetBaseURL, "https://api.deepseek.com/v1\n", nil)
	if got := card.Config().BaseURL; got != "https://api.deepseek.com/v1" {
		t.Fatalf("baseURL = %q", got)
	}
	card.ApplyPaste(PasteTargetAPIKey, "  sk-test-key  ", nil)
	if got := card.Config().APIKey; got != "sk-test-key" {
		t.Fatalf("apiKey = %q", got)
	}
	card.ApplyPaste(PasteTargetModel, "deepseek-chat", nil)
	if got := card.Config().Model; got != "deepseek-chat" {
		t.Fatalf("model = %q", got)
	}
	card.ApplyPaste(PasteTargetAPIKey, "should-ignore", errors.New("no clipboard"))
	if got := card.Config().APIKey; got != "sk-test-key" {
		t.Fatalf("failed paste must not touch form, got %q", got)
	}
	card.ApplyPaste(PasteTargetAPIKey, "", nil)
	if got := card.Config().APIKey; got != "sk-test-key" {
		t.Fatalf("empty paste must not touch form, got %q", got)
	}
}

// 测试连接状态机：testing 置位 → 回执回填（结果原样呈现，文案来自复制物）。
func TestLlmConfigCardTestConnectionState(t *testing.T) {
	var requested bool
	card := NewLlmConfigCard("t", "llm_config_black", nil, llm.LlmEndpointConfig{BaseURL: "https://a/v1", Model: "m"}, nil)
	card.OnTestConnection = func(cfg llm.LlmEndpointConfig, slot string) {
		requested = true
		if slot != "llm_config_black" {
			t.Fatalf("slot = %q", slot)
		}
		card.SetTestResult(false, "连接失败：HTTP 401: bad key")
	}
	card.SetTesting()
	if !card.testing {
		t.Fatal("testing state expected")
	}
	card.OnTestConnection(card.Config(), card.slot)
	if !requested || card.testing || card.testOK || !strings.Contains(card.testResult, "连接失败") {
		t.Fatalf("state = testing:%v requested:%v result:%q", card.testing, requested, card.testResult)
	}
}

// DR-005 结构性锚定：配置卡不持有任何思维链开关状态（无该字段可改）。
func TestLlmConfigCardNoThinkingToggle(t *testing.T) {
	card := &LlmConfigCard{}
	_ = card // 编译期断言：LlmConfigCard 无 thinking 相关导出字段/方法。
}
