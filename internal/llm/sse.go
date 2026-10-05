package llm

// SSE 流重组（五层过滤管线第 1+2 层，05 文档 §4；Electron 版 sse.ts 逐字移植，
// llm_move_source.dart:438-480 同源）。
//
// 第 1 层：按行处理 SSE——空行与 ':' 注释心跳丢弃、data: [DONE] 结束、
//          delta.content → 正文缓冲、delta.reasoning_content/reasoning → 思维链缓冲、
//          chunk.error → 返回 LlmApiError（计一次失败重试）、非 data 行/非 JSON 行忽略；
// 第 2 层：文本提取——正文非空取正文；正文为空（思考型 token 耗尽）退回思维链文本。
//
// 纯 Go：禁止 import Wails / frontend（铁律 #1）。

import (
	"encoding/json"
	"strings"
)

// Delta 单条增量（00 文档 §3.1 delta：content?/reasoning?）。
type Delta struct {
	Content   *string `json:"content,omitempty"`
	Reasoning *string `json:"reasoning,omitempty"`
}

// SseLineResult 单行解析结果。
type SseLineResult struct {
	// Ended true 表示流已结束（data: [DONE]）。
	Ended bool
	// Delta 本行携带的增量；无可转发增量时为 nil。
	Delta *Delta
}

// SseAssembler 累积正文与思维链缓冲的逐行解析器。
type SseAssembler struct {
	contentBuf   string
	reasoningBuf string
}

// Content 正文缓冲（测试观察用）。
func (s *SseAssembler) Content() string { return s.contentBuf }

// Reasoning 思维链缓冲（测试观察用）。
func (s *SseAssembler) Reasoning() string { return s.reasoningBuf }

// HandleLine 解析一行 SSE 数据（sse.ts:44-88 逐字移植）。
// chunk.error 返回 LlmApiError（计一次失败重试，05 §4 第 1 层）。
func (s *SseAssembler) HandleLine(line string) (SseLineResult, error) {
	trimmed := strings.TrimSpace(line)
	// 空行与注释（如 OpenRouter 的 ": OPENROUTER PROCESSING" 心跳）跳过。
	if trimmed == "" || strings.HasPrefix(trimmed, ":") {
		return SseLineResult{}, nil
	}
	if !strings.HasPrefix(trimmed, "data:") {
		return SseLineResult{}, nil
	}
	payload := strings.TrimSpace(trimmed[5:])
	if payload == "[DONE]" {
		return SseLineResult{Ended: true}, nil
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return SseLineResult{}, nil // 非 JSON 行忽略
	}
	if err, ok := chunk["error"]; ok && err != nil {
		raw, merr := json.Marshal(err)
		if merr != nil {
			raw = []byte("null")
		}
		return SseLineResult{}, &LlmApiError{Message: "流式响应错误: " + string(raw)}
	}
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) == 0 {
		return SseLineResult{}, nil
	}
	first, ok := choices[0].(map[string]any)
	if !ok {
		return SseLineResult{}, nil
	}
	delta, ok := first["delta"].(map[string]any)
	if !ok {
		return SseLineResult{}, nil
	}
	out := Delta{}
	hasDelta := false
	if c, ok := delta["content"].(string); ok {
		content := c
		out.Content = &content
		s.contentBuf += content
		hasDelta = true
	}
	// reasoning_content ?? reasoning（兼容双字段名，sse.ts:79）。
	r, hasReasoning := delta["reasoning_content"]
	if !hasReasoning || r == nil {
		r = delta["reasoning"]
	}
	if rs, ok := r.(string); ok {
		reasoning := rs
		out.Reasoning = &reasoning
		s.reasoningBuf += reasoning
		hasDelta = true
	}
	if !hasDelta {
		return SseLineResult{}, nil
	}
	return SseLineResult{Delta: &out}, nil
}

// PickAnswer 最终答案（sse.ts:94-96）：正文优先；正文为空（思考型模型耗尽
// token）时退回思维链文本，交给着法解析器尽力提取（llm_move_source.dart:476-480）。
func (s *SseAssembler) PickAnswer() string {
	if strings.TrimSpace(s.contentBuf) != "" {
		return s.contentBuf
	}
	return s.reasoningBuf
}
