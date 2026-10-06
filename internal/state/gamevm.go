package state

// 对局 VM（翻译源 = 上游 frontend/src/stores/gameVm.ts，322 行；对应 board_vm.dart，
// 02 文档 §5 / 07 文档 §6.2）。纯 Go（铁律 #G1）：仅依赖 internal/rules。
// 每局一个实例（铁律 #G4），禁止全局单例；内部字段免锁——state 全部归 UI 主
// goroutine 所有（07 §6.2），-race 兜底验证。
//
// 与上游的差异：订阅回调退化为普通字段回调（notify 语义不变）；
// 裁决触发由上游 React useEffect（依赖 moveHistory.length）改为内联收口点回调
//（07 §3：executeMove push 侧 / restore 重放完成侧 → OnHistoryGrow；
// 悔棋 pop 与新局 → OnHistoryRewound 重置 declined 与未决和棋框）。

import (
	"reflect"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// GameMode 对局模式（07 文档 §1.1 game_records.mode 注释的六值枚举；
// 翻译源 = 上游 shared/ipc/types.ts）。
type GameMode string

const (
	ModeHumanVsAi    GameMode = "humanVsAi"
	ModeHumanVsHuman GameMode = "humanVsHuman"
	ModeAiVsAi       GameMode = "aiVsAi"
	ModeHumanVsLlm   GameMode = "humanVsLlm"
	ModeLlmVsLlm     GameMode = "llmVsLlm"
	ModeEndgame      GameMode = "endgame"
)

// GameResult 对局结果（07 文档 §1.1 result 注释）；nil 指针 = 进行中。
type GameResult string

const (
	ResultRedWins   GameResult = "redWins"
	ResultBlackWins GameResult = "blackWins"
	ResultDraw      GameResult = "draw"
)

// GameSnapshot 对局状态快照（对应 board_state.dart，不可变；出参均为防御性拷贝）。
type GameSnapshot struct {
	Fen string
	// MoveHistory 走法历史（含棋子/吃子信息，供悔棋与记谱）。
	MoveHistory []rules.Move
	// FenHistory 逐手局面 FEN 序列（初始局面→当前，含轮走方；DR-018 供 L2/L3 使用），
	// 长度恒 = len(MoveHistory) + 1（四收口点见 07 §3）。
	FenHistory []string
	IsRedTurn  bool
	IsCheck    bool
	// Result 对局结果，nil 表示进行中。
	Result *GameResult
	// Selected 选中位置。
	Selected *rules.Position
	// LegalTargets 选中棋子的合法走法目标。
	LegalTargets []rules.Position
	// LastMove 最近一步（高亮起止点）。
	LastMove *rules.Move
}

// SerializedGame 序列化结果（对应上游 restore 入参形状 {fen, moves}；
// moves 为裸四元组 [fCol, fRow, tCol, tRow]，翻译源 = storage-schema/moveStack.ts）。
type SerializedGame struct {
	// Fen 本局起始 FEN（DR-008：serialize 存它 + 完整着法栈，restore 以此重放重建整局）。
	Fen string
	// Moves 完整着法栈裸四元组。
	Moves [][]int
}

// EncodeMoveStack 对局走法历史 → 存档四元组（repository.dart:25-27 的序列化语义）。
func EncodeMoveStack(moves []rules.Move) [][]int {
	out := make([][]int, len(moves))
	for i, m := range moves {
		out[i] = []int{m.From.Col, m.From.Row, m.To.Col, m.To.Row}
	}
	return out
}

// GameVm 对局视图模型（board_vm.dart 同构）。
type GameVm struct {
	board    *rules.Board
	startFen string
	history  []rules.Move
	// fenHistory 逐手局面 FEN（含初始局面，与 history 一一对应+1；
	// executeMove push / undo pop / restore 重放重建 / newGame 重置——四收口点）。
	fenHistory  []string
	inputLocked bool
	listeners   []func()
	snap        GameSnapshot

	// OnHistoryGrow 裁决触发收口（07 §3）：历史增长（executeMove push 侧 /
	// restore 重放完成侧）后以完整 fenHistory 拷贝调用；app→ui 层据此调
	// rules.JudgeRepetition 派发 k=2 警告 / k=3 判负判和交互。等价上游
	// useEffect 依赖 moveHistory.length（仅增长时裁决）。
	OnHistoryGrow func(fenHistory []string)
	// OnHistoryRewound 收口（07 §3）：悔棋（pop）与新局（newGame/newGameFromFen）
	// 后调用——ui 重置 declined 标志与未决和棋框。
	OnHistoryRewound func()
}

// safeBoardFromFen FEN 无效回退初始局面，避免崩溃（board_vm.dart:283-287）。
func safeBoardFromFen(fen string) *rules.Board {
	if fen != "" {
		if b, err := rules.FromFen(fen); err == nil {
			return b
		}
	}
	return rules.Initial()
}

// NewGameVm 创建对局 VM（initialFen 为空串 = 标准初始局面）。
func NewGameVm(initialFen string) *GameVm {
	vm := &GameVm{}
	vm.board = safeBoardFromFen(initialFen)
	vm.startFen = vm.board.ToFen()
	vm.fenHistory = []string{vm.board.ToFen()}
	vm.snap = vm.buildSnapshot(nil, nil, nil)
	return vm
}

// Subscribe 订阅快照变更，返回反注册函数（上游 Set<Unsubscribe> 语义；每局
// 订阅方固定为 store 桥接函数，按具名函数值反注册）。
func (vm *GameVm) Subscribe(listener func()) func() {
	vm.listeners = append(vm.listeners, listener)
	return func() {
		for i, l := range vm.listeners {
			if reflect.ValueOf(l).Pointer() == reflect.ValueOf(listener).Pointer() {
				vm.listeners = append(vm.listeners[:i], vm.listeners[i+1:]...)
				break
			}
		}
	}
}

func (vm *GameVm) notify() {
	for _, l := range append([]func(){}, vm.listeners...) {
		l()
	}
}

// Board 当前棋盘（board_vm.dart:42；供动画层查询起点棋子等——只读使用）。
func (vm *GameVm) Board() *rules.Board { return vm.board }

// Current 当前状态快照（board_vm.dart:46；切片字段为拷贝，调用方修改不影响内部）。
func (vm *GameVm) Current() GameSnapshot { return copySnapshot(vm.snap) }

// IsRedTurn 是否轮到红方。
func (vm *GameVm) IsRedTurn() bool { return vm.board.IsRedTurn() }

// IsFinished 对局是否已终局（result != null）。
func (vm *GameVm) IsFinished() bool { return vm.snap.Result != nil }

func copySnapshot(s GameSnapshot) GameSnapshot {
	out := s
	out.MoveHistory = append([]rules.Move(nil), s.MoveHistory...)
	out.FenHistory = append([]string(nil), s.FenHistory...)
	out.LegalTargets = append([]rules.Position(nil), s.LegalTargets...)
	return out
}

// buildSnapshot 用当前棋盘生成快照（board_vm.dart:55-80：终局判定在此统一计算）。
func (vm *GameVm) buildSnapshot(selected *rules.Position, legal []rules.Position, lastMove *rules.Move) GameSnapshot {
	turn := vm.board.Turn()
	isCheck := vm.board.IsCheck(turn)
	var result *GameResult
	if vm.board.IsCheckmate(turn) {
		r := resultOfLoser(turn)
		result = &r
	} else if vm.board.IsStalemate(turn) {
		// 困毙（无子可动且未被将军）判困毙方负，中国象棋无逼和（02 §3）。
		r := resultOfLoser(turn)
		result = &r
	}
	snap := GameSnapshot{
		Fen:          vm.board.ToFen(),
		MoveHistory:  append([]rules.Move(nil), vm.history...),
		FenHistory:   append([]string(nil), vm.fenHistory...),
		IsRedTurn:    vm.board.IsRedTurn(),
		IsCheck:      isCheck,
		Result:       result,
		Selected:     selected,
		LegalTargets: append([]rules.Position(nil), legal...),
		LastMove:     lastMove,
	}
	return snap
}

// resultOfLoser 被将死/困毙方判负 → 对方胜。
func resultOfLoser(loser rules.Side) GameResult {
	if loser == rules.Red {
		return ResultBlackWins
	}
	return ResultRedWins
}

func (vm *GameVm) commit(snapshot GameSnapshot) {
	vm.snap = snapshot
	vm.notify()
}

// Restore 恢复历史走法，逐手 replay（board_vm.dart:87-132）。
// 重放基准 = 存档的本局起始 FEN（DR-008：serialize 存起始 FEN + 完整着法栈）。
// 跳脏记录：长度≠4 / 越界 / 源格无子；完成后仅保留 lastMove 高亮；
// fenHistory 逐手采集（跳脏记录不 push），重放完成后统一触发 OnHistoryGrow。
func (vm *GameVm) Restore(fen string, moves [][]int) {
	if b, err := rules.FromFen(fen); err == nil {
		vm.board = b
	} else {
		vm.board = rules.Initial()
	}
	vm.startFen = vm.board.ToFen()
	vm.history = nil
	vm.fenHistory = []string{vm.board.ToFen()}
	for _, m := range moves {
		if len(m) != 4 {
			continue // 跳过不完整的数据
		}
		from := rules.Pos(m[0], m[1])
		to := rules.Pos(m[2], m[3])
		if !rules.InBoard(from.Col, from.Row) || !rules.InBoard(to.Col, to.Row) {
			continue // 越界跳过
		}
		piece := vm.board.PieceAtP(from)
		if piece == nil {
			continue // 源格无棋子（数据不一致），跳过
		}
		applied := vm.board.ApplyMove(rules.Move{From: from, To: to})
		vm.history = append(vm.history, rules.Move{From: applied.From, To: applied.To, Piece: piece, Captured: applied.Captured})
		vm.fenHistory = append(vm.fenHistory, vm.board.ToFen())
	}
	var lastMove *rules.Move
	if len(vm.history) > 0 {
		last := vm.history[len(vm.history)-1]
		lastMove = &last
	}
	vm.commit(vm.buildSnapshot(nil, nil, lastMove))
	vm.fireHistoryGrow()
}

func (vm *GameVm) fireHistoryGrow() {
	if vm.OnHistoryGrow != nil {
		vm.OnHistoryGrow(append([]string(nil), vm.fenHistory...))
	}
}

func (vm *GameVm) fireHistoryRewound() {
	if vm.OnHistoryRewound != nil {
		vm.OnHistoryRewound()
	}
}

// OnTap 点击处理（board_vm.dart:140-170）。
// 输入锁期间/终局后忽略；选中→换选→走子→取消。
func (vm *GameVm) OnTap(col, row int) {
	if vm.inputLocked || vm.IsFinished() {
		return
	}
	tapped := vm.board.PieceAt(col, row)
	selected := vm.snap.Selected

	if selected != nil {
		for _, p := range vm.snap.LegalTargets {
			if p.Col == col && p.Row == row {
				vm.executeMove(rules.Move{From: *selected, To: rules.Pos(col, row)})
				return
			}
		}
		// 点击己方另一棋子 → 切换选中
		if tapped != nil && tapped.Side == vm.board.Turn() {
			vm.selectPosition(col, row)
			return
		}
		// 否则取消选中
		canceled := vm.snap
		canceled.Selected = nil
		canceled.LegalTargets = nil
		vm.commit(canceled)
		return
	}

	// 未选中：必须点击己方棋子
	if tapped != nil && tapped.Side == vm.board.Turn() {
		vm.selectPosition(col, row)
	}
}

func (vm *GameVm) selectPosition(col, row int) {
	p := rules.Pos(col, row)
	legal := make([]rules.Position, 0)
	for _, m := range vm.board.LegalMovesFor(p) {
		legal = append(legal, m.To)
	}
	next := vm.snap
	next.Selected = &p
	next.LegalTargets = legal
	vm.commit(next)
}

func (vm *GameVm) executeMove(move rules.Move) {
	from := move.From
	to := move.To
	piece := vm.board.PieceAtP(from)
	applied := vm.board.ApplyMove(rules.Move{From: from, To: to})
	vm.history = append(vm.history, rules.Move{From: applied.From, To: applied.To, Piece: piece, Captured: applied.Captured})
	vm.fenHistory = append(vm.fenHistory, vm.board.ToFen())
	last := rules.Move{From: applied.From, To: applied.To, Captured: applied.Captured}
	vm.commit(vm.buildSnapshot(nil, nil, &last))
	vm.fireHistoryGrow()
}

// PlayMove 强校验走子入口（供 AI/LLM 应手，board_vm.dart:208-217）。
// 起点须有轮走方棋子且目标 ∈ legalMovesFor；非法返回 false 不改状态——
// 这是对弈链路的最终裁决点（铁律 #G10 的本地落点）。
func (vm *GameVm) PlayMove(from, to rules.Position) bool {
	if vm.IsFinished() {
		return false
	}
	piece := vm.board.PieceAtP(from)
	if piece == nil || piece.Side != vm.board.Turn() {
		return false
	}
	isLegal := false
	for _, m := range vm.board.LegalMovesFor(from) {
		if rules.SamePos(m.To, to) {
			isLegal = true
			break
		}
	}
	if !isLegal {
		return false
	}
	vm.executeMove(rules.Move{From: from, To: to})
	return true
}

// LockInput 输入锁（AI/LLM 思考期间锁定，board_vm.dart:220-221）。
func (vm *GameVm) LockInput() { vm.inputLocked = true }

// UnlockInput 解除输入锁。
func (vm *GameVm) UnlockInput() { vm.inputLocked = false }

// UndoRound 悔一整轮（人机模式：同时撤 AI 应手与玩家最近一手，board_vm.dart:230-244）。
func (vm *GameVm) UndoRound(playerSide rules.Side) {
	if vm.inputLocked || len(vm.history) == 0 {
		return
	}
	aiSide := rules.OpponentOf(playerSide)
	lastSide := func() (rules.Side, bool) {
		if len(vm.history) == 0 {
			return "", false
		}
		last := vm.history[len(vm.history)-1]
		if last.Piece == nil {
			return "", false
		}
		return last.Piece.Side, true
	}
	if side, ok := lastSide(); ok && side == aiSide {
		vm.undoOnceInternal()
	}
	if side, ok := lastSide(); ok && side == playerSide {
		vm.undoOnceInternal()
		if side, ok := lastSide(); ok && side == aiSide {
			vm.undoOnceInternal()
		}
	}
}

// Undo 悔棋一步（board_vm.dart:249-252）。
func (vm *GameVm) Undo() {
	if len(vm.history) == 0 {
		return
	}
	vm.undoOnceInternal()
}

func (vm *GameVm) undoOnceInternal() {
	if len(vm.history) == 0 {
		return
	}
	last := vm.history[len(vm.history)-1]
	vm.history = vm.history[:len(vm.history)-1]
	vm.board.UndoMove(last)
	vm.fenHistory = vm.fenHistory[:len(vm.fenHistory)-1]
	var lastMove *rules.Move
	if len(vm.history) > 0 {
		lm := vm.history[len(vm.history)-1]
		lastMove = &lm
	}
	next := vm.buildSnapshot(nil, nil, lastMove)
	next.Selected = nil
	next.LegalTargets = nil
	vm.commit(next)
	vm.fireHistoryRewound()
}

// NewGame 重置为新游戏（board_vm.dart:266-275）。
func (vm *GameVm) NewGame() {
	vm.inputLocked = false
	vm.board = rules.Initial()
	vm.startFen = vm.board.ToFen()
	vm.history = nil
	vm.fenHistory = []string{vm.board.ToFen()}
	vm.commit(vm.buildSnapshot(nil, nil, nil))
	vm.fireHistoryRewound()
}

// NewGameFromFen 以指定 FEN 开始新对局；FEN 无效回退初始局面（board_vm.dart:281-294）。
func (vm *GameVm) NewGameFromFen(fen string) {
	vm.inputLocked = false
	vm.board = safeBoardFromFen(fen)
	vm.startFen = vm.board.ToFen()
	vm.history = nil
	vm.fenHistory = []string{vm.board.ToFen()}
	vm.commit(vm.buildSnapshot(nil, nil, nil))
	vm.fireHistoryRewound()
}

// Resign 认输/判负（loser 方负）：显式终局，防软死锁（board_vm.dart:298-305）。
func (vm *GameVm) Resign(loser rules.Side) {
	vm.inputLocked = false
	r := resultOfLoser(loser)
	next := vm.snap
	next.Result = &r
	next.Selected = nil
	next.LegalTargets = nil
	vm.commit(next)
}

// AgreeDraw 判和（重复局面判和 / 双方长将不变作和，DR-018）：显式终局。
func (vm *GameVm) AgreeDraw() {
	vm.inputLocked = false
	r := ResultDraw
	next := vm.snap
	next.Result = &r
	next.Selected = nil
	next.LegalTargets = nil
	vm.commit(next)
}

// Serialize 序列化为可保存数据（board_vm.dart:308-310；moves 为裸四元组）。
// DR-008：fen 存本局起始 FEN（原版存当前局面与 restore 的起始重放语义错位，
// 导致恢复时着法被误重放/历史清空——实机缺陷 F3），restore 据此重建整局。
func (vm *GameVm) Serialize() SerializedGame {
	return SerializedGame{Fen: vm.startFen, Moves: EncodeMoveStack(vm.history)}
}
