// 求解器协议层（04 文档 §2/§9.4；00 §3.2 通道清单，DR-003 goroutine + ctx）。
//
// Worker 消息形状在内部协议保留（铁律 #7）：
//   - 请求 {id, type: 'solve'|'isWinningFirstMove'|'cancel', payload}
//   - 响应 {id, ok, result?, error?}
//
// 与 Electron 版 solver.worker 的差异（同 DR-003）：Go 侧每请求独立 goroutine +
// context 取消（Runner 注册表），无单线程 FIFO 队列；迟到响应仍由前端按 id 丢弃
// （00 §3.2 主语义），协议层收口：ctx 已取消时 Handle 直接回 canceled，不把取消
// 后的 timeout 结果当有效应答。
//
// wire 载荷形状对齐前端 src/api/solverProtocol.ts（SolveResult = {status,
// solutions:[{moves:[{from,to,captured?}]}], elapsed, searchedPlies}）。
// wire 形状与 engine 包各自独立（TS 包边界 mirror：solver 不依赖 engine）。
// 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。
package solver

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// RequestType 请求类型（对齐前端 SolverRequestType）。
type RequestType string

const (
	ReqSolve              RequestType = "solve"
	ReqIsWinningFirstMove RequestType = "isWinningFirstMove"
	ReqCancel             RequestType = "cancel"
)

// Request 请求消息 {id, type, payload}。
type Request struct {
	ID      string          `json:"id"`
	Type    RequestType     `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Response 响应消息 {id, ok, result?, error?}。
type Response struct {
	ID     string `json:"id"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// ErrCanceled 取消错误（文案对齐前端 SOLVER_CANCELED_ERROR = 'canceled'）。
var ErrCanceled = errors.New("canceled")

// WirePos wire 坐标（对齐 TS Position JSON）。
type WirePos struct {
	Col int `json:"col"`
	Row int `json:"row"`
}

// WirePiece wire 棋子（kind/side 字符串）。
type WirePiece struct {
	Kind string `json:"kind"`
	Side string `json:"side"`
}

// WireMove wire 走法（对齐 TS Move：captured 走空格时省略）。
type WireMove struct {
	From     WirePos    `json:"from"`
	To       WirePos    `json:"to"`
	Captured *WirePiece `json:"captured,omitempty"`
}

// WireMoveFromRules 规则层 Move → wire。
func WireMoveFromRules(m rules.Move) WireMove {
	w := WireMove{
		From: WirePos{Col: m.From.Col, Row: m.From.Row},
		To:   WirePos{Col: m.To.Col, Row: m.To.Row},
	}
	if m.Captured != nil {
		w.Captured = &WirePiece{Kind: string(m.Captured.Kind), Side: string(m.Captured.Side)}
	}
	return w
}

// ToRules wire → 规则层 Move（首着验证用，from/to 之外字段不参与判定）。
func (w WireMove) ToRules() rules.Move {
	m := rules.Move{
		From: rules.Pos(w.From.Col, w.From.Row),
		To:   rules.Pos(w.To.Col, w.To.Row),
	}
	if w.Captured != nil {
		m.Captured = &rules.Piece{Kind: rules.Kind(w.Captured.Kind), Side: rules.Side(w.Captured.Side)}
	}
	return m
}

// SolvePayload solve 请求载荷（对齐前端 SolvePayload；0=未设 → 缺省 30s/9）。
type SolvePayload struct {
	Fen         string `json:"fen"`
	TimeLimitMs int    `json:"timeLimitMs,omitempty"`
	MaxPlies    int    `json:"maxPlies,omitempty"`
}

// IsWinningFirstMovePayload isWinningFirstMove 请求载荷；firstMove 以 RawMessage
// 承接（app.go 传入渲染层原始 JSON 对象），Handle 内解为 WireMove。
type IsWinningFirstMovePayload struct {
	Fen         string          `json:"fen"`
	FirstMove   json.RawMessage `json:"firstMove"`
	Plies       int             `json:"plies,omitempty"`
	TimeLimitMs int             `json:"timeLimitMs,omitempty"`
}

// WireSolution 一条强制线路（对齐 TS SolverSolution）。
type WireSolution struct {
	Moves []WireMove `json:"moves"`
}

// WireSolveResult 求解结果 wire（对齐 TS SolveResult；elapsed 毫秒）。
type WireSolveResult struct {
	Status        string         `json:"status"`
	Solutions     []WireSolution `json:"solutions"`
	Elapsed       int64          `json:"elapsed"`
	SearchedPlies int            `json:"searchedPlies"`
}

// wireResultFromRules 领域结果 → wire（solutions 恒非 nil，对齐 TS 数组形状）。
func wireResultFromRules(r Result) WireSolveResult {
	out := WireSolveResult{
		Status:        string(r.Status),
		Solutions:     make([]WireSolution, 0, len(r.Solutions)),
		Elapsed:       r.Elapsed,
		SearchedPlies: r.SearchedPlies,
	}
	for _, s := range r.Solutions {
		moves := make([]WireMove, 0, len(s.Moves))
		for _, m := range s.Moves {
			moves = append(moves, WireMoveFromRules(m))
		}
		out.Solutions = append(out.Solutions, WireSolution{Moves: moves})
	}
	return out
}

// Handle 协议分发：payload 反解 → 求解器调用 → Response。
// ctx 已取消时直接回 canceled（取消=调用方丢弃语义收口，04 §4）：
// 入口即回不空算，计算后复查不把取消期间完成的 timeout 结果当有效应答。
func Handle(ctx context.Context, req Request) Response {
	if req.Type != ReqCancel && ctx.Err() != nil {
		return Response{ID: req.ID, Error: ErrCanceled.Error()}
	}
	switch req.Type {
	case ReqCancel:
		// cancel 由 Runner/绑定层注册表执行；协议层幂等空响应。
		return Response{ID: req.ID, OK: true}
	case ReqSolve:
		var p SolvePayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		result, err := Solve(p.Fen, Options{
			TimeLimitMs: p.TimeLimitMs,
			MaxPlies:    p.MaxPlies,
			Ctx:         ctx,
		})
		if err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		if ctx.Err() != nil {
			return Response{ID: req.ID, Error: ErrCanceled.Error()}
		}
		return Response{ID: req.ID, OK: true, Result: wireResultFromRules(result)}
	case ReqIsWinningFirstMove:
		var p IsWinningFirstMovePayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		var wm WireMove
		if err := json.Unmarshal(p.FirstMove, &wm); err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		win, err := IsWinningFirstMove(p.Fen, wm.ToRules(), Options{
			TimeLimitMs: p.TimeLimitMs,
			MaxPlies:    p.Plies,
			Ctx:         ctx,
		})
		if err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		if ctx.Err() != nil {
			return Response{ID: req.ID, Error: ErrCanceled.Error()}
		}
		return Response{ID: req.ID, OK: true, Result: win}
	default:
		return Response{ID: req.ID, Error: "Unknown solver request type: " + string(req.Type)}
	}
}

// Runner 请求运行器：每请求独立 goroutine + context 取消注册表（DR-003）。
// Cancel 对在途请求触发 ctx 取消（搜索在 512 节点探针处中止）；
// 对未开始/已结束的请求幂等。响应经带缓冲通道回投，goroutine 不泄漏。
type Runner struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewRunner 构造运行器。
func NewRunner() *Runner {
	return &Runner{cancels: make(map[string]context.CancelFunc)}
}

// Submit 受理请求：立即返回响应通道（单请求单 goroutine，无 FIFO 排队；
// 串行语义由调用方按需自保证——工作室页同一时刻至多一个在途求解）。
// requestID 唯一性是 00 §3.2 调用方契约（前端 UUID 工厂）；同 id 并发在途
// 属契约违例，注册表以后到者为准（同 KNOWN_ISSUES K17 口径）。
func (r *Runner) Submit(req Request) <-chan Response {
	ch := make(chan Response, 1)
	if req.Type == ReqCancel {
		r.Cancel(req.ID)
		ch <- Response{ID: req.ID, OK: true}
		return ch
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancels[req.ID] = cancel
	r.mu.Unlock()
	go func() {
		resp := Handle(ctx, req)
		r.mu.Lock()
		delete(r.cancels, req.ID)
		r.mu.Unlock()
		cancel() // 释放 ctx 资源
		ch <- resp
	}()
	return ch
}

// Cancel 取消在途请求（幂等；未知 id 无操作）。
func (r *Runner) Cancel(requestID string) {
	r.mu.Lock()
	cancel, ok := r.cancels[requestID]
	delete(r.cancels, requestID)
	r.mu.Unlock()
	if ok {
		cancel()
	}
}
