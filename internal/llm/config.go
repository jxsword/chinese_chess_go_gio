package llm

// LLM 端点配置与请求构建（Electron 版 config.ts 1:1 移植；
// llm_config.dart + llm_move_source.dart:_chat 同源）。
//
// 请求格式（05 文档 §3.1）：OpenAI 兼容 /chat/completions，temperature 0.3、流式。
// 【DR-005 思维链强制关闭——Go 版与 Electron 版的唯一请求差异】构造函数
// 无条件注入关闭参数、无任何开关，按端点预设映射：
//   - DashScope / Qwen 系（含识图预设 qwen-vl-max）→ "enable_thinking": false
//   - 智谱 GLM（glm-4.5 及以上）→ "thinking": {"type": "disabled"}
//   - DeepSeek / Kimi / OpenAI / OpenRouter / 自定义 → "enable_thinking": false 兜底
//     （无思维链或未知参数被忽略，无害）。
//
// Key 只经调用方注入（DR-010 对应：渲染层持掩码 Key 时走 AuthSlot）。
// 纯 Go：禁止 import Wails / net/http / frontend（铁律 #1）。

import (
	"encoding/json"
	"strings"
)

// MaxTokensV1 v1 单次回复的最大 token 数（思考型模型的思维链也计入，需留足预算）。
const MaxTokensV1 = 4096

// MaxTokensV2 Prompt v2 的 token 预算（分析段 + 更长历史/清单）。
const MaxTokensV2 = 8192

// ThinkingStyle 思维链关闭参数形态（DR-005 预设映射；05 §3.1 表）。
type ThinkingStyle string

const (
	// ThinkingDefault 兜底形态："enable_thinking": false（DeepSeek/Kimi/OpenAI/
	// OpenRouter/自定义/未知预设；DashScope 语义与兜底同形）。
	ThinkingDefault ThinkingStyle = "default"
	// ThinkingGLM 智谱 GLM（glm-4.5 及以上）："thinking": {"type": "disabled"}。
	ThinkingGLM ThinkingStyle = "glm"
	// ThinkingDashScope DashScope / Qwen 系："enable_thinking": false（顶层布尔开关）。
	ThinkingDashScope ThinkingStyle = "dashscope"
)

// LlmEndpointConfig 端点配置（07 §4 槽位 JSON：{baseUrl, apiKey, model, preset}；
// 无 disableThinking 字段——DR-005，关闭参数在请求构造层恒发）。
type LlmEndpointConfig struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
	// Preset 配置卡选择的预设名（ThinkingStyleFor 据此选关闭参数形态；
	// 空串/未知走兜底形态）。
	Preset string `json:"preset"`
}

// LlmPreset 常用 OpenAI 兼容端点预设（仅公开地址与示例模型 ID，不含任何凭据；
// llm_config.dart:88-104）。
type LlmPreset struct {
	Name         string
	BaseURL      string
	ExampleModel string
	// ThinkingStyle DR-005 关闭参数形态。
	Style ThinkingStyle
}

// LlmPresetCustom 自定义预设。
var LlmPresetCustom = LlmPreset{Name: "自定义", BaseURL: "", ExampleModel: "", Style: ThinkingDefault}

// LlmPresets 对话端点预设（llm_config.dart:88-104 同序）。
var LlmPresets = []LlmPreset{
	{Name: "智谱 GLM", BaseURL: "https://open.bigmodel.cn/api/paas/v4", ExampleModel: "glm-4-flash", Style: ThinkingGLM},
	{Name: "DeepSeek", BaseURL: "https://api.deepseek.com/v1", ExampleModel: "deepseek-chat", Style: ThinkingDefault},
	{Name: "Kimi（Moonshot）", BaseURL: "https://api.moonshot.cn/v1", ExampleModel: "moonshot-v1-8k", Style: ThinkingDefault},
	{Name: "OpenRouter", BaseURL: "https://openrouter.ai/api/v1", ExampleModel: "openai/gpt-4o-mini", Style: ThinkingDefault},
	{Name: "OpenAI", BaseURL: "https://api.openai.com/v1", ExampleModel: "gpt-4o-mini", Style: ThinkingDefault},
	LlmPresetCustom,
}

// VisionLlmPresets 视觉理解模型预设（研究助手/识图专用，M6 使用；llm_config.dart:106-137）。
// 注意区分：qwen-image-*、各类"图片生成"模型走的是原生多模态生成接口，
// qwen-mt-* 是翻译模型，均不支持 OpenAI 兼容 chat/completions 识图用途。
var VisionLlmPresets = []LlmPreset{
	{Name: "通义千问 3.8-Max（旗舰视觉，阿里云百炼）", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", ExampleModel: "qwen3.8-max", Style: ThinkingDashScope},
	{Name: "通义千问 VL（阿里云百炼）", BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1", ExampleModel: "qwen-vl-max", Style: ThinkingDashScope},
	{Name: "智谱 GLM-4.5V（视觉）", BaseURL: "https://open.bigmodel.cn/api/paas/v4", ExampleModel: "glm-4.5v", Style: ThinkingGLM},
	{Name: "OpenAI GPT-4o mini（视觉）", BaseURL: "https://api.openai.com/v1", ExampleModel: "gpt-4o-mini", Style: ThinkingDefault},
	{Name: "OpenRouter（视觉）", BaseURL: "https://openrouter.ai/api/v1", ExampleModel: "openai/gpt-4o-mini", Style: ThinkingDefault},
	LlmPresetCustom,
}

// ThinkingStyleFor 按预设名解析关闭参数形态（DR-005：构造层唯一入口；
// 自定义/未知/空预设一律兜底形态，未知参数被端点忽略、无害）。
func ThinkingStyleFor(preset string) ThinkingStyle {
	for _, p := range LlmPresets {
		if p.Name == preset {
			return p.Style
		}
	}
	for _, p := range VisionLlmPresets {
		if p.Name == preset {
			return p.Style
		}
	}
	return ThinkingDefault
}

// TestConnectionSystem 测试连接的最小请求文本（llm_move_source.dart:488-497）。
const TestConnectionSystem = "你是一个连通性测试助手。"

// TestConnectionUser 测试连接的用户消息。
const TestConnectionUser = "请回复：ok"

// IsConfigured 端点与模型齐备即认为可调用（部分本地网关允许空 Key，llm_config.dart:27）。
func IsConfigured(config LlmEndpointConfig) bool {
	return strings.TrimSpace(config.BaseURL) != "" && strings.TrimSpace(config.Model) != ""
}

// RequestURL 归一化后的 chat/completions 请求地址（llm_config.dart:32-39）：
// 用户填根地址（推荐）或完整路径均可——去尾斜杠，无 /chat/completions 则自动补。
func RequestURL(baseURL string) string {
	url := strings.TrimSpace(baseURL)
	for strings.HasSuffix(url, "/") {
		url = url[:len(url)-1]
	}
	if strings.HasSuffix(url, "/chat/completions") {
		return url
	}
	return url + "/chat/completions"
}

// BuiltChatRequest 组装完成的请求（body 为 JSON 字符串，供传输层直发）。
type BuiltChatRequest struct {
	URL     string
	Headers map[string]string
	Body    string
	// AuthSlot 掩码 Key 回读场景下由传输/绑定层注入真实 Authorization
	// （DR-010 对应）；渲染层持完整 Key 时直接内联鉴权头、本字段为空。
	AuthSlot string
}

// BuildChatOptions 构建选项。
type BuildChatOptions struct {
	// UseV2 Prompt v2（max_tokens 8192）或 v1（4096）。
	UseV2 bool
	// AuthSlot 掩码 Key 回读场景的凭据槽位。
	AuthSlot string
}

// chatMessage 请求消息（固定字段序，与 TS JSON.stringify 一致）。
type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// thinkingConfig 智谱思维链参数（DR-005：{"type":"disabled"}）。
type thinkingConfig struct {
	Type string `json:"type"`
}

// chatBody 请求体（字段序 = TS 对象插入序：model/messages/temperature/
// max_tokens/stream/关闭参数）。
type chatBody struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	MaxTokens   int           `json:"max_tokens"`
	Stream      bool          `json:"stream"`
	// DR-005 恒发关闭参数（按预设映射二选一； omitempty 由指针 nil 控制）：
	EnableThinking *bool           `json:"enable_thinking,omitempty"`
	Thinking       *thinkingConfig `json:"thinking,omitempty"`
}

// BuildChatRequest 组装一次流式对话请求（llm_move_source.dart:355-374 + DR-005）。
// 未配置（端点/模型缺失）返回 LlmConfigError（等价 Dart LlmConfigException）。
func BuildChatRequest(config LlmEndpointConfig, system, user string, options BuildChatOptions) (BuiltChatRequest, error) {
	if !IsConfigured(config) {
		return BuiltChatRequest{}, &LlmConfigError{Message: "模型端点未配置（需填写端点与模型 ID）"}
	}
	body := chatBody{
		Model: strings.TrimSpace(config.Model),
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		Temperature: 0.3,
		MaxTokens:   MaxTokensV1,
		Stream:      true,
	}
	if options.UseV2 {
		body.MaxTokens = MaxTokensV2
	}
	// DR-005：无条件注入关闭参数（无任何开关路径）。GLM 形态走 thinking 对象，
	// 其余（含 DashScope 语义与兜底）走顶层布尔。
	if ThinkingStyleFor(config.Preset) == ThinkingGLM {
		body.Thinking = &thinkingConfig{Type: "disabled"}
	} else {
		off := false
		body.EnableThinking = &off
	}

	headers := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "text/event-stream",
	}
	built := BuiltChatRequest{URL: RequestURL(config.BaseURL), Headers: headers}

	raw, err := json.Marshal(body)
	if err != nil {
		return BuiltChatRequest{}, &LlmConfigError{Message: "请求体序列化失败：" + err.Error()}
	}
	built.Body = string(raw)

	key := strings.TrimSpace(config.APIKey)
	if key == "" {
		// 本地网关等允许空 Key：不携带鉴权头（llm_move_source.dart:371-373）。
		return built, nil
	}
	if strings.HasPrefix(key, "****") {
		// secure.get 回读的掩码 Key：完整 Key 不回渲染层内存（07 §4），走注入。
		if options.AuthSlot == "" {
			return BuiltChatRequest{}, &LlmConfigError{Message: "模型 API Key 尚未加载完成，请稍候重试或重新保存配置"}
		}
		built.AuthSlot = options.AuthSlot
		return built, nil
	}
	built.Headers["Authorization"] = "Bearer " + key
	return built, nil
}

// BuildTestConnectionChat 配置卡"测试连接"请求（单次流式，v1 预算）。
func BuildTestConnectionChat(config LlmEndpointConfig, authSlot string) (BuiltChatRequest, error) {
	return BuildChatRequest(config, TestConnectionSystem, TestConnectionUser, BuildChatOptions{
		UseV2:    false,
		AuthSlot: authSlot,
	})
}

// IsEmptyLlmConfig 是否三个字段全部为空（含纯空白）——空侧允许镜像对方的配置
// （DR-012；preset 不参与判定）。
func IsEmptyLlmConfig(config LlmEndpointConfig) bool {
	return strings.TrimSpace(config.BaseURL) == "" &&
		strings.TrimSpace(config.APIKey) == "" &&
		strings.TrimSpace(config.Model) == ""
}

// ResolvedLlmSideConfig 解析后一方实际生效的配置（DR-012）。
type ResolvedLlmSideConfig struct {
	// Config 实际生效的配置（空侧 = 对方的配置）。
	Config LlmEndpointConfig
	// AuthSlot 掩码 Key 回读时注入鉴权的槽位：镜像时为对方槽位。
	AuthSlot string
}

// ResolveLlmSideConfig 解析对局中一方实际生效的配置（DR-012）：自身三字段全空 →
// 返回对方配置与对方槽位（运行时跟随，不落盘——对方后续改动即时生效）；否则返回自身。
// 注意：仅"全空"触发镜像；填了部分字段（如只填 Key）仍是独立无效配置，
// 由开始校验拦截并提示。
func ResolveLlmSideConfig(own, other LlmEndpointConfig, ownSlot, otherSlot string) ResolvedLlmSideConfig {
	if IsEmptyLlmConfig(own) {
		return ResolvedLlmSideConfig{Config: other, AuthSlot: otherSlot}
	}
	return ResolvedLlmSideConfig{Config: own, AuthSlot: ownSlot}
}
