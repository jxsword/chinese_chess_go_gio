package ui

// 引擎客户端（T3'.1，design_docs/03 消费方式注记 + 00 §4 engine Worker 行）：
// 包装复制物 engine.Runner（NewRunner/Submit/Cancel 语义——goroutine+ctx 内建，
// 消息形状 {id,type,payload} 在 Runner 内部保留），结果经 app 事件总线回主循环
// （铁律 #G3 正方向）；requestId 取消与迟到丢弃由总线收口（铁律 #G5，app 包单测）。
//
// 接口口径（03 §6.2/§6.3）：
//   - FindBestMove（对局 AI 应手）：唯一接受 HistoryFens 的接口——对局方 fenHistory
//     拷贝传入（重复治理 L1/L2 随复制物内置，参数缺省=关闭）；
//   - 参谋接口 FindBestMoveEx / EvaluateMove：一律不传历史（可复现铁律），
//     本客户端不提供任何传入途径。
//
// 每页一实例（上游 EngineClient 同构）；离页 Dispose 由页面 cancel 在途请求。

import (
	"encoding/json"
	"errors"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// errText 协议错误串 → error（canceled 文案对齐 engine.ErrCanceled）。
func errText(s string) error { return errors.New(s) }

// EngineSubmitter 引擎运行器面（*engine.Runner 实现；测试注入 fake——
// 00 §4"测试可注入 fake"口径）。
type EngineSubmitter interface {
	Submit(req engine.Request) <-chan engine.Response
	Cancel(requestID string)
}

// EngineClient 引擎客户端：runner 直调 + 响应 goroutine 经 emit 回事件总线。
// emit/cancel 仅在构造期写定（env 注入），goroutine 只读——-race 干净。
type EngineClient struct {
	runner EngineSubmitter
	emit   func(requestID string, payload any, err error)
	cancel func(requestID string)
}

// NewEngineClient 创建客户端（runner 传 nil = 复制物 engine.NewRunner()）。
func NewEngineClient(env GameEnv, runner EngineSubmitter) *EngineClient {
	if runner == nil {
		runner = engine.NewRunner()
	}
	return &EngineClient{runner: runner, emit: env.Emit, cancel: env.Cancel}
}

// FindBestMoveAsync 对局 AI 应手（异步）：historyFens 为对局方 fenHistory 拷贝
// （nil = 关闭重复治理）。回执 ui.EngineMoveDone（含解码后的 rules.Move）。
func (c *EngineClient) FindBestMoveAsync(requestID, fen string, difficulty int, historyFens []string) {
	payload := engine.FindBestMovePayload{
		Fen:         fen,
		Difficulty:  difficulty,
		HistoryFens: append([]string(nil), historyFens...), // 拷贝（DR-018 入参）
	}
	c.submitAsync(requestID, engine.ReqFindBestMove, payload, func(resp engine.Response) (any, error) {
		done := EngineMoveDone{RequestID: requestID}
		if resp.Error != "" {
			return done, errText(resp.Error)
		}
		if resp.Result == nil {
			return done, nil // 无合法走法（将死/困毙）
		}
		var wm engine.WireMove
		if err := remarshal(resp.Result, &wm); err != nil {
			return done, errText(err.Error())
		}
		m := wm.ToRules()
		done.Move = &m
		return done, nil
	})
}

// FindBestMoveExAsync 参谋报告（异步，不传历史——可复现铁律）。
// 回执 ui.EngineReportDone（Top-K 名单真实分差）。
func (c *EngineClient) FindBestMoveExAsync(requestID, fen string, depth, topK, timeLimitMs int) {
	payload := engine.FindBestMoveExPayload{Fen: fen, Depth: depth, TopK: topK, TimeLimitMs: timeLimitMs}
	c.submitAsync(requestID, engine.ReqFindBestMoveEx, payload, func(resp engine.Response) (any, error) {
		done := EngineReportDone{RequestID: requestID}
		if resp.Error != "" {
			return done, errText(resp.Error)
		}
		if resp.Result == nil {
			return done, nil
		}
		var wr engine.WireReport
		if err := remarshal(resp.Result, &wr); err != nil {
			return done, errText(err.Error())
		}
		done.Report = &engine.EngineReport{BestCp: wr.BestCp}
		done.Report.Best = wr.Best.ToRules()
		for _, e := range wr.TopK {
			done.Report.TopK = append(done.Report.TopK, engine.ReportEntry{Move: e.Move.ToRules(), Cp: e.Cp})
		}
		return done, nil
	})
}

// EvaluateMoveAsync 单着法评估（异步，护航否决用，不传历史——可复现铁律）。
// 回执 ui.EngineEvalDone（Cp=nil = 无评估值）。
func (c *EngineClient) EvaluateMoveAsync(requestID, fen string, move rules.Move, depth int) {
	payload := engine.EvaluateMovePayload{
		Fen:   fen,
		Move:  mustJSON(engine.WireMoveFromRules(move)),
		Depth: depth,
	}
	c.submitAsync(requestID, engine.ReqEvaluateMove, payload, func(resp engine.Response) (any, error) {
		done := EngineEvalDone{RequestID: requestID}
		if resp.Error != "" {
			return done, errText(resp.Error)
		}
		if resp.Result == nil {
			return done, nil
		}
		var cp int
		if err := remarshal(resp.Result, &cp); err != nil {
			return done, errText(err.Error())
		}
		done.Cp = &cp
		return done, nil
	})
}

// Cancel 取消在途请求：Runner ctx 取消（搜索中止）+ 总线取消（迟到回执按 id
// 丢弃——铁律 #G5 双收口，上游 client.cancel 语义）。
func (c *EngineClient) Cancel(requestID string) {
	if requestID == "" {
		return
	}
	c.runner.Cancel(requestID)
	if c.cancel != nil {
		c.cancel(requestID)
	}
}

// submitAsync 提交请求并起响应 goroutine：Submit 立即返回通道（Runner 内部
// 每请求独立 goroutine+ctx），此处仅等待回执并经 emit 回主循环——goroutine
// 生存期=请求生存期（00 §4"阻塞至结算"语义的 Go 对应）。
func (c *EngineClient) submitAsync(requestID string, typ engine.RequestType, payload any, decode func(engine.Response) (any, error)) {
	raw, err := json.Marshal(payload)
	if err != nil {
		c.emit(requestID, nil, err)
		return
	}
	ch := c.runner.Submit(engine.Request{ID: requestID, Type: typ, Payload: raw})
	go func() {
		resp := <-ch
		payload, err := decode(resp)
		c.emit(requestID, payload, err)
	}()
}

// remarshal any（Handle 的 Result）→ 目标形状（wire 形状已由协议层约定）。
func remarshal(v any, out any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("null")
	}
	return raw
}
