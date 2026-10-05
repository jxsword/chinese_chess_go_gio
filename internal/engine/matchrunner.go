package engine

// 无 UI 对局运行器（05 文档 §9 沿用 Electron 版 05 §8.2；matchRunner.ts 逐行翻译，
// match_runner.dart 1:1 同源）：两枚 MoveSource 对打并产出五期能力评估统计。
//
// - 单手超时默认 5 分钟；失败/无着 = 当方认输；合法性终审兜底（铁律 #3）；
// - EvaluateQuality 开启时每手用 FindBestMoveEx + EvaluateMove（深度对齐口径）
//   算失误数（分差 >250 厘兵）与 Top-3 命中/失随；
// - 也可在测试中以脚本化假 LLM 驱动做确定性断言。
// 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。
//
// Go 侧与 TS 的两处结构差异（行为等价）：
//   - TS 单手超时用 Promise.race（败者继续跑、结果丢弃）；Go 用 buffered chan
//     goroutine 竞速，迟到结果落在缓冲不泄漏，语义一致；
//   - TS runMatch 无外部取消；Go 按铁律 #5 接受 ctx 并透传给 MoveSource，
//     来源返回取消错误时以 ctx.Err() 上抛（TS 无此路径，CLI/测试用）。

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// MatchReport 一场对局的统计报告（五期能力评估用；JSON 键与 Dart toJson 逐字一致）。
type MatchReport struct {
	// Winner 'red' | 'black' | 'draw-limit' | 'red-resign' | 'black-resign'
	//（illegal/source-error 在 Dart 原版实现中折叠为 red/black-resign，此处一致）。
	Winner string `json:"winner"`
	// EndReason 'checkmate' | 'stalemate' | 'resign' | 'no-legal-move' |
	// 'illegal-move' | 'source-error' | 'move-limit'。
	EndReason   string `json:"endReason"`
	Plies       int    `json:"plies"`
	RedTimeMs   int64  `json:"redTimeMs"`
	BlackTimeMs int64  `json:"blackTimeMs"`

	// RedFallbacks/BlackFallbacks 走子来源触发兜底（FromFallback）的次数。
	RedFallbacks   int `json:"redFallbacks"`
	BlackFallbacks int `json:"blackFallbacks"`

	// RedBlunders/BlackBlunders 相对引擎最佳损失 > BlunderThresholdCp 的手数（质量指标）。
	RedBlunders    int `json:"redBlunders"`
	BlackBlunders  int `json:"blackBlunders"`
	EvaluatedPlies int `json:"evaluatedPlies"`

	// 所选着法 ∈ 引擎当层 Top-3 的跟随统计（质量跟随度指标）。
	RedTop3Hits     int      `json:"redTop3Hits"`
	RedTop3Misses   int      `json:"redTop3Misses"`
	BlackTop3Hits   int      `json:"blackTop3Hits"`
	BlackTop3Misses int      `json:"blackTop3Misses"`
	MovesIccs       []string `json:"movesIccs"`
}

// MatchRunnerBlunderMate 走完即杀对方时的损失按将杀级计（match_runner.dart MatchRunnerBlunder）。
const MatchRunnerBlunderMate = 30000

// BlunderThresholdCp 逐手质量评估的"失误"阈值（厘兵，docs/phase5/02 §4）。
const BlunderThresholdCp = 250

// MatchRunnerOptions RunMatch 选项。
type MatchRunnerOptions struct {
	// Initial 初始局面，nil = 标准开局。
	Initial *rules.Board
	// MaxPlies 最大半回合数（draw-limit 终止），0 兜底 120。
	MaxPlies int
	// EvaluateQuality 是否逐手质量评估（每手多付一次引擎搜索成本）。
	EvaluateQuality bool
	// QualityDepth 质量评估搜索深度（缺省 4；EvaluateMove 取 depth-1 对齐口径）。
	QualityDepth int
	// PerMoveTimeoutMs 单手超时（毫秒），0 兜底 5 分钟（match_runner.dart:124）。
	PerMoveTimeoutMs int64
}

// sideResign 结算 winner 字段（Dart sideResign：失败方-resign）。
func sideResign(loser rules.Side) string {
	if loser == rules.Red {
		return "red-resign"
	}
	return "black-resign"
}

// iccsOf 起终点 ICCS 坐标（"h7e7" 形态，无分隔符）。
func iccsOf(from, to rules.Position) string {
	return fmt.Sprintf("%c%d%c%d", rune('a'+from.Col), from.Row, rune('a'+to.Col), to.Row)
}

// moveOutcome 单手竞速的结算包（buffered chan 载荷）。
type moveOutcome struct {
	result MoveSourceResult
	err    error
}

// awaitMove 单手取着 + 超时竞速（等价 Dart .timeout(onTimeout: failed)）：
// 超时返回 failed/'单手超时'，来源 goroutine 继续跑、迟到结果丢弃不泄漏
// （等价 TS Promise.race 败者语义）。
func awaitMove(ctx context.Context, source MoveSource, b *rules.Board, history []rules.Move, timeout time.Duration) (MoveSourceResult, error) {
	// TS 每次 nextMove 传 [...history] 浅拷贝；Go 侧拷贝并把 cap 收紧到 len，
	// 主循环后续 append 必然换底，避免与仍持有的来源 goroutine 竞争底层数组。
	histCopy := make([]rules.Move, len(history))
	copy(histCopy, history)
	ch := make(chan moveOutcome, 1)
	go func() {
		result, err := source.NextMove(ctx, b, histCopy)
		ch <- moveOutcome{result: result, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case out := <-ch:
		return out.result, out.err
	case <-timer.C:
		return MoveSourceResult{Status: StatusFailed, Note: "单手超时"}, nil
	}
}

// RunMatch 无 UI 对局运行：两枚 MoveSource 对打并产出统计报告。
// 结算路径与 Dart 逐条一致：source 异常 → resign(source-error)；
// 失败/无着 → resign/no-legal-move；非法着法 → resign(illegal-move)；
// 将死/困毙 → 当方胜；跑满 MaxPlies → draw-limit。
// ctx 透传给 MoveSource（铁律 #5）；来源以取消错误结算时上抛 ctx.Err()。
func RunMatch(ctx context.Context, red, black MoveSource, options MatchRunnerOptions) (MatchReport, error) {
	maxPlies := options.MaxPlies
	if maxPlies == 0 {
		maxPlies = 120
	}
	qualityDepth := options.QualityDepth
	if qualityDepth == 0 {
		qualityDepth = 4
	}
	perMoveTimeout := time.Duration(options.PerMoveTimeoutMs) * time.Millisecond
	if perMoveTimeout == 0 {
		perMoveTimeout = 5 * 60 * time.Second
	}
	var board *rules.Board
	if options.Initial != nil {
		board = options.Initial.Copy()
	} else {
		board = rules.Initial()
	}
	history := make([]rules.Move, 0, 2*maxPlies)
	movesIccs := []string{}
	var redTimeMs, blackTimeMs int64
	var redFallbacks, blackFallbacks int
	var redBlunders, blackBlunders int
	var evaluatedPlies int
	var redTop3Hits, redTop3Misses, blackTop3Hits, blackTop3Misses int

	// 以当前累计统计结算（Dart _finish 的等价收口）。
	finish := func(winner, endReason string, plies int) MatchReport {
		return MatchReport{
			Winner:          winner,
			EndReason:       endReason,
			Plies:           plies,
			RedTimeMs:       redTimeMs,
			BlackTimeMs:     blackTimeMs,
			RedFallbacks:    redFallbacks,
			BlackFallbacks:  blackFallbacks,
			RedBlunders:     redBlunders,
			BlackBlunders:   blackBlunders,
			EvaluatedPlies:  evaluatedPlies,
			RedTop3Hits:     redTop3Hits,
			RedTop3Misses:   redTop3Misses,
			BlackTop3Hits:   blackTop3Hits,
			BlackTop3Misses: blackTop3Misses,
			MovesIccs:       movesIccs,
		}
	}

	for ply := 0; ply < maxPlies; ply++ {
		mover := board.Turn()
		source := red
		if mover != rules.Red {
			source = black
		}

		start := time.Now()
		result, err := awaitMove(ctx, source, board, history, perMoveTimeout)
		if err != nil {
			// 取消链收口（Go 侧语义）：来源按取消结算时整体上抛，不计入报告。
			if errors.Is(err, ErrCanceled) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return MatchReport{}, ctx.Err()
			}
			// Dart 原版 sideResign 的 note 参数即被忽略（报告无 note 字段），异常文本不落报告。
			return finish(sideResign(mover), "source-error", ply), nil
		}
		if mover == rules.Red {
			redTimeMs += time.Since(start).Milliseconds()
		} else {
			blackTimeMs += time.Since(start).Milliseconds()
		}

		// 失败/无着 → 当方认输。
		if result.Status != StatusOK || result.Move == nil {
			endReason := "resign"
			if result.Status == StatusNoLegalMove {
				endReason = "no-legal-move"
			}
			return finish(sideResign(mover), endReason, ply), nil
		}

		move := result.Move
		// 合法性终审（来源自带校验，这里兜底）。
		legal := false
		for _, m := range board.LegalMovesFor(move.From) {
			if rules.SamePos(m.To, move.To) {
				legal = true
				break
			}
		}
		if !legal {
			return finish(sideResign(mover), "illegal-move", ply), nil
		}

		if result.FromFallback {
			if mover == rules.Red {
				redFallbacks++
			} else {
				blackFallbacks++
			}
		}

		// 逐手质量评估：所选着法相对引擎最佳的损失（深度对齐口径）。
		if options.EvaluateQuality {
			snapshot := board.Copy()
			// FindBestMoveEx(depth) 的根分 = 走 1 步后对手搜 depth-1 层（总深 depth ply）；
			// EvaluateMove 须取 depth-1 对齐，否则不同深度的分相减会系统性失真。
			evalDepth := qualityDepth - 1
			if evalDepth < 1 {
				evalDepth = 1
			}
			if evalDepth > 6 {
				evalDepth = 6
			}
			report, err := FindBestMoveEx(snapshot.ToFen(), FindBestMoveExOptions{Depth: qualityDepth})
			if err != nil {
				// TS 侧引擎内部异常才会走到这里（FEN 恒合法）；按无报告处理不中断对局。
				report = nil
			}
			if report != nil {
				evaluatedPlies++
				pickedCp, evalErr := EvaluateMove(snapshot.ToFen(), *move, EvaluateMoveOptions{Depth: evalDepth})
				loss := MatchRunnerBlunderMate
				if evalErr == nil && pickedCp != nil {
					loss = report.BestCp - *pickedCp
				}
				if loss > BlunderThresholdCp {
					if mover == rules.Red {
						redBlunders++
					} else {
						blackBlunders++
					}
				}
				inTop3 := false
				top3 := report.TopK
				if len(top3) > 3 {
					top3 = top3[:3]
				}
				for _, entry := range top3 {
					if rules.SamePos(entry.Move.From, move.From) && rules.SamePos(entry.Move.To, move.To) {
						inTop3 = true
						break
					}
				}
				if mover == rules.Red {
					if inTop3 {
						redTop3Hits++
					} else {
						redTop3Misses++
					}
				} else if inTop3 {
					blackTop3Hits++
				} else {
					blackTop3Misses++
				}
			}
		}

		// 落子。
		piece := board.PieceAtP(move.From)
		applied := board.ApplyMove(*move)
		history = append(history, rules.Move{
			From:     applied.From,
			To:       applied.To,
			Piece:    piece,
			Captured: applied.Captured,
		})
		movesIccs = append(movesIccs, iccsOf(applied.From, applied.To))

		// 终局判定。
		next := board.Turn()
		if board.IsCheckmate(next) {
			return finish(string(mover), "checkmate", ply+1), nil
		}
		// 困毙判负（中国象棋规则）。
		if board.IsStalemate(next) {
			return finish(string(mover), "stalemate", ply+1), nil
		}
	}

	return finish("draw-limit", "move-limit", maxPlies), nil
}

// RunSeriesOptions RunMatchSeries 选项（MatchRunnerOptions + Games）。
type RunSeriesOptions struct {
	MatchRunnerOptions
	// Games 对局数；奇数局红黑换边，消除执先偏差。
	Games int
}

// RunMatchSeries 连跑 Games 局，红黑换边；返回每局报告（match_runner.dart runSeries）。
// 两个 builder 每局都各构造一次（Dart 同款）；buildRed 恒收红、buildBlack 恒收黑，
// 与座位无关。ctx 取消时返回已完成的报告与错误。
func RunMatchSeries(ctx context.Context, buildRed, buildBlack func(rules.Side) MoveSource, options RunSeriesOptions) ([]MatchReport, error) {
	reports := make([]MatchReport, 0, options.Games)
	for i := 0; i < options.Games; i++ {
		var redSeat, blackSeat MoveSource
		if i%2 == 1 {
			// 换边局：黑方 builder 坐红席（先构造），红方 builder 坐黑席。
			redSeat, blackSeat = buildBlack(rules.Black), buildRed(rules.Red)
		} else {
			redSeat, blackSeat = buildRed(rules.Red), buildBlack(rules.Black)
		}
		report, err := RunMatch(ctx, redSeat, blackSeat, options.MatchRunnerOptions)
		if err != nil {
			return reports, err
		}
		reports = append(reports, report)
	}
	return reports, nil
}
