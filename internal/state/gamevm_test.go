package state

// GameVm fenHistory 维护翻译测试（DR-G002 翻译纪律：用例名保留上游对应关系）。
// 翻译源 = 上游 frontend/test/stores/gameVmFenHistory.spec.ts（T3.8，DR-018）。
// 上游经 createGameStore 驱动（订阅层等价性由 gameStore 翻译用例覆盖）；
// Go 侧快照读取直接经 vm.Current()（同一份 commit 快照，语义等价）。

import (
	"reflect"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

const fenStart = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

// playCannonMid 走一步炮二平五（(1,7)→(4,7)），返回走后快照（上游 helper 同名同义）。
func playCannonMid(t *testing.T, vm *GameVm) GameSnapshot {
	t.Helper()
	vm.OnTap(1, 7)
	vm.OnTap(4, 7)
	return vm.Current()
}

// 上游 spec: gameVmFenHistory.spec.ts > "初始快照 fenHistory = [初始局面 FEN]"
func TestGameVmFenHistory_InitialSnapshot(t *testing.T) {
	vm := NewGameVm("")
	state := vm.Current()
	if !reflect.DeepEqual(state.FenHistory, []string{fenStart}) {
		t.Fatalf("fenHistory = %v, want [fenStart]", state.FenHistory)
	}
}

// 上游 spec: gameVmFenHistory.spec.ts > "落子 push / 悔棋 pop，与 moveHistory 一一对应"
func TestGameVmFenHistory_ExecuteMovePushUndoPop(t *testing.T) {
	vm := NewGameVm("")
	after := playCannonMid(t, vm)
	if len(after.MoveHistory) != 1 {
		t.Fatalf("moveHistory.length = %d, want 1", len(after.MoveHistory))
	}
	if len(after.FenHistory) != 2 {
		t.Fatalf("fenHistory.length = %d, want 2", len(after.FenHistory))
	}
	if after.FenHistory[1] != after.Fen {
		t.Fatalf("fenHistory[1] = %q, want 当前局面 FEN %q", after.FenHistory[1], after.Fen)
	}
	// 落子后 FEN 应等于规则层重放结果。
	replay, err := rules.FromFen(fenStart)
	if err != nil {
		t.Fatalf("前置 FEN 解析失败: %v", err)
	}
	replay.ApplyMove(rules.Move{From: rules.Pos(1, 7), To: rules.Pos(4, 7)})
	if after.FenHistory[1] != replay.ToFen() {
		t.Fatalf("fenHistory[1] = %q, want 重放结果 %q", after.FenHistory[1], replay.ToFen())
	}
	// 悔棋后回到初始。
	vm.Undo()
	undone := vm.Current()
	if len(undone.MoveHistory) != 0 {
		t.Fatalf("悔棋后 moveHistory.length = %d, want 0", len(undone.MoveHistory))
	}
	if !reflect.DeepEqual(undone.FenHistory, []string{fenStart}) {
		t.Fatalf("悔棋后 fenHistory = %v, want [fenStart]", undone.FenHistory)
	}
}

// 上游 spec: gameVmFenHistory.spec.ts >
// "restore 重放逐手采集 fenHistory（DR-008：存档含起始 FEN，恢复即重建整局）"
func TestGameVmFenHistory_RestoreReplayCollects(t *testing.T) {
	vm := NewGameVm("")
	playCannonMid(t, vm)
	saved := vm.Serialize()
	// serialize 存本局起始 FEN + 完整着法栈（DR-008）：restore 以起始 FEN 建盘重放，
	// 历史与 fenHistory 完整重建。原版存终局 FEN 致重放全部跳过（历史清空）且起点
	// 恰有子的着法被误重放改写盘面（实机缺陷 F3），Go 版修复并留档。
	if saved.Fen != fenStart {
		t.Fatalf("serialize.fen = %q, want 起始 FEN", saved.Fen)
	}
	restored := NewGameVm("")
	restored.Restore(saved.Fen, saved.Moves)
	if got := restored.Current(); len(got.MoveHistory) != 1 {
		t.Fatalf("restore 后 moveHistory.length = %d, want 1", len(got.MoveHistory))
	} else if len(got.FenHistory) != 2 {
		t.Fatalf("restore 后 fenHistory.length = %d, want 2", len(got.FenHistory))
	} else if got.Fen != vm.Current().Fen {
		t.Fatalf("restore 后 fen = %q, want 原局终态 %q", got.Fen, vm.Current().Fen)
	}
	// 重放路径与直接对局等价：起始 FEN + 有效四元组。
	replayed := NewGameVm("")
	replayed.Restore(fenStart, [][]int{{1, 7, 4, 7}})
	state := replayed.Current()
	if len(state.MoveHistory) != 1 {
		t.Fatalf("重放 moveHistory.length = %d, want 1", len(state.MoveHistory))
	}
	if len(state.FenHistory) != 2 {
		t.Fatalf("重放 fenHistory.length = %d, want 2", len(state.FenHistory))
	}
	if state.FenHistory[1] != state.Fen {
		t.Fatalf("重放 fenHistory[1] = %q, want 当前局面 FEN %q", state.FenHistory[1], state.Fen)
	}
}

// 上游 spec: gameVmFenHistory.spec.ts > "restore 跳过脏记录时 fenHistory 保持与有效走法一致"
func TestGameVmFenHistory_RestoreSkipsDirtyRecords(t *testing.T) {
	vm := NewGameVm("")
	// 脏记录：越界坐标 (99,99)→(0,0) 应被跳过。
	vm.Restore(fenStart, [][]int{{99, 99, 0, 0}, {1, 7, 4, 7}})
	state := vm.Current()
	if len(state.MoveHistory) != 1 {
		t.Fatalf("moveHistory.length = %d, want 1（脏记录跳过）", len(state.MoveHistory))
	}
	if len(state.FenHistory) != 2 {
		t.Fatalf("fenHistory.length = %d, want 2", len(state.FenHistory))
	}
}

// 上游 spec: gameVmFenHistory.spec.ts > "newGame 重置 fenHistory"
func TestGameVmFenHistory_NewGameResets(t *testing.T) {
	vm := NewGameVm("")
	playCannonMid(t, vm)
	vm.NewGame()
	if !reflect.DeepEqual(vm.Current().FenHistory, []string{fenStart}) {
		t.Fatalf("newGame 后 fenHistory = %v, want [fenStart]", vm.Current().FenHistory)
	}
}

// 上游 spec: gameVmFenHistory.spec.ts > "newGameFromFen 以来源 FEN 为 fenHistory 起点"
func TestGameVmFenHistory_NewGameFromFenSeeds(t *testing.T) {
	vm := NewGameVm("")
	fen := "4k4/9/9/9/9/9/4C4/9/4C4/4K4 w - - 0 1"
	vm.NewGameFromFen(fen)
	if !reflect.DeepEqual(vm.Current().FenHistory, []string{fen}) {
		t.Fatalf("newGameFromFen 后 fenHistory = %v, want [来源 FEN]", vm.Current().FenHistory)
	}
}

// 上游 spec: gameVmFenHistory.spec.ts >
// "agreeDraw 写入 result=draw 并终局（DR-018）；终局后 playMove 拒绝"
func TestGameVmFenHistory_AgreeDrawAndFinishedGuard(t *testing.T) {
	vm := NewGameVm("")
	playCannonMid(t, vm)
	vm.AgreeDraw()
	state := vm.Current()
	if state.Result == nil || *state.Result != ResultDraw {
		t.Fatalf("result = %v, want draw", state.Result)
	}
	if !vm.IsFinished() {
		t.Fatal("agreeDraw 后应为终局")
	}
	// 终局后走子被拒（不改状态）。
	if vm.PlayMove(rules.Pos(1, 7), rules.Pos(4, 7)) {
		t.Fatal("终局后 playMove 应返回 false")
	}
	if vm.Current().Result == nil || *vm.Current().Result != ResultDraw {
		t.Fatalf("终局后 result 被改动 = %v, want 仍为 draw", vm.Current().Result)
	}
}

// 裁决触发内联收口点（design_docs/07 §3，Gio 实现映射；无上游 spec——07 §3 裁决）：
// executeMove push 侧 / restore 重放完成侧 → OnHistoryGrow（完整 fenHistory）；
// 悔棋（pop）与新局（newGame/newGameFromFen）→ OnHistoryRewound（重置 declined
// 标志与未决和棋框）。等价上游 React useEffect 依赖 moveHistory.length（仅增长时裁决）。
func TestGameVmVerdictHookInlineCollectionPoints(t *testing.T) {
	t.Run("executeMove push 侧触发 OnHistoryGrow", func(t *testing.T) {
		vm := NewGameVm("")
		var grown [][]string
		vm.OnHistoryGrow = func(fens []string) { grown = append(grown, append([]string(nil), fens...)) }
		playCannonMid(t, vm)
		if len(grown) != 1 || len(grown[0]) != 2 || grown[0][1] != vm.Current().Fen {
			t.Fatalf("OnHistoryGrow 触发 %v, want 一次且携带完整 fenHistory", grown)
		}
	})
	t.Run("restore 重放完成侧触发 OnHistoryGrow（跳脏记录不单独触发）", func(t *testing.T) {
		vm := NewGameVm("")
		var grown [][]string
		vm.OnHistoryGrow = func(fens []string) { grown = append(grown, append([]string(nil), fens...)) }
		vm.Restore(fenStart, [][]int{{99, 99, 0, 0}, {1, 7, 4, 7}})
		if len(grown) != 1 {
			t.Fatalf("OnHistoryGrow 触发 %d 次, want 重放完成一次", len(grown))
		}
		if len(grown[0]) != 2 {
			t.Fatalf("重放完成携带 fenHistory 长度 = %d, want 2（脏记录不 push）", len(grown[0]))
		}
	})
	t.Run("undo pop 触发 OnHistoryRewound 且不触发 OnHistoryGrow", func(t *testing.T) {
		vm := NewGameVm("")
		growCount, rewindCount := 0, 0
		vm.OnHistoryGrow = func([]string) { growCount++ }
		vm.OnHistoryRewound = func() { rewindCount++ }
		playCannonMid(t, vm)
		vm.Undo()
		if growCount != 1 || rewindCount != 1 {
			t.Fatalf("grow=%d rewind=%d, want grow=1 rewind=1", growCount, rewindCount)
		}
	})
	t.Run("undoRound 每手 pop 各触发一次 OnHistoryRewound", func(t *testing.T) {
		vm := NewGameVm("")
		vm.NewGameFromFen("4k4/9/9/9/9/9/4C4/9/4C4/4K4 w - - 0 1") // 对齐上游用例的双炮残局盘面
		rewindCount := 0
		vm.OnHistoryRewound = func() { rewindCount++ }
		if !vm.PlayMove(rules.Pos(4, 9), rules.Pos(5, 9)) || !vm.PlayMove(rules.Pos(4, 0), rules.Pos(3, 0)) {
			t.Fatal("前置走子应成功")
		}
		vm.UndoRound(rules.Black)
		if rewindCount != 2 {
			t.Fatalf("undoRound（撤两手）rewind 触发 = %d, want 2", rewindCount)
		}
	})
	t.Run("newGame/newGameFromFen 触发 OnHistoryRewound", func(t *testing.T) {
		vm := NewGameVm("")
		rewindCount := 0
		vm.OnHistoryRewound = func() { rewindCount++ }
		playCannonMid(t, vm)
		vm.NewGame()
		vm.NewGameFromFen("4k4/9/9/9/9/9/4C4/9/4C4/4K4 w - - 0 1")
		if rewindCount != 2 {
			t.Fatalf("newGame/newGameFromFen rewind 触发 = %d, want 2", rewindCount)
		}
	})
}
