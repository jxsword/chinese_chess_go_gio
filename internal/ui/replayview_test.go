package ui

// 重放器状态机用例（T5'.3；上游 PuzzleDetailView 交互语义的状态级等价——
// 09 §4 口径：不渲染，只测状态迁移；自动播放 tick 以事件注入替代）。

import (
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

func testStrPtr(s string) *string { return &s }

func replayPuzzle(moves ...string) *state.ParsedPuzzleView {
	// 两子残局：兵九进一式（可连续走）——用单兵对空盘确保每步合法可重放。
	return &state.ParsedPuzzleView{
		ID:            "t",
		InitialFen:    "4k4/9/9/9/9/9/9/9/9/4K4 w - - 0 1",
		SolutionMoves: moves,
		Title:         testStrPtr("测试局"),
		Source:        "残局/测试",
		Format:        "xqf",
		Difficulty:    1,
		Endgame:       true,
	}
}

func newReplay(puzzle *state.ParsedPuzzleView) *ReplayView {
	r := NewReplayView(nil)
	r.SetPuzzle(puzzle)
	return r
}

func TestReplaySetPuzzleParsesMoves(t *testing.T) {
	r := newReplay(replayPuzzle("e0e1", "e9e8"))
	if len(r.moves) != 2 || r.pos != 0 || r.Playing() {
		t.Fatalf("moves=%d pos=%d playing=%v", len(r.moves), r.pos, r.Playing())
	}
	if len(r.notation) != 2 {
		t.Fatalf("记谱应 2 条，实际 %d", len(r.notation))
	}
}

// 换局停播放并归零（SetPuzzle halt 语义）。
func TestReplaySetPuzzleStopsPlayback(t *testing.T) {
	r := newReplay(replayPuzzle("e0e1", "e9e8", "e1d1"))
	r.TogglePlay()
	if !r.Playing() {
		t.Fatal("前置：应处于播放中")
	}
	r.SetPuzzle(replayPuzzle("e0e1"))
	if r.Playing() || r.Pos() != 0 {
		t.Fatalf("换局应停播放并归零：playing=%v pos=%d", r.Playing(), r.Pos())
	}
}

// 步进边界：◀/▶/⇤/⇥ 的 clamp。
func TestReplayJumpClamp(t *testing.T) {
	r := newReplay(replayPuzzle("e0e1", "e9e8"))
	r.Jump(-5)
	if r.Pos() != 0 {
		t.Fatalf("下界 clamp，实际 %d", r.Pos())
	}
	r.Jump(99)
	if r.Pos() != 2 {
		t.Fatalf("上界 clamp 到 len，实际 %d", r.Pos())
	}
	r.Jump(1)
	if r.Pos() != 1 {
		t.Fatalf("正常跳转，实际 %d", r.Pos())
	}
}

// 自动播放：tick 推进到末尾并标记完毕；迟到代次丢弃。
func TestReplayAutoPlayTicks(t *testing.T) {
	r := newReplay(replayPuzzle("e0e1", "e9e8"))
	r.TogglePlay()
	if !r.Playing() {
		t.Fatal("TogglePlay 应开始播放")
	}
	r.OnTick(ReplayTick{Gen: r.gen})
	if r.Pos() != 1 {
		t.Fatalf("tick 后应推进到 1，实际 %d", r.Pos())
	}
	// 暂停后过期代次忽略
	gen := r.gen
	r.haltPlay()
	r.OnTick(ReplayTick{Gen: gen})
	if r.Pos() != 1 {
		t.Fatalf("暂停后 tick 不应推进，实际 %d", r.Pos())
	}
	// 续播（非从头）：playing 但 pos 保留
	r.TogglePlay()
	if !r.Playing() || r.Pos() != 1 {
		t.Fatalf("续播应保留位置：playing=%v pos=%d", r.Playing(), r.Pos())
	}
	r.OnTick(ReplayTick{Gen: r.gen}) // 第 2 步（末着应用，仍 playing）
	r.OnTick(ReplayTick{Gen: r.gen}) // 完成心跳 → completed 自动停拍（spec ③）
	if r.Playing() {
		t.Fatal("到达末尾应自动停止")
	}
	r.OnTick(ReplayTick{Gen: r.gen})
	if r.Pos() != 2 {
		t.Fatalf("末尾 tick 不应越界，实际 %d", r.Pos())
	}
}

// 从头播放：completed 后 TogglePlay 归零重播（上游 idle/completed → startDemo）。
func TestReplayRestartAfterComplete(t *testing.T) {
	r := newReplay(replayPuzzle("e0e1"))
	r.TogglePlay()
	r.OnTick(ReplayTick{Gen: r.gen}) // 末着应用
	r.OnTick(ReplayTick{Gen: r.gen}) // 完成心跳 → completed
	if r.Pos() != 1 {
		t.Fatalf("前置：应播完，实际 %d", r.Pos())
	}
	r.TogglePlay() // completed → 从头
	if !r.Playing() || r.Pos() != 0 {
		t.Fatalf("完成后重播应归零：playing=%v pos=%d", r.Playing(), r.Pos())
	}
}

// 停止回开局。
func TestReplayStopResets(t *testing.T) {
	r := newReplay(replayPuzzle("e0e1", "e9e8"))
	r.Jump(1)
	r.Stop()
	if r.Playing() || r.Pos() != 0 {
		t.Fatalf("停止应回开局：playing=%v pos=%d", r.Playing(), r.Pos())
	}
}

// 进入对战起点：残局题=initialFen；全局对局=终局局面（recordBattle 语义）。
func TestReplayBattleFen(t *testing.T) {
	endgame := newReplay(replayPuzzle("e0e1"))
	if endgame.BattleFen() != "4k4/9/9/9/9/9/9/9/9/4K4 w - - 0 1" {
		t.Fatalf("残局题起点应= initialFen，实际 %s", endgame.BattleFen())
	}
	opening := &state.ParsedPuzzleView{
		InitialFen:    rules.FENInitial,
		SolutionMoves: []string{"e0e1", "e9e8"},
		Source:        "全局",
		Format:        "pgn",
	}
	game := newReplay(opening)
	if game.BattleFen() == opening.InitialFen {
		t.Fatal("全局对局起点应为终局局面（≠ initialFen）")
	}
	if game.BattleFen() != state.FinalFenOf(opening.InitialFen, game.moves) {
		t.Fatalf("全局对局起点应= FinalFenOf，实际 %s", game.BattleFen())
	}
}

// 导出投影：标题/模式/初始 FEN/走法（PGN 文本面由 state 快照覆盖）。
func TestReplayExportRecord(t *testing.T) {
	r := newReplay(replayPuzzle("e0e1"))
	rec := r.ExportRecord()
	if rec.Title != "测试局" || rec.Mode != "endgame" || len(rec.Moves) != 1 {
		t.Fatalf("导出投影不符：%+v", rec)
	}
	if state.IccsFallbackFormat(rec.Moves[0]) != "e0e1" {
		t.Fatalf("导出走法坐标不符：%s", state.IccsFallbackFormat(rec.Moves[0]))
	}
}
