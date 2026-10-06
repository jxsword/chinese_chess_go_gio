package ui

// T4'.1 测试：LLM 客户端（流式 tee / AuthSlot 注入 / 走子结算 / 取消链 /
// 参谋适配器 / 测试连接）。fake 注入口径 = 00 §4"测试可注入 fake"。

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// ---- fakes ----

// llmEventCollector 事件收集器（requestId/payload 顺序记录；emit 来自
// TestConnection 等后台 goroutine，读侧加锁——-race 干净）。
type llmEventCollector struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	requestID string
	payload   any
	err       error
}

func (c *llmEventCollector) emit(requestID string, payload any, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, capturedEvent{requestID, payload, err})
}

func (c *llmEventCollector) snapshot() []capturedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]capturedEvent(nil), c.events...)
}

// ofType 以 %T 类型名匹配过滤。
func (c *llmEventCollector) ofType(t any) []capturedEvent {
	want := fmt.Sprintf("%T", t)
	var out []capturedEvent
	for _, e := range c.snapshot() {
		if fmt.Sprintf("%T", e.payload) == want {
			out = append(out, e)
		}
	}
	return out
}

// fakeInnerTransport 复制物 Transport 的 fake：记录请求、回放脚本化事件。
type fakeInnerTransport struct {
	requests  []llm.ChatRequest
	script    func(req llm.ChatRequest, h llm.ChatHandlers)
	cancelled []string
}

func (f *fakeInnerTransport) Chat(ctx context.Context, req llm.ChatRequest, handlers llm.ChatHandlers) error {
	f.requests = append(f.requests, req)
	if f.script != nil {
		f.script(req, handlers)
	}
	return nil
}

func (f *fakeInnerTransport) Cancel(requestID string) { f.cancelled = append(f.cancelled, requestID) }

// fakeMoveSource LlmMoveSource fake：阻塞至 signal，返回脚本化结果。
type fakeMoveSource struct {
	started   chan struct{}
	signal    chan struct{}
	result    engine.MoveSourceResult
	err       error
	cancelled bool
	lastCtx   context.Context
}

func newFakeMoveSource(result engine.MoveSourceResult, err error) *fakeMoveSource {
	return &fakeMoveSource{started: make(chan struct{}), signal: make(chan struct{}), result: result, err: err}
}

func (f *fakeMoveSource) NextMove(ctx context.Context, _ *rules.Board, _ []rules.Move) (engine.MoveSourceResult, error) {
	f.lastCtx = ctx
	close(f.started)
	<-f.signal
	return f.result, f.err
}

func (f *fakeMoveSource) DisplayName() string { return "fake-llm" }

func (f *fakeMoveSource) CancelCurrent() {
	f.cancelled = true
	select {
	case <-f.signal:
	default:
		close(f.signal)
	}
}

// fakeRunnerForAdvisor EngineSubmitter fake：按类型回放 canned Response。
type fakeRunnerForAdvisor struct {
	resp      engine.Response
	submits   []engine.Request
	cancels   []string
	neverSend bool // true = 回执永不到达（ctx 取消路径）
}

func (f *fakeRunnerForAdvisor) Submit(req engine.Request) <-chan engine.Response {
	f.submits = append(f.submits, req)
	ch := make(chan engine.Response, 1)
	if !f.neverSend {
		ch <- f.resp
	}
	return ch
}

func (f *fakeRunnerForAdvisor) Cancel(requestID string) { f.cancels = append(f.cancels, requestID) }

// ---- moveTransport：流式 tee（00 §4 llm:chunk/done/error 行）----

func TestMoveTransportTeeEvents(t *testing.T) {
	collector := &llmEventCollector{}
	content := "分析:测试"
	inner := &fakeInnerTransport{script: func(_ llm.ChatRequest, h llm.ChatHandlers) {
		if h.OnChunk != nil {
			h.OnChunk(llm.Delta{Content: &content})
		}
		if h.OnDone != nil {
			h.OnDone("着法: h2-e2")
		}
	}}
	tr := &moveTransport{inner: inner, outerID: "llm-move-7", resolve: func(string) string { return "" }, emit: collector.emit}
	err := tr.Chat(context.Background(), llm.ChatRequest{RequestID: "inner-1"}, llm.ChatHandlers{
		OnDone: func(string) {},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	chunks := collector.ofType(LlmStreamChunk{})
	if len(chunks) != 1 || chunks[0].requestID != "llm-move-7" {
		t.Fatalf("chunk events = %+v", chunks)
	}
	chunk := chunks[0].payload.(LlmStreamChunk)
	if chunk.Delta.Content == nil || *chunk.Delta.Content != "分析:测试" {
		t.Fatalf("chunk delta = %+v", chunk.Delta)
	}
	dones := collector.ofType(LlmStreamDone{})
	if len(dones) != 1 || dones[0].requestID != "llm-move-7" {
		t.Fatalf("done events = %+v", dones)
	}
	if dones[0].payload.(LlmStreamDone).Text != "着法: h2-e2" {
		t.Fatalf("done text mismatch")
	}
}

func TestMoveTransportTeeError(t *testing.T) {
	collector := &llmEventCollector{}
	inner := &fakeInnerTransport{script: func(_ llm.ChatRequest, h llm.ChatHandlers) {
		if h.OnError != nil {
			h.OnError("连接失败：boom")
		}
	}}
	tr := &moveTransport{inner: inner, outerID: "llm-move-8", emit: collector.emit}
	_ = tr.Chat(context.Background(), llm.ChatRequest{}, llm.ChatHandlers{})
	errs := collector.ofType(LlmStreamError{})
	if len(errs) != 1 || errs[0].requestID != "llm-move-8" {
		t.Fatalf("error events = %+v", errs)
	}
	if errs[0].payload.(LlmStreamError).Message != "连接失败：boom" {
		t.Fatalf("error message mismatch")
	}
}

// DR-010：掩码 Key 场景 AuthSlot 注入真实 Authorization。
func TestMoveTransportAuthSlotInjection(t *testing.T) {
	collector := &llmEventCollector{}
	var gotHeaders []map[string]string
	inner := &fakeInnerTransport{script: func(req llm.ChatRequest, _ llm.ChatHandlers) {
		hh := map[string]string{}
		for k, v := range req.Headers {
			hh[k] = v
		}
		gotHeaders = append(gotHeaders, hh)
	}}
	resolveCalls := []string{}
	keys := map[string]string{"llm_config_black": "sk-real-key", "llm_config_red": ""}
	tr := &moveTransport{inner: inner, outerID: "llm-move-9",
		resolve: func(slot string) string { resolveCalls = append(resolveCalls, slot); return keys[slot] },
		emit:    collector.emit}

	// 掩码 Key：请求构造层带 AuthSlot、无 Authorization → 传输层注入。
	_ = tr.Chat(context.Background(), llm.ChatRequest{RequestID: "a", AuthSlot: "llm_config_black",
		Headers: map[string]string{"Content-Type": "application/json"}}, llm.ChatHandlers{})
	h := gotHeaders[0]
	if h["Authorization"] != "Bearer sk-real-key" {
		t.Fatalf("Authorization = %q", h["Authorization"])
	}
	if len(resolveCalls) != 1 || resolveCalls[0] != "llm_config_black" {
		t.Fatalf("resolve calls = %v", resolveCalls)
	}

	// 槽位无 Key：删除 Authorization（本地网关语义）。
	_ = tr.Chat(context.Background(), llm.ChatRequest{RequestID: "b", AuthSlot: "llm_config_red",
		Headers: map[string]string{"Authorization": "Bearer masked"}}, llm.ChatHandlers{})
	if _, ok := gotHeaders[1]["Authorization"]; ok {
		t.Fatalf("Authorization must be dropped when slot has no key")
	}

	// 完整 Key 内联：无 AuthSlot → 原样透传，不调 resolve。
	_ = tr.Chat(context.Background(), llm.ChatRequest{RequestID: "c",
		Headers: map[string]string{"Authorization": "Bearer inline"}}, llm.ChatHandlers{})
	if gotHeaders[2]["Authorization"] != "Bearer inline" || len(resolveCalls) != 2 {
		t.Fatalf("inline key path broken: %v / %v", gotHeaders[2], resolveCalls)
	}
}

// ---- Cancel（#G5 三收口；结算回执经事件总线在页面测试覆盖）----

func TestLlmClientCancelInFlight(t *testing.T) {
	collector := &llmEventCollector{}
	client := NewLlmClient(GameEnv{Emit: collector.emit, Cancel: func(string) {}}, nil, nil)
	client.moveRequest = "llm-move-1"
	client.moveCancel = func() {}
	fakePlayer := newFakeMoveSource(engine.MoveSourceResult{}, nil)
	client.movePlayer = fakePlayer
	client.Cancel("llm-move-1")
	if !fakePlayer.cancelled {
		t.Fatalf("Cancel must call player.CancelCurrent")
	}
	if client.moveRequest != "" || client.movePlayer != nil || client.moveCancel != nil {
		t.Fatalf("in-flight state must be cleared")
	}
}

// fakeMoveSource 保留给取消链测试与后续页面测试复用。

func TestLlmClientCancelUnknownIDNoop(t *testing.T) {
	collector := &llmEventCollector{}
	cancelled := []string{}
	client := NewLlmClient(GameEnv{Emit: collector.emit, Cancel: func(id string) { cancelled = append(cancelled, id) }}, nil, nil)
	client.Cancel("nonexistent")
	if len(cancelled) != 1 || cancelled[0] != "nonexistent" {
		t.Fatalf("bus cancel must still fire (drop-late registration)")
	}
	if client.moveRequest != "" {
		t.Fatalf("no in-flight expected")
	}
}

func TestLlmClientCancelEmptyIDNoop(t *testing.T) {
	client := NewLlmClient(GameEnv{Emit: func(string, any, error) {}, Cancel: func(string) {}}, nil, nil)
	client.Cancel("") // 不 panic、不触碰总线
}

// ---- EngineRunnerAdapter（参谋同步面）----

func TestEngineRunnerAdapterFindBestMoveEx(t *testing.T) {
	best := engine.WireMoveFromRules(rules.Move{From: rules.Position{Col: 7, Row: 9}, To: rules.Position{Col: 7, Row: 4}})
	runner := &fakeRunnerForAdvisor{resp: engine.Response{Result: engine.WireReport{
		Best:   best,
		BestCp: 120,
		TopK:   []engine.WireTopKEntry{{Move: best, Cp: 120}},
	}}}
	a := NewEngineRunnerAdapter(runner)
	report, err := a.FindBestMoveEx(context.Background(), "fen", 3, 5, 5000)
	if err != nil {
		t.Fatalf("FindBestMoveEx: %v", err)
	}
	if report == nil || report.BestCp != 120 || len(report.TopK) != 1 {
		t.Fatalf("report = %+v", report)
	}
	if len(runner.submits) != 1 || runner.submits[0].Type != engine.ReqFindBestMoveEx {
		t.Fatalf("submits = %+v", runner.submits)
	}
	// 可复现铁律：参谋请求载荷不含 HistoryFens 字段。
	if strings.Contains(string(runner.submits[0].Payload), "historyFens") {
		t.Fatalf("advisor payload must not carry historyFens")
	}
}

func TestEngineRunnerAdapterEvaluateMove(t *testing.T) {
	m := rules.Move{From: rules.Position{Col: 7, Row: 9}, To: rules.Position{Col: 7, Row: 4}}
	runner := &fakeRunnerForAdvisor{resp: engine.Response{Result: 42}}
	a := NewEngineRunnerAdapter(runner)
	cp, err := a.EvaluateMove(context.Background(), "fen", m, 2)
	if err != nil || cp == nil || *cp != 42 {
		t.Fatalf("cp = %v, err = %v", cp, err)
	}
	if runner.submits[0].Type != engine.ReqEvaluateMove {
		t.Fatalf("type = %q", runner.submits[0].Type)
	}
}

func TestEngineRunnerAdapterErrorAndCancel(t *testing.T) {
	runner := &fakeRunnerForAdvisor{resp: engine.Response{Error: "engine boom"}}
	a := NewEngineRunnerAdapter(runner)
	if _, err := a.FindBestMoveEx(context.Background(), "fen", 3, 5, 5000); err == nil || err.Error() != "engine boom" {
		t.Fatalf("err = %v", err)
	}

	// ctx 取消：回执永不到达 → ErrCanceled + 引擎请求撤销。
	never := &fakeRunnerForAdvisor{neverSend: true}
	a2 := NewEngineRunnerAdapter(never)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := a2.EvaluateMove(ctx, "fen", rules.Move{}, 2)
	if !errors.Is(err, llm.ErrCanceled) {
		t.Fatalf("err = %v, want ErrCanceled", err)
	}
	if len(never.cancels) != 1 {
		t.Fatalf("engine request must be cancelled on ctx done, cancels = %v", never.cancels)
	}
}

// ---- TestConnectionAsync（真实 SSE 形状；mock 端点 httptest）----

func TestTestConnectionAsyncOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()
	collector := &llmEventCollector{}
	client := NewLlmClient(GameEnv{Emit: collector.emit, Cancel: func(string) {}}, nil, nil)
	cfg := llm.LlmEndpointConfig{BaseURL: srv.URL, Model: "test-model"}
	client.TestConnectionAsync("test-1", cfg, "")
	waitForEvent(t, collector, LlmTestDone{}, 5*time.Second, func(e capturedEvent) bool {
		done := e.payload.(LlmTestDone)
		return done.OK && strings.Contains(done.Message, "连接成功")
	})
}

func TestTestConnectionAsyncHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer srv.Close()
	collector := &llmEventCollector{}
	client := NewLlmClient(GameEnv{Emit: collector.emit, Cancel: func(string) {}}, nil, nil)
	cfg := llm.LlmEndpointConfig{BaseURL: srv.URL, Model: "test-model"}
	client.TestConnectionAsync("test-2", cfg, "")
	waitForEvent(t, collector, LlmTestDone{}, 5*time.Second, func(e capturedEvent) bool {
		done := e.payload.(LlmTestDone)
		return !done.OK && strings.Contains(done.Message, "连接失败")
	})
}

// ---- helpers ----

// waitForEvent 等待首个满足条件的事件（测试 goroutine 轮询；typeOf 以 exemplar 匹配）。
func waitForEvent(t *testing.T, c *llmEventCollector, typeOf any, timeout time.Duration, pred func(capturedEvent) bool) {
	t.Helper()
	want := fmt.Sprintf("%T", typeOf)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, e := range c.snapshot() {
			if fmt.Sprintf("%T", e.payload) == want && pred(e) {
				return
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for %s event; got %+v", want, c.snapshot())
}
