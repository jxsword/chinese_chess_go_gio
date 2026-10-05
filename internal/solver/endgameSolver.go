// Package solver 中国象棋残局求解器：迭代加深 AND/OR 杀棋搜索
// （Electron 版 endgameSolver.ts 1:1 移植，04 文档；其源为 endgame_solver.dart）。
//
// 与评分引擎的本质区别：这是**证明问题**（存在必胜策略并给出强制线路）。
// 语义（04 文档 §1，与 Electron 版一致）：
//   - OR 节点（求解方行棋）：存在一路必胜走法即胜；
//   - AND 节点（应对方行棋）：所有防着皆败才胜——防着分叉即"多条破解走法"；
//   - 将杀**或困毙**均判胜（中国象棋无逼和）；
//   - 路径去重：搜索路径内局面重复即剪枝（近似长将判负，已知限制）；
//   - 限时内未决返回 timeout；深度树闭合返回 noSolution。
//
// 置换表：沿 Electron 版表项语义（winAt/failAt 按局面+轮走方记"已在 r 半着内
// 证明"），Go 版键用 uint64 Zobrist（04 §2；本包私有键表，独立于引擎搜索键，
// 避免互相污染）。Go 版差异：ctx 取消（04 §5）——探针每 512 节点接入，
// 取消与超时同形（timeout 结算，协议层随后以 canceled 收口，04 §4）。
// 纯 Go：不依赖引擎，仅依赖 rules 的 Board 层（04 §9.1）；禁止 import
// Wails / net/http / frontend（铁律 #1）。
package solver

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// Status 求解结论状态（与棋谱 SolveStatus 一一对应：solved/noSolution/timeout）。
type Status string

const (
	StatusSolved     Status = "solved"
	StatusNoSolution Status = "noSolution"
	StatusTimeout    Status = "timeout"
)

// Solution 一条强制线路（自初始局面起，求解方先行；裸坐标，落库前经
// fillMovePieces 补齐）。
type Solution struct {
	Moves []rules.Move
}

// Result 求解结果。
type Result struct {
	Status Status
	// Solutions 首解着法序列列表（solved 且对方已被将死时为空）。
	Solutions []Solution
	// Elapsed 用时（毫秒）。
	Elapsed int64
	// SearchedPlies 实际搜索到的深度（半着数）。
	SearchedPlies int
}

// SolveIsUnique 解是否唯一：solved 且只有一条破解走法（endgame_solver.dart:39）。
func SolveIsUnique(result Result) bool {
	return result.Status == StatusSolved && len(result.Solutions) == 1
}

// Options 求解选项。
type Options struct {
	// TimeLimitMs 限时毫秒数，默认 30s；0=未设（wire 缺省约定）同默认；
	// <0 视为不限（原版按 1 小时兜底）。
	TimeLimitMs int
	// MaxPlies 深度上限（半着数，奇数：求解方最后收官），默认 9；0=未设同默认。
	MaxPlies int
	// Ctx 取消（工作室关闭页/重新求解；nil = 不可取消）。
	Ctx context.Context
}

// MaxSolutions 最多枚举的解数量（防组合爆炸，endgame_solver.dart:68）。
const MaxSolutions = 64

const hourMs = int64(60 * 60 * 1000)

// errSearchTimeout 期限耗尽 / 外部中止（内部控制流，对应 TS SearchTimeout /
// Dart _SearchTimeout；Go 以哨兵错误沿递归传播，等价原版异常控制流）。
var errSearchTimeout = errors.New("search timeout")

// searchState 递归状态（一次 Solve/IsWinningFirstMove 一份，对应原版实例字段）。
type searchState struct {
	board *rules.Board
	// winAt[key]=r：该局面（含轮走方）已在 r 半着内证明（OR→胜 / AND→全败）。
	winAt map[uint64]int
	// failAt[key]=r：该局面已在 r 半着预算内证明无解（OR→不能胜 / AND→有逃着）。
	failAt map[uint64]int
	// path 当前搜索路径上的局面键：重复即剪枝（近似长将判负）。
	path map[uint64]struct{}
	// shouldAbort 外部中止探针（ctx 取消接入点；每 512 节点检查）。
	shouldAbort func() bool
	nodes       int
	deadline    time.Time
	maxPlies    int
}

func newState(fen string, timeLimitMs int, maxPlies int, shouldAbort func() bool) (*searchState, error) {
	limit := int64(0)
	if timeLimitMs > 0 {
		limit = int64(timeLimitMs)
	} else if timeLimitMs < 0 {
		limit = hourMs
	} else {
		limit = 30_000
	}
	board, err := rules.FromFen(fen)
	if err != nil {
		return nil, err
	}
	return &searchState{
		board:       board,
		winAt:       make(map[uint64]int),
		failAt:      make(map[uint64]int),
		path:        make(map[uint64]struct{}),
		shouldAbort: shouldAbort,
		deadline:    time.Now().Add(time.Duration(limit) * time.Millisecond),
		maxPlies:    maxPlies,
	}, nil
}

// reset 重置搜索状态（IsWinningFirstMove 复用，endgame_solver.dart:304-308）。
func (s *searchState) reset(fen string) error {
	board, err := rules.FromFen(fen)
	if err != nil {
		return err
	}
	s.board = board
	s.winAt = make(map[uint64]int)
	s.failAt = make(map[uint64]int)
	s.path = make(map[uint64]struct{})
	s.nodes = 0
	return nil
}

// Solve 入口：求解一个残局（渲染层经 App.SolverSolve 绑定在 goroutine 内调用，
// 不阻塞 UI；对应 EndgameSolver.solve，原版经 Isolate、Electron 版由 Worker 薄壳承载）。
// 非法 FEN 返回 rules.FenFormatError（工作室校验层拦截，不进入求解）。
func Solve(fen string, options Options) (Result, error) {
	started := time.Now()
	maxPlies := options.MaxPlies
	if maxPlies == 0 {
		maxPlies = 9
	}
	ctx := options.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	state, err := newState(fen, options.TimeLimitMs, maxPlies, func() bool {
		return ctx.Err() != nil
	})
	if err != nil {
		return Result{}, err
	}
	result, err := runSolve(state, fen)
	if err != nil {
		return Result{}, err
	}
	result.Elapsed = time.Since(started).Milliseconds()
	return result, nil
}

func runSolve(state *searchState, fen string) (Result, error) {
	if err := state.reset(fen); err != nil {
		return Result{}, err
	}

	// 对方已被将死（处于被将军且无着可走）：0 步解。
	// 注意：对方"暂无着"但未被将军时不构成胜势——此刻轮走方是己方，
	// 己方一手后对方可能重新获得着法（endgame_solver.dart:124-134）。
	opponent := rules.OpponentOf(state.board.Turn())
	if state.board.IsCheck(opponent) && !state.board.HasAnyLegalMoveFor(opponent) {
		return Result{Status: StatusSolved, Solutions: []Solution{}, SearchedPlies: 0}, nil
	}

	for plies := 1; plies <= state.maxPlies; plies += 2 {
		win, err := attackWin(state, plies)
		if err != nil {
			if errors.Is(err, errSearchTimeout) {
				return Result{Status: StatusTimeout, Solutions: []Solution{}, SearchedPlies: plies}, nil
			}
			return Result{}, err
		}
		if win {
			solutions, err := enumerate(state, plies)
			if err != nil {
				if errors.Is(err, errSearchTimeout) {
					return Result{Status: StatusTimeout, Solutions: []Solution{}, SearchedPlies: plies}, nil
				}
				return Result{}, err
			}
			return Result{Status: StatusSolved, Solutions: solutions, SearchedPlies: plies}, nil
		}
	}
	return Result{Status: StatusNoSolution, Solutions: []Solution{}, SearchedPlies: state.maxPlies}, nil
}

// -----------------------------------------------------------------------------
// AND/OR 搜索（endgame_solver.dart:166-232）
// -----------------------------------------------------------------------------

// attackWin OR 节点：求解方行棋，能否在 r 半着内强制获胜。
func attackWin(state *searchState, r int) (bool, error) {
	if r <= 0 {
		return false, nil
	}
	if err := tick(state); err != nil {
		return false, err
	}
	key := stateKey(state)
	if _, repeated := state.path[key]; repeated {
		return false, nil // 重复局面：不视为必胜（近似长将判负）
	}
	if win, ok := state.winAt[key]; ok && win <= r {
		return true, nil
	}
	if fail, ok := state.failAt[key]; ok && fail >= r {
		return false, nil
	}

	moves := orderedMoves(state)
	state.path[key] = struct{}{}
	won := false
	for _, m := range moves {
		snapshot := state.board.ApplyMove(m)
		opponent := state.board.Turn()
		if !state.board.HasAnyLegalMoveFor(opponent) {
			// 将死或困毙：应对方无着可走即判负（中国象棋无逼和）。
			won = true
		} else if r >= 2 {
			lose, err := defendLose(state, r-1)
			if err != nil {
				state.board.UndoMove(snapshot)
				return false, err
			}
			if lose {
				won = true
			}
		}
		state.board.UndoMove(snapshot)
		if won {
			break
		}
	}
	delete(state.path, key)
	if won {
		if win, ok := state.winAt[key]; !ok || r < win {
			state.winAt[key] = r
		}
		return true, nil
	}
	if fail, ok := state.failAt[key]; !ok || r > fail {
		state.failAt[key] = r
	}
	return false, nil
}

// defendLose AND 节点：应对方行棋，是否所有防着都在 r 半着内被制服。
func defendLose(state *searchState, r int) (bool, error) {
	if err := tick(state); err != nil {
		return false, err
	}
	key := stateKey(state)
	if _, repeated := state.path[key]; repeated {
		return false, nil
	}
	if win, ok := state.winAt[key]; ok && win <= r {
		return true, nil
	}
	if fail, ok := state.failAt[key]; ok && fail >= r {
		return false, nil
	}

	// 应对方无着可走 = 被将死/困毙 = 求解方胜。
	if !state.board.HasAnyLegalMoveFor(state.board.Turn()) {
		return true, nil
	}

	moves := orderedMoves(state)
	state.path[key] = struct{}{}
	allLose := len(moves) > 0
	for _, d := range moves {
		snapshot := state.board.ApplyMove(d)
		var lose bool
		var err error
		if r >= 1 {
			lose, err = attackWin(state, r-1)
		}
		state.board.UndoMove(snapshot)
		if err != nil {
			return false, err
		}
		if !lose {
			allLose = false
			break
		}
	}
	delete(state.path, key)
	if allLose {
		if win, ok := state.winAt[key]; !ok || r < win {
			state.winAt[key] = r
		}
		return true, nil
	}
	if fail, ok := state.failAt[key]; !ok || r > fail {
		state.failAt[key] = r
	}
	return false, nil
}

// -----------------------------------------------------------------------------
// 多解枚举：根节点所有必胜首着 × 应对方每种防着的分支线路（endgame_solver.dart:238-300）
// -----------------------------------------------------------------------------

func enumerate(state *searchState, plies int) ([]Solution, error) {
	out := []Solution{}
	for _, m := range orderedMoves(state) {
		if len(out) >= MaxSolutions {
			break
		}
		snapshot := state.board.ApplyMove(m)
		win := false
		if !state.board.HasAnyLegalMoveFor(state.board.Turn()) {
			win = true // 一着制胜
		} else if plies >= 2 {
			lose, err := defendLose(state, plies-1)
			if err != nil {
				state.board.UndoMove(snapshot)
				return nil, err
			}
			if lose {
				win = true
			}
		}
		if win {
			line := []rules.Move{m}
			if state.board.HasAnyLegalMoveFor(state.board.Turn()) {
				if err := extendLine(state, line, plies-1, &out); err != nil {
					state.board.UndoMove(snapshot)
					return nil, err
				}
			}
			out = append(out, Solution{Moves: append([]rules.Move(nil), line...)})
		}
		state.board.UndoMove(snapshot)
	}
	return out, nil
}

// extendLine 从"应对方行棋、已被证明必败"的局面继续，把每种防着展开成一条线路。
func extendLine(state *searchState, prefix []rules.Move, r int, out *[]Solution) error {
	if len(*out) >= MaxSolutions || r <= 0 {
		return nil
	}
	for _, d := range orderedMoves(state) {
		if len(*out) >= MaxSolutions {
			return nil
		}
		snapshot := state.board.ApplyMove(d)
		if r >= 1 {
			win, err := attackWin(state, r-1)
			if err != nil {
				state.board.UndoMove(snapshot)
				return err
			}
			if win {
				reply, err := findWinningReply(state, r-1)
				if err != nil {
					state.board.UndoMove(snapshot)
					return err
				}
				if reply != nil {
					line := append(append([]rules.Move(nil), prefix...), d, *reply)
					replySnapshot := state.board.ApplyMove(*reply)
					finished := !state.board.HasAnyLegalMoveFor(state.board.Turn())
					state.board.UndoMove(replySnapshot)
					if finished {
						*out = append(*out, Solution{Moves: line})
					} else {
						if err := extendLine(state, line, r-2, out); err != nil {
							state.board.UndoMove(snapshot)
							return err
						}
					}
				}
			}
		}
		state.board.UndoMove(snapshot)
		// 该防着在当前预算下未被证明必败（理论不应发生）：跳过该分支。
	}
	return nil
}

// findWinningReply OR 节点（求解方行棋）找一个必胜应手；无则 nil。
func findWinningReply(state *searchState, r int) (*rules.Move, error) {
	for _, m := range orderedMoves(state) {
		snapshot := state.board.ApplyMove(m)
		win := false
		if !state.board.HasAnyLegalMoveFor(state.board.Turn()) {
			win = true
		} else if r >= 2 {
			lose, err := defendLose(state, r-1)
			if err != nil {
				state.board.UndoMove(snapshot)
				return nil, err
			}
			if lose {
				win = true
			}
		}
		state.board.UndoMove(snapshot)
		if win {
			reply := m
			return &reply, nil
		}
	}
	return nil, nil
}

// IsWinningFirstMove 验证某条"首着"是否属于必胜着法集合（LLM 求解辅助的裁判，
// 04 文档 §3；endgame_solver.dart:102-110 / 303-320）。
//
// 语义：把 firstMove 当作根节点唯一首着跑 _attackWin——若成立，该首着必胜。
// 仅当 Solve 得出 solved 结论后调用才有证明意义。搜索超时/取消返回 false
// （与 TS catch SearchTimeout → false 一致）。
func IsWinningFirstMove(fen string, firstMove rules.Move, options Options) (bool, error) {
	maxPlies := options.MaxPlies
	if maxPlies == 0 {
		maxPlies = 9
	}
	ctx := options.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	state, err := newState(fen, options.TimeLimitMs, maxPlies, func() bool {
		return ctx.Err() != nil
	})
	if err != nil {
		return false, err
	}
	if err := state.reset(fen); err != nil {
		return false, err
	}
	legal := false
	for _, m := range state.board.LegalMovesFor(firstMove.From) {
		if m.To.Col == firstMove.To.Col && m.To.Row == firstMove.To.Row {
			legal = true
			break
		}
	}
	if !legal {
		return false, nil
	}
	snapshot := state.board.ApplyMove(firstMove)
	defer state.board.UndoMove(snapshot)
	if !state.board.HasAnyLegalMoveFor(state.board.Turn()) {
		return true, nil
	}
	plies := state.maxPlies
	if plies < 2 {
		return false, nil
	}
	return defendLose(state, plies-1)
}

// -----------------------------------------------------------------------------
// 辅助（endgame_solver.dart:326-368）
// -----------------------------------------------------------------------------

// captureValue 吃子子力价值（车90/炮45/马40/士象20/兵10/将1000）。
var captureValue = map[rules.Kind]int{
	rules.Rook:     90,
	rules.Cannon:   45,
	rules.Knight:   40,
	rules.Minister: 20,
	rules.Advisor:  20,
	rules.Pawn:     10,
	rules.King:     1000,
}

// orderedMoves 着法排序：将军 > 吃子（按子力价值）> 其他，显著改善剪枝效率。
// 同分保持生成序（TS sort 稳定排序语义，决定解法枚举顺序逐位一致）。
func orderedMoves(state *searchState) []rules.Move {
	type scoredMove struct {
		score int
		move  rules.Move
	}
	scored := []scoredMove{}
	turn := state.board.Turn()
	for row := 0; row < 10; row++ {
		for col := 0; col < 9; col++ {
			piece := state.board.PieceAt(col, row)
			if piece == nil || piece.Side != turn {
				continue
			}
			// LegalMovesFor 已过滤自将，且每条 Move 带被吃子信息。
			for _, m := range state.board.LegalMovesFor(rules.Pos(col, row)) {
				scored = append(scored, scoredMove{score: scoreMove(state, m), move: m})
			}
		}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	moves := make([]rules.Move, len(scored))
	for i, e := range scored {
		moves[i] = e.move
	}
	return moves
}

// scoreMove 着法评分：吃子按子力价值 + 走完后将军加 500（不含 tick，不会超时）。
func scoreMove(state *searchState, m rules.Move) int {
	score := 0
	if m.Captured != nil {
		score += captureValue[m.Captured.Kind]
	} else {
		if target := state.board.PieceAtP(m.To); target != nil {
			score += captureValue[target.Kind]
		}
	}
	// 将军加成：走完后对方王被将。
	snapshot := state.board.ApplyMove(m)
	if state.board.IsCheck(state.board.Turn()) {
		score += 500
	}
	state.board.UndoMove(snapshot)
	return score
}

// tick 每 512 节点查一次截止时间 / 中止探针（endgame_solver.dart:363-368）。
func tick(state *searchState) error {
	state.nodes++
	if state.nodes%512 == 0 {
		if state.shouldAbort() || time.Now().After(state.deadline) {
			return errSearchTimeout
		}
	}
	return nil
}

// -----------------------------------------------------------------------------
// 置换表键（04 §2：uint64 Zobrist，本包私有——独立于引擎搜索键）
// -----------------------------------------------------------------------------

// 棋子键编码：7 棋种 × 双方 = 14 种（0=空不占键），第 15 位留空。
const zobristCodeSpan = 15

// solverPieceKeys / solverTurnKey 本包私有键表：固定种子 xorshift64 惰性初始化
// （种子与引擎 zobrist 不同，键值序列互不相干；keys 仅在求解器内部使用）。
var (
	zobristOnce     sync.Once
	solverPieceKeys [zobristCodeSpan][90]uint64
	solverTurnKey   uint64
)

func initSolverZobrist() {
	zobristOnce.Do(func() {
		s := uint64(0x2545F4914F6CDD1D)
		next := func() uint64 {
			s ^= s << 13
			s ^= s >> 7
			s ^= s << 17
			return s
		}
		for i := 0; i < zobristCodeSpan; i++ {
			for sq := 0; sq < 90; sq++ {
				solverPieceKeys[i][sq] = next()
			}
		}
		solverTurnKey = next()
	})
}

// solverPieceCode 棋子 → 键表下标（红方 0..6，黑方 7..13；与引擎编码无关）。
func solverPieceCode(p *rules.Piece) int {
	base := 0
	if p.Side == rules.Black {
		base = 7
	}
	switch p.Kind {
	case rules.King:
		return base + 0
	case rules.Advisor:
		return base + 1
	case rules.Minister:
		return base + 2
	case rules.Knight:
		return base + 3
	case rules.Rook:
		return base + 4
	case rules.Cannon:
		return base + 5
	case rules.Pawn:
		return base + 6
	}
	return -1
}

// stateKey 当前局面（含轮走方）的置换表键：逐格异或 (棋子,格) 键，
// 黑方走子异或轮走方键——语义等价 TS 的完整 FEN 字符串键（FEN 亦含轮走方）。
func stateKey(state *searchState) uint64 {
	initSolverZobrist()
	var key uint64
	for row := 0; row < 10; row++ {
		for col := 0; col < 9; col++ {
			p := state.board.PieceAt(col, row)
			if p == nil {
				continue
			}
			if code := solverPieceCode(p); code >= 0 {
				key ^= solverPieceKeys[code][row*9+col]
			}
		}
	}
	if state.board.Turn() == rules.Black {
		key ^= solverTurnKey
	}
	return key
}
