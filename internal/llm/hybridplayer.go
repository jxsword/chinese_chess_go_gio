package llm

// 引擎参谋制走子来源（05 文档 §5 单手棋总决策流程；Electron 版 hybridPlayer.ts
// 1:1 移植，hybrid_llm_move_source.dart 同源）。
//
// 每手棋先由本地引擎搜索出带评分的候选报告，再按 advisorMode 决定 LLM 的决策空间：
//   - 候选模式（candidate）：Prompt 只给引擎 Top-K 短名单（附分数分桶），LLM 从中
//     选一——"只在好棋里挑"；
//   - 护航模式（gate）：LLM 在全量清单中自由选择，引擎对其选择单独评估，
//     相对最佳分差超过否决阈值（丢大子/被杀）时行使否决权——带理由再问
//     一次，仍不行由引擎最佳着法代走（这是参谋职责，不走"模型失败"的
//     resign 降级分支）；
//   - 关闭（off）：等价 P0（Prompt v2 + 全量清单），用于能力评估基线对比。
//
// 棋力旋钮 strengthBlend（0~100）：候选模式 K = 3 + blend/20（3~8）；
// 护航模式否决阈值 = 80 + 3.2×blend 厘兵（blend=100 时不否决）。
// 缺省值（candidate/50/5/3）由设置层保证（05 §8 DEFAULT）；构造器对
// 枚举空串与次数 0 仍按 TS `??` 语义兜底（StrengthBlend 的 0 是合法"最严"档，
// 不做兜底）。兜底链：LLM 失效 → 引擎最佳着法代走（note 注明），永不卡死。
// 纯 Go：禁止 import Wails / frontend（铁律 #1）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// AdvisorEngine 引擎参谋接口（EngineClient 结构兼容；测试注入 fake）。
type AdvisorEngine interface {
	// FindBestMoveEx 报告 nil = 无候选（等价 TS null）。
	FindBestMoveEx(ctx context.Context, fen string, depth, topK, timeLimitMs int) (*engine.EngineReport, error)
	// EvaluateMove 返回 nil = 着法无评估（等价 TS null）。
	EvaluateMove(ctx context.Context, fen string, m rules.Move, depth int) (*int, error)
}

// HybridLlmPlayerOptions HybridLlmPlayer 选项。
type HybridLlmPlayerOptions struct {
	// AdvisorMode 引擎参谋模式，空串兜底 candidate。
	AdvisorMode AdvisorMode
	// StrengthBlend 棋力旋钮 0~100（0 = 最严档，合法值不做兜底）。
	StrengthBlend int
	// AdvisorDifficulty 参谋搜索深度档 1~5（迭代深度 = 档 + 1），0 兜底 5。
	AdvisorDifficulty int
	// MaxAttempts 最多请求次数（含首次），0 兜底 3。
	MaxAttempts int
	// Fallback 模型持续失败时的降级策略（off 模式与候选/护航兜底共用）。
	Fallback LlmFallback
	// BuiltinAiSource off 模式（纯 Prompt v2）降级链所需的内置 AI 棋手工厂。
	BuiltinAiSource func() engine.MoveSource
	// OnAttempt 每次尝试开始时的进度回调（UI 显示"第 N/M 次尝试"用）。
	OnAttempt func(attempt, totalAttempts int)
}

// vetoSecond 否决复评结果。
type vetoSecond struct {
	move rules.Move
	cp   int
}

// HybridLlmPlayer 引擎参谋制大模型棋手（实现 engine.MoveSource）。
type HybridLlmPlayer struct {
	advisor           AdvisorEngine
	client            *LlmChatClient
	advisorMode       AdvisorMode
	strengthBlend     int
	advisorDifficulty int
	maxAttempts       int
	options           HybridLlmPlayerOptions

	// offDelegate off 模式的纯 Prompt v2 委托（懒构造，取消链路覆盖）；
	// NextMove 与 CancelCurrent 可能来自不同 goroutine，构造须互斥。
	offDelegate *LlmPlayer
	delegateMu  sync.Mutex
}

// NewHybridLlmPlayer 构造参谋制棋手。
func NewHybridLlmPlayer(config LlmEndpointConfig, transport Transport, advisor AdvisorEngine,
	options HybridLlmPlayerOptions, clientOptions ChatClientOptions) *HybridLlmPlayer {
	advisorMode := options.AdvisorMode
	if advisorMode == "" {
		advisorMode = AdvisorCandidate
	}
	advisorDifficulty := options.AdvisorDifficulty
	if advisorDifficulty == 0 {
		advisorDifficulty = 5
	}
	maxAttempts := options.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 3
	}
	return &HybridLlmPlayer{
		advisor:           advisor,
		client:            NewLlmChatClient(config, transport, clientOptions),
		advisorMode:       advisorMode,
		strengthBlend:     options.StrengthBlend,
		advisorDifficulty: advisorDifficulty,
		maxAttempts:       maxAttempts,
		options:           options,
	}
}

// DisplayName 展示名。
func (h *HybridLlmPlayer) DisplayName() string { return h.client.DisplayName() }

// CancelCurrent 取消在途请求（off 委托与参谋链路都覆盖；对局重开/暂停/离开页面时调用）。
func (h *HybridLlmPlayer) CancelCurrent() {
	h.delegateMu.Lock()
	delegate := h.offDelegate
	h.delegateMu.Unlock()
	if delegate != nil {
		delegate.CancelCurrent()
	}
	h.client.CancelCurrent()
}

// ShortlistSize 候选名单宽度：blend 0→3，100→8（hybrid_llm_move_source.dart:74）。
func ShortlistSize(blend int) int {
	return minInt(8, maxInt(3, 3+floorDiv(blend, 20)))
}

// VetoThresholdCp 护航否决阈值（厘兵）：blend 0→80（严），100→400（最宽）（:77-78）。
func VetoThresholdCp(blend int) int {
	return 80 + (320*minInt(100, maxInt(0, blend)))/100
}

// depth 参谋迭代深度：档位 1~5 → 2~6（:85）。
func (h *HybridLlmPlayer) depth() int {
	return minInt(5, maxInt(1, h.advisorDifficulty)) + 1
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// floorDiv 向下取整整除（负数语义与 TS Math.floor 一致）。
func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

// NextMove 单手棋总决策（hybridPlayer.ts:122-259 逐字移植）。
func (h *HybridLlmPlayer) NextMove(ctx context.Context, board *rules.Board, history []rules.Move) (engine.MoveSourceResult, error) {
	if h.advisorMode == AdvisorOff {
		// P0 基线：Prompt v2 + 全量清单，无参谋（:92-103；等价每次新建
		// LlmMoveSource(usePromptV2: true) 的 Dart 写法，此处惰性单例保取消语义）。
		h.delegateMu.Lock()
		if h.offDelegate == nil {
			// 对齐 TS hybridPlayer.ts:127-137：off 委托不转发 onAttempt
			//（off 模式无参谋进度语义，避免多发"第 N/M 次尝试"回调）。
			h.offDelegate = NewLlmPlayer(h.client.config, h.client.transport, LlmPlayerOptions{
				UsePromptV2:     true,
				MaxAttempts:     h.maxAttempts,
				Fallback:        h.options.Fallback,
				BuiltinAiSource: h.options.BuiltinAiSource,
			}, ChatClientOptions{AuthSlot: h.client.options.AuthSlot, NewID: h.client.options.NewID})
		}
		delegate := h.offDelegate
		h.delegateMu.Unlock()
		return delegate.NextMove(ctx, board, history)
	}
	return h.nextMoveWithAdvisor(ctx, board, history)
}

func (h *HybridLlmPlayer) nextMoveWithAdvisor(ctx context.Context, board *rules.Board, history []rules.Move) (engine.MoveSourceResult, error) {
	legal := board.AllLegalMoves(board.Turn())
	if len(legal) == 0 {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}

	// 1. 引擎参谋搜索（EngineClient 内部已含 Isolate/Worker 兜底语义）。
	fen := board.ToFen()
	depth := h.depth()
	topK := ShortlistSize(h.strengthBlend)
	report, err := h.advisor.FindBestMoveEx(ctx, fen, depth, topK, 5000)
	if err != nil {
		// 取消原样上抛；其余引擎错误同样上抛（对齐 TS hybridPlayer.ts:155
		// await 拒绝传播 → 页面 onSideFailed("走子来源异常：…")，不吞为终局状态）。
		if errors.Is(err, ErrCanceled) || ctx.Err() != nil {
			return engine.MoveSourceResult{}, ErrCanceled
		}
		return engine.MoveSourceResult{}, err
	}
	if report == nil {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}

	// 2. 本轮 LLM 可见池 + Prompt v2。
	pool := legal
	if h.advisorMode == AdvisorCandidate {
		pool = make([]rules.Move, 0, len(report.TopK))
		for _, entry := range report.TopK {
			pool = append(pool, entry.Move)
		}
	}
	poolByCode := make(map[string]rules.Move, len(pool))
	for _, m := range pool {
		poolByCode[EncodeMove(m)] = m
	}
	// 分档引导只在清单真的带分桶时出现（候选模式）。
	system := SystemV2(board.Turn(), h.advisorMode == AdvisorCandidate)
	user := h.buildUser(board, history, pool, report)

	// 3. LLM 提议（重试带失败原因）。lastReason 仅记录"模型未给出有效
	// 着法"类真失败；一旦给出有效着法即置空。
	var lastReason string
	var pick *rules.Move
	var pickCp *int
	for attempt := 1; attempt <= h.maxAttempts && pick == nil; attempt++ {
		if h.options.OnAttempt != nil {
			h.options.OnAttempt(attempt, h.maxAttempts)
		}
		content, chatErr := h.client.ChatOnce(ctx, system, user, true)
		if chatErr != nil {
			if errors.Is(chatErr, ErrCanceled) {
				return engine.MoveSourceResult{}, ErrCanceled
			}
			lastReason = fmt.Sprintf("第 %d 次调用失败：%s", attempt, chatErr.Error())
			continue
		}
		lastCode := ExtractMove(content)
		if lastCode != nil {
			if m, ok := poolByCode[*lastCode]; ok {
				pick = &m
				lastReason = ""
				break
			}
		}
		reason := "无法从回复中解析出着法"
		if lastCode != nil {
			reason = fmt.Sprintf("着法 %s 不在候选清单中", *lastCode)
		}
		lastReason = reason
		feedbackCode := ""
		if lastCode != nil {
			feedbackCode = *lastCode
		}
		user += RetryFeedbackV2(reason, feedbackCode)
	}

	// 4. 护航模式：引擎否决权。否决是参谋的正常职责——即使最终由引擎
	// 最佳代走，也不走"模型失败"的降级分支（该分支遵循 resign 设置）。
	var vetoEvalCp *int
	vetoOverridden := false
	if pick != nil && h.advisorMode == AdvisorGate && h.strengthBlend < 100 {
		vetoDepth := depth - 1
		vetoEvalCp, err = h.advisor.EvaluateMove(ctx, fen, *pick, vetoDepth)
		if err != nil {
			// 取消原样上抛；其余引擎错误上抛（对齐 TS hybridPlayer.ts:200 无捕获传播）。
			if errors.Is(err, ErrCanceled) || ctx.Err() != nil {
				return engine.MoveSourceResult{}, ErrCanceled
			}
			return engine.MoveSourceResult{}, err
		}
		if vetoEvalCp == nil {
			pick = nil // 非法着法（理论上不会发生，池内均合法）
		} else {
			loss := report.BestCp - *vetoEvalCp
			if loss > VetoThresholdCp(h.strengthBlend) {
				second, secondErr := h.askAgainWithVeto(ctx, fen, poolByCode, report, system, user, *pick, loss)
				if secondErr != nil {
					// 取消原样上抛；其余引擎错误上抛（对齐 TS 无捕获传播）。
					if errors.Is(secondErr, ErrCanceled) {
						return engine.MoveSourceResult{}, ErrCanceled
					}
					return engine.MoveSourceResult{}, secondErr
				}
				if second != nil {
					pick = &second.move
					pickCp = &second.cp
				} else {
					// 两次都违抗否决：采用引擎最佳（参谋职责，非模型失效）。
					vetoOverridden = true
					best := report.Best
					pick = &best
					bestCp := report.BestCp
					pickCp = &bestCp
				}
			}
		}
	}

	// 5. 兜底链与注解。
	if pick == nil {
		reason := "模型未给出有效着法"
		if lastReason != "" {
			reason = fmt.Sprintf("模型未给出有效着法（%s）", lastReason)
		}
		if h.options.Fallback == FallbackBuiltinAI {
			best := report.Best
			return engine.MoveSourceResult{
				Status:       engine.StatusOK,
				Move:         &best,
				Note:         reason + "，已由参谋（内置引擎）代走",
				FromFallback: true,
			}, nil
		}
		return engine.MoveSourceResult{Status: engine.StatusFailed, Note: reason + "，按判负处理"}, nil
	}

	if vetoOverridden {
		return engine.MoveSourceResult{
			Status:       engine.StatusOK,
			Move:         pick,
			Note:         fmt.Sprintf("已由参谋否决（两次选择均造成%s的损失），改为引擎最佳着法", ScoreBucket(report.BestCp-*pickCp)),
			FromFallback: true,
		}, nil
	}

	chosenCp := pickCp
	if chosenCp == nil {
		if cp := CpOf(report, *pick); cp != nil {
			chosenCp = cp
		}
	}
	if chosenCp == nil {
		chosenCp = vetoEvalCp
	}
	note := "参谋评分: 未单独评估"
	if chosenCp != nil {
		note = fmt.Sprintf("参谋评分: %s", ScoreBucket(report.BestCp-*chosenCp))
	}
	return engine.MoveSourceResult{Status: engine.StatusOK, Move: pick, Note: note}, nil
}

// askAgainWithVeto 否决后的再问：带否决理由重新提议一次（:280-318）。
// 返回 (着法, 引擎评分)；第二次选择仍超阈值、无效或调用失败返回 nil。
func (h *HybridLlmPlayer) askAgainWithVeto(ctx context.Context, fen string, poolByCode map[string]rules.Move,
	report *engine.EngineReport, system, user string, vetoed rules.Move, loss int) (*vetoSecond, error) {
	vetoNote := VetoFeedback(user, EncodeMove(vetoed), ScoreBucket(loss), loss)
	content, err := h.client.ChatOnce(ctx, system, vetoNote, true)
	if err != nil {
		if errors.Is(err, ErrCanceled) {
			return nil, ErrCanceled
		}
		return nil, nil
	}
	code := ExtractMove(content)
	if code == nil {
		return nil, nil
	}
	second, ok := poolByCode[*code]
	if !ok {
		return nil, nil
	}

	secondCp, evalErr := h.advisor.EvaluateMove(ctx, fen, second, h.depth()-1)
	if evalErr != nil {
		// 取消原样上抛；其余引擎错误上抛（对齐 TS hybridPlayer.ts:287 无捕获传播）。
		if errors.Is(evalErr, ErrCanceled) {
			return nil, ErrCanceled
		}
		return nil, evalErr
	}
	if secondCp == nil {
		return nil, nil
	}
	secondLoss := report.BestCp - *secondCp
	if secondLoss > VetoThresholdCp(h.strengthBlend) {
		return nil, nil
	}
	return &vetoSecond{move: second, cp: *secondCp}, nil
}

// buildUser v2 用户提示的 Hybrid 版：清单为 pool（候选模式附分数分桶，:321-348）。
func (h *HybridLlmPlayer) buildUser(board *rules.Board, history []rules.Move, pool []rules.Move, report *engine.EngineReport) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "【当前局面 FEN】%s\n", board.ToFen())
	buf.WriteString("【棋盘图】\n")
	buf.WriteString(AsciiBoard(board))
	turn := "黑方"
	if rules.IsRedSide(board.Turn()) {
		turn = "红方"
	}
	fmt.Fprintf(&buf, "【轮走方】%s（该方是你）\n", turn)
	fmt.Fprintf(&buf, "【对局着法（中文记法，最新在最后）】%s\n", HistoryTextV2(history))
	if h.advisorMode == AdvisorCandidate {
		buf.WriteString(fmt.Sprintf("【候选着法清单（共 %d 条，由本地引擎选出，", len(pool)) +
			"必须从中选择一条；「—」后为引擎评估分档）】\n")
		for _, entry := range report.TopK {
			buf.WriteString(AnnotatedWithBucket(board, entry.Move, report.BestCp-entry.Cp))
			buf.WriteString("\n")
		}
	} else {
		fmt.Fprintf(&buf, "【合法着法清单（共 %d 条，必须从中选择一条）】\n", len(pool))
		for _, m := range pool {
			buf.WriteString(AnnotateMove(board, m))
			buf.WriteString("\n")
		}
	}
	buf.WriteString("【输出】先输出「分析:」段，最后一行输出「着法: 起点-终点」")
	return buf.String()
}

// CpOf 护航模式的选择不在 Top-K 内时返回 nil（:362-367）。
func CpOf(report *engine.EngineReport, m rules.Move) *int {
	for _, entry := range report.TopK {
		if entry.Move.From == m.From && entry.Move.To == m.To {
			cp := entry.Cp
			return &cp
		}
	}
	return nil
}
