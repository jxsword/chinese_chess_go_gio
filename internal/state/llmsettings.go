package state

// LLM 对局设置（翻译源 = 上游 frontend/src/stores/llmSettings.ts 58 行 +
// frontend/src/packages/llm/settings.ts fromRaw/toMap；05 文档 §9 +
// llm_settings.dart:121-169）。
//
// 非敏感项，经复制物 storage.Settings 落盘（JSON 配置，键名 llm_settings_ 原样保留，
// 07 §5）；枚举以 index 整数存档，缺省/越界回落默认（fromRaw 兜底，K15 口径，
// 防错 #9）；数值越界一律 clamp（timeout 0 会让每次调用秒失败、maxAttempts 0
// 会跳过全部重试）。
//
// 枚举类型与键名常量复用复制物 internal/llm/settings.go（零改动纪律）。

import (
	"math"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// LlmGameSettings LLM 对局设置（settings.ts LlmGameSettings 同名字段）。
type LlmGameSettings struct {
	TimeoutSeconds         int             // 空闲超时（秒），5–600
	MaxAttempts            int             // 无效回复最大请求次数（含首次），1–10
	Fallback               llm.LlmFallback // 模型持续失败时的降级策略
	IntervalSeconds        int             // 大模型对战每手之间的等待秒数，0–60
	AdvisorMode            llm.AdvisorMode // 引擎参谋模式
	StrengthBlend          int             // 棋力旋钮 0~100
	AdvisorDifficulty      int             // 参谋引擎搜索深度档（1~5）
	RedStrengthBlend       int             // 大模型对战中红方的参谋强度（仅 llm_vs_llm）
	BlackStrengthBlend     int             // 大模型对战中黑方的参谋强度
	RedSideType            llm.SideEngineType
	BlackSideType          llm.SideEngineType
	HumanVsLlmOpponentType llm.SideEngineType
}

// DefaultLlmSettings 默认值（settings.ts DEFAULT_LLM_SETTINGS）。
func DefaultLlmSettings() LlmGameSettings {
	return LlmGameSettings{
		TimeoutSeconds:         60,
		MaxAttempts:            3,
		Fallback:               llm.FallbackBuiltinAI,
		IntervalSeconds:        1,
		AdvisorMode:            llm.AdvisorCandidate,
		StrengthBlend:          50,
		AdvisorDifficulty:      5,
		RedStrengthBlend:       50,
		BlackStrengthBlend:     50,
		RedSideType:            llm.SideEngineLLM,
		BlackSideType:          llm.SideEngineLLM,
		HumanVsLlmOpponentType: llm.SideEngineLLM,
	}
}

// llmFallbackValues / advisorModeValues / sideEngineTypeValues 枚举取值表
// （settings.ts LLM_FALLBACK_VALUES / ADVISOR_MODE_VALUES / SIDE_ENGINE_TYPE_VALUES）。
var (
	llmFallbackValues    = []llm.LlmFallback{llm.FallbackBuiltinAI, llm.FallbackResign}
	advisorModeValues    = []llm.AdvisorMode{llm.AdvisorOff, llm.AdvisorCandidate, llm.AdvisorGate}
	sideEngineTypeValues = []llm.SideEngineType{llm.SideEngineLLM, llm.SideEngineBuiltin}
)

// LlmSettingsToMap toMap()（llm_settings.dart:72-82）：枚举转 index 整数；
// 键为持久化全名（settings.ts LLM_SETTING_KEYS）。
func LlmSettingsToMap(s LlmGameSettings) map[string]int {
	return map[string]int{
		llm.SettingKeyTimeoutSeconds:     s.TimeoutSeconds,
		llm.SettingKeyMaxAttempts:        s.MaxAttempts,
		llm.SettingKeyFallbackIndex:      indexOfFallback(s.Fallback),
		llm.SettingKeyIntervalSeconds:    s.IntervalSeconds,
		llm.SettingKeyAdvisorModeIndex:   indexOfAdvisorMode(s.AdvisorMode),
		llm.SettingKeyStrengthBlend:      s.StrengthBlend,
		llm.SettingKeyAdvisorDifficulty:  s.AdvisorDifficulty,
		llm.SettingKeyRedStrengthBlend:   s.RedStrengthBlend,
		llm.SettingKeyBlackStrengthBlend: s.BlackStrengthBlend,
		llm.SettingKeyRedSideType:        indexOfSideEngineType(s.RedSideType),
		llm.SettingKeyBlackSideType:      indexOfSideEngineType(s.BlackSideType),
		llm.SettingKeyHumanVsLlmOpponent: indexOfSideEngineType(s.HumanVsLlmOpponentType),
	}
}

func indexOfFallback(v llm.LlmFallback) int {
	for i, e := range llmFallbackValues {
		if e == v {
			return i
		}
	}
	return 0
}

func indexOfAdvisorMode(v llm.AdvisorMode) int {
	for i, e := range advisorModeValues {
		if e == v {
			return i
		}
	}
	return 0
}

func indexOfSideEngineType(v llm.SideEngineType) int {
	for i, e := range sideEngineTypeValues {
		if e == v {
			return i
		}
	}
	return 0
}

// LlmSettingsFromRaw fromRaw（llm_settings.dart:85-118）：缺省/越界均回落默认值
// 或 clamp。红黑强度缺省回落到 strengthBlend 原始值（可能越界，再 clamp），与 Dart 一致。
func LlmSettingsFromRaw(raw map[string]any) LlmGameSettings {
	read := func(key string) any { return raw[key] }
	// intOr 数值读取：JSON 反序列化为 float64；storage.Settings 内存态可能为 int
	//（TS 侧 number 单态，两路终点一致）。
	intOr := func(v any, dflt int) int {
		switch n := v.(type) {
		case float64:
			if !math.IsInf(n, 0) && !math.IsNaN(n) {
				return int(n)
			}
		case float32:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
		return dflt
	}
	clampInt := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	pickEnum := func(v any, n int, dflt int) int {
		f, ok := v.(float64)
		if !ok {
			if i, ok := v.(int); ok && i >= 0 && i < n {
				return i
			}
			return dflt
		}
		if f != math.Trunc(f) || f < 0 || int(f) >= n {
			return dflt
		}
		return int(f)
	}
	strengthBlend := intOr(read(llm.SettingKeyStrengthBlend), 50)
	return LlmGameSettings{
		TimeoutSeconds:         clampInt(intOr(read(llm.SettingKeyTimeoutSeconds), 60), 5, 600),
		MaxAttempts:            clampInt(intOr(read(llm.SettingKeyMaxAttempts), 3), 1, 10),
		Fallback:               llmFallbackValues[pickEnum(read(llm.SettingKeyFallbackIndex), len(llmFallbackValues), 0)],
		IntervalSeconds:        clampInt(intOr(read(llm.SettingKeyIntervalSeconds), 1), 0, 60),
		AdvisorMode:            advisorModeValues[pickEnum(read(llm.SettingKeyAdvisorModeIndex), len(advisorModeValues), 1)],
		StrengthBlend:          clampInt(strengthBlend, 0, 100),
		AdvisorDifficulty:      clampInt(intOr(read(llm.SettingKeyAdvisorDifficulty), 5), 1, 5),
		RedStrengthBlend:       clampInt(intOr(read(llm.SettingKeyRedStrengthBlend), strengthBlend), 0, 100),
		BlackStrengthBlend:     clampInt(intOr(read(llm.SettingKeyBlackStrengthBlend), strengthBlend), 0, 100),
		RedSideType:            sideEngineTypeValues[pickEnum(read(llm.SettingKeyRedSideType), len(sideEngineTypeValues), 0)],
		BlackSideType:          sideEngineTypeValues[pickEnum(read(llm.SettingKeyBlackSideType), len(sideEngineTypeValues), 0)],
		HumanVsLlmOpponentType: sideEngineTypeValues[pickEnum(read(llm.SettingKeyHumanVsLlmOpponent), len(sideEngineTypeValues), 0)],
	}
}

// LlmSettingsField 保存字段子集选择（上游 saveLlmSettings 的 fields 参数语义：
// 页面回写只传自己的字段，防清空共享设置）。
type LlmSettingsField int

const (
	LlmFieldTimeoutSeconds LlmSettingsField = iota
	LlmFieldMaxAttempts
	LlmFieldFallback
	LlmFieldIntervalSeconds
	LlmFieldAdvisorMode
	LlmFieldStrengthBlend
	LlmFieldAdvisorDifficulty
	LlmFieldRedStrengthBlend
	LlmFieldBlackStrengthBlend
	LlmFieldRedSideType
	LlmFieldBlackSideType
	LlmFieldHumanVsLlmOpponentType
)

// LoadLlmSettings 读全部对局设置（llmSettings.ts:15-31：单键读失败按缺省处理，
// 等价 Dart load 的 try/catch；最终经 fromRaw 兜底）。
func LoadLlmSettings(st *storage.Settings) LlmGameSettings {
	raw := map[string]any{}
	if st != nil {
		raw[llm.SettingKeyTimeoutSeconds] = st.Get(llm.SettingKeyTimeoutSeconds)
		raw[llm.SettingKeyMaxAttempts] = st.Get(llm.SettingKeyMaxAttempts)
		raw[llm.SettingKeyFallbackIndex] = st.Get(llm.SettingKeyFallbackIndex)
		raw[llm.SettingKeyIntervalSeconds] = st.Get(llm.SettingKeyIntervalSeconds)
		raw[llm.SettingKeyAdvisorModeIndex] = st.Get(llm.SettingKeyAdvisorModeIndex)
		raw[llm.SettingKeyStrengthBlend] = st.Get(llm.SettingKeyStrengthBlend)
		raw[llm.SettingKeyAdvisorDifficulty] = st.Get(llm.SettingKeyAdvisorDifficulty)
		raw[llm.SettingKeyRedStrengthBlend] = st.Get(llm.SettingKeyRedStrengthBlend)
		raw[llm.SettingKeyBlackStrengthBlend] = st.Get(llm.SettingKeyBlackStrengthBlend)
		raw[llm.SettingKeyRedSideType] = st.Get(llm.SettingKeyRedSideType)
		raw[llm.SettingKeyBlackSideType] = st.Get(llm.SettingKeyBlackSideType)
		raw[llm.SettingKeyHumanVsLlmOpponent] = st.Get(llm.SettingKeyHumanVsLlmOpponent)
	}
	return LlmSettingsFromRaw(raw)
}

// SaveLlmSettings 写全部或指定字段（llmSettings.ts:34-58；fields 为空 = 全部字段）。
func SaveLlmSettings(st *storage.Settings, settings LlmGameSettings, fields ...LlmSettingsField) error {
	if st == nil {
		return nil
	}
	m := LlmSettingsToMap(settings)
	keyOf := map[LlmSettingsField]string{
		LlmFieldTimeoutSeconds:         llm.SettingKeyTimeoutSeconds,
		LlmFieldMaxAttempts:            llm.SettingKeyMaxAttempts,
		LlmFieldFallback:               llm.SettingKeyFallbackIndex,
		LlmFieldIntervalSeconds:        llm.SettingKeyIntervalSeconds,
		LlmFieldAdvisorMode:            llm.SettingKeyAdvisorModeIndex,
		LlmFieldStrengthBlend:          llm.SettingKeyStrengthBlend,
		LlmFieldAdvisorDifficulty:      llm.SettingKeyAdvisorDifficulty,
		LlmFieldRedStrengthBlend:       llm.SettingKeyRedStrengthBlend,
		LlmFieldBlackStrengthBlend:     llm.SettingKeyBlackStrengthBlend,
		LlmFieldRedSideType:            llm.SettingKeyRedSideType,
		LlmFieldBlackSideType:          llm.SettingKeyBlackSideType,
		LlmFieldHumanVsLlmOpponentType: llm.SettingKeyHumanVsLlmOpponent,
	}
	if len(fields) == 0 {
		fields = []LlmSettingsField{
			LlmFieldTimeoutSeconds, LlmFieldMaxAttempts, LlmFieldFallback,
			LlmFieldIntervalSeconds, LlmFieldAdvisorMode, LlmFieldStrengthBlend,
			LlmFieldAdvisorDifficulty, LlmFieldRedStrengthBlend, LlmFieldBlackStrengthBlend,
			LlmFieldRedSideType, LlmFieldBlackSideType, LlmFieldHumanVsLlmOpponentType,
		}
	}
	for _, f := range fields {
		if err := st.Set(keyOf[f], m[keyOf[f]]); err != nil {
			return err
		}
	}
	return nil
}
