package state

// 全局设置 / LLM 设置翻译测试。
// 上游无独立 vitest spec（07 §6.1 行为锚点=设置页手测 + K15 口径），本组用例锚定：
//   - globalSettings.ts：读失败/缺省按默认开启；写失败保留内存值；
//   - llmSettings.ts / packages/llm settings.ts llmSettingsFromRaw：缺省/越界
//     回落默认或 clamp（K15 口径，防错 #9）；枚举 index 存档与还原；
//   - 07 §5：load 时 clamp、渲染侧 fromRaw 兜底、键名 llm_settings_*。

import (
	"path/filepath"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

func openTestSettings(t *testing.T) *storage.Settings {
	t.Helper()
	st, err := storage.OpenSettings(filepath.Join(t.TempDir(), "settings.json"))
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	return st
}

// ---- globalSettings.ts ----

// 上游锚点: globalSettings.ts:18-27（缺省/读失败按默认开启，loaded 置位）
func TestGlobalSettings_DefaultTrueOnMissing(t *testing.T) {
	g := NewGlobalSettings(openTestSettings(t))
	g.Load()
	if !g.AutoSave {
		t.Fatal("缺省 autoSave 应为 true（07 §3 默认值）")
	}
	if !g.Loaded {
		t.Fatal("load 后 loaded 应置位")
	}
}

// 上游锚点: globalSettings.ts:30-37（setAutoSave 先改内存再落盘；读回一致）
func TestGlobalSettings_SetAutoSavePersists(t *testing.T) {
	st := openTestSettings(t)
	g := NewGlobalSettings(st)
	g.Load()
	g.SetAutoSave(false)
	if g.AutoSave {
		t.Fatal("内存值应立即更新")
	}
	// 新实例 load 读回持久化值（写失败保留内存值的分支由存储降级路径 M2' 手测承担）
	g2 := NewGlobalSettings(st)
	g2.Load()
	if g2.AutoSave {
		t.Fatal("重新 load 应读到持久化的 false")
	}
}

// 上游锚点: globalSettings.ts:22-27（读失败/存储不可用按默认开启）
func TestGlobalSettings_NilStorageFallsBackDefault(t *testing.T) {
	g := NewGlobalSettings(nil)
	g.Load()
	if !g.AutoSave || !g.Loaded {
		t.Fatalf("存储不可用应回默认开启+loaded，got %v/%v", g.AutoSave, g.Loaded)
	}
}

// ---- llmSettings.ts / packages/llm settings.ts llmSettingsFromRaw ----

// 上游锚点: settings.ts:54-67 DEFAULT_LLM_SETTINGS
func TestLlmSettings_Defaults(t *testing.T) {
	s := LlmSettingsFromRaw(map[string]any{})
	d := DefaultLlmSettings()
	if s != d {
		t.Fatalf("空 raw 应得默认值：got %+v want %+v", s, d)
	}
	if s.TimeoutSeconds != 60 || s.MaxAttempts != 3 || s.Fallback != llm.FallbackBuiltinAI ||
		s.IntervalSeconds != 1 || s.AdvisorMode != llm.AdvisorCandidate ||
		s.StrengthBlend != 50 || s.AdvisorDifficulty != 5 ||
		s.RedStrengthBlend != 50 || s.BlackStrengthBlend != 50 ||
		s.RedSideType != llm.SideEngineLLM || s.BlackSideType != llm.SideEngineLLM ||
		s.HumanVsLlmOpponentType != llm.SideEngineLLM {
		t.Fatalf("默认值字段不符：%+v", s)
	}
}

// 上游锚点: settings.ts:119-141 fromRaw clamp（K15 口径，防错 #9）
func TestLlmSettings_FromRawClampsOutOfRange(t *testing.T) {
	raw := map[string]any{
		llm.SettingKeyTimeoutSeconds:     0,    // < 5 → 5
		llm.SettingKeyMaxAttempts:        99,   // > 10 → 10
		llm.SettingKeyIntervalSeconds:    -3,   // < 0 → 0
		llm.SettingKeyStrengthBlend:      1000, // > 100 → 100
		llm.SettingKeyAdvisorDifficulty:  0,    // < 1 → 1
		llm.SettingKeyFallbackIndex:      7,    // 越界枚举 → 默认 builtinAi
		llm.SettingKeyAdvisorModeIndex:   42,   // 越界枚举 → 默认 candidate
		llm.SettingKeyRedSideType:        -1,   // 越界枚举 → 默认 llm
		llm.SettingKeyHumanVsLlmOpponent: 1.5,  // 非整数枚举 → 默认 llm
	}
	s := LlmSettingsFromRaw(raw)
	if s.TimeoutSeconds != 5 {
		t.Fatalf("timeoutSeconds = %d, want 5", s.TimeoutSeconds)
	}
	if s.MaxAttempts != 10 {
		t.Fatalf("maxAttempts = %d, want 10", s.MaxAttempts)
	}
	if s.IntervalSeconds != 0 {
		t.Fatalf("intervalSeconds = %d, want 0", s.IntervalSeconds)
	}
	if s.StrengthBlend != 100 {
		t.Fatalf("strengthBlend = %d, want 100", s.StrengthBlend)
	}
	if s.AdvisorDifficulty != 1 {
		t.Fatalf("advisorDifficulty = %d, want 1", s.AdvisorDifficulty)
	}
	if s.Fallback != llm.FallbackBuiltinAI {
		t.Fatalf("fallback = %q, want builtinAi", s.Fallback)
	}
	if s.AdvisorMode != llm.AdvisorCandidate {
		t.Fatalf("advisorMode = %q, want candidate", s.AdvisorMode)
	}
	if s.RedSideType != llm.SideEngineLLM {
		t.Fatalf("redSideType = %q, want llm", s.RedSideType)
	}
	if s.HumanVsLlmOpponentType != llm.SideEngineLLM {
		t.Fatalf("humanVsLlmOpponentType = %q, want llm", s.HumanVsLlmOpponentType)
	}
}

// 上游锚点: settings.ts:119-141（红黑强度缺省回落 strengthBlend 原始值，可能越界再 clamp）
func TestLlmSettings_RedBlackStrengthFallbackToStrengthBlend(t *testing.T) {
	s := LlmSettingsFromRaw(map[string]any{llm.SettingKeyStrengthBlend: 200})
	if s.StrengthBlend != 100 {
		t.Fatalf("strengthBlend = %d, want clamp 到 100", s.StrengthBlend)
	}
	if s.RedStrengthBlend != 100 || s.BlackStrengthBlend != 100 {
		t.Fatalf("红黑强度 = %d/%d, want 回落原始值 200 再 clamp 100", s.RedStrengthBlend, s.BlackStrengthBlend)
	}
	s2 := LlmSettingsFromRaw(map[string]any{llm.SettingKeyStrengthBlend: 30})
	if s2.RedStrengthBlend != 30 || s2.BlackStrengthBlend != 30 {
		t.Fatalf("红黑强度 = %d/%d, want 回落 30", s2.RedStrengthBlend, s2.BlackStrengthBlend)
	}
}

// 上游锚点: settings.ts:86-103 llmSettingsToMap（枚举转 index 整数；键为全名）
func TestLlmSettings_ToMapEnumIndexes(t *testing.T) {
	s := DefaultLlmSettings()
	m := LlmSettingsToMap(s)
	if m[llm.SettingKeyFallbackIndex] != 0 {
		t.Fatalf("builtinAi index = %d, want 0", m[llm.SettingKeyFallbackIndex])
	}
	if m[llm.SettingKeyAdvisorModeIndex] != 1 {
		t.Fatalf("candidate index = %d, want 1", m[llm.SettingKeyAdvisorModeIndex])
	}
	if m[llm.SettingKeyRedSideType] != 0 || m[llm.SettingKeyBlackSideType] != 0 || m[llm.SettingKeyHumanVsLlmOpponent] != 0 {
		t.Fatal("llm 引擎类型 index 应为 0")
	}
	if m[llm.SettingKeyTimeoutSeconds] != 60 || m[llm.SettingKeyStrengthBlend] != 50 {
		t.Fatal("数值键应原样写入")
	}
}

// 上游锚点: llmSettings.ts:15-31 loadLlmSettings（逐键读、缺省兜底）+ 34-58 saveLlmSettings
func TestLlmSettings_LoadSaveRoundtripAndFieldSubset(t *testing.T) {
	st := openTestSettings(t)
	got := LoadLlmSettings(st)
	if got != DefaultLlmSettings() {
		t.Fatalf("空库 load 应得默认值：got %+v", got)
	}
	// 写全部字段
	s := DefaultLlmSettings()
	s.TimeoutSeconds = 120
	s.MaxAttempts = 5
	s.Fallback = llm.FallbackResign
	s.IntervalSeconds = 7
	s.AdvisorMode = llm.AdvisorGate
	s.StrengthBlend = 80
	s.AdvisorDifficulty = 3
	s.RedStrengthBlend = 66
	s.BlackStrengthBlend = 44
	s.RedSideType = llm.SideEngineBuiltin
	s.BlackSideType = llm.SideEngineLLM
	s.HumanVsLlmOpponentType = llm.SideEngineBuiltin
	if err := SaveLlmSettings(st, s); err != nil {
		t.Fatalf("SaveLlmSettings: %v", err)
	}
	round := LoadLlmSettings(st)
	if round != s {
		t.Fatalf("roundtrip 不一致：got %+v want %+v", round, s)
	}
	// 指定字段子集写（页面回写只传自己的字段，防清空共享设置——llmSettings.ts:33-37）
	s2 := round
	s2.TimeoutSeconds = 300
	if err := SaveLlmSettings(st, s2, LlmFieldTimeoutSeconds); err != nil {
		t.Fatalf("字段子集保存: %v", err)
	}
	after := LoadLlmSettings(st)
	if after.TimeoutSeconds != 300 {
		t.Fatalf("timeoutSeconds = %d, want 300", after.TimeoutSeconds)
	}
	if after.MaxAttempts != 5 || after.Fallback != llm.FallbackResign || after.BlackStrengthBlend != 44 {
		t.Fatalf("其余字段应保持：%+v", after)
	}
}
