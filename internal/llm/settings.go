package llm

// LLM 对局设置（非敏感项，05 文档 §8；Electron 版 settings.ts 1:1 移植，
// llm_settings.dart 同源）。
//
// Go 侧持久化由 internal/storage Settings（JSON 配置文件，键前缀 llm_settings_）
// 承担；本文件提供枚举类型与原始值解析（缺省/越界 clamp），供传输层
// （空闲超时）与 M7 MatchRunner 复用。数值设置项由前端经 StoreSet 落盘
// （枚举以 index 整数存档，与 Electron 版 llmSettingsToMap 同键）。
//
// 纯 Go：禁止 import Wails / net/http / frontend（铁律 #1）。

// LlmFallback 降级策略（llm_move_source.dart:527-533）。
type LlmFallback string

const (
	FallbackBuiltinAI LlmFallback = "builtinAi"
	FallbackResign    LlmFallback = "resign"
)

// AdvisorMode 引擎参谋模式（hybrid_llm_move_source.dart:15-23）。
type AdvisorMode string

const (
	AdvisorOff       AdvisorMode = "off"
	AdvisorCandidate AdvisorMode = "candidate"
	AdvisorGate      AdvisorMode = "gate"
)

// SideEngineType 一方对局引擎类型（DR-014：大模型/内置AI 直接可选）。
type SideEngineType string

const (
	SideEngineLLM     SideEngineType = "llm"
	SideEngineBuiltin SideEngineType = "builtin"
)

// LlmSettingsPrefix electron-store/JSON 配置 Key 前缀（07 文档 §3：Key 原样保留）。
const LlmSettingsPrefix = "llm_settings_"

// LlmSettingKeys 持久化 Key（与 settings.ts:70-83 的键名一致）。
const (
	SettingKeyTimeoutSeconds     = LlmSettingsPrefix + "timeoutSeconds"
	SettingKeyMaxAttempts        = LlmSettingsPrefix + "maxAttempts"
	SettingKeyFallbackIndex      = LlmSettingsPrefix + "fallbackIndex"
	SettingKeyIntervalSeconds    = LlmSettingsPrefix + "intervalSeconds"
	SettingKeyAdvisorModeIndex   = LlmSettingsPrefix + "advisorModeIndex"
	SettingKeyStrengthBlend      = LlmSettingsPrefix + "strengthBlend"
	SettingKeyAdvisorDifficulty  = LlmSettingsPrefix + "advisorDifficulty"
	SettingKeyRedStrengthBlend   = LlmSettingsPrefix + "redStrengthBlend"
	SettingKeyBlackStrengthBlend = LlmSettingsPrefix + "blackStrengthBlend"
	SettingKeyRedSideType        = LlmSettingsPrefix + "redSideType"
	SettingKeyBlackSideType      = LlmSettingsPrefix + "blackSideType"
	SettingKeyHumanVsLlmOpponent = LlmSettingsPrefix + "humanVsLlmOpponentType"
)

// ResolveTimeoutSeconds 传输层用：空闲超时秒数（未配置回落默认，越界 clamp 5~600，
// settings.ts:145-147 同语义）。raw 为 settings.Get 的原始值（nil/数值/其他）。
func ResolveTimeoutSeconds(raw any) int {
	return clampInt(numberOr(raw, 60), 5, 600)
}

// numberOr 原始值 → int（JSON 反序列化数值为 float64；其余类型回落默认）。
func numberOr(v any, dflt int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case float32:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return dflt
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
