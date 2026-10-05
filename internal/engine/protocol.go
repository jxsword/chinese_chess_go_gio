package engine

// 引擎协议层（03 文档 §7；00 §3.2 通道清单，DR-003 goroutine + ctx）。
//
// Worker 消息形状在内部协议保留（铁律 #7）：
//   - 请求 {id, type: 'findBestMove'|'findBestMoveEx'|'evaluateMove'|'cancel', payload}
//   - 响应 {id, ok, result?, error?}
//
// 与 Electron 版 engine.worker 的差异（DR-003）：Go 侧每请求独立 goroutine +
// context 取消（Runner 注册表），无单线程 FIFO 队列；迟到响应仍由前端按 id 丢弃
// （00 §3.2 主语义），协议层收口：ctx 已取消时 Handle 直接回 canceled，不把取消
// 后的部分结果当有效应答。
//
// wire 载荷形状对齐前端 src/api/engineProtocol.ts（Move = {from:{col,row}, to:{col,row},
// captured?}，EngineReport.topK = [move, cp] 二元组数组）。
// 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// RequestType 请求类型（对齐前端 EngineRequestType）。
type RequestType string

const (
	ReqFindBestMove   RequestType = "findBestMove"
	ReqFindBestMoveEx RequestType = "findBestMoveEx"
	ReqEvaluateMove   RequestType = "evaluateMove"
	ReqCancel         RequestType = "cancel"
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

// ErrCanceled 取消错误（文案对齐前端 CANCELED_ERROR = 'canceled'）。
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

// ToRules wire → 规则层 Move（不含 piece 字段，与 packedToMove 同口径）。
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

// FindBestMovePayload findBestMove 请求载荷（对齐前端 FindBestMovePayload）。
type FindBestMovePayload struct {
	Fen         string   `json:"fen"`
	Difficulty  int      `json:"difficulty,omitempty"`  // 0=未设 → 缺省 3（03 §7 wire 缺省约定）
	HistoryFens []string `json:"historyFens,omitempty"` // 缺省/空 = 关闭 L2 回避
}

// FindBestMoveExPayload findBestMoveEx 请求载荷。
type FindBestMoveExPayload struct {
	Fen         string `json:"fen"`
	Depth       int    `json:"depth,omitempty"`       // 0=未设 → 6
	TopK        int    `json:"topK,omitempty"`        // 0=未设 → 5
	TimeLimitMs int    `json:"timeLimitMs,omitempty"` // 0=未设 → 5000
}

// EvaluateMovePayload evaluateMove 请求载荷；move 以 RawMessage 承接
// （app.go 传入渲染层原始 JSON 对象），Handle 内解为 WireMove。
type EvaluateMovePayload struct {
	Fen   string          `json:"fen"`
	Move  json.RawMessage `json:"move"`
	Depth int             `json:"depth,omitempty"` // 0=未设 → 4
}

// WireTopKEntry topK 二元组表项：JSON 序列化为 [move, cp]（对齐 TS Array<[Move, number]>）。
type WireTopKEntry struct {
	Move WireMove
	Cp   int
}

// MarshalJSON 序列化为二元组数组。
func (e WireTopKEntry) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{e.Move, e.Cp})
}

// UnmarshalJSON 从二元组数组反解。
func (e *WireTopKEntry) UnmarshalJSON(data []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(data, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return errors.New("engine: topK 表项须为 [move, cp] 二元组")
	}
	if err := json.Unmarshal(pair[0], &e.Move); err != nil {
		return err
	}
	return json.Unmarshal(pair[1], &e.Cp)
}

// WireReport wire 引擎报告（对齐 TS EngineReport JSON 形状）。
type WireReport struct {
	Best   WireMove        `json:"best"`
	BestCp int             `json:"bestCp"`
	TopK   []WireTopKEntry `json:"topK"`
}

// wireReportFromRules 规则层报告 → wire。
func wireReportFromRules(r *EngineReport) WireReport {
	out := WireReport{Best: WireMoveFromRules(r.Best), BestCp: r.BestCp, TopK: make([]WireTopKEntry, 0, len(r.TopK))}
	for _, e := range r.TopK {
		out.TopK = append(out.TopK, WireTopKEntry{Move: WireMoveFromRules(e.Move), Cp: e.Cp})
	}
	return out
}

// Handle 协议分发：payload 反解 → 引擎调用 → Response。
// ctx 已取消时直接回 canceled（取消=调用方丢弃语义收口，03 §7）：
// 入口即回不空算，计算后复查不把取消期间完成的部分结果当有效应答。
func Handle(ctx context.Context, req Request) Response {
	if req.Type != ReqCancel && ctx.Err() != nil {
		return Response{ID: req.ID, Error: ErrCanceled.Error()}
	}
	switch req.Type {
	case ReqCancel:
		// cancel 由 Runner/绑定层注册表执行；协议层幂等空响应。
		return Response{ID: req.ID, OK: true}
	case ReqFindBestMove:
		var p FindBestMovePayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		move, err := FindBestMove(p.Fen, FindBestMoveOptions{
			Difficulty:  p.Difficulty,
			HistoryFens: p.HistoryFens,
			Ctx:         ctx,
		})
		if err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		if ctx.Err() != nil {
			return Response{ID: req.ID, Error: ErrCanceled.Error()}
		}
		if move == nil {
			return Response{ID: req.ID, OK: true} // result 省略 = null（无合法走法）
		}
		return Response{ID: req.ID, OK: true, Result: WireMoveFromRules(*move)}
	case ReqFindBestMoveEx:
		var p FindBestMoveExPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		report, err := FindBestMoveEx(p.Fen, FindBestMoveExOptions{
			Depth:       p.Depth,
			TopK:        p.TopK,
			TimeLimitMs: p.TimeLimitMs,
			Ctx:         ctx,
		})
		if err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		if ctx.Err() != nil {
			return Response{ID: req.ID, Error: ErrCanceled.Error()}
		}
		if report == nil {
			return Response{ID: req.ID, OK: true}
		}
		return Response{ID: req.ID, OK: true, Result: wireReportFromRules(report)}
	case ReqEvaluateMove:
		var p EvaluateMovePayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		var wm WireMove
		if err := json.Unmarshal(p.Move, &wm); err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		cp, err := EvaluateMove(p.Fen, wm.ToRules(), EvaluateMoveOptions{Depth: p.Depth, Ctx: ctx})
		if err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		if ctx.Err() != nil {
			return Response{ID: req.ID, Error: ErrCanceled.Error()}
		}
		if cp == nil {
			return Response{ID: req.ID, OK: true}
		}
		return Response{ID: req.ID, OK: true, Result: *cp}
	default:
		return Response{ID: req.ID, Error: "Unknown engine request type: " + string(req.Type)}
	}
}

// Runner 请求运行器：每请求独立 goroutine + context 取消注册表（DR-003）。
// Cancel 对在途请求触发 ctx 取消（搜索在 64 节点探针处中止）；
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
// 串行语义由调用方按需自保证——对局页同一时刻至多一个在途搜索）。
// requestID 唯一性是 00 §3.2 调用方契约（前端 UUID 工厂）；同 id 并发在途
// 属契约违例，注册表以后到者为准（见 KNOWN_ISSUES K17）。
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
