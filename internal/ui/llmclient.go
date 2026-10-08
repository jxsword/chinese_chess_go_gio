package ui

// LLM 客户端（T4'.1，design_docs/00 §4 LLM 行 + 05 附录消费方式注记）：
//   - 走子管线：复制物 HybridLlmPlayer 在 goroutine 直调（goroutine 生存期=请求
//     生存期，"阻塞至结算恒 resolve"语义），结算经 app 事件总线回主循环（#G3 正方向）；
//   - 流式面：tee 传输把 chunk/done/error 事件按**页面级 requestId** 发往事件总线
//     （00 §4 llm:chunk/done/error 行）；取消 = ctx + player.CancelCurrent + 总线
//     Cancel 三收口（铁律 #G5；StreamTransport.Cancel 幂等，重复取消安全）；
//   - 掩码 Key（DR-010）：页面持掩码 Key，请求构造层产出 AuthSlot，传输层在此按
//     槽位注入真实 Authorization（resolve 由 app 注入——完整 Key 不进 UI/日志）；
//   - 测试连接：goroutine 直调复制物 Proxy.TestConnection，结果回事件总线。
//
// 并发契约：LlmClient 方法仅由页面在主 goroutine 调用（至多一个在途走子，页面
// 守卫 + 此处防御）；内部 goroutine 只读构造期写定的 emit/resolve 与当次 spec
// 拷贝——-race 干净（同 EngineClient 口径）。

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// LlmStore LLM 页异步存储面（app/llmrepo.go 实现；测试注入 fake）。
// 读侧 Settings（*storage.Settings，内存态）由 LlmEnv 直携；写侧一律后台 I/O。
type LlmStore interface {
	// LoadSlotAsync 后台读凭据槽位（掩码回读），回执 ui.SecureSlotLoaded。
	LoadSlotAsync(requestID, slot string)
	// SaveSlotAsync 后台写凭据槽位（掩码合并，防错 #8），回执 ui.SecureSlotSaved。
	SaveSlotAsync(requestID, slot string, cfg llm.LlmEndpointConfig)
	// SaveSettings 后台写 llm_settings_*（fire-and-forget：错误记日志，上游
	// 防抖保存 .catch(()=>{}) 同语义）。
	SaveSettings(settings state.LlmGameSettings, fields ...state.LlmSettingsField)
	// ResolveAPIKey 槽位完整 API Key（仅供传输层注入 Authorization，DR-010；
	// 返回值禁止进入 UI/日志/异常消息——铁律 #G7）。仅后台 goroutine 调用。
	ResolveAPIKey(slot string) string
}

// LlmEnv LLM 页环境（GameEnv 之上追加 LLM 面；app llmEnv() 注入）。
type LlmEnv struct {
	GameEnv
	// Store 凭据槽位/设置异步存储面（nil = 存储降级：配置不可读写）。
	Store LlmStore
	// Settings llm_settings_* 读侧（内存态；nil = 全按默认值）。
	Settings *storage.Settings
}

// advisorSeq 参谋引擎请求 id 序列（仅后台走子 goroutine 调用，原子即可）。
var advisorSeq atomic.Int64

// LlmMoveSpec 一次走子请求的规格（页面构造；字段在 MoveAsync 前定死）。
type LlmMoveSpec struct {
	// Endpoint 生效端点配置（空侧镜像后解析结果，DR-012）。
	Endpoint llm.LlmEndpointConfig
	// AuthSlot 掩码 Key 注入槽位（配置完整 Key 时为空串）。
	AuthSlot string
	// Board/History 局面快照与着法历史（页面深拷贝/拷贝——后台只读）。
	Board   *rules.Board
	History []rules.Move
	// Advisor 引擎参谋（HybridLlmPlayer 必需；off 模式不触达）。
	Advisor AdvisorEngine
	// 对局设置直传（复制物 HybridLlmPlayerOptions 语义）。
	AdvisorMode       llm.AdvisorMode
	StrengthBlend     int
	AdvisorDifficulty int
	MaxAttempts       int
	Fallback          llm.LlmFallback
	// OnAttempt 每次尝试进度回调（后台 goroutine 调用；实现须只发事件）。
	OnAttempt func(attempt, totalAttempts int)
}

// LlmMoveSource 走子来源面（*llm.HybridLlmPlayer / *llm.LlmPlayer 实现；测试注入）。
type LlmMoveSource interface {
	engine.MoveSource
	CancelCurrent()
}

// LlmRunner 页面消费的 LLM 客户端面（*LlmClient 实现；页面测试注入 fake）。
type LlmRunner interface {
	// MoveAsync 发起一次走子：结算经 ui.LlmMoveDone 回执（requestId 关联）。
	MoveAsync(requestID string, spec LlmMoveSpec)
	// TestConnectionAsync 发起测试连接：结果经 ui.LlmTestDone 回执。
	TestConnectionAsync(requestID string, cfg llm.LlmEndpointConfig, authSlot string)
	// Cancel 取消在途请求（幂等）：ctx 取消 + CancelCurrent + 总线迟到丢弃。
	Cancel(requestID string)
}

// AdvisorEngine 引擎参谋同步接口（复制物 llm.AdvisorEngine 别名——
// 在后台走子 goroutine 内阻塞调用，EngineRunnerAdapter 实现）。
type AdvisorEngine = llm.AdvisorEngine

// EngineRunnerAdapter 把异步 EngineSubmitter（engine.Runner）适配为参谋同步接口。
// 仅在走子 goroutine 内调用（阻塞等待回执合法——#G3：I/O 不在主循环）；
// ctx 取消（新局/悔棋/离页）时取消引擎请求并返回 llm.ErrCanceled。
type EngineRunnerAdapter struct {
	runner EngineSubmitter
}

// NewEngineRunnerAdapter 构造（runner 为页面 EngineClient 同源的 Runner 面）。
func NewEngineRunnerAdapter(runner EngineSubmitter) *EngineRunnerAdapter {
	return &EngineRunnerAdapter{runner: runner}
}

// FindBestMoveEx 参谋报告（同步阻塞；不传历史——可复现铁律，无传入途径）。
func (a *EngineRunnerAdapter) FindBestMoveEx(ctx context.Context, fen string, depth, topK, timeLimitMs int) (*engine.EngineReport, error) {
	raw, err := json.Marshal(engine.FindBestMoveExPayload{Fen: fen, Depth: depth, TopK: topK, TimeLimitMs: timeLimitMs})
	if err != nil {
		return nil, err
	}
	id := newAdvisorRequestID()
	ch := a.runner.Submit(engine.Request{ID: id, Type: engine.ReqFindBestMoveEx, Payload: raw})
	resp, err := waitEngineResp(ctx, a.runner, id, ch)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errText(resp.Error)
	}
	if resp.Result == nil {
		return nil, nil
	}
	var wr engine.WireReport
	if err := remarshal(resp.Result, &wr); err != nil {
		return nil, err
	}
	report := &engine.EngineReport{BestCp: wr.BestCp, Best: wr.Best.ToRules()}
	for _, e := range wr.TopK {
		report.TopK = append(report.TopK, engine.ReportEntry{Move: e.Move.ToRules(), Cp: e.Cp})
	}
	return report, nil
}

// EvaluateMove 单着法评估（同步阻塞；护航否决用）。
func (a *EngineRunnerAdapter) EvaluateMove(ctx context.Context, fen string, m rules.Move, depth int) (*int, error) {
	raw, err := json.Marshal(engine.EvaluateMovePayload{Fen: fen, Move: mustJSON(engine.WireMoveFromRules(m)), Depth: depth})
	if err != nil {
		return nil, err
	}
	id := "advisor-" + newAdvisorRequestID()
	ch := a.runner.Submit(engine.Request{ID: id, Type: engine.ReqEvaluateMove, Payload: raw})
	resp, err := waitEngineResp(ctx, a.runner, id, ch)
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, errText(resp.Error)
	}
	if resp.Result == nil {
		return nil, nil
	}
	var cp int
	if err := remarshal(resp.Result, &cp); err != nil {
		return nil, err
	}
	return &cp, nil
}

// waitEngineResp 等待引擎回执或 ctx 取消（取消时撤销引擎请求，回收搜索 goroutine）。
func waitEngineResp(ctx context.Context, runner EngineSubmitter, id string, ch <-chan engine.Response) (engine.Response, error) {
	type result struct {
		resp engine.Response
	}
	done := make(chan result, 1)
	go func() {
		done <- result{resp: <-ch}
	}()
	select {
	case r := <-done:
		return r.resp, nil
	case <-ctx.Done():
		runner.Cancel(id)
		return engine.Response{}, llm.ErrCanceled
	}
}

func newAdvisorRequestID() string {
	return fmt.Sprintf("advisor-%d", advisorSeq.Add(1))
}

// LlmClient LLM 客户端（每页一实例，同 EngineClient 口径）。
type LlmClient struct {
	emit     func(requestID string, payload any, err error)
	cancel   func(requestID string)
	resolve  func(slot string) string // 完整 Key 注入（app 注入；仅后台 goroutine 调用）
	idleSecs func() int               // 空闲超时秒数（设置驱动；transport 内部 clamp 5~600）

	// 在途走子（至多一个）；仅主 goroutine 读写（MoveAsync/Cancel 均主 goroutine）。
	moveCancel  context.CancelFunc
	movePlayer  LlmMoveSource
	moveRequest string
}

// NewLlmClient 创建（idleSecs 为 nil 时用复制物默认 60s，内部 clamp 5~600）。
func NewLlmClient(env GameEnv, resolve func(slot string) string, idleSecs func() int) *LlmClient {
	return &LlmClient{emit: env.Emit, cancel: env.Cancel, resolve: resolve, idleSecs: idleSecs}
}

// moveTransport 走子管线传输：AuthSlot 注入（DR-010）+ 流式事件 tee 到总线。
// 外层 requestId 绑定构造期 outerID——管线内部每次 ChatOnce 的 id 仅用于传输层
// 取消路由，事件全部以 outerID 标记（页面按 outerID 收口，K21 防线之一环）。
type moveTransport struct {
	inner   llm.Transport
	outerID string
	resolve func(slot string) string
	emit    func(requestID string, payload any, err error)
}

// Chat 实现 llm.Transport（阻塞至结算；handler 转发 + 事件 tee）。
func (t *moveTransport) Chat(ctx context.Context, req llm.ChatRequest, handlers llm.ChatHandlers) error {
	headers := make(map[string]string, len(req.Headers)+1)
	for k, v := range req.Headers {
		headers[k] = v
	}
	if req.AuthSlot != "" && t.resolve != nil {
		if realKey := t.resolve(req.AuthSlot); realKey != "" {
			headers["Authorization"] = "Bearer " + realKey
		} else {
			delete(headers, "Authorization")
		}
	}
	inner := llm.ChatRequest{RequestID: req.RequestID, URL: req.URL, Headers: headers, Body: req.Body}
	return t.inner.Chat(ctx, inner, llm.ChatHandlers{
		OnChunk: func(delta llm.Delta) {
			if handlers.OnChunk != nil {
				handlers.OnChunk(delta)
			}
			t.emit(t.outerID, LlmStreamChunk{RequestID: t.outerID, Delta: delta}, nil)
		},
		OnDone: func(text string) {
			if handlers.OnDone != nil {
				handlers.OnDone(text)
			}
			t.emit(t.outerID, LlmStreamDone{RequestID: t.outerID, Text: text}, nil)
		},
		OnError: func(message string) {
			if handlers.OnError != nil {
				handlers.OnError(message)
			}
			t.emit(t.outerID, LlmStreamError{RequestID: t.outerID, Message: message}, nil)
		},
	})
}

// Cancel 实现 llm.Transport（幂等；复制物 StreamTransport.Cancel 语义）。
func (t *moveTransport) Cancel(requestID string) { t.inner.Cancel(requestID) }

// MoveAsync 发起一次 LLM 走子（页面主 goroutine 调用）：构造参谋制棋手 →
// goroutine 直调 NextMove（生存期=请求生存期）→ 结算经 LlmMoveDone 回执。
// 至多一个在途：重复调用先取消前一个（页面侧亦有守卫，双保险）。
func (c *LlmClient) MoveAsync(requestID string, spec LlmMoveSpec) {
	c.Cancel(c.moveRequest) // 防御：至多一个在途
	ctx, cancel := context.WithCancel(context.Background())
	transport := c.NewMoveTransport(requestID)
	player := llm.NewHybridLlmPlayer(spec.Endpoint, transport, spec.Advisor, llm.HybridLlmPlayerOptions{
		AdvisorMode:       spec.AdvisorMode,
		StrengthBlend:     spec.StrengthBlend,
		AdvisorDifficulty: spec.AdvisorDifficulty,
		MaxAttempts:       spec.MaxAttempts,
		Fallback:          spec.Fallback,
		OnAttempt:         spec.OnAttempt,
	}, llm.ChatClientOptions{AuthSlot: spec.AuthSlot})
	c.moveCancel = cancel
	c.movePlayer = player
	c.moveRequest = requestID
	go func() {
		defer cancel() // 结算/取消后回收 ctx 资源
		result, err := player.NextMove(ctx, spec.Board, spec.History)
		c.emit(requestID, LlmMoveDone{RequestID: requestID, Result: result}, err)
	}()
}

// NewMoveTransport 创建绑定外层 requestId 的走子传输（页面为每次请求取一次——
// 上游每手 new HybridLlmPlayer 同构）。
func (c *LlmClient) NewMoveTransport(outerID string) llm.Transport {
	inner := llm.NewStreamTransport(c.idleSecs)
	return &moveTransport{inner: inner, outerID: outerID, resolve: c.resolve, emit: c.emit}
}

// TestConnectionAsync 测试连接（goroutine 直调复制物 Proxy.TestConnection，
// 结果经 LlmTestDone 回执；不可取消——最小请求，迟到回执由总线按 id 丢弃）。
func (c *LlmClient) TestConnectionAsync(requestID string, cfg llm.LlmEndpointConfig, authSlot string) {
	proxy := llm.NewProxy(llm.ProxyOptions{ResolveAPIKey: c.resolve})
	go func() {
		res := proxy.TestConnection(cfg, authSlot)
		c.emit(requestID, LlmTestDone{RequestID: requestID, OK: res.OK, Message: res.Message}, nil)
	}()
}

// Cancel 取消在途走子（幂等）：走子 ctx 取消（参谋搜索随 ctx 中止回收）+
// player.CancelCurrent（在途 chat 立即结算 ErrCanceled）+ 总线 Cancel（迟到
// 回执按 id 丢弃——#G5 三收口）。对无在途请求的调用为空操作。
func (c *LlmClient) Cancel(requestID string) {
	if requestID == "" {
		return
	}
	if requestID == c.moveRequest {
		if c.movePlayer != nil {
			c.movePlayer.CancelCurrent()
		}
		if c.moveCancel != nil {
			c.moveCancel()
		}
		c.moveRequest = ""
		c.movePlayer = nil
		c.moveCancel = nil
	}
	c.cancel(requestID)
}
