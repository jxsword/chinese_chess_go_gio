package llm

// 大模型棋手（05 文档 §4 五层过滤管线 + 兜底链；Electron 版 llmPlayer.ts 1:1 移植，
// llm_move_source.dart LlmMoveSource 同源）。
//
// 每手流程：合法清单 → Prompt(v1/v2) → 调用 → 解析 → 白名单校验 →
// （失败）user 末尾追加反馈重试 →（耗尽）降级 builtinAi/resign。
// 模型只"提议"，本地规则是唯一事实源——白名单比对是精确字符串匹配（铁律 #3）。
// 纯 Go：禁止 import Wails / frontend（铁律 #1）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// idCounter 缺省 requestId 生成器自增序号。
var idCounter atomic.Int64

func defaultNewID() string {
	return fmt.Sprintf("llm-%d-%d", time.Now().UnixNano(), idCounter.Add(1))
}

// ChatClientOptions LlmChatClient 选项。
type ChatClientOptions struct {
	// AuthSlot 掩码 Key 回读场景：传输/绑定层按槽位注入真实 Authorization（DR-010）。
	AuthSlot string
	// NewID requestId 工厂（测试注入可预测 id）。
	NewID func() string
}

// inFlightCall 在途请求登记（cancelCurrent 结算用）。
type inFlightCall struct {
	requestID string
	cancel    context.CancelFunc
}

// LlmChatClient 单次对话客户端：BuildChatRequest → transport → 结局结算。
// 取消：CancelCurrent 立即以 ErrCanceled 结算在途请求并通知传输层。
type LlmChatClient struct {
	config    LlmEndpointConfig
	transport Transport
	options   ChatClientOptions

	mu       sync.Mutex
	inFlight *inFlightCall
}

// NewLlmChatClient 构造对话客户端。
func NewLlmChatClient(config LlmEndpointConfig, transport Transport, options ChatClientOptions) *LlmChatClient {
	return &LlmChatClient{config: config, transport: transport, options: options}
}

// DisplayName 状态栏展示名（llm_move_source.dart:257）。
func (c *LlmChatClient) DisplayName() string {
	model := strings.TrimSpace(c.config.Model)
	if model == "" {
		return "（未配置模型）"
	}
	return model
}

// ChatOnce 一次性的文本问答（残局求解辅助等复用同一条流式通道）。
// 与 NextMove 的区别：不附加合法清单协议，也不做重试降级。
// useV2 决定 max_tokens 预算（8192/4096，llm_move_source.dart:365）。
func (c *LlmChatClient) ChatOnce(ctx context.Context, system, user string, useV2 bool) (string, error) {
	built, err := BuildChatRequest(c.config, system, user, BuildChatOptions{
		UseV2:    useV2,
		AuthSlot: c.options.AuthSlot,
	})
	if err != nil {
		return "", err
	}
	newID := c.options.NewID
	if newID == nil {
		newID = defaultNewID
	}
	requestID := newID()

	callCtx, cancel := context.WithCancel(ctx)
	type outcome struct {
		text string
		err  error
	}
	done := make(chan outcome, 1)

	c.mu.Lock()
	c.inFlight = &inFlightCall{requestID: requestID, cancel: cancel}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		if c.inFlight != nil && c.inFlight.requestID == requestID {
			c.inFlight = nil
		}
		c.mu.Unlock()
		cancel()
	}()

	go func() {
		chatErr := c.transport.Chat(callCtx, ChatRequest{
			RequestID: requestID,
			URL:       built.URL,
			Headers:   built.Headers,
			Body:      built.Body,
			AuthSlot:  built.AuthSlot,
		}, ChatHandlers{
			OnDone: func(text string) { done <- outcome{text: text} },
			OnError: func(message string) {
				done <- outcome{err: &LlmApiError{Message: message}}
			},
		})
		_ = chatErr // ctx 取消：transport 不回调 handlers，结局由下方 select 收口
	}()

	select {
	case out := <-done:
		if out.err != nil {
			return "", out.err
		}
		return out.text, nil
	case <-callCtx.Done():
		// 结局可能与取消同时就绪：已就绪者胜（先结算先赢，TS 同语义）。
		select {
		case out := <-done:
			if out.err != nil {
				return "", out.err
			}
			return out.text, nil
		default:
			// 通知传输层取消（幂等；对局重开/悔棋/离开页面入口）。
			c.transport.Cancel(requestID)
			return "", ErrCanceled
		}
	}
}

// CancelCurrent 取消在途请求：本地立即结算 + 通知传输层（幂等）。
func (c *LlmChatClient) CancelCurrent() {
	c.mu.Lock()
	inflight := c.inFlight
	c.inFlight = nil
	c.mu.Unlock()
	if inflight != nil {
		inflight.cancel()
	}
}

// LlmPlayerOptions LlmPlayer 选项。
type LlmPlayerOptions struct {
	// UsePromptV2 Prompt v2（棋盘图 + 注解 + 分析段）；缺省 false 保留 v1 协议（能力评估基线）。
	UsePromptV2 bool
	// MaxAttempts 最多请求次数（含首次），缺省 3。
	MaxAttempts int
	// Fallback 模型持续失败时的降级策略。
	Fallback LlmFallback
	// BuiltinAiSource builtinAi 降级时的内置 AI 棋手工厂（对局页注入 ChessAiPlayer(难度 3)）。
	BuiltinAiSource func() engine.MoveSource
	// OnAttempt 每次尝试开始时的进度回调（思考型模型单次可达数分钟，UI 借此显示"第 N/M 次尝试"）。
	OnAttempt func(attempt, totalAttempts int)
}

// LlmPlayer 大模型棋手（实现 engine.MoveSource）。
type LlmPlayer struct {
	client      *LlmChatClient
	usePromptV2 bool
	maxAttempts int
	options     LlmPlayerOptions
}

// NewLlmPlayer 构造大模型棋手。
func NewLlmPlayer(config LlmEndpointConfig, transport Transport, options LlmPlayerOptions, clientOptions ChatClientOptions) *LlmPlayer {
	maxAttempts := options.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	return &LlmPlayer{
		client:      NewLlmChatClient(config, transport, clientOptions),
		usePromptV2: options.UsePromptV2,
		maxAttempts: maxAttempts,
		options:     options,
	}
}

// DisplayName 展示名。
func (p *LlmPlayer) DisplayName() string { return p.client.DisplayName() }

// CancelCurrent 取消在途请求（对局重开/悔棋/离开页面时调用，00 §3.2）。
func (p *LlmPlayer) CancelCurrent() { p.client.CancelCurrent() }

// NextMove 走子管线（llmPlayer.ts:140-183 逐字移植）。
func (p *LlmPlayer) NextMove(ctx context.Context, board *rules.Board, history []rules.Move) (engine.MoveSourceResult, error) {
	legal := board.AllLegalMoves(board.Turn())
	if len(legal) == 0 {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}

	// 白名单（编码 → 着法）；codes 保持合法清单顺序（去重语义与 TS Map 一致）。
	codesByMove := make(map[string]rules.Move, len(legal))
	legalCodes := make([]string, 0, len(legal))
	for _, m := range legal {
		code := EncodeMove(m)
		if _, seen := codesByMove[code]; !seen {
			legalCodes = append(legalCodes, code)
		}
		codesByMove[code] = m
	}

	system := SystemV1(board.Turn())
	if p.usePromptV2 {
		system = SystemV2(board.Turn(), false)
	}
	user := UserV1(board, history, legalCodes)
	if p.usePromptV2 {
		user = UserV2(board, history, legal)
	}

	var lastNote string
	for attempt := 1; attempt <= p.maxAttempts; attempt++ {
		if p.options.OnAttempt != nil {
			p.options.OnAttempt(attempt, p.maxAttempts)
		}
		content, err := p.client.ChatOnce(ctx, system, user, p.usePromptV2)
		if err != nil {
			// 取消（新局/悔棋/离开页面）：原样上抛，页面按取消收口（等价 ChessAiPlayer）。
			if errors.Is(err, ErrCanceled) {
				return engine.MoveSourceResult{}, err
			}
			lastNote = fmt.Sprintf("第 %d 次调用失败：%s", attempt, err.Error())
			// 网络类错误重试意义有限，但仍给满次数（端点偶发抖动常见）。
			continue
		}

		code := ExtractMove(content)
		if code != nil {
			if m, ok := codesByMove[*code]; ok {
				// 严格单行格式下回复无附加信息，成功时 note 留空。
				return engine.MoveSourceResult{Status: engine.StatusOK, Move: &m}, nil
			}
		}

		reason := "无法从回复中解析出着法"
		if code != nil {
			reason = fmt.Sprintf("着法 %s 不在合法清单中", *code)
		}
		lastNote = fmt.Sprintf("第 %d 次回复无效（%s）", attempt, reason)
		if p.usePromptV2 {
			lastCode := ""
			if code != nil {
				lastCode = *code
			}
			user += RetryFeedbackV2(reason, lastCode)
		} else {
			user += RetryFeedback(reason)
		}
	}

	if lastNote == "" {
		lastNote = fmt.Sprintf("模型连续 %d 次未给出合法着法", p.maxAttempts)
	}
	return p.fallback(ctx, board, lastNote)
}

// fallback 降级：内置 AI 代走（默认）或判负（llm_move_source.dart:317-335）。
func (p *LlmPlayer) fallback(ctx context.Context, board *rules.Board, reason string) (engine.MoveSourceResult, error) {
	if p.options.Fallback == FallbackBuiltinAI {
		result, err := p.options.BuiltinAiSource().NextMove(ctx, board, nil)
		if err != nil {
			return engine.MoveSourceResult{}, err
		}
		if result.Status == engine.StatusOK && result.Move != nil {
			return engine.MoveSourceResult{
				Status:       engine.StatusOK,
				Move:         result.Move,
				Note:         reason + "，已由内置 AI 兜底走子",
				FromFallback: true,
			}, nil
		}
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}
	return engine.MoveSourceResult{Status: engine.StatusFailed, Note: reason + "，按判负处理"}, nil
}

// TestLlmConnectionResult 测试连接结果。
type TestLlmConnectionResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// TestLlmConnection 配置卡"测试连接"：发一条最小请求，返回 (是否成功, 说明)
// （llm_move_source.dart:488-497；错误消息附模型类型误用提示）。
func TestLlmConnection(config LlmEndpointConfig, transport Transport, authSlot string, newID func() string) TestLlmConnectionResult {
	if !IsConfigured(config) {
		return TestLlmConnectionResult{OK: false, Message: "连接失败：模型端点未配置（需填写端点与模型 ID）"}
	}
	client := NewLlmChatClient(config, transport, ChatClientOptions{AuthSlot: authSlot, NewID: newID})
	if _, err := client.ChatOnce(context.Background(), TestConnectionSystem, TestConnectionUser, false); err != nil {
		message := err.Error()
		return TestLlmConnectionResult{OK: false, Message: "连接失败：" + AnnotateModelHint(message)}
	}
	return TestLlmConnectionResult{OK: true, Message: fmt.Sprintf("连接成功，模型 %s 响应正常", config.Model)}
}
