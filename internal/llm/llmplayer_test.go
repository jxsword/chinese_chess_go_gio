package llm

// LlmPlayer 等价集（T4.3，09 §2 move_source.spec 30 用例 + §2.3 失败模式表；
// test/llm/moveSource.spec.ts 移植）：SSE 解析/五层过滤/重试/降级
//（管线第 3~6 层 + 兜底链；第 1/2 层与 HTTP 细节由 transport_test.go 覆盖）。

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// ---------------------------------------------------------------------------
// 可编程 fake transport（可编程响应序列，09 §2.3）
// ---------------------------------------------------------------------------

type scriptItem struct {
	kind    string // "text" | "error" | "hang"
	text    string
	message string
}

func scriptText(text string) scriptItem { return scriptItem{kind: "text", text: text} }
func scriptError(msg string) scriptItem { return scriptItem{kind: "error", message: msg} }
func scriptHang() scriptItem            { return scriptItem{kind: "hang"} }
func scriptExhausted() scriptItem       { return scriptItem{kind: "error", message: "脚本耗尽"} }

type fakeTransport struct {
	mu           sync.Mutex
	script       []scriptItem
	calls        []ChatRequest
	cancelledIDs []string
}

func (f *fakeTransport) Chat(_ context.Context, req ChatRequest, handlers ChatHandlers) error {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	idx := len(f.calls) - 1
	item := scriptExhausted()
	if idx < len(f.script) {
		item = f.script[idx]
	}
	f.mu.Unlock()

	switch item.kind {
	case "text":
		handlers.OnDone(item.text)
	case "error":
		handlers.OnError(item.message)
	case "hang":
		// 永不结算（模拟只取消才结束的请求）
		select {}
	}
	return nil
}

func (f *fakeTransport) Cancel(requestID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cancelledIDs = append(f.cancelledIDs, requestID)
}

func (f *fakeTransport) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeTransport) cancelled() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.cancelledIDs...)
}

// chatContent 从捕获的请求体解析 messages[].content。
func chatContent(t *testing.T, f *fakeTransport, call int) (system, user string) {
	t.Helper()
	f.mu.Lock()
	body := f.calls[call].Body
	f.mu.Unlock()
	var parsed struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("请求体非 JSON：%v", err)
	}
	return parsed.Messages[0].Content, parsed.Messages[1].Content
}

var playerCfg = LlmEndpointConfig{BaseURL: "https://api.example.com/v1", APIKey: "sk-test-abcd", Model: "test-model", Preset: ""}

// simpleBoard 双王 + 红车 a9 + 黑卒 a5 的小局面；红方合法含 a9-a5（吃卒）等。
func simpleBoard(t *testing.T) *rules.Board {
	return mustBoard(t, "4k4/9/9/9/9/p8/9/9/9/R2K5 w - - 0 1")
}

func noopBuiltin() engine.MoveSource {
	return failBuiltin{message: "builtin source should not be called"}
}

// failBuiltin 断言不应被调用的内置 AI 桩。
type failBuiltin struct{ message string }

func (f failBuiltin) DisplayName() string { return "内置 AI（高级）" }
func (f failBuiltin) NextMove(context.Context, *rules.Board, []rules.Move) (engine.MoveSourceResult, error) {
	return engine.MoveSourceResult{Status: engine.StatusFailed, Note: f.message}, fmt.Errorf("%s", f.message)
}

// seqID 顺序 id 工厂（测试断言可预测 requestId）。
func seqID() func() string {
	i := 0
	return func() string {
		i++
		return fmt.Sprintf("id-%d", i)
	}
}

func makePlayer(transport *fakeTransport, over func(*LlmPlayerOptions)) *LlmPlayer {
	opts := LlmPlayerOptions{
		Fallback:        FallbackBuiltinAI,
		BuiltinAiSource: noopBuiltin,
	}
	if over != nil {
		over(&opts)
	}
	return NewLlmPlayer(playerCfg, transport, opts, ChatClientOptions{NewID: seqID()})
}

// ---------------------------------------------------------------------------
// 管线：解析 → 白名单 → 重试
// ---------------------------------------------------------------------------

func TestPipelineWhitelistHit(t *testing.T) {
	// 01 正常回复命中白名单 → ok（白名单精确匹配）
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a5")}}
	result, err := makePlayer(ft, nil).NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK || result.Move == nil {
		t.Fatalf("结果不符：%+v", result)
	}
	want := rules.Move{
		From:     rules.Pos(0, 9),
		To:       rules.Pos(0, 5),
		Captured: &rules.Piece{Kind: rules.Pawn, Side: rules.Black},
	}
	if result.Move.From != want.From || result.Move.To != want.To || !reflect.DeepEqual(result.Move.Captured, want.Captured) {
		t.Fatalf("着法不符：%+v", result.Move)
	}
	if result.Note != "" || result.FromFallback {
		t.Fatalf("成功时 note/fromFallback 应为空：%+v", result)
	}
}

func TestPipelineNormalizedHits(t *testing.T) {
	// 02 白名单精确匹配：归一化 b2e2 → b2-e2 命中；大小写/全角同样命中
	for _, text := range []string{"着法: a9a5", "着法: A9-A5", "着法：Ａ９－Ａ５"} {
		ft := &fakeTransport{script: []scriptItem{scriptText(text)}}
		result, err := makePlayer(ft, nil).NextMove(t.Context(), simpleBoard(t), nil)
		if err != nil || result.Status != engine.StatusOK {
			t.Fatalf("%q 应命中白名单：%+v err=%v", text, result, err)
		}
		if result.Move.From != rules.Pos(0, 9) || result.Move.To != rules.Pos(0, 5) {
			t.Fatalf("%q 着法不符：%+v", text, result.Move)
		}
	}
}

func TestPipelineRetryFeedbackV2(t *testing.T) {
	// 03 编造清单外着法 → 重试且反馈含原因与上次着法（v2）
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: e9-e0"), scriptText("着法: a9-a5")}}
	result, err := makePlayer(ft, func(o *LlmPlayerOptions) { o.UsePromptV2 = true }).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil || result.Status != engine.StatusOK {
		t.Fatalf("二次应命中：%+v err=%v", result, err)
	}
	_, secondUser := chatContent(t, ft, 1)
	if !strings.Contains(secondUser, "你上一次的回复的着法 e9-e0无效（着法 e9-e0 不在合法清单中）") {
		t.Fatalf("v2 反馈不符：\n%s", secondUser)
	}
	// 重试是在原 user 末尾追加后整段重发（无状态两消息协议）
	_, firstUser := chatContent(t, ft, 0)
	if !strings.HasPrefix(secondUser, firstUser) {
		t.Fatal("重试请求应以原 user 为前缀")
	}
}

func TestPipelineRetryUnparseable(t *testing.T) {
	// 04 无法解析出坐标 → 重试反馈为"无法从回复中解析出着法"
	ft := &fakeTransport{script: []scriptItem{scriptText("抱歉，我不会。"), scriptText("着法: a9-a5")}}
	if _, err := makePlayer(ft, nil).NextMove(t.Context(), simpleBoard(t), nil); err != nil {
		t.Fatal(err)
	}
	_, secondUser := chatContent(t, ft, 1)
	if !strings.Contains(secondUser, "你上一次的回复无效（无法从回复中解析出着法）") {
		t.Fatalf("v1 反馈不符：\n%s", secondUser)
	}
}

func TestPipelineNormalizationHit(t *testing.T) {
	// 05 markdown 围栏/全角/零宽包裹 → 归一化后提取成功
	ft := &fakeTransport{script: []scriptItem{scriptText("```\n着法：Ａ\u200b９－Ａ５\n```")}}
	result, err := makePlayer(ft, nil).NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil || result.Status != engine.StatusOK {
		t.Fatalf("归一化后应命中：%+v err=%v", result, err)
	}
}

func TestPipelineReasoningPickAnswer(t *testing.T) {
	// 06 思维链正文空 → 用 reasoning 文本提取（传输层 pickAnswer 语义）
	ft := &fakeTransport{script: []scriptItem{scriptText("让我想想……最终 着法: a9-a5")}}
	result, err := makePlayer(ft, nil).NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil || result.Status != engine.StatusOK {
		t.Fatalf("应命中：%+v err=%v", result, err)
	}
}

func TestPipelineExhaustedFallbackBuiltin(t *testing.T) {
	// 07 连续 maxAttempts 次无效 → builtinAi 兜底（note 注明 + fromFallback）
	ft := &fakeTransport{script: []scriptItem{
		scriptText("着法: e9-e0"), scriptText("着法: e9-e0"), scriptText("着法: e9-e0"),
	}}
	player := NewLlmPlayer(playerCfg, ft, LlmPlayerOptions{
		Fallback:    FallbackBuiltinAI,
		MaxAttempts: 3,
		BuiltinAiSource: func() engine.MoveSource {
			return stubBuiltin{move: rules.Move{From: rules.Pos(0, 9), To: rules.Pos(0, 4)}}
		},
	}, ChatClientOptions{NewID: func() string { return "x" }})
	result, err := player.NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !result.FromFallback {
		t.Fatal("应标记 fromFallback")
	}
	wantNote := "第 3 次回复无效（着法 e9-e0 不在合法清单中），已由内置 AI 兜底走子"
	if result.Note != wantNote {
		t.Fatalf("note = %q, want %q", result.Note, wantNote)
	}
	if result.Move == nil || result.Move.From != rules.Pos(0, 9) || result.Move.To != rules.Pos(0, 4) {
		t.Fatalf("兜底着法不符：%+v", result.Move)
	}
	if ft.callCount() != 3 {
		t.Fatalf("调用次数 = %d", ft.callCount())
	}
}

func TestPipelineOnAttemptProgress(t *testing.T) {
	// 07b onAttempt 回调逐次上报尝试进度
	ft := &fakeTransport{script: []scriptItem{
		scriptText("着法: e9-e0"), scriptText("着法: e9-e0"), scriptText("着法: e9-e0"),
	}}
	var attempts [][2]int
	player := NewLlmPlayer(playerCfg, ft, LlmPlayerOptions{
		Fallback:        FallbackResign,
		MaxAttempts:     3,
		BuiltinAiSource: noopBuiltin,
		OnAttempt:       func(n, total int) { attempts = append(attempts, [2]int{n, total}) },
	}, ChatClientOptions{NewID: func() string { return "x" }})
	if _, err := player.NextMove(t.Context(), simpleBoard(t), nil); err != nil {
		t.Fatal(err)
	}
	want := [][2]int{{1, 3}, {2, 3}, {3, 3}}
	if !reflect.DeepEqual(attempts, want) {
		t.Fatalf("attempts = %v", attempts)
	}
}

func TestPipelineResignOnErrors(t *testing.T) {
	// 08 连续失败 + resign 降级 → failed（该方判负终局）
	ft := &fakeTransport{script: []scriptItem{
		scriptError("HTTP 500: boom"), scriptError("HTTP 500: boom"), scriptError("HTTP 500: boom"),
	}}
	result, err := makePlayer(ft, func(o *LlmPlayerOptions) { o.Fallback = FallbackResign }).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusFailed {
		t.Fatalf("status = %q", result.Status)
	}
	wantNote := "第 3 次调用失败：HTTP 500: boom，按判负处理"
	if result.Note != wantNote {
		t.Fatalf("note = %q, want %q", result.Note, wantNote)
	}
}

func TestPipelineHTTPErrorThenSuccess(t *testing.T) {
	// 09 HTTP 4xx/5xx（传输层 error）→ 计一次失败重试，耗尽 → 降级
	ft := &fakeTransport{script: []scriptItem{scriptError("HTTP 429: rate limited"), scriptText("着法: a9-a5")}}
	result, err := makePlayer(ft, nil).NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil || result.Status != engine.StatusOK {
		t.Fatalf("重试后应命中：%+v err=%v", result, err)
	}
	if ft.callCount() != 2 {
		t.Fatalf("调用次数 = %d", ft.callCount())
	}
}

func TestPipelineKeepsLastReason(t *testing.T) {
	// 10 调用异常计入重试次数并保留最近原因
	ft := &fakeTransport{script: []scriptItem{
		scriptError("连接失败：ECONNREFUSED"),
		scriptError("空闲超时（60s 内无响应数据）"),
		scriptError("总耗时超过 240s"),
	}}
	result, err := makePlayer(ft, func(o *LlmPlayerOptions) { o.Fallback = FallbackResign }).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusFailed {
		t.Fatalf("status = %q", result.Status)
	}
	wantNote := "第 3 次调用失败：总耗时超过 240s，按判负处理"
	if result.Note != wantNote {
		t.Fatalf("note = %q, want %q", result.Note, wantNote)
	}
}

func TestPipelineNoLegalMoveSkipsCall(t *testing.T) {
	// 11 无合法着法（将死/困毙）→ noLegalMove，不发起调用
	ft := &fakeTransport{}
	result, err := makePlayer(ft, nil).
		NextMove(t.Context(), mustBoard(t, "k8/1P7/9/9/9/R8/9/9/9/2K6 b - - 0 1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusNoLegalMove {
		t.Fatalf("status = %q", result.Status)
	}
	if ft.callCount() != 0 {
		t.Fatal("不应发起调用")
	}
}

func TestPipelineV1V2BodyDiff(t *testing.T) {
	// 12 v1/v2 请求体差异：max_tokens 与系统提示
	ft1 := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a5")}}
	if _, err := makePlayer(ft1, nil).NextMove(t.Context(), simpleBoard(t), nil); err != nil {
		t.Fatal(err)
	}
	system1, _ := chatContent(t, ft1, 0)
	ft1.mu.Lock()
	var body1 map[string]any
	_ = json.Unmarshal([]byte(ft1.calls[0].Body), &body1)
	ft1.mu.Unlock()
	if body1["max_tokens"] != float64(4096) {
		t.Fatalf("v1 max_tokens = %v", body1["max_tokens"])
	}
	if !strings.Contains(system1, "【回复格式（唯一允许的格式，违反即视为无效）】") {
		t.Fatal("v1 系统提示不符")
	}

	ft2 := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a5")}}
	if _, err := makePlayer(ft2, func(o *LlmPlayerOptions) { o.UsePromptV2 = true }).
		NextMove(t.Context(), simpleBoard(t), nil); err != nil {
		t.Fatal(err)
	}
	system2, user2 := chatContent(t, ft2, 0)
	ft2.mu.Lock()
	var body2 map[string]any
	_ = json.Unmarshal([]byte(ft2.calls[0].Body), &body2)
	ft2.mu.Unlock()
	if body2["max_tokens"] != float64(8192) {
		t.Fatalf("v2 max_tokens = %v", body2["max_tokens"])
	}
	if !strings.Contains(system2, "【回复格式（唯一允许的格式，共两段）】") {
		t.Fatal("v2 系统提示不符")
	}
	if !strings.Contains(user2, "【棋盘图】") {
		t.Fatal("v2 用户提示应含棋盘图")
	}
}

func TestPipelineAuthHeader(t *testing.T) {
	// 13 鉴权头：完整 Key 内联 Bearer；掩码 Key 走 authSlot（DR-010）
	ftFull := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a5")}}
	if _, err := makePlayer(ftFull, nil).NextMove(t.Context(), simpleBoard(t), nil); err != nil {
		t.Fatal(err)
	}
	ftFull.mu.Lock()
	auth := ftFull.calls[0].Headers["Authorization"]
	slot := ftFull.calls[0].AuthSlot
	ftFull.mu.Unlock()
	if auth != "Bearer sk-test-abcd" || slot != "" {
		t.Fatalf("完整 Key 鉴权不符：auth=%q slot=%q", auth, slot)
	}

	ftMasked := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a5")}}
	maskedCfg := playerCfg
	maskedCfg.APIKey = "****abcd"
	player := NewLlmPlayer(maskedCfg, ftMasked, LlmPlayerOptions{
		Fallback:        FallbackBuiltinAI,
		BuiltinAiSource: noopBuiltin,
	}, ChatClientOptions{AuthSlot: "llm_config_black"})
	if _, err := player.NextMove(t.Context(), simpleBoard(t), nil); err != nil {
		t.Fatal(err)
	}
	ftMasked.mu.Lock()
	auth = ftMasked.calls[0].Headers["Authorization"]
	slot = ftMasked.calls[0].AuthSlot
	ftMasked.mu.Unlock()
	if auth != "" {
		t.Fatalf("掩码 Key 不应内联鉴权头：%q", auth)
	}
	if slot != "llm_config_black" {
		t.Fatalf("AuthSlot = %q", slot)
	}
}

func TestPipelineUnconfiguredFails(t *testing.T) {
	// 14 未配置端点 → 调用失败路径（LlmConfigError 消息）
	ft := &fakeTransport{script: []scriptItem{scriptError("模型端点未配置（需填写端点与模型 ID）")}}
	player := NewLlmPlayer(LlmEndpointConfig{}, ft, LlmPlayerOptions{
		Fallback:        FallbackResign,
		MaxAttempts:     1,
		BuiltinAiSource: noopBuiltin,
	}, ChatClientOptions{NewID: func() string { return "x" }})
	result, err := player.NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusFailed {
		t.Fatalf("status = %q", result.Status)
	}
	wantNote := "第 1 次调用失败：模型端点未配置（需填写端点与模型 ID），按判负处理"
	if result.Note != wantNote {
		t.Fatalf("note = %q, want %q", result.Note, wantNote)
	}
}

func TestPipelineCancelCurrent(t *testing.T) {
	// 15 cancelCurrent：在途请求本地结算 + 通知传输层；迟到 onDone 被忽略
	ft := &fakeTransport{script: []scriptItem{scriptHang()}}
	player := makePlayer(ft, nil)

	type res struct {
		r   engine.MoveSourceResult
		err error
	}
	done := make(chan res, 1)
	go func() {
		r, err := player.NextMove(t.Context(), simpleBoard(t), nil)
		done <- res{r, err}
	}()
	sleep(10)
	player.CancelCurrent()
	out := <-done
	if out.err != ErrCanceled {
		t.Fatalf("应以取消结算：err=%v", out.err)
	}
	if ids := ft.cancelled(); len(ids) != 1 || ids[0] != "id-1" {
		t.Fatalf("cancelledIds = %v", ids)
	}
}

// stubBuiltin 可编程内置 AI 桩。
type stubBuiltin struct {
	move   rules.Move
	status engine.MoveSourceStatus
}

func (s stubBuiltin) DisplayName() string { return "内置 AI（高级）" }
func (s stubBuiltin) NextMove(context.Context, *rules.Board, []rules.Move) (engine.MoveSourceResult, error) {
	if s.status == engine.StatusNoLegalMove {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}
	m := s.move
	return engine.MoveSourceResult{Status: engine.StatusOK, Move: &m}, nil
}

func TestChatOnceIndependentChannel(t *testing.T) {
	// 16 chatOnce 独立通道：不带合法清单协议
	ft := &fakeTransport{script: []scriptItem{scriptText("ok")}}
	client := NewLlmChatClient(playerCfg, ft, ChatClientOptions{NewID: func() string { return "c1" }})
	text, err := client.ChatOnce(t.Context(), "你是测试助手。", "请回复：ok", false)
	if err != nil {
		t.Fatal(err)
	}
	if text != "ok" {
		t.Fatalf("text = %q", text)
	}
	ft.mu.Lock()
	var body struct {
		MaxTokens int `json:"max_tokens"`
		Messages  []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal([]byte(ft.calls[0].Body), &body)
	ft.mu.Unlock()
	if body.MaxTokens != 4096 {
		t.Fatalf("max_tokens = %d", body.MaxTokens)
	}
	if body.Messages[1].Content != "请回复：ok" {
		t.Fatalf("user = %q", body.Messages[1].Content)
	}
}

// ---------------------------------------------------------------------------
// 兜底链细节（llm_move_source.dart:_fallback）
// ---------------------------------------------------------------------------

func TestFallbackBuiltinNoLegalMove(t *testing.T) {
	// 17 builtinAi 兜底无合法着法 → noLegalMove（不 failed）
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: e9-e0")}}
	player := NewLlmPlayer(playerCfg, ft, LlmPlayerOptions{
		Fallback:    FallbackBuiltinAI,
		MaxAttempts: 1,
		BuiltinAiSource: func() engine.MoveSource {
			return stubBuiltin{status: engine.StatusNoLegalMove}
		},
	}, ChatClientOptions{NewID: func() string { return "x" }})
	result, err := player.NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusNoLegalMove {
		t.Fatalf("status = %q", result.Status)
	}
}

func TestDisplayName(t *testing.T) {
	// 18 displayName：模型名/未配置占位
	ft := &fakeTransport{}
	if got := makePlayer(ft, nil).DisplayName(); got != "test-model" {
		t.Fatalf("displayName = %q", got)
	}
	empty := NewLlmPlayer(LlmEndpointConfig{BaseURL: "https://a.com"}, ft, LlmPlayerOptions{
		Fallback:        FallbackResign,
		BuiltinAiSource: noopBuiltin,
	}, ChatClientOptions{})
	if got := empty.DisplayName(); got != "（未配置模型）" {
		t.Fatalf("空模型 displayName = %q", got)
	}
}

func TestLegalCodesUnique(t *testing.T) {
	// 19 合法清单去重（codesByMove 语义）
	board := simpleBoard(t)
	legal := board.AllLegalMoves(board.Turn())
	codes := make([]string, 0, len(legal))
	for _, m := range legal {
		codes = append(codes, EncodeMove(m))
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if seen[c] {
			t.Fatalf("编码重复：%s", c)
		}
		seen[c] = true
	}
	if !seen["a9-a5"] {
		t.Fatal("清单应含 a9-a5")
	}
}

// ---------------------------------------------------------------------------
// 测试连接（llm_move_source.dart:testConnection）
// ---------------------------------------------------------------------------

func TestTestLlmConnectionOK(t *testing.T) {
	// 20 成功：固定成功消息
	ft := &fakeTransport{script: []scriptItem{scriptText("ok")}}
	res := TestLlmConnection(playerCfg, ft, "", nil)
	if !res.OK || res.Message != "连接成功，模型 test-model 响应正常" {
		t.Fatalf("结果不符：%+v", res)
	}
	if ft.callCount() != 1 {
		t.Fatalf("调用次数 = %d", ft.callCount())
	}
}

func TestTestLlmConnectionTranslateModelHint(t *testing.T) {
	// 21 失败：连接失败 + annotateModelHint（翻译模型）
	ft := &fakeTransport{script: []scriptItem{scriptError("HTTP 400: Streaming translation is not supported")}}
	res := TestLlmConnection(playerCfg, ft, "", nil)
	if res.OK {
		t.Fatal("应失败")
	}
	if !strings.Contains(res.Message, "连接失败：") || !strings.Contains(res.Message, "翻译模型（qwen-mt-* 系列）") {
		t.Fatalf("失败消息不符：%q", res.Message)
	}
}

func TestTestLlmConnectionUnconfigured(t *testing.T) {
	// 22 未配置 → 失败消息（不发起请求）
	ft := &fakeTransport{}
	res := TestLlmConnection(LlmEndpointConfig{}, ft, "", nil)
	if res.OK || !strings.Contains(res.Message, "模型端点未配置") {
		t.Fatalf("结果不符：%+v", res)
	}
	if ft.callCount() != 0 {
		t.Fatal("不应发起请求")
	}
}

func TestTestLlmConnectionImageModelHint(t *testing.T) {
	// 23 图片生成模型报错 → 通用误用提示
	ft := &fakeTransport{script: []scriptItem{
		scriptError("HTTP 400: invalid_parameter_error: Input should be 'user': input.messages"),
	}}
	res := TestLlmConnection(playerCfg, ft, "", nil)
	if !strings.Contains(res.Message, "该模型可能不支持 OpenAI 兼容对话接口") {
		t.Fatalf("失败消息不符：%q", res.Message)
	}
}

// ---------------------------------------------------------------------------
// 解析器补充（管线第 4 层入口一致性）
// ---------------------------------------------------------------------------

func TestExtractMoveMatchesWhitelistKey(t *testing.T) {
	// 24 extractMove 输出即白名单键（encodeMove 归一）
	board := simpleBoard(t)
	codes := map[string]bool{}
	for _, m := range board.AllLegalMoves(board.Turn()) {
		codes[EncodeMove(m)] = true
	}
	code := ExtractMove("着法: a9 - a5")
	if code == nil {
		t.Fatal("应解析出着法")
	}
	if !codes[*code] {
		t.Fatalf("%q 不在白名单", *code)
	}
}

func TestLlmConfigErrorType(t *testing.T) {
	// 25 LlmConfigError 可作为异常抛出并被消息匹配
	e := &LlmConfigError{Message: "x"}
	if e.Error() != "x" {
		t.Fatalf("message = %q", e.Error())
	}
}
