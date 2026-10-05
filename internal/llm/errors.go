package llm

// LLM 协议错误类型（Electron 版 errors.ts 1:1 移植；llm_move_source.dart:535-549）。
// 纯 Go：禁止 import Wails / net/http / frontend（铁律 #1）。
import (
	"fmt"
	"strings"
)

// LlmConfigError 配置缺失类错误（等价 Dart LlmConfigException）。
type LlmConfigError struct{ Message string }

func (e *LlmConfigError) Error() string { return e.Message }

// LlmApiError API 调用类错误（HTTP 非 200 / 流式错误 / 超时，等价 Dart LlmApiException）。
type LlmApiError struct{ Message string }

func (e *LlmApiError) Error() string { return e.Message }

// AnnotateModelHint 把"模型类型用错"的典型服务端报错翻译成可操作的提示
// （errors.ts:41-68 逐字移植，公开以便单测）。
//
// 两类典型：
//   - 图片生成模型（qwen-image-* 等）：请求被路由到原生生成接口并用
//     input.messages 的格式校验，报 "Input should be 'user': input.messages ..."；
//   - 翻译模型（qwen-mt-* 等）：不支持流式，报 "Streaming translation is
//     not supported"——输入虽可多模态，但任务是翻译，不能用于识图。
//
// HTTP≠200 路径已附加过提示（05 §3.2），本函数幂等——消息已含提示时原样返回。
func AnnotateModelHint(message string) string {
	if strings.Contains(message, "提示：这是翻译模型") ||
		strings.Contains(message, "提示：该模型可能不支持 OpenAI 兼容对话接口") {
		return message
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "streaming translation") ||
		strings.Contains(lower, "translation is not supported") {
		return fmt.Sprintf("%s\n\n提示：这是翻译模型（qwen-mt-* 系列），"+
			"不支持流式输出、也不能做棋盘识图。识图请改用视觉理解模型"+
			"（如 qwen-vl-max、qwen3-vl-plus、glm-4.5v）。", message)
	}
	looksLikeWrongModelType :=
		strings.Contains(message, "invalid_parameter_error") ||
			strings.Contains(message, "should be 'user'") ||
			strings.Contains(message, "input.messages")
	if looksLikeWrongModelType {
		return fmt.Sprintf("%s\n\n提示：该模型可能不支持 OpenAI 兼容对话接口"+
			"（图片生成类模型会这样报错）。识图请改用视觉理解模型"+
			"（如 qwen-vl-max、glm-4.5v），对话请改用对应对话模型。", message)
	}
	return message
}
