package state

// GameStore 工厂翻译测试（DR-G002 翻译纪律：用例名保留上游对应关系）。
// 翻译源 = 上游 frontend/test/stores/gameStore.spec.ts。
// 等价集：test/features/board/viewmodel/board_vm_test.dart（12 例，09 §1 映射 board_vm 12）；
// 通过 NewGameStore 工厂驱动（同时覆盖订阅桥接层）；每例独立实例（铁律 #G4）。

import (
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

const fenDoubleCannon = "4k4/9/9/9/9/9/4C4/9/4C4/4K4 w - - 0 1"

func newTestStore(mode GameMode) *GameStore {
	return NewGameStore(GameStoreConfig{Mode: mode})
}

// ---- BoardViewModel 等价集（board_vm_test.dart） ----

// 上游 spec: gameStore.spec.ts > "初始局面：红方先行，无选中，无走法历史"
func TestGameStore_InitialState(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	state := store.State()
	if !state.IsRedTurn {
		t.Fatal("初始应红方先行")
	}
	if state.Selected != nil {
		t.Fatal("初始应无选中")
	}
	if len(state.LegalTargets) != 0 {
		t.Fatal("初始应无合法目标")
	}
	if len(state.MoveHistory) != 0 {
		t.Fatal("初始应无走法历史")
	}
	if state.Result != nil {
		t.Fatal("初始 result 应为 null")
	}
}

// 上游 spec: gameStore.spec.ts > "点击红车（col=0,row=9）能选中并产生合法走法"
func TestGameStore_TapRedRookSelects(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	store.VM.OnTap(0, 9)
	state := store.State()
	if state.Selected == nil {
		t.Fatal("应产生选中")
	}
	if len(state.LegalTargets) == 0 {
		t.Fatal("合法走法应大于 0")
	}
}

// 上游 spec: gameStore.spec.ts > "走子后切换轮走方，记录走法历史"
func TestGameStore_MoveSwitchesTurn(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	vm := store.VM
	vm.OnTap(0, 9)
	vm.OnTap(0, 8)
	state := store.State()
	if state.IsRedTurn {
		t.Fatal("走子后应轮黑方")
	}
	if len(state.MoveHistory) != 1 {
		t.Fatalf("moveHistory.length = %d, want 1", len(state.MoveHistory))
	}
	if state.LastMove == nil {
		t.Fatal("lastMove 应非空")
	}
}

// 上游 spec: gameStore.spec.ts > "悔棋后走法历史减少，轮走方回退"
func TestGameStore_UndoRestoresTurn(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	vm := store.VM
	vm.OnTap(0, 9)
	vm.OnTap(0, 8)
	vm.Undo()
	state := store.State()
	if len(state.MoveHistory) != 0 {
		t.Fatalf("悔棋后 moveHistory = %d, want 0", len(state.MoveHistory))
	}
	if !state.IsRedTurn {
		t.Fatal("悔棋后应回红方")
	}
}

// 上游 spec: gameStore.spec.ts > "新游戏后恢复初始局面"
func TestGameStore_NewGameResets(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	vm := store.VM
	vm.OnTap(0, 9)
	vm.OnTap(0, 8)
	vm.NewGame()
	state := store.State()
	if len(state.MoveHistory) != 0 {
		t.Fatalf("newGame 后 moveHistory = %d, want 0", len(state.MoveHistory))
	}
	if !state.IsRedTurn {
		t.Fatal("newGame 后应红方先行")
	}
}

// 上游 spec: gameStore.spec.ts > "newGameFromFen：以残局 FEN 开局，轮走方随 FEN"
func TestGameStore_NewGameFromFenTurnFollows(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	vm := store.VM
	vm.NewGameFromFen(fenDoubleCannon)
	state := store.State()
	if state.Fen != fenDoubleCannon {
		t.Fatalf("fen = %q, want 来源 FEN", state.Fen)
	}
	if !state.IsRedTurn {
		t.Fatal("w FEN 应红方先行")
	}
	if len(state.MoveHistory) != 0 {
		t.Fatal("moveHistory 应为空")
	}
	if state.Result != nil {
		t.Fatal("result 应为 null")
	}
}

// 上游 spec: gameStore.spec.ts > "newGameFromFen：黑先 FEN 时轮走方为黑"
func TestGameStore_NewGameFromFenBlackTurn(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	store.VM.NewGameFromFen("4k4/9/9/9/9/9/4C4/9/4C4/4K4 b - - 0 1")
	if store.State().IsRedTurn {
		t.Fatal("b FEN 应黑方先行")
	}
}

// 上游 spec: gameStore.spec.ts > "newGameFromFen：无效 FEN 回退标准初始局面"
func TestGameStore_NewGameFromFenInvalidFallsBack(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	store.VM.NewGameFromFen("不是 FEN 的字符串")
	state := store.State()
	if state.Fen != fenStart {
		t.Fatalf("fen = %q, want 标准初始局面", state.Fen)
	}
	if !state.IsRedTurn {
		t.Fatal("应红方先行")
	}
}

// 上游 spec: gameStore.spec.ts > "newGameFromFen 后走子正常：红炮进一更新局面"
func TestGameStore_PlayMoveAfterNewGameFromFen(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	vm := store.VM
	vm.NewGameFromFen(fenDoubleCannon)
	applied := vm.PlayMove(rules.Pos(4, 6), rules.Pos(4, 5))
	if !applied {
		t.Fatal("红炮进一应合法")
	}
	if store.State().IsRedTurn {
		t.Fatal("走子后应轮黑方")
	}
	if len(store.State().MoveHistory) != 1 {
		t.Fatalf("moveHistory = %d, want 1", len(store.State().MoveHistory))
	}
}

// 上游 spec: gameStore.spec.ts > "undoRound：黑方玩家语义（先撤红方 AI 一手，再撤黑方玩家一手）"
func TestGameStore_UndoRoundBlackPlayer(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	vm := store.VM
	// 红先残局：AI(红)先行 1 着，黑方玩家应手 1 着
	vm.NewGameFromFen(fenDoubleCannon)
	if !vm.PlayMove(rules.Pos(4, 9), rules.Pos(5, 9)) {
		t.Fatal("红帅 e0→f0 应合法")
	}
	if !vm.PlayMove(rules.Pos(4, 0), rules.Pos(3, 0)) {
		t.Fatal("黑将 e9→d9 应合法（离开红炮纵线）")
	}
	if len(store.State().MoveHistory) != 2 {
		t.Fatalf("前置 moveHistory = %d, want 2", len(store.State().MoveHistory))
	}
	vm.UndoRound(rules.Black)
	// 一轮 = 撤黑方玩家 + 撤红方 AI 各一手
	if len(store.State().MoveHistory) != 0 {
		t.Fatalf("undoRound 后 moveHistory = %d, want 0", len(store.State().MoveHistory))
	}
	if !store.State().IsRedTurn {
		t.Fatal("应回红方先行")
	}
}

// 上游 spec: gameStore.spec.ts > "困毙判负：黑方无子可动且未被将军 → 红方胜（非和棋）"
func TestGameStore_StalemateBlackLoses(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	store.VM.NewGameFromFen("4k4/3P1P3/4P4/9/9/9/9/9/9/3K5 b - - 0 1")
	state := store.State()
	if state.Result == nil || *state.Result != ResultRedWins {
		t.Fatalf("result = %v, want redWins（困毙方判负）", state.Result)
	}
}

// 上游 spec: gameStore.spec.ts > "困毙判负：红方无子可动且未被将军 → 黑方胜"
func TestGameStore_StalemateRedLoses(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	store.VM.NewGameFromFen("4k4/9/9/9/9/9/9/4p4/3p1p3/4K4 w - - 0 1")
	state := store.State()
	if state.Result == nil || *state.Result != ResultBlackWins {
		t.Fatalf("result = %v, want blackWins（困毙方判负）", state.Result)
	}
}

// ---- GameVm 扩展（输入锁/强校验/序列化/恢复，board_vm.dart:140,220 等锚点） ----

// 上游 spec: gameStore.spec.ts > "playMove 非法走子返回 false 且状态不变"
func TestGameStore_PlayMoveRejectsIllegal(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	vm := store.VM
	// 起点无子
	if vm.PlayMove(rules.Pos(4, 4), rules.Pos(4, 5)) {
		t.Fatal("起点无子应返回 false")
	}
	// 起点有子但非本方
	if vm.PlayMove(rules.Pos(4, 0), rules.Pos(4, 1)) {
		t.Fatal("非本方棋子应返回 false")
	}
	// 目标不合法（红帅不能出九宫/一步跨多格）
	if vm.PlayMove(rules.Pos(4, 9), rules.Pos(4, 6)) {
		t.Fatal("红帅跨多格应返回 false")
	}
	if len(store.State().MoveHistory) != 0 {
		t.Fatal("状态不应改变")
	}
	if store.State().Fen != fenStart {
		t.Fatalf("fen 被改动 = %q, want 起始 FEN", store.State().Fen)
	}
}

// 上游 spec: gameStore.spec.ts > "输入锁：锁定期间 onTap 无效；newGame/resign 解锁（防错 #2 VM 半边）"
func TestGameStore_InputLockAndResignUnlocks(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	vm := store.VM
	vm.LockInput()
	vm.OnTap(0, 9)
	if store.State().Selected != nil {
		t.Fatal("输入锁期间 onTap 应无效")
	}
	vm.UnlockInput()
	vm.OnTap(0, 9)
	if store.State().Selected == nil {
		t.Fatal("解锁后应可选中")
	}

	// resign 解锁（终局后本就无法点击，锁不应残留）
	store2 := newTestStore(ModeHumanVsLlm)
	store2.VM.LockInput()
	store2.VM.Resign(rules.Black)
	state2 := store2.State()
	if state2.Result == nil || *state2.Result != ResultRedWins {
		t.Fatalf("resign(black) result = %v, want redWins", state2.Result)
	}
	if !store2.VM.IsFinished() {
		t.Fatal("resign 后应终局")
	}
	// 终局后 onTap/playMove 忽略（防错 #3 的 VM 半边）
	store2.VM.OnTap(0, 9)
	if store2.State().Selected != nil {
		t.Fatal("终局后 onTap 应忽略")
	}
	if store2.VM.PlayMove(rules.Pos(0, 9), rules.Pos(0, 8)) {
		t.Fatal("终局后 playMove 应拒绝")
	}
}

// 上游 spec: gameStore.spec.ts > "undoRound 在输入锁期间不动作（对齐 board_vm.dart:231）"
func TestGameStore_UndoRoundNoopWhileLocked(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	vm := store.VM
	if !vm.PlayMove(rules.Pos(0, 9), rules.Pos(0, 8)) {
		t.Fatal("前置走子应成功")
	}
	vm.LockInput()
	vm.UndoRound(rules.Red)
	if len(store.State().MoveHistory) != 1 {
		t.Fatalf("输入锁期间 undoRound 应不动作，moveHistory = %d", len(store.State().MoveHistory))
	}
	vm.UnlockInput()
	vm.UndoRound(rules.Red)
	// 一轮：撤红方玩家一手；若之前还有黑方一手一并撤（此处仅 1 着）
	if len(store.State().MoveHistory) != 0 {
		t.Fatalf("解锁后 undoRound 后 moveHistory = %d, want 0", len(store.State().MoveHistory))
	}
}

// 上游 spec: gameStore.spec.ts > "serialize 返回本局起始 FEN（DR-008）与裸四元组"
func TestGameStore_SerializeStartFen(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	vm := store.VM
	vm.OnTap(0, 9)
	vm.OnTap(0, 8)
	data := vm.Serialize()
	want := [][]int{{0, 9, 0, 8}}
	if len(data.Moves) != 1 || data.Moves[0][0] != 0 || data.Moves[0][1] != 9 || data.Moves[0][2] != 0 || data.Moves[0][3] != 8 {
		t.Fatalf("moves = %v, want %v", data.Moves, want)
	}
	// DR-008：fen 为本局起始 FEN（restore 据此重放重建整局）；当前局面在快照上
	if data.Fen == store.State().Fen {
		t.Fatal("serialize.fen 不应为当前局面（DR-008 起始 FEN）")
	}
	if data.Fen != "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1" {
		t.Fatalf("serialize.fen = %q, want 起始 FEN", data.Fen)
	}
	if !strings.HasSuffix(store.State().Fen, " b - - 0 1") {
		t.Fatalf("当前局面轮走方应为 b，got %q", store.State().Fen)
	}
}

// 上游 spec: gameStore.spec.ts > "restore 重放四元组并跳过脏记录（长度≠4/越界/源格无子）"
func TestGameStore_RestoreSkipsDirty(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	vm := store.VM
	vm.Restore(fenStart, [][]int{
		{7, 7, 4, 7},  // 炮二平五
		{1},           // 长度≠4 → 跳过
		{4, 4, 4, 5},  // 源格无子 → 跳过
		{0, -1, 0, 8}, // 越界 → 跳过
		{7, 0, 6, 2},  // 马8进7
	})
	state := store.State()
	if len(state.MoveHistory) != 2 {
		t.Fatalf("moveHistory = %d, want 2", len(state.MoveHistory))
	}
	if !state.IsRedTurn {
		t.Fatal("两着后应回红方")
	}
	if state.LastMove == nil {
		t.Fatal("lastMove 应非空")
	}
	if state.Selected != nil {
		t.Fatal("selected 应为 null")
	}
	// 重放后的 FEN 与真实走两步一致
	reference := newTestStore(ModeHumanVsHuman)
	reference.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7))
	reference.VM.PlayMove(rules.Pos(7, 0), rules.Pos(6, 2))
	if state.Fen != reference.State().Fen {
		t.Fatalf("重放 FEN = %q, want %q", state.Fen, reference.State().Fen)
	}
}

// 上游 spec: gameStore.spec.ts >
// "restore 后已分胜负可直接从快照读出（供恢复流程死局清理，game_restore.dart:32）"
func TestGameStore_RestoreExposesResult(t *testing.T) {
	store := newTestStore(ModeHumanVsAi)
	store.VM.Restore("4k4/3P1P3/4P4/9/9/9/9/9/9/3K5 b", [][]int{})
	state := store.State()
	if state.Result == nil || *state.Result != ResultRedWins {
		t.Fatalf("result = %v, want redWins", state.Result)
	}
}

// 上游 spec: gameStore.spec.ts > "每局一实例：两个 store 互不影响（铁律 #6）"
func TestGameStore_InstanceIsolation(t *testing.T) {
	a := newTestStore(ModeHumanVsHuman)
	b := newTestStore(ModeHumanVsHuman)
	a.VM.OnTap(0, 9)
	if a.State().Selected == nil {
		t.Fatal("a 应有选中")
	}
	if b.State().Selected != nil {
		t.Fatal("b 不应受 a 影响（铁律 #G4）")
	}
}

// 订阅桥接：VM 快照每次变化整体替换到 store（上游 vm.subscribe → store.setState）。
func TestGameStore_SubscriptionBridgesSnapshot(t *testing.T) {
	store := newTestStore(ModeHumanVsHuman)
	seen := make([]int, 0, 2)
	unsub := store.VM.Subscribe(func() {
		seen = append(seen, len(store.State().MoveHistory))
	})
	store.VM.OnTap(0, 9) // select → 通知（0 手快照）
	store.VM.OnTap(0, 8) // executeMove → 通知（1 手快照）
	unsub()
	store.VM.Undo()
	if len(seen) != 2 || seen[0] != 0 || seen[1] != 1 {
		t.Fatalf("订阅通知序列 = %v, want [0 1]（反注册后不再通知）", seen)
	}
	if len(store.State().MoveHistory) != 0 {
		t.Fatalf("反注册后 undo 仍应生效（订阅桥不受影响），moveHistory = %d", len(store.State().MoveHistory))
	}
}
