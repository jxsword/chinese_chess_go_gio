package ui

// 引擎客户端单测（T3'.1，09 §4：纯 Go 表驱动，不渲染）。
// 锚点 = 00 §4 engine Worker 行（Runner 直调/结果经事件总线/requestId 收口）
// + 03 §6.3（参谋接口不传历史——可复现铁律）+ 上游 engineClient.ts 迟到丢弃语义。

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// fakeRunner 测试注入 fake（00 §4：消息形状 {id,type,payload} 在 Runner 内部
// 保留，测试可注入 fake）；响应由测试经 deliver 手工投递。
type fakeRunner struct {
	mu       sync.Mutex
	requests []engine.Request
	cancels  []string
	resp     chan engine.Response
}

func newFakeRunner() *fakeRunner { return &fakeRunner{resp: make(chan engine.Response, 4)} }

func (f *fakeRunner) Submit(req engine.Request) <-chan engine.Response {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	ch := make(chan engine.Response, 1)
	go func() {
		select {
		case r := <-f.resp:
			ch <- r
		case <-time.After(5 * time.Second):
			ch <- engine.Response{ID: req.ID, Error: "fake timeout"}
		}
	}()
	return ch
}

func (f *fakeRunner) Cancel(id string) {
	f.mu.Lock()
	f.cancels = append(f.cancels, id)
	f.mu.Unlock()
}

func (f *fakeRunner) deliver(resp engine.Response) { f.resp <- resp }

func (f *fakeRunner) lastRequest(t *testing.T) engine.Request {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("fake 未收到请求")
	}
	return f.requests[len(f.requests)-1]
}

// jsonAny RawMessage → any（配合 remarshal 反解载荷）。
func jsonAny(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("载荷反序列化: %v", err)
	}
	return v
}

// emitCollector 收集 EngineClient 回执（模拟事件总线→页面 OnAppEvent 方向）。
type emitCollector struct {
	mu   sync.Mutex
	got  []any
	errs []error
}

func (c *emitCollector) emitFunc() func(string, any, error) {
	return func(_ string, payload any, err error) {
		c.mu.Lock()
		c.got = append(c.got, payload)
		c.errs = append(c.errs, err)
		c.mu.Unlock()
	}
}

// waitPayload 等待并弹出首个回执（响应 goroutine 异步 emit；多次调用按序消费）。
func (c *emitCollector) waitPayload(t *testing.T) (any, error) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		if len(c.got) > 0 {
			payload, err := c.got[0], c.errs[0]
			c.got = c.got[1:]
			c.errs = c.errs[1:]
			c.mu.Unlock()
			return payload, err
		}
		c.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
	t.Fatal("回执超时")
	return nil, nil
}

func newTestEngineClient(runner EngineSubmitter) (*EngineClient, *emitCollector) {
	c := &emitCollector{}
	env := GameEnv{Emit: c.emitFunc(), Cancel: func(string) {}}
	return NewEngineClient(env, runner), c
}

const engineTestFen = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w"

// spec: FindBestMoveAsync——请求载荷含 difficulty 与 historyFens 拷贝；
// ok 响应解码为 rules.Move 回执。
func TestEngineClientFindBestMove(t *testing.T) {
	fake := newFakeRunner()
	client, collector := newTestEngineClient(fake)

	history := []string{"a", "b"}
	client.FindBestMoveAsync("ai-1", engineTestFen, 3, history)
	history[0] = "mutated" // 调用方随后改写不影响已拷贝入参（DR-018）

	req := fake.lastRequest(t)
	if req.Type != engine.ReqFindBestMove {
		t.Fatalf("请求类型 = %q, 欲 %q", req.Type, engine.ReqFindBestMove)
	}
	if req.ID != "ai-1" {
		t.Fatalf("请求 id = %q", req.ID)
	}
	var p engine.FindBestMovePayload
	if err := remarshal(jsonAny(t, req.Payload), &p); err != nil {
		t.Fatalf("载荷反解: %v", err)
	}
	if p.Fen != engineTestFen || p.Difficulty != 3 {
		t.Fatalf("载荷 = %+v", p)
	}
	if len(p.HistoryFens) != 2 || p.HistoryFens[0] != "a" {
		t.Fatalf("historyFens 应为调用时拷贝: %v", p.HistoryFens)
	}

	fake.deliver(engine.Response{ID: "ai-1", OK: true, Result: engine.WireMove{
		From: engine.WirePos{Col: 7, Row: 7}, To: engine.WirePos{Col: 7, Row: 0},
	}})
	got, err := collector.waitPayload(t)
	if err != nil {
		t.Fatalf("回执错误: %v", err)
	}
	done := got.(EngineMoveDone)
	if done.RequestID != "ai-1" || done.Move == nil || done.Move.From != rules.Pos(7, 7) || done.Move.To != rules.Pos(7, 0) {
		t.Fatalf("回执 = %+v", done)
	}
}

// spec: 不传历史（historyFens=nil）→ 载荷 HistoryFens 缺省（关闭重复治理，
// 对拍口径不变）。
func TestEngineClientFindBestMoveNoHistory(t *testing.T) {
	fake := newFakeRunner()
	client, _ := newTestEngineClient(fake)
	client.FindBestMoveAsync("ai-2", engineTestFen, 5, nil)

	req := fake.lastRequest(t)
	var p engine.FindBestMovePayload
	if err := remarshal(jsonAny(t, req.Payload), &p); err != nil {
		t.Fatalf("载荷反解: %v", err)
	}
	if len(p.HistoryFens) != 0 {
		t.Fatalf("缺省应不携带 HistoryFens: %v", p.HistoryFens)
	}
}

// spec: 参谋接口一律不传历史（可复现铁律）——FindBestMoveEx/EvaluateMove
// 载荷形状不含 HistoryFens 字段。
func TestEngineClientAdvisoryNoHistory(t *testing.T) {
	fake := newFakeRunner()
	client, collector := newTestEngineClient(fake)

	client.FindBestMoveExAsync("ex-1", engineTestFen, 4, 5, 1000)
	req := fake.lastRequest(t)
	if req.Type != engine.ReqFindBestMoveEx {
		t.Fatalf("请求类型 = %q", req.Type)
	}
	var ex engine.FindBestMoveExPayload
	if err := remarshal(jsonAny(t, req.Payload), &ex); err != nil {
		t.Fatalf("载荷反解: %v", err)
	}
	if ex.Depth != 4 || ex.TopK != 5 || ex.TimeLimitMs != 1000 {
		t.Fatalf("载荷 = %+v", ex)
	}

	client.EvaluateMoveAsync("ev-1", engineTestFen, rules.Move{From: rules.Pos(7, 7), To: rules.Pos(7, 0)}, 3)
	req = fake.lastRequest(t)
	if req.Type != engine.ReqEvaluateMove {
		t.Fatalf("请求类型 = %q", req.Type)
	}
	var ev engine.EvaluateMovePayload
	if err := remarshal(jsonAny(t, req.Payload), &ev); err != nil {
		t.Fatalf("载荷反解: %v", err)
	}
	if ev.Depth != 3 || ev.Fen != engineTestFen {
		t.Fatalf("载荷 = %+v", ev)
	}

	// 参谋报告解码回执（Top-K 二元组形状）
	fake.deliver(engine.Response{ID: "ex-1", OK: true, Result: engine.WireReport{
		Best:   engine.WireMove{From: engine.WirePos{Col: 0, Row: 0}, To: engine.WirePos{Col: 0, Row: 1}},
		BestCp: 42,
		TopK:   []engine.WireTopKEntry{{Move: engine.WireMove{From: engine.WirePos{Col: 0, Row: 0}, To: engine.WirePos{Col: 0, Row: 1}}, Cp: 42}},
	}})
	got, err := collector.waitPayload(t)
	if err != nil {
		t.Fatalf("参谋回执错误: %v", err)
	}
	report := got.(EngineReportDone)
	if report.Report == nil || report.Report.BestCp != 42 || len(report.Report.TopK) != 1 {
		t.Fatalf("参谋回执 = %+v", report)
	}
}

// spec: Cancel——Runner 取消 + 总线取消双收口（#G5）。
func TestEngineClientCancel(t *testing.T) {
	fake := newFakeRunner()
	c := &emitCollector{}
	var canceled []string
	var mu sync.Mutex
	env := GameEnv{Emit: c.emitFunc(), Cancel: func(id string) { mu.Lock(); canceled = append(canceled, id); mu.Unlock() }}
	client := NewEngineClient(env, fake)

	client.FindBestMoveAsync("ai-3", engineTestFen, 1, nil)
	client.Cancel("ai-3")

	fake.mu.Lock()
	n := len(fake.cancels)
	fake.mu.Unlock()
	if n != 1 || fake.cancels[0] != "ai-3" {
		t.Fatalf("Runner 取消记录 = %v", fake.cancels)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(canceled) != 1 || canceled[0] != "ai-3" {
		t.Fatalf("总线取消记录 = %v", canceled)
	}
}

// spec: 错误/取消响应 → 回执 Err 非空（迟到丢弃由 app 事件总线承担，见
// eventbus_test；此处验证解码层透传）。
func TestEngineClientErrorResponse(t *testing.T) {
	fake := newFakeRunner()
	client, collector := newTestEngineClient(fake)
	client.FindBestMoveAsync("ai-4", engineTestFen, 1, nil)
	fake.deliver(engine.Response{ID: "ai-4", Error: "canceled"})

	if _, err := collector.waitPayload(t); err == nil || err.Error() != "canceled" {
		t.Fatalf("应透传 canceled 错误: %v", err)
	}
}

// spec: 真复制物 Runner 端到端（difficulty 1 浅搜索）——回执 Move 为合法着法
// （接口契约：playMove 仍做最终校验）。
func TestEngineClientRealRunnerEndToEnd(t *testing.T) {
	client, collector := newTestEngineClient(nil) // nil = 复制物 engine.NewRunner()
	client.FindBestMoveAsync("ai-5", engineTestFen, 1, nil)

	got, err := collector.waitPayload(t)
	if err != nil {
		t.Fatalf("回执错误: %v", err)
	}
	done := got.(EngineMoveDone)
	if done.Move == nil {
		t.Fatal("初始局面应返回着法")
	}
	b, err := rules.FromFen(engineTestFen)
	if err != nil {
		t.Fatalf("FEN 解析: %v", err)
	}
	legal := false
	for _, m := range b.LegalMovesFor(done.Move.From) {
		if rules.SamePos(m.To, done.Move.To) {
			legal = true
			break
		}
	}
	if !legal {
		t.Fatalf("回执着法不合法: %+v", done.Move)
	}
}
