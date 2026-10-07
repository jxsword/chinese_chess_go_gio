package ui

// 求解器客户端（T6'.1，design_docs/04 消费方式注记 + 00 §4 engine/solver Worker 行）：
// 包装复制物 solver.Runner（NewRunner/Submit/Cancel 语义——goroutine+ctx 内建，
// 消息形状 {id,type,payload} 在 Runner 内部保留），结果经 app 事件总线回主循环
//（铁律 #G3 正方向）；requestId 取消与迟到丢弃由总线收口（铁律 #G5，app 包单测）。
//
// 接口口径（04 §2/§3）：
//   - SolveAsync：AND/OR 迭代加深求解（TimeLimitMs 0=缺省 30s；MaxPlies 0=缺省 9）；
//   - IsWinningFirstMoveAsync：单着验证裁判（LLM 求解辅助用，05 §6）。
//
// 每页一实例（上游 SolverClient 同构）；离页 Dispose 由页面 cancel 在途请求。
// 串行语义由调用方自保证——工作室页同一时刻至多一个在途求解（04 protocol 注）。

import (
	"encoding/json"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/solver"
)

// SolverSubmitter 求解运行器面（*solver.Runner 实现；测试注入 fake——
// 00 §4"测试可注入 fake"口径）。
type SolverSubmitter interface {
	Submit(req solver.Request) <-chan solver.Response
	Cancel(requestID string)
}

// SolverClient 求解器客户端：runner 直调 + 响应 goroutine 经 emit 回事件总线。
// emit/cancel 仅在构造期写定（env 注入），goroutine 只读——-race 干净。
type SolverClient struct {
	runner SolverSubmitter
	emit   func(requestID string, payload any, err error)
	cancel func(requestID string)
}

// NewSolverClient 创建客户端（runner 传 nil = 复制物 solver.NewRunner()）。
func NewSolverClient(env GameEnv, runner SolverSubmitter) *SolverClient {
	if runner == nil {
		runner = solver.NewRunner()
	}
	return &SolverClient{runner: runner, emit: env.Emit, cancel: env.Cancel}
}

// SolveAsync 发起求解（异步）：回执 ui.SolveDone（含解码后的 wire 结果）。
func (c *SolverClient) SolveAsync(requestID, fen string, timeLimitMs, maxPlies int) {
	payload := solver.SolvePayload{Fen: fen, TimeLimitMs: timeLimitMs, MaxPlies: maxPlies}
	c.submitAsync(requestID, solver.ReqSolve, payload, func(resp solver.Response) (any, error) {
		done := SolveDone{RequestID: requestID}
		if resp.Error != "" {
			return done, errText(resp.Error)
		}
		if resp.Result == nil {
			return done, nil
		}
		var wr solver.WireSolveResult
		if err := remarshal(resp.Result, &wr); err != nil {
			return done, errText(err.Error())
		}
		done.Result = &wr
		return done, nil
	})
}

// IsWinningFirstMoveAsync 发起单着验证（异步）：回执 ui.SolveWinDone。
func (c *SolverClient) IsWinningFirstMoveAsync(requestID, fen string, firstMove rules.Move, plies, timeLimitMs int) {
	payload := solver.IsWinningFirstMovePayload{
		Fen:         fen,
		FirstMove:   mustJSON(solver.WireMoveFromRules(firstMove)),
		Plies:       plies,
		TimeLimitMs: timeLimitMs,
	}
	c.submitAsync(requestID, solver.ReqIsWinningFirstMove, payload, func(resp solver.Response) (any, error) {
		done := SolveWinDone{RequestID: requestID}
		if resp.Error != "" {
			return done, errText(resp.Error)
		}
		var win bool
		if err := remarshal(resp.Result, &win); err != nil {
			return done, errText(err.Error())
		}
		done.Win = win
		return done, nil
	})
}

// Cancel 取消在途请求：Runner ctx 取消（搜索在节点探针处中止）+ 总线取消
// （迟到回执按 id 丢弃——铁律 #G5 双收口，上游 client.cancel 语义）。
func (c *SolverClient) Cancel(requestID string) {
	if requestID == "" {
		return
	}
	c.runner.Cancel(requestID)
	if c.cancel != nil {
		c.cancel(requestID)
	}
}

// submitAsync 提交请求并起响应 goroutine（EngineClient.submitAsync 同口径）。
func (c *SolverClient) submitAsync(requestID string, typ solver.RequestType, payload any, decode func(solver.Response) (any, error)) {
	raw, err := json.Marshal(payload)
	if err != nil {
		c.emit(requestID, nil, err)
		return
	}
	ch := c.runner.Submit(solver.Request{ID: requestID, Type: typ, Payload: raw})
	go func() {
		resp := <-ch
		payload, err := decode(resp)
		c.emit(requestID, payload, err)
	}()
}
