package engine

// 单次搜索任务（03 文档 §5；Electron 版 search.ts 的逐行翻译，
// Dart ai_engine.dart `_Search` 的 1:1 移植同源）：
//
//   - Negamax + Alpha-Beta（fail-hard），MVV-LVA 排序（等级内嵌走法高位）；
//   - 迭代加深：超时返回上一层完整结果（lastScored 保留上一层）；
//   - Quiescence：叶子只延伸吃子（ply<8），被将军强制全应将（ply<16）；
//   - 根节点全窗口模式：runScored 强制 / randomness>0 时启用，
//     保证根节点各着法分数为真实分差（否决阈值计算的前提，03 §5.1）；
//   - 每 64 节点检查一次 deadline / 取消标志（对齐 _bumpNode 节奏），
//     超时或取消以 error sentinel 上抛，iterate 内捕获返回部分结果，
//     棋盘停留在中途状态随实例废弃（03 §5：Go 用 error sentinel，语义一致）。
//
// 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。

import (
	"errors"
	"math/rand/v2"
	"slices"
	"time"
)

// MATE_SCORE 将杀评分（区分被杀步数，越早被杀分越差）。
const MATE_SCORE = 30000

// INFINITY 搜索无穷大。
const INFINITY = 100000

// 静态搜索（吃子延伸）最大层数。
const maxQuiescencePly = 8

// maxPly 递归 ply 上限：主搜索深度 ≤8 + 应将延伸 ≤16，取 24 富余。
const maxPly = 24

// moveStride 每层走法缓冲容量（单方伪合法走法数远小于此）。
const moveStride = 128

// 搜索中断 sentinel（超时 / 取消，内部使用；等价 Dart `_TimeUp` / TS SearchAbort）。
var (
	errSearchTimeout  = errors.New("engine: search timeout")
	errSearchCanceled = errors.New("engine: search canceled")
)

// interrupted 标记取值（对齐 TS `interrupted: 'timeout' | 'canceled' | null`）。
const (
	interruptedNone     = ""
	interruptedTimeout  = "timeout"
	interruptedCanceled = "canceled"
)

// ScoredMove 根节点评分表元素：走法（packed）与分数（厘兵，走子方视角）。
type ScoredMove struct {
	Move  int32
	Score int
}

// SearchConfig 搜索配置（TS SearchConfig 的 Go 化）。
type SearchConfig struct {
	MaxDepth int
	// DeadlineMs 绝对截止时间戳（time.Now().UnixMilli() 基准，毫秒）。
	DeadlineMs int64
	// Randomness 根节点随机窗口（厘兵）；> 0 时根节点全窗口并在近最佳中随机取一。
	Randomness int
	// ShouldAbort 取消探针：与 deadline 同节奏（每 64 节点）轮询，返回 true 即中止。
	ShouldAbort func() bool
	// HistoryCounts 全局对局历史出现次数表（DR-006 前置，03 §6.1）。
	// nil = 完全关闭重复检测，行为与 Dart 口径逐位一致（向后兼容铁律）。
	HistoryCounts map[uint64]int
}

// 路径重复阶梯惩罚（厘兵，03 §6.1）：occ=2 → +50 / occ=3 → +150 / occ≥4 → 和棋分 0。
var pathRepeatPenalty = [2]int{50, 150}

// globalRepeatPenalty 全局历史命中（count≥2）的单次惩罚基数（厘兵）。
const globalRepeatPenalty = 100

// globalCheckMaxPly 全局历史检查只在浅层启用（03 §6.1：深层仅查路径内重复，防棋力劣化）。
const globalCheckMaxPly = 3

// Search 单次搜索实例。
type Search struct {
	board       *EngineBoard
	maxDepth    int
	deadlineMs  int64
	randomness  int
	shouldAbort func() bool

	nodes int
	// lastScored 最后一层完整搜索的根节点评分表（未排序）。
	lastScored []ScoredMove
	// best 最佳走法（packed；hasBest=false 等价 TS null；迭代加深逐层覆盖）。
	best    int32
	hasBest bool
	// Interrupted 中断原因：超时/取消后置位（对齐 Dart `_TimeUp` 捕获语义，不向上抛）。
	Interrupted string

	moveBufs [maxPly * moveStride]int32

	historyCounts map[uint64]int
	// pathKeys 搜索路径局面键栈（按 ply 下标覆盖写，negamax 入口赋值即等效 push）。
	pathKeys [maxPly]uint64
}

// NewSearch 构造搜索实例。
func NewSearch(board *EngineBoard, config SearchConfig) *Search {
	return &Search{
		board:         board,
		maxDepth:      config.MaxDepth,
		deadlineMs:    config.DeadlineMs,
		randomness:    config.Randomness,
		shouldAbort:   config.ShouldAbort,
		historyCounts: config.HistoryCounts,
	}
}

// Run 迭代加深主入口：返回最佳走法（packed）。
// 超时返回上一深度已得到的最佳走法；无合法走法 ok=false。
func (s *Search) Run() (best int32, ok bool) {
	s.iterate(false)
	return s.best, s.hasBest
}

// RunScored 迭代加深并返回最后一层完整搜索的根节点评分表（按分数降序）。
// 强制根节点全窗口（不剪枝）：表中每个着法的分数是真实分差。
// 空表 = 无合法走法（被将死/困毙）。
func (s *Search) RunScored() []ScoredMove {
	s.iterate(true)
	out := make([]ScoredMove, len(s.lastScored))
	copy(out, s.lastScored)
	slices.SortStableFunc(out, func(a, b ScoredMove) int { return b.Score - a.Score })
	return out
}

// NodeCount 已消耗节点数（测试/统计用）。
func (s *Search) NodeCount() int { return s.nodes }

func (s *Search) iterate(forceFullRootWindow bool) {
	b := s.board
	buf := s.moveBufs[:]
	// 根节点走法必须做合法性过滤（深层节点在递归内过滤）：
	// 否则被将军时 AI 可能"吃掉将军的子"而不真正解将。
	n := b.GenerateMoves(buf, 0, false)
	// 按等级降序。
	ordered := make([]int32, n)
	copy(ordered, buf[:n])
	slices.SortFunc(ordered, func(x, y int32) int { return int(y) - int(x) })
	rootMoves := make([]int32, 0, n)
	for _, move := range ordered {
		from := PackedFrom(move)
		to := PackedTo(move)
		cap := b.ApplyMove(from, to)
		leavesSelfInCheck := b.IsCheck(!b.isRedTurn)
		b.UndoMove(from, to, cap)
		if !leavesSelfInCheck {
			rootMoves = append(rootMoves, move)
		}
	}
	if len(rootMoves) == 0 {
		return // 被将死或困毙
	}

	// 根节点是否全窗口：低难度需要真实分差做随机挑选；
	// runScored 强制全窗口（分数即真实分差）。
	fullRootWindow := forceFullRootWindow || s.randomness > 0

	// 根节点排序：上层最佳走法放最前（浅层结果指导深层剪枝）。
	if !s.hasBest {
		s.best = rootMoves[0]
		s.hasBest = true
	}

	// 根局面压入路径栈 ply=0（L1 路径重复基准，DR-006 前置）。
	s.pathKeys[0] = b.key

	for depth := 1; depth <= s.maxDepth; depth++ {
		alpha := -INFINITY
		bestScore := -INFINITY
		var bestThisDepth int32
		haveBestThisDepth := false
		scored := make([]ScoredMove, 0, len(rootMoves))
		timedOut := false

		for _, move := range rootMoves {
			from := PackedFrom(move)
			to := PackedTo(move)
			cap := b.ApplyMove(from, to)
			var score int
			var err error
			if fullRootWindow {
				// 全窗口：根节点不剪枝，每个着法的分数都是真实分差。
				score, err = s.negamax(depth-1, -INFINITY, INFINITY, 1)
			} else {
				// 剪枝模式下，未超过 alpha 的走法会返回边界值，
				// 因此只在严格更优时更新 best。
				score, err = s.negamax(depth-1, -INFINITY, -alpha, 1)
			}
			if err != nil {
				// TS catch：置 interrupted 后 break（finally 判 timedOut 不回退盘面，
				// 棋盘停留中途状态随实例废弃）。
				s.Interrupted = interruptedCanceled
				if errors.Is(err, errSearchTimeout) {
					s.Interrupted = interruptedTimeout
				}
				timedOut = true
				break
			}
			score = -score
			b.UndoMove(from, to, cap)
			scored = append(scored, ScoredMove{Move: move, Score: score})
			if score > bestScore {
				bestScore = score
				bestThisDepth = move
				haveBestThisDepth = true
			}
			if score > alpha {
				alpha = score
			}
		}

		if timedOut {
			break
		}

		s.lastScored = scored
		if s.randomness > 0 {
			if picked, ok := s.pickRootMove(scored); ok {
				s.best = picked
			}
		} else if haveBestThisDepth {
			s.best = bestThisDepth
		}

		// 已找到确定的将杀路线，无需更深搜索。
		if alpha >= MATE_SCORE-100 {
			break
		}
	}
}

// pickRootMove 按分数挑选根节点走法；带随机窗口时在接近最佳的走法中随机取一。
func (s *Search) pickRootMove(scored []ScoredMove) (int32, bool) {
	if len(scored) == 0 {
		return 0, false
	}
	if s.randomness <= 0 {
		var best *ScoredMove
		for i := range scored {
			if best == nil || scored[i].Score > best.Score {
				best = &scored[i]
			}
		}
		return best.Move, true
	}
	bestScore := -INFINITY
	for _, sm := range scored {
		if sm.Score > bestScore {
			bestScore = sm.Score
		}
	}
	candidates := make([]int32, 0, len(scored))
	for _, sm := range scored {
		if sm.Score >= bestScore-s.randomness {
			candidates = append(candidates, sm.Move)
		}
	}
	if len(candidates) == 0 {
		return scored[0].Move, true
	}
	// 洗牌后取首个（等价 Dart candidates.shuffle()）。
	for i := len(candidates) - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		candidates[i], candidates[j] = candidates[j], candidates[i]
	}
	return candidates[0], true
}

// -----------------------------------------------------------------------------
// Negamax + Alpha-Beta
// -----------------------------------------------------------------------------

func (s *Search) negamax(depth, alpha, beta, ply int) (int, error) {
	if err := s.bumpNode(); err != nil {
		return 0, err
	}
	if depth <= 0 {
		return s.quiescence(alpha, beta, ply)
	}

	b := s.board
	buf := s.moveBufs[:]
	base := ply * moveStride

	// L1 重复检测（DR-006 前置，03 §6.1）：historyCounts 缺省（nil）时整体关闭，保持旧口径。
	// 路径栈按 ply 覆盖写 = push；qsearch 不参与（吃子线不可能成环）。
	if s.historyCounts != nil {
		key := b.key
		s.pathKeys[ply] = key
		earlier := 0 // 路径 0..ply-1 中同键出现次数（occ = earlier + 1）
		for i := ply - 1; i >= 0; i-- {
			if s.pathKeys[i] == key {
				earlier++
				if earlier >= 3 {
					break
				}
			}
		}
		if earlier > 0 {
			// 造成重复的一方（父节点走子方）受罚：节点分（走子方视角）加惩罚。
			if earlier >= 3 {
				return 0, nil // 第 3 次及以上：和棋分
			}
			return b.Evaluate() + pathRepeatPenalty[earlier-1], nil
		}
		if ply <= globalCheckMaxPly {
			// count≥2 = 真实重复威胁（第 3 次将现）；count=1 不罚，避免误伤正常巡回。
			if count, ok := s.historyCounts[key]; ok && count >= 2 {
				return b.Evaluate() + globalRepeatPenalty*(count-1), nil
			}
		}
	}

	n := b.GenerateMoves(buf, base, false)
	// MVV-LVA：等级在 packed 高位，段内升序排后倒序遍历即等级降序（吃大子优先）。
	slices.Sort(buf[base : base+n])
	anyLegal := false
	for i := 0; i < n; i++ {
		// 升序排后倒序遍历 = 等级降序（MVV-LVA，吃大子优先）。
		move := buf[base+i]
		from := PackedFrom(move)
		to := PackedTo(move)
		cap := b.ApplyMove(from, to)
		// 伪合法走法：走完自将则跳过。
		if b.IsCheck(!b.isRedTurn) {
			b.UndoMove(from, to, cap)
			continue
		}
		anyLegal = true
		// TS：异常上抛时不回退盘面（与 catch 语义一致）。
		score, err := s.negamax(depth-1, -beta, -alpha, ply+1)
		if err != nil {
			return 0, err
		}
		score = -score
		b.UndoMove(from, to, cap)
		if score >= beta {
			return beta, nil
		}
		if score > alpha {
			alpha = score
		}
	}

	if !anyLegal {
		// 无合法走法：被将死或困毙，均判负；越早被杀分越差。
		return -MATE_SCORE + ply, nil
	}
	return alpha, nil
}

// quiescence 静态搜索：只延伸吃子走法，避免在叶子节点因"刚好吃亏"误判。
func (s *Search) quiescence(alpha, beta, ply int) (int, error) {
	if err := s.bumpNode(); err != nil {
		return 0, err
	}

	b := s.board
	// 被将军时必须搜索全部应将走法，否则评估失真。
	if b.IsCheck(b.isRedTurn) && ply < maxQuiescencePly*2 {
		return s.searchEvasions(alpha, beta, ply)
	}

	standPat := b.Evaluate()
	if standPat >= beta {
		return beta, nil
	}
	if standPat > alpha {
		alpha = standPat
	}
	if ply >= maxQuiescencePly {
		return alpha, nil
	}

	buf := s.moveBufs[:]
	base := ply * moveStride
	n := b.GenerateMoves(buf, base, true)
	slices.Sort(buf[base : base+n]) // MVV-LVA（吃大子优先）
	for i := 0; i < n; i++ {
		move := buf[base+i]
		from := PackedFrom(move)
		to := PackedTo(move)
		cap := b.ApplyMove(from, to)
		if b.IsCheck(!b.isRedTurn) {
			b.UndoMove(from, to, cap)
			continue
		}
		score, err := s.quiescence(-beta, -alpha, ply+1)
		if err != nil {
			return 0, err
		}
		score = -score
		b.UndoMove(from, to, cap)
		if score >= beta {
			return beta, nil
		}
		if score > alpha {
			alpha = score
		}
	}
	return alpha, nil
}

// searchEvasions 被将军时的全部应将搜索（含解将失败即被将死的判定）。
func (s *Search) searchEvasions(alpha, beta, ply int) (int, error) {
	b := s.board
	buf := s.moveBufs[:]
	base := ply * moveStride
	n := b.GenerateMoves(buf, base, false)
	slices.Sort(buf[base : base+n]) // MVV-LVA（吃大子优先）
	anyLegal := false
	for i := 0; i < n; i++ {
		move := buf[base+i]
		from := PackedFrom(move)
		to := PackedTo(move)
		cap := b.ApplyMove(from, to)
		if b.IsCheck(!b.isRedTurn) {
			b.UndoMove(from, to, cap)
			continue
		}
		anyLegal = true
		var score int
		var err error
		if ply >= maxQuiescencePly*2 {
			score = b.Evaluate()
		} else {
			score, err = s.quiescence(-beta, -alpha, ply+1)
			if err != nil {
				return 0, err
			}
			score = -score
		}
		b.UndoMove(from, to, cap)
		if score >= beta {
			return beta, nil
		}
		if score > alpha {
			alpha = score
		}
	}
	if !anyLegal {
		return -MATE_SCORE + ply, nil
	}
	return alpha, nil
}

// bumpNode 每 64 节点查一次 deadline / 取消标志，超时或取消即以 sentinel 中止。
func (s *Search) bumpNode() error {
	s.nodes++
	if s.nodes&0x3f == 0 {
		if time.Now().UnixMilli() > s.deadlineMs {
			return errSearchTimeout
		}
		if s.shouldAbort != nil && s.shouldAbort() {
			return errSearchCanceled
		}
	}
	return nil
}
