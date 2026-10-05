package engine

// ChessAi 三接口（03 文档 §2/§6.2/§6.3；Electron 版 chessAi.ts 的逐行翻译，
// Dart ai_engine.dart `ChessAi` 的 1:1 移植同源）：
//
//   - FindBestMove(board, difficulty)：对局 AI 应手（难度 1-5，低难度带随机窗口）；
//   - FindBestMoveEx(board, depth, topK)：参谋报告——零随机 + 根节点强制全窗口，
//     分数为真实分差、名单稳定可复现（否决阈值计算的前提，03 §6.3）；
//   - EvaluateMove(board, move, depth)：单着法评估（护航否决用），毫秒级，
//     必须先做几何合法性校验（ApplyMove 不校验蹩腿等伪非法着法）。
//
// 无将杀/困毙时返回 nil 的语义与 Dart 一致；引擎内部对棋盘深拷贝后搜索，
// 不修改调用方传入的棋盘。
// 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。

import (
	"context"
	"math"
	"math/rand/v2"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// levelParam 难度参数（03 §2；区别于参谋深度档）。
type levelParam struct {
	depth      int
	timeMs     int
	randomness int
}

// levelParams 难度参数表（逐值一致，03 §2）。
var levelParams = map[int]levelParam{
	1: {depth: 2, timeMs: 300, randomness: 120}, // 初级
	2: {depth: 3, timeMs: 800, randomness: 50},  // 中级
	3: {depth: 4, timeMs: 1600, randomness: 0},  // 高级
	4: {depth: 5, timeMs: 3000, randomness: 0},  // 专家
	5: {depth: 6, timeMs: 5000, randomness: 0},  // 大师
}

// avoidThresholdBase L2 回避阈值基础值（厘兵，DR-006 前置，03 §6.2；随难度递减保棋力）。
var avoidThresholdBase = map[int]int{1: 200, 2: 200, 3: 100, 4: 50, 5: 30}

// forcedChangeFloor 长将强制变着底线（厘兵）：非将军替代着法劣化超过此值则宁可重复交规则裁决。
const forcedChangeFloor = 500

// defaultDifficulty 缺省难度（03 §6.2：Difficulty 0=未设即缺省 3）。
const defaultDifficulty = 3

// EngineReport 引擎搜索报告：最佳着法、最佳评分（厘兵，正数=当前方占优）与 Top-K 候选。
type EngineReport struct {
	Best   rules.Move
	BestCp int
	// TopK 候选，按 cp 降序；cp 为从当前走子方视角的评分。
	TopK []ReportEntry
}

// ReportEntry Top-K 表项。
type ReportEntry struct {
	Move rules.Move
	Cp   int
}

// FindBestMoveOptions FindBestMove 选项（03 §6.2）。
type FindBestMoveOptions struct {
	// Difficulty 难度 1-5（初级-大师），0=未设即缺省 3，越界取 clamp（Dart 同语义）。
	Difficulty int
	// ShouldAbort 取消探针：与 deadline 同节奏（每 64 节点）轮询（03 §6）。
	ShouldAbort func() bool
	// Ctx 取消：与 ShouldAbort 同节奏并入探针（03 §6.2）。
	Ctx context.Context
	// HistoryFens 对局历史局面 FEN 序列（初始局面→当前，含轮走方；DR-006 前置 L2 根节点回避）。
	// 缺省/空 = 完全关闭重复回避，行为与 Dart 口径逐位一致（向后兼容铁律）。
	// 仅 FindBestMove 接受该参数；FindBestMoveEx/EvaluateMove 不接受（03 §6.3 可复现性）。
	HistoryFens []string
}

// FindBestMoveExOptions FindBestMoveEx 选项（03 §6.3；签名不含 HistoryFens）。
type FindBestMoveExOptions struct {
	// Depth 搜索深度 1-8；0=未设即缺省 6。
	Depth int
	// TopK 返回候选数，最小 1；0=未设即缺省 5。
	TopK int
	// TimeLimitMs 时间上限（毫秒）；0=未设即缺省 5000。
	TimeLimitMs int
	// ShouldAbort 取消探针。
	ShouldAbort func() bool
	// Ctx 取消。
	Ctx context.Context
}

// EvaluateMoveOptions EvaluateMove 选项（03 §6.3；签名不含 HistoryFens）。
type EvaluateMoveOptions struct {
	// Depth 对手视角搜索深度 1-6；0=未设即缺省 4。
	Depth int
	// ShouldAbort 取消探针。
	ShouldAbort func() bool
	// Ctx 取消。
	Ctx context.Context
}

// clampInt 越界钳制（TS clampInt 同义）。
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// abortProbe 合成取消探针：ShouldAbort 与 ctx 并入同一节奏（03 §6.2 Ctx 探针）。
func abortProbe(shouldAbort func() bool, ctx context.Context) func() bool {
	return func() bool {
		if shouldAbort != nil && shouldAbort() {
			return true
		}
		return ctx != nil && ctx.Err() != nil
	}
}

// buildHistoryCounts 由历史 FEN 序列构建局面出现次数表（键 = uint64 Zobrist）；
// 无效 FEN 静默跳过；空表返回 nil（整体关闭）。
func buildHistoryCounts(historyFens []string) map[uint64]int {
	if len(historyFens) == 0 {
		return nil
	}
	counts := make(map[uint64]int, len(historyFens))
	for _, f := range historyFens {
		b, err := FromFen(f)
		if err != nil {
			continue // 无效 FEN 跳过（页面侧数据容错）
		}
		counts[b.key]++
	}
	if len(counts) == 0 {
		return nil
	}
	return counts
}

// AvoidanceContext L2 回避决策入参（03 §6.2 流程 L2 段的纯函数化，供直测）。
type AvoidanceContext struct {
	// Scored 全窗口根节点评分表（降序）。
	Scored []ScoredMove
	// Best 当前最佳着法（packed）。
	Best int32
	// ThresholdBase 阈值基础值（厘兵，随难度递减）。
	ThresholdBase int
	// PostCount 着法落子后局面在历史中的出现次数。
	PostCount func(packed int32) int
	// IsCheckMove 着法是否为将军着法。
	IsCheckMove func(packed int32) bool
	// Random 随机源（nil = rand/v2 全局源；测试注入固定序列）。
	Random func() float64
}

// PickAvoidanceMove L2 根节点回避决策（DR-006 前置，03 §6.2）：
//  1. 阈值内（base × 优劣势系数）且落子后非重复的候选 → 随机取一；
//  2. 长将形态（最佳为将军且造成重复）→ 强制选非将军非重复的最高分着法，
//     底线 −500 厘兵防送子；
//  3. 无合理替代 → 保留原着交规则裁决（L3）。
//
// 返回最终应走的 packed 着法。
func PickAvoidanceMove(ctx AvoidanceContext) int32 {
	random := ctx.Random
	if random == nil {
		random = rand.Float64
	}
	bestScore := ctx.Scored[0].Score
	coef := 1.0
	if bestScore > 200 {
		coef = 0.5 // 优势求变
	} else if bestScore < -200 {
		coef = 2.0 // 劣势可重复
	}
	threshold := int(float64(ctx.ThresholdBase) * coef)
	candidates := make([]int32, 0, len(ctx.Scored))
	for _, sm := range ctx.Scored {
		if sm.Score >= bestScore-threshold && ctx.PostCount(sm.Move) == 0 {
			candidates = append(candidates, sm.Move)
		}
	}
	if len(candidates) > 0 {
		for i := len(candidates) - 1; i > 0; i-- {
			j := int(random() * float64(i+1))
			candidates[i], candidates[j] = candidates[j], candidates[i]
		}
		return candidates[0]
	}
	if ctx.IsCheckMove(ctx.Best) {
		for _, sm := range ctx.Scored {
			// Scored 已降序，首个满足条件者即最高分。
			if !ctx.IsCheckMove(sm.Move) && ctx.PostCount(sm.Move) == 0 && sm.Score >= bestScore-forcedChangeFloor {
				return sm.Move
			}
		}
	}
	return ctx.Best
}

// FindBestMove 为 fen 的当前走子方寻找最佳走法。
// 若当前方无任何合法走法（被将死/困毙）返回 (nil, nil)；无效 FEN 返回 error。
//
// L2 根节点历史回避（DR-006 前置，03 §6.2）：提供 HistoryFens 时，若最佳着法落子后
// 局面已在历史中出现（count≥1），在剩余时限内补一次全窗口搜索，于阈值内
// （基础值随难度递减 × 优劣势系数）随机换非重复着法；长将形态强制变着
// （底线 −500 厘兵）；无合理替代则保留原着交规则裁决（L3）。
// 未提供 HistoryFens 时行为与 Dart 口径逐位一致。
func FindBestMove(fen string, opts FindBestMoveOptions) (*rules.Move, error) {
	difficulty := opts.Difficulty
	if difficulty == 0 {
		difficulty = defaultDifficulty
	}
	difficulty = clampInt(difficulty, 1, 5)
	params, ok := levelParams[difficulty]
	if !ok {
		params = levelParams[defaultDifficulty]
	}
	historyCounts := buildHistoryCounts(opts.HistoryFens)
	board, err := FromFen(fen)
	if err != nil {
		return nil, err
	}
	deadlineMs := time.Now().UnixMilli() + int64(params.timeMs)
	search := NewSearch(board, SearchConfig{
		MaxDepth:      params.depth,
		DeadlineMs:    deadlineMs,
		Randomness:    params.randomness,
		ShouldAbort:   abortProbe(opts.ShouldAbort, opts.Ctx),
		HistoryCounts: historyCounts,
	})
	best, ok := search.Run()
	if !ok {
		return nil, nil
	}
	if historyCounts == nil {
		move := PackedToMove(best)
		return &move, nil
	}

	// ---- L2 根节点回避（仅在最佳着法命中历史时付出全窗口重搜成本）----
	rootBoard, err := FromFen(fen)
	if err != nil {
		return nil, err
	}
	// postCount packed 走法落子后局面键及其在历史中的出现次数（apply/undo 平衡）。
	postCount := func(packed int32) int {
		from, to := PackedFrom(packed), PackedTo(packed)
		cap := rootBoard.ApplyMove(from, to)
		key := rootBoard.key
		rootBoard.UndoMove(from, to, cap)
		return historyCounts[key]
	}
	if postCount(best) == 0 {
		move := PackedToMove(best)
		return &move, nil
	}

	remaining := deadlineMs - time.Now().UnixMilli()
	if remaining < 100 {
		remaining = 100
	}
	scored := NewSearch(mustFromFen(fen), SearchConfig{
		MaxDepth:      params.depth,
		DeadlineMs:    time.Now().UnixMilli() + remaining,
		Randomness:    0,
		ShouldAbort:   abortProbe(opts.ShouldAbort, opts.Ctx),
		HistoryCounts: historyCounts,
	}).RunScored()
	if len(scored) == 0 {
		move := PackedToMove(best)
		return &move, nil
	}
	moverIsRed := rootBoard.isRedTurn
	isCheckMove := func(packed int32) bool {
		from, to := PackedFrom(packed), PackedTo(packed)
		cap := rootBoard.ApplyMove(from, to)
		check := rootBoard.IsCheck(!moverIsRed)
		rootBoard.UndoMove(from, to, cap)
		return check
	}
	base := avoidThresholdBase[difficulty]
	if base == 0 {
		base = 100
	}
	picked := PickAvoidanceMove(AvoidanceContext{
		Scored:        scored,
		Best:          best,
		ThresholdBase: base,
		PostCount:     postCount,
		IsCheckMove:   isCheckMove,
	})
	move := PackedToMove(picked)
	return &move, nil
}

// mustFromFen 同一 FEN 二次构板（前面已解析成功，失败不可能）。
func mustFromFen(fen string) *EngineBoard {
	b, err := FromFen(fen)
	if err != nil {
		panic("engine: 同一 FEN 二次解析失败（不可能发生）: " + err.Error())
	}
	return b
}

// FindBestMoveEx 引擎参谋报告：以固定深度、无随机性搜索一次，返回最佳着法与
// 按分数降序的 Top-K 候选（根节点强制全窗口，分数为真实分差，03 §6.3）。
// 无合法走法（被将死/困毙）返回 (nil, nil)。签名不含 HistoryFens（可复现铁律）。
func FindBestMoveEx(fen string, opts FindBestMoveExOptions) (*EngineReport, error) {
	depth, topK, timeLimitMs := opts.Depth, opts.TopK, opts.TimeLimitMs
	if depth == 0 {
		depth = 6
	}
	if topK == 0 {
		topK = 5
	}
	if timeLimitMs == 0 {
		timeLimitMs = 5000
	}
	board, err := FromFen(fen)
	if err != nil {
		return nil, err
	}
	search := NewSearch(board, SearchConfig{
		MaxDepth:    clampInt(depth, 1, 8),
		DeadlineMs:  time.Now().UnixMilli() + int64(timeLimitMs),
		Randomness:  0,
		ShouldAbort: abortProbe(opts.ShouldAbort, opts.Ctx),
	})
	scored := search.RunScored()
	if len(scored) == 0 {
		return nil, nil
	}
	k := clampInt(topK, 1, len(scored))
	topKList := make([]ReportEntry, 0, k)
	for _, sm := range scored[:k] {
		topKList = append(topKList, ReportEntry{Move: PackedToMove(sm.Move), Cp: sm.Score})
	}
	return &EngineReport{Best: topKList[0].Move, BestCp: topKList[0].Cp, TopK: topKList}, nil
}

// EvaluateMove 单着法评估（护航否决用）：走 move 后以浅搜索取对手最佳分，
// 返回从当前走子方视角的评分（厘兵）。毫秒级（浅 1-6 层 + 2s 上限）。
//
// move 必须是 board 当前方的一步合法走法；非法（起点无己方子/走完自将）
// 返回 (nil, nil)。几何合法性必须先行校验——ApplyMove 不校验蹩腿等伪非法着法。
// 签名不含 HistoryFens（03 §6.3 可复现铁律）。
func EvaluateMove(fen string, move rules.Move, opts EvaluateMoveOptions) (*int, error) {
	depth := opts.Depth
	if depth == 0 {
		depth = 4
	}
	probe, err := FromFen(fen)
	if err != nil {
		return nil, err
	}
	from := PosToIndex(move.From)
	to := PosToIndex(move.To)
	mover := probe.PieceAt(from)
	if mover == 0 {
		return nil, nil
	}
	if (mover > 0) != probe.isRedTurn {
		return nil, nil
	}
	// 几何合法性校验（ApplyMove 不校验蹩腿/隔子等伪非法着法）。
	if !probe.HasPseudoMove(from, to) {
		return nil, nil
	}
	cap := probe.ApplyMove(from, to)
	if probe.IsCheck(mover > 0) {
		probe.UndoMove(from, to, cap)
		return nil, nil // 走完自将，非法
	}
	// 对手视角搜索（probe 所有权移交 Search，用完即弃）。
	scored := NewSearch(probe, SearchConfig{
		MaxDepth:    clampInt(depth, 1, 6),
		DeadlineMs:  time.Now().UnixMilli() + 2000,
		Randomness:  0,
		ShouldAbort: abortProbe(opts.ShouldAbort, opts.Ctx),
	}).RunScored()
	bestOpp := math.MinInt
	if len(scored) == 0 {
		// 走完后对手被将死/困毙。
		cp := MATE_SCORE
		return &cp, nil
	}
	for _, sm := range scored {
		if sm.Score > bestOpp {
			bestOpp = sm.Score
		}
	}
	cp := -bestOpp
	return &cp, nil
}
