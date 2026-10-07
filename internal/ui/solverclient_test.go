package ui

// 求解器客户端单测（T6'.1，09 §4：纯 Go 表驱动，不渲染）。
// 锚点 = 00 §4 solve:done / solve:win:done 行（Runner 直调/结果经事件总线/
// requestId 收口）+ 04 §2/§3（solve 与 isWinningFirstMove 载荷形状）。

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/solver"
)

// fakeSolver 测试注入 fake；响应由测试经 deliver 手工投递。
type fakeSolver struct {
	mu       sync.Mutex
	requests []solver.Request
	cancels  []string
	resp     chan solver.Response
}

func newFakeSolver() *fakeSolver { return &fakeSolver{resp: make(chan solver.Response, 4)} }

func (f *fakeSolver) Submit(req solver.Request) <-chan solver.Response {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	f.mu.Unlock()
	ch := make(chan solver.Response, 1)
	go func() {
		select {
		case r := <-f.resp:
			ch <- r
		case <-time.After(5 * time.Second):
			ch <- solver.Response{ID: req.ID, Error: "fake timeout"}
		}
	}()
	return ch
}

func (f *fakeSolver) Cancel(id string) {
	f.mu.Lock()
	f.cancels = append(f.cancels, id)
	f.mu.Unlock()
}

func (f *fakeSolver) deliver(resp solver.Response) { f.resp <- resp }

func (f *fakeSolver) lastRequest(t *testing.T) solver.Request {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("fake 未收到请求")
	}
	return f.requests[len(f.requests)-1]
}

func newTestSolverClient(runner SolverSubmitter) (*SolverClient, *emitCollector) {
	c := &emitCollector{}
	env := GameEnv{Emit: c.emitFunc(), Cancel: func(string) {}}
	return NewSolverClient(env, runner), c
}

const solverTestFen = "3k5/9/9/9/R8/8R/9/9/9/4K4 w"

// spec: SolveAsync——载荷形状 {fen,timeLimitMs,maxPlies}；ok 响应解码为
// WireSolveResult 回执（solutions 保持 wire 形状）。
func TestSolverClientSolve(t *testing.T) {
	fake := newFakeSolver()
	client, collector := newTestSolverClient(fake)

	client.SolveAsync("solve-1", solverTestFen, 30_000, 9)

	req := fake.lastRequest(t)
	if req.Type != solver.ReqSolve {
		t.Fatalf("请求类型 = %q, 欲 %q", req.Type, solver.ReqSolve)
	}
	if req.ID != "solve-1" {
		t.Fatalf("请求 id = %q", req.ID)
	}
	var p solver.SolvePayload
	if err := remarshal(jsonAny(t, req.Payload), &p); err != nil {
		t.Fatalf("载荷反解: %v", err)
	}
	if p.Fen != solverTestFen || p.TimeLimitMs != 30_000 || p.MaxPlies != 9 {
		t.Fatalf("载荷 = %+v", p)
	}

	want := solver.WireSolveResult{
		Status: "solved",
		Solutions: []solver.WireSolution{{
			Moves: []solver.WireMove{{From: solver.WirePos{Col: 0, Row: 5}, To: solver.WirePos{Col: 3, Row: 5}}},
		}},
		Elapsed:       12,
		SearchedPlies: 3,
	}
	fake.deliver(solver.Response{ID: "solve-1", OK: true, Result: want})

	got, err := collector.waitPayload(t)
	if err != nil {
		t.Fatalf("回执错误: %v", err)
	}
	done := got.(SolveDone)
	if done.RequestID != "solve-1" || done.Result == nil {
		t.Fatalf("回执 = %+v", done)
	}
	if !reflect.DeepEqual(*done.Result, want) {
		t.Fatalf("结果解码不一致: %+v vs %+v", *done.Result, want)
	}
}

// spec: IsWinningFirstMoveAsync——载荷 {fen,firstMove,plies,timeLimitMs}；
// ok 响应解码为 bool 回执。
func TestSolverClientIsWinningFirstMove(t *testing.T) {
	fake := newFakeSolver()
	client, collector := newTestSolverClient(fake)

	move := rules.Move{From: rules.Pos(0, 5), To: rules.Pos(3, 5)}
	client.IsWinningFirstMoveAsync("win-1", solverTestFen, move, 3, 10_000)

	req := fake.lastRequest(t)
	if req.Type != solver.ReqIsWinningFirstMove {
		t.Fatalf("请求类型 = %q", req.Type)
	}
	var p solver.IsWinningFirstMovePayload
	if err := remarshal(jsonAny(t, req.Payload), &p); err != nil {
		t.Fatalf("载荷反解: %v", err)
	}
	if p.Fen != solverTestFen || p.Plies != 3 || p.TimeLimitMs != 10_000 {
		t.Fatalf("载荷 = %+v", p)
	}
	var wm solver.WireMove
	if err := json.Unmarshal(p.FirstMove, &wm); err != nil {
		t.Fatalf("firstMove 反解: %v", err)
	}
	if wm.From.Col != 0 || wm.From.Row != 5 || wm.To.Col != 3 || wm.To.Row != 5 {
		t.Fatalf("firstMove = %+v", wm)
	}

	fake.deliver(solver.Response{ID: "win-1", OK: true, Result: true})
	got, err := collector.waitPayload(t)
	if err != nil {
		t.Fatalf("回执错误: %v", err)
	}
	done := got.(SolveWinDone)
	if done.RequestID != "win-1" || !done.Win {
		t.Fatalf("回执 = %+v", done)
	}
}

// spec: Cancel——Runner 取消 + 总线取消双收口（#G5）；空 id 无操作。
func TestSolverClientCancel(t *testing.T) {
	fake := newFakeSolver()
	c := &emitCollector{}
	var mu sync.Mutex
	var canceled []string
	env := GameEnv{Emit: c.emitFunc(), Cancel: func(id string) { mu.Lock(); canceled = append(canceled, id); mu.Unlock() }}
	client := NewSolverClient(env, fake)

	client.SolveAsync("solve-2", solverTestFen, 1000, 3)
	client.Cancel("solve-2")
	client.Cancel("")

	fake.mu.Lock()
	n := len(fake.cancels)
	first := fake.cancels[0]
	fake.mu.Unlock()
	if n != 1 || first != "solve-2" {
		t.Fatalf("Runner 取消记录 = %v", fake.cancels)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(canceled) != 1 || canceled[0] != "solve-2" {
		t.Fatalf("总线取消记录 = %v", canceled)
	}
}

// spec: 错误响应（含 canceled）→ 回执 Err 透传（解码层；迟到丢弃由总线承担）。
func TestSolverClientErrorResponse(t *testing.T) {
	fake := newFakeSolver()
	client, collector := newTestSolverClient(fake)
	client.SolveAsync("solve-3", solverTestFen, 1000, 3)
	fake.deliver(solver.Response{ID: "solve-3", Error: solver.ErrCanceled.Error()})

	if _, err := collector.waitPayload(t); err == nil || err.Error() != "canceled" {
		t.Fatalf("应透传 canceled 错误: %v", err)
	}
}

// spec: 真复制物 Runner 端到端——FEN-A 多解金标准局面（04 §5 验证门），
// solve 回执 status=solved；isWinningFirstMove 裁判杀着为真。
func TestSolverClientRealRunnerEndToEnd(t *testing.T) {
	client, collector := newTestSolverClient(nil) // nil = 复制物 solver.NewRunner()

	client.SolveAsync("solve-4", solverTestFen, 10_000, 3)
	got, err := collector.waitPayload(t)
	if err != nil {
		t.Fatalf("求解回执错误: %v", err)
	}
	done := got.(SolveDone)
	if done.Result == nil || done.Result.Status != "solved" || len(done.Result.Solutions) == 0 {
		t.Fatalf("FEN-A 应有解: %+v", done.Result)
	}

	// 杀着 h6-h5（col 0 row 5 → col 3 row 5？FEN-A 杀着按金标准锚点取回执着法首着）
	first := done.Result.Solutions[0].Moves[0].ToRules()
	client.IsWinningFirstMoveAsync("win-2", solverTestFen, first, 3, 10_000)
	got, err = collector.waitPayload(t)
	if err != nil {
		t.Fatalf("裁判回执错误: %v", err)
	}
	win := got.(SolveWinDone)
	if !win.Win {
		t.Fatalf("解法首着应通过必胜验证: %+v", first)
	}
}
