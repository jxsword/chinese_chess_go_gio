package main

// eval CLI 测试（T7.1，09 §2.3）：报告 JSON 协议面快照 + httptest mock SSE 端点
// 跑通一局验证报告字段齐全（Electron 版 tools/mock-llm-server.mjs 的 Go 等价内嵌）。
// DR-005 断言：eval 的 LLM 请求体恒带思维链关闭参数。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// ---------------------------------------------------------------------------
// reportToJson：Dart toJson 等价映射 + 协议面快照
// ---------------------------------------------------------------------------

func TestReportToJsonMapsMovesIccsToMoves(t *testing.T) {
	red := &scriptedEvalSource{moves: []string{"h7-e7"}, failAt: -1}
	black := &scriptedEvalSource{failAt: 0}
	report, err := engine.RunMatch(t.Context(), red, black, engine.MatchRunnerOptions{})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	// 用时字段不可复现，先归零再快照（其余字段确定性）。
	report.RedTimeMs = 0
	report.BlackTimeMs = 0

	m := reportToJson(report)
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 快照：Dart MatchReport.toJson 逐字段（movesIccs → moves）。
	want := `{"blackBlunders":0,"blackFallbacks":0,"blackTimeMs":0,"blackTop3Hits":0,` +
		`"blackTop3Misses":0,"endReason":"resign","evaluatedPlies":0,` +
		`"moves":["h7e7"],"plies":1,"redBlunders":0,"redFallbacks":0,"redTimeMs":0,` +
		`"redTop3Hits":0,"redTop3Misses":0,"winner":"black-resign"}`
	if string(raw) != want {
		t.Fatalf("报告 JSON 快照不符：\n got %s\nwant %s", raw, want)
	}
}

func TestReportFieldSetComplete(t *testing.T) {
	// 验收口径"报告字段齐全"：五期评估所需键逐一在列。
	red := &scriptedEvalSource{moves: []string{"h7-e7"}, failAt: -1}
	black := &scriptedEvalSource{failAt: 0}
	report, err := engine.RunMatch(t.Context(), red, black, engine.MatchRunnerOptions{})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	m := reportToJson(report)
	for _, key := range []string{
		"winner", "endReason", "plies", "redTimeMs", "blackTimeMs",
		"redFallbacks", "blackFallbacks", "redBlunders", "blackBlunders",
		"evaluatedPlies", "redTop3Hits", "redTop3Misses", "blackTop3Hits",
		"blackTop3Misses", "moves",
	} {
		if _, ok := m[key]; !ok {
			t.Fatalf("报告缺键 %q", key)
		}
	}
	if _, has := m["movesIccs"]; has {
		t.Fatalf("movesIccs 应已映射为 moves")
	}
}

// scriptedEvalSource 固定着法序列棋手（moves 依次出着；failAt 层次改为失败结算）。
type scriptedEvalSource struct {
	moves  []string
	failAt int // -1 = 永不失败
	idx    int
}

func (s *scriptedEvalSource) DisplayName() string { return "脚本棋手" }

func (s *scriptedEvalSource) NextMove(_ context.Context, _ *rules.Board, _ []rules.Move) (engine.MoveSourceResult, error) {
	if s.idx == s.failAt {
		return engine.MoveSourceResult{Status: engine.StatusFailed}, nil
	}
	if s.idx >= len(s.moves) {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}
	iccs := s.moves[s.idx]
	s.idx++
	parse := func(b byte) int {
		switch {
		case b >= 'a' && b <= 'i':
			return int(b - 'a')
		default:
			return int(b - '0')
		}
	}
	move := rules.Move{
		From: rules.Pos(parse(iccs[0]), parse(iccs[1])),
		To:   rules.Pos(parse(iccs[3]), parse(iccs[4])),
	}
	return engine.MoveSourceResult{Status: engine.StatusOK, Move: &move}, nil
}

// ---------------------------------------------------------------------------
// mock SSE 端点（tools/mock-llm-server.mjs 的 Go httptest 等价）跑通一局
// ---------------------------------------------------------------------------

var evalCodeRe = regexp.MustCompile(`([a-i]\d)-([a-i]\d)`)

// extractWhitelist 从 user 提示词提取清单条目的坐标前缀（与 Electron mock 同款：
// 取「清单」之后的段落）。
func extractWhitelist(userContent string) []string {
	seen := map[string]bool{}
	var codes []string
	scope := userContent
	if idx := strings.Index(userContent, "清单"); idx >= 0 {
		scope = userContent[idx:]
	}
	for _, m := range evalCodeRe.FindAllString(scope, -1) {
		if !seen[m] {
			seen[m] = true
			codes = append(codes, m)
		}
	}
	return codes
}

// newMockLlmServer 起 OpenAI 兼容 SSE mock：
//   - 从 user 提示词清单随机（此处取首条以确定性）回复一条着法；
//   - 断言请求体恒带思维链关闭参数（DR-005）。
func newMockLlmServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/chat/completions") {
			http.NotFound(w, r)
			return
		}
		rawBody, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("请求体读取失败：%v", err)
		}
		body := string(rawBody)

		// 【DR-005】eval 请求同样恒关思维链：无 UI 开关、请求体恒带关闭参数。
		if !strings.Contains(body, `"enable_thinking":false`) &&
			!strings.Contains(body, `"thinking":{"type":"disabled"}`) {
			t.Errorf("LLM 请求体缺思维链关闭参数（DR-005）：%s", body)
		}
		// stream:true 恒开（流式协议）。
		if !strings.Contains(body, `"stream":true`) {
			t.Errorf("LLM 请求体缺 stream:true：%s", body)
		}

		var parsed struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal([]byte(body), &parsed); err != nil {
			t.Errorf("请求体解析失败：%v", err)
		}
		user := ""
		for _, m := range parsed.Messages {
			if m.Role == "user" {
				user = m.Content
			}
		}
		codes := extractWhitelist(user)
		text := "抱歉，我一时想不出着法。"
		if len(codes) > 0 {
			text = "分析: 随机选一步清单内着法（本地 mock）。\n着法: " + codes[0]
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := 0; i < len(text); {
			end := i + 8
			if end > len(text) {
				end = len(text)
			}
			chunk, _ := json.Marshal(map[string]any{
				"choices": []map[string]any{{"delta": map[string]any{"content": text[i:end]}}},
			})
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			if flusher != nil {
				flusher.Flush()
			}
			i = end
		}
		done, _ := json.Marshal(map[string]any{"choices": []map[string]any{{"delta": map[string]any{}}}})
		fmt.Fprintf(w, "data: %s\n\n", done)
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestEvalOneGameAgainstMockEndpoint 验收口径：mock 端点跑通一局、报告字段齐全。
func TestEvalOneGameAgainstMockEndpoint(t *testing.T) {
	server := newMockLlmServer(t)
	config := llm.LlmEndpointConfig{BaseURL: server.URL, APIKey: "sk-mock", Model: "mock-chess", Preset: ""}
	transport := llm.NewStreamTransport(func() int { return 60 })

	profiles := buildProfiles(config, transport, 50, func() engine.MoveSource { return chessAiSource(3) }, nil)
	var build func() engine.MoveSource
	for _, p := range profiles {
		if p.name == "baseline-v1" {
			build = p.build
		}
	}
	if build == nil {
		t.Fatal("baseline-v1 profile 缺失")
	}

	reports, err := matchVsEngine(t.Context(), build(), 1, 24, 4, "baseline-v1")
	if err != nil {
		t.Fatalf("matchVsEngine: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("应产出 1 局报告：%d", len(reports))
	}
	entry := reports[0]
	// 报告字段齐全（含 llmSide 注记）。
	for _, key := range []string{
		"winner", "endReason", "plies", "redTimeMs", "blackTimeMs",
		"redFallbacks", "blackFallbacks", "redBlunders", "blackBlunders",
		"evaluatedPlies", "redTop3Hits", "redTop3Misses", "blackTop3Hits",
		"blackTop3Misses", "moves", "llmSide",
	} {
		if _, ok := entry[key]; !ok {
			t.Fatalf("报告缺键 %q：%v", key, entry)
		}
	}
	plies, ok := entry["plies"].(float64)
	if !ok || plies < 1 {
		t.Fatalf("plies 应 ≥ 1：%v", entry["plies"])
	}
	moves, ok := entry["moves"].([]string)
	if !ok || len(moves) != int(plies) {
		t.Fatalf("moves 长度应等于 plies：%v vs %v", entry["moves"], entry["plies"])
	}
	if entry["llmSide"] != "red" {
		t.Fatalf("局 0 LLM 应执红：%v", entry["llmSide"])
	}
	if entry["endReason"] == "source-error" {
		t.Fatalf("mock 端点不应触发 source-error：%v", entry)
	}
	// 质量评估开启：逐手评估计数应与评估过的手数一致（≥1）。
	evaluated, ok := entry["evaluatedPlies"].(float64)
	if !ok || evaluated < 1 {
		t.Fatalf("evaluatedPlies 应 ≥ 1：%v", entry["evaluatedPlies"])
	}
}
