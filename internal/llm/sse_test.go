package llm

// SseAssembler 单元测试（T4.1，05 文档 §4 第 1+2 层；test/llm/sse.spec.ts 移植）：
// 行过滤 / [DONE] / 增量缓冲 / chunk.error / 正文为空退思维链。

import (
	"strings"
	"testing"
)

func TestSseAssemblerLineFiltering(t *testing.T) {
	s := &SseAssembler{}
	// 空行与 ":" 注释心跳丢弃继续
	for _, line := range []string{"", "   ", ": OPENROUTER PROCESSING", ": keep-alive"} {
		r, err := s.HandleLine(line)
		if err != nil || r.Ended || r.Delta != nil {
			t.Fatalf("心跳行应静默忽略：%q → %+v err=%v", line, r, err)
		}
	}
	// 非 data 行与非 JSON data 行忽略
	s2 := &SseAssembler{}
	for _, line := range []string{"event: message", "data: {not json", "data: plain text"} {
		r, err := s2.HandleLine(line)
		if err != nil || r.Ended || r.Delta != nil {
			t.Fatalf("杂质行应忽略：%q → %+v err=%v", line, r, err)
		}
	}
	// data: [DONE] 结束流
	s3 := &SseAssembler{}
	r, err := s3.HandleLine("data: [DONE]")
	if err != nil || !r.Ended {
		t.Fatalf("[DONE] 应结束流：%+v err=%v", r, err)
	}
}

func TestSseAssemblerContentDelta(t *testing.T) {
	s := &SseAssembler{}
	r1, err := s.HandleLine(`data: {"choices":[{"delta":{"content":"着法"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := s.HandleLine(`data: {"choices":[{"delta":{"content":": b2-e2"}}]}`)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Delta == nil || r1.Delta.Content == nil || *r1.Delta.Content != "着法" {
		t.Fatalf("增量 1 不符：%+v", r1)
	}
	if r2.Delta == nil || r2.Delta.Content == nil || *r2.Delta.Content != ": b2-e2" {
		t.Fatalf("增量 2 不符：%+v", r2)
	}
	if r1.Ended {
		t.Error("增量行不应结束流")
	}
	if s.Content() != "着法: b2-e2" {
		t.Fatalf("正文缓冲 = %q", s.Content())
	}
}

func TestSseAssemblerReasoning(t *testing.T) {
	// reasoning_content 优先 reasoning，累积思维链（兼容双字段名）
	s := &SseAssembler{}
	for _, line := range []string{
		`data: {"choices":[{"delta":{"reasoning_content":"思考A"}}]}`,
		`data: {"choices":[{"delta":{"reasoning":"思考B"}}]}`,
		`data: {"choices":[{"delta":{"reasoning_content":"思考C"}}]}`,
	} {
		if _, err := s.HandleLine(line); err != nil {
			t.Fatal(err)
		}
	}
	if s.Reasoning() != "思考A思考B思考C" {
		t.Fatalf("思维链缓冲 = %q", s.Reasoning())
	}
}

func TestSseAssemblerChunkError(t *testing.T) {
	s := &SseAssembler{}
	_, err := s.HandleLine(`data: {"error":{"code":"x","message":"boom"}}`)
	if err == nil {
		t.Fatal("chunk.error 应报错")
	}
	if _, ok := err.(*LlmApiError); !ok {
		t.Fatalf("应返回 LlmApiError，实际 %T", err)
	}
	if !strings.Contains(err.Error(), "流式响应错误: ") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("错误文案不符：%v", err)
	}
}

func TestSseAssemblerIgnoresMalformed(t *testing.T) {
	s := &SseAssembler{}
	for _, line := range []string{
		`data: {"id":"1"}`,
		`data: {"choices":[]}`,
		`data: {"choices":[{"delta":"str"}]}`,
		`data: {"choices":[{"delta":{"content":123}}]}`,
	} {
		r, err := s.HandleLine(line)
		if err != nil || r.Ended || r.Delta != nil {
			t.Fatalf("畸形行应忽略：%q → %+v err=%v", line, r, err)
		}
	}
}

func TestSseAssemblerFullStream(t *testing.T) {
	s := &SseAssembler{}
	lines := []string{
		`data: {"choices":[{"delta":{"content":"分"}}]}`,
		"",
		`data: {"choices":[{"delta":{"content":"析"}}]}`,
		`data: {"choices":[{"delta":{"content":"\n着法: h2-e2"}}]}`,
		"data: [DONE]",
	}
	ended := false
	var deltas []string
	for _, line := range lines {
		r, err := s.HandleLine(line)
		if err != nil {
			t.Fatal(err)
		}
		if r.Delta != nil && r.Delta.Content != nil {
			deltas = append(deltas, *r.Delta.Content)
		}
		if r.Ended {
			ended = true
		}
	}
	if !ended {
		t.Error("ended 应在 DONE 行返回")
	}
	if strings.Join(deltas, "") != "分析\n着法: h2-e2" {
		t.Fatalf("增量序列 = %q", deltas)
	}
}

func TestSseAssemblerPickAnswer(t *testing.T) {
	// 正文非空取正文
	s := &SseAssembler{}
	_, _ = s.HandleLine(`data: {"choices":[{"delta":{"reasoning_content":"内心独白"}}]}`)
	_, _ = s.HandleLine(`data: {"choices":[{"delta":{"content":"着法: b2-e2"}}]}`)
	if s.PickAnswer() != "着法: b2-e2" {
		t.Fatalf("正文优先不符：%q", s.PickAnswer())
	}

	// 正文为空（思考型 token 耗尽）退回思维链全文
	s2 := &SseAssembler{}
	_, _ = s2.HandleLine(`data: {"choices":[{"delta":{"reasoning_content":"先出子力"}}]}`)
	_, _ = s2.HandleLine(`data: {"choices":[{"delta":{"reasoning_content":"，再谋中路 着法: b2-e2"}}]}`)
	if s2.PickAnswer() != "先出子力，再谋中路 着法: b2-e2" {
		t.Fatalf("思维链退回不符：%q", s2.PickAnswer())
	}

	// 两者皆空返回空串
	if (&SseAssembler{}).PickAnswer() != "" {
		t.Error("空流应返回空串")
	}
}
