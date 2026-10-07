package state

// 残局演示播放器状态机测试（T6'.5，puzzle_vm 9 用例等价集，09 §1/06 §7；
// 逐项对齐上游 frontend/test/stores/puzzleDemo.spec.ts ①~⑪）。
// Gio 形态：定时器由 UI 驱动——spec 的 fake-timer 推进以"按间隔毫秒数累计
// 虚拟时钟 + Advance()"等价替代；间隔语义用 CurrentInterval 断言。

import (
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

const demoPuzzleFen = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

func newDemoPlayer(t *testing.T, fen string, moves ...string) *PuzzleDemoPlayer {
	t.Helper()
	p := NewPuzzleDemoPlayer()
	p.InitializePuzzle(fen, moves)
	return p
}

// ① interval = round(基准间隔 / 倍率)；自定义间隔 clamp 200–4000。
func TestDemoIntervalOf(t *testing.T) {
	if got := DemoIntervalOf(0.5, 800); got != 1600 {
		t.Fatalf("慢速 = %d", got)
	}
	if got := DemoIntervalOf(1, 800); got != 800 {
		t.Fatalf("正常 = %d", got)
	}
	if got := DemoIntervalOf(2, 800); got != 400 {
		t.Fatalf("快速 = %d", got)
	}
	if DemoIntervalMinMs != 200 || DemoIntervalMaxMs != 4000 {
		t.Fatalf("clamp 边界 = %d~%d", DemoIntervalMinMs, DemoIntervalMaxMs)
	}
}

// ② 初始化：idle、局面为残局初始局面、无 lastMove。
func TestDemoInit(t *testing.T) {
	p := newDemoPlayer(t, demoPuzzleFen, "h2e2", "h9g7", "e3e4")
	s := p.Snapshot()
	if s.Status != DemoIdle || s.Fen != demoPuzzleFen || s.LastMove != nil ||
		s.CurrentMoveIndex != -1 || s.CurrentSide != rules.Red {
		t.Fatalf("初始化快照 = %+v", s)
	}
}

// ③ 播放推进：走法应用并高亮 lastMove；播完进入 completed（停在末着）。
func TestDemoPlayThrough(t *testing.T) {
	p := newDemoPlayer(t, demoPuzzleFen, "h2e2", "h9g7", "e3e4")
	p.StartDemo()
	if p.Snapshot().Status != DemoPlaying {
		t.Fatal("前置：应处于 playing")
	}
	p.Advance() // 第 1 步 h2e2（红炮平中）
	s := p.Snapshot()
	if s.CurrentMoveIndex != 0 || s.LastMove == nil {
		t.Fatalf("第 1 步快照 = %+v", s)
	}
	if s.LastMove.From.Col != 7 || s.LastMove.From.Row != 7 || s.LastMove.To.Col != 4 {
		t.Fatalf("lastMove = %+v", s.LastMove)
	}
	if s.Fen == demoPuzzleFen {
		t.Fatal("FEN 应已变化")
	}
	p.Advance()
	p.Advance() // 剩余步走完
	p.Advance() // 完成心跳 → completed
	s = p.Snapshot()
	if s.Status != DemoCompleted || s.CurrentMoveIndex != 2 {
		t.Fatalf("播完快照 = %+v", s)
	}
}

// ④ 慢速档位按倍率放慢：间隔换算语义（1600ms/步）。
func TestDemoSlowInterval(t *testing.T) {
	p := newDemoPlayer(t, demoPuzzleFen, "h2e2")
	p.SetSpeedMultiplier(DemoSlow)
	if p.CurrentInterval() != 1600 {
		t.Fatalf("慢速间隔 = %d", p.CurrentInterval())
	}
	p.StartDemo()
	if p.Advance() && p.Snapshot().CurrentMoveIndex != 0 {
		t.Fatal("第一次 Advance 即应走第 1 步（节拍由 UI 按 1600ms 驱动）")
	}
}

// ⑤ 自定义间隔（滑块）按设定毫秒数走子；倍率回 1x。
func TestDemoCustomInterval(t *testing.T) {
	p := newDemoPlayer(t, demoPuzzleFen, "h2e2")
	p.SetCustomInterval(3000)
	p.StartDemo()
	if p.CurrentInterval() != 3000 || p.Snapshot().Params.MoveInterval != 3000 {
		t.Fatalf("自定义间隔 = %d", p.CurrentInterval())
	}
	if p.Snapshot().Params.SpeedMultiplier != DemoNormal {
		t.Fatal("自定义间隔应覆盖倍率回 1x")
	}
}

// ⑥ 重新初始化保留用户已设速度（点播放不重置速度）。
func TestDemoInitKeepsSpeed(t *testing.T) {
	p := newDemoPlayer(t, demoPuzzleFen, "h2e2")
	p.SetCustomInterval(4000)
	p.InitializePuzzle(demoPuzzleFen, []string{"h2e2", "h9g7", "e3e4"})
	p.StartDemo()
	if p.Snapshot().Params.MoveInterval != 4000 {
		t.Fatalf("速度应保留 = %d", p.Snapshot().Params.MoveInterval)
	}
	if p.CurrentInterval() != 4000 {
		t.Fatalf("生效间隔 = %d", p.CurrentInterval())
	}
}

// ⑦ 停止演示：棋盘重置到初始局面（idle、清 lastMove）。
func TestDemoStopResets(t *testing.T) {
	p := newDemoPlayer(t, demoPuzzleFen, "h2e2", "h9g7")
	p.StartDemo()
	p.Advance()
	p.StopDemo()
	s := p.Snapshot()
	if s.Status != DemoIdle || s.CurrentMoveIndex != -1 || s.LastMove != nil || s.Fen != demoPuzzleFen {
		t.Fatalf("停止快照 = %+v", s)
	}
}

// ⑧ 暂停/续播：暂停不再推进，续播从当前进度继续。
func TestDemoPauseResume(t *testing.T) {
	p := newDemoPlayer(t, demoPuzzleFen, "h2e2", "h9g7", "e3e4")
	p.StartDemo()
	p.Advance() // 1 步
	p.PauseDemo()
	if p.Snapshot().Status != DemoPaused {
		t.Fatal("前置：应处于 paused")
	}
	if p.Advance() {
		t.Fatal("暂停期间 Advance 不应走子")
	}
	if p.Snapshot().CurrentMoveIndex != 0 {
		t.Fatalf("暂停期间 index = %d", p.Snapshot().CurrentMoveIndex)
	}
	p.ResumeDemo()
	if p.Snapshot().Status != DemoPlaying {
		t.Fatal("续播应回到 playing")
	}
	p.Advance()
	if p.Snapshot().CurrentMoveIndex != 1 {
		t.Fatalf("续播应从当前进度继续: %d", p.Snapshot().CurrentMoveIndex)
	}
}

// ⑨ 循环播放：播完自动重置棋盘从头重播。
func TestDemoLoop(t *testing.T) {
	p := newDemoPlayer(t, demoPuzzleFen, "h2e2", "h9g7", "e3e4")
	p.SetDemoLoop(true)
	p.StartDemo()
	p.Advance()
	p.Advance()
	p.Advance()
	if p.Snapshot().CurrentMoveIndex != 2 {
		t.Fatalf("前置：应停在末着 = %d", p.Snapshot().CurrentMoveIndex)
	}
	p.Advance() // 完成心跳 → 循环重置
	s := p.Snapshot()
	if s.Status != DemoPlaying || s.CurrentMoveIndex != -1 || s.Fen != demoPuzzleFen {
		t.Fatalf("循环重置快照 = %+v", s)
	}
	p.Advance()
	if p.Snapshot().CurrentMoveIndex != 0 {
		t.Fatalf("第二轮第 1 步 = %d", p.Snapshot().CurrentMoveIndex)
	}
}

// ⑩ 失步跳过：坐标非法/起点无子的步被跳过不崩溃（防错 #8）。
func TestDemoSkipInvalidMoves(t *testing.T) {
	p := newDemoPlayer(t, "4k4/9/9/9/9/9/9/9/9/4K4 w - - 0 1", "a0a9", "e9e8", "e0e1")
	p.StartDemo()
	p.Advance() // a0a9 起点 (0,9) 无子 → 跳过
	p.Advance() // e9e8 应用黑将
	p.Advance() // e0e1 应用红帅
	s := p.Snapshot()
	if s.LastMove == nil || s.CurrentMoveIndex != 2 || s.Status != DemoPlaying {
		t.Fatalf("失步跳过快照 = %+v", s)
	}
	p.Advance() // 完成心跳
	if p.Snapshot().Status != DemoCompleted {
		t.Fatalf("应 completed = %+v", p.Snapshot().Status)
	}
}

// ⑪ 初始化失败（坏 FEN）→ error 态且不可播放。
func TestDemoBadFenError(t *testing.T) {
	p := NewPuzzleDemoPlayer()
	p.InitializePuzzle("not-a-fen", nil)
	s := p.Snapshot()
	if s.Status != DemoError || !strings.Contains(s.Error, "初始化残局失败") {
		t.Fatalf("error 快照 = %+v", s)
	}
	p.StartDemo()
	if p.Snapshot().Status != DemoError {
		t.Fatal("error 态不可播放")
	}
}
