package state

// 研究助手配置运行时借用（T4'.2，DR-009，design_docs/05 §6/§7）：
// 助手槽三字段全空时，识图/求解辅助按 黑→红 优先级借用对战配置——
// 仅存在于本次请求内存，**永不写入助手槽**（不落盘，DR-012 同构语义）；
// authSlot 随来源槽（DR-010 掩码注入闭环）。助手槽部分填写 = 独立无效
// 配置，不借用（沿 DR-012 边界，由既有校验/错误提示拦截）。
// 翻译源 = 上游 frontend/src/features/studio/assistantConfig.ts（零行为偏差）；
// M4' 落 helper 与翻译用例，识图/求解消费随 M6'。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// AssistantConfigSource 借用来源（空串 = 三槽全空，维持"请先配置"提示）。
type AssistantConfigSource string

const (
	AssistantSourceAssistant AssistantConfigSource = "assistant"
	AssistantSourceBlack     AssistantConfigSource = "black"
	AssistantSourceRed       AssistantConfigSource = "red"
)

// 凭据三槽位名（复制物 storage 常量原样沿用）。
const (
	AssistantSlotAssistant = storage.SlotAssistant
	AssistantSlotBlack     = storage.SlotBlack
	AssistantSlotRed       = storage.SlotRed
)

// ResolvedAssistantConfig 解析结果（assistantConfig.ts ResolvedAssistantConfig 同形）。
type ResolvedAssistantConfig struct {
	// Config 实际生效的配置；nil = 三槽全空（调用方维持"请先配置"提示）。
	Config *llm.LlmEndpointConfig
	// AuthSlot 掩码 Key 回读时注入鉴权的槽位（DR-010）：借用时为来源对战槽；
	// 三槽全空为空串（不产生 authSlot）。
	AuthSlot string
	// Source 生效配置来源；空串 = 三槽全空。
	Source AssistantConfigSource
}

// ResolveAssistantConfig 解析研究助手实际生效配置（DR-009）：助手槽三字段全空 →
// 借用黑方（非空）→ 红方（非空）→ 三槽全空返回零值；助手槽非空（含部分
// 填写）→ 原样返回助手槽（不借用）。借用判定 = IsEmptyLlmConfig 三字段
// 全空（preset 不参与，DR-012 同口径）。**永不写入助手槽**（纯内存解析）。
func ResolveAssistantConfig(assistant, black, red *llm.LlmEndpointConfig) ResolvedAssistantConfig {
	if assistant != nil && !llm.IsEmptyLlmConfig(*assistant) {
		return ResolvedAssistantConfig{Config: assistant, AuthSlot: AssistantSlotAssistant, Source: AssistantSourceAssistant}
	}
	if black != nil && !llm.IsEmptyLlmConfig(*black) {
		return ResolvedAssistantConfig{Config: black, AuthSlot: AssistantSlotBlack, Source: AssistantSourceBlack}
	}
	if red != nil && !llm.IsEmptyLlmConfig(*red) {
		return ResolvedAssistantConfig{Config: red, AuthSlot: AssistantSlotRed, Source: AssistantSourceRed}
	}
	return ResolvedAssistantConfig{}
}

// AssistantSourceLabel 借用来源的中文显示名（提示文案用；source=assistant/空 无需显示）。
func AssistantSourceLabel(source AssistantConfigSource) string {
	switch source {
	case AssistantSourceBlack:
		return "黑方"
	case AssistantSourceRed:
		return "红方"
	default:
		return ""
	}
}
