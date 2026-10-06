package ui

// BoardView 正式版单测（09 §4：能在逻辑层测的不进渲染层；交互/动画视觉面走
// 手测清单）。锚点 = 08 §4 帧时间戳权威结束 + CancelAnim 作废语义（#4）。

import (
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// spec: easeOutCubic——t=0 → 0；t≥1 → 1；easeOutCubic(0.5)=0.875。
func TestEaseOutCubic(t *testing.T) {
	if easeOutCubic(0) != 0 {
		t.Fatal("easeOutCubic(0) 应为 0")
	}
	if easeOutCubic(1) != 1 || easeOutCubic(1.5) != 1 {
		t.Fatal("easeOutCubic(t≥1) 应为 1")
	}
	if !almostEq(easeOutCubic(0.5), 0.875) {
		t.Fatalf("easeOutCubic(0.5)=%v want 0.875", easeOutCubic(0.5))
	}
}

// spec: 08 §4 帧时间戳权威判定——t<1 未结束；t≥1（含掉帧后大幅超时）权威结束。
func TestAnimProgressAuthoritative(t *testing.T) {
	start := time.Now()
	if _, done := animProgress(start, start.Add(100*time.Millisecond), moveAnimDuration); done {
		t.Fatal("100ms/220ms 不应结束")
	}
	if _, done := animProgress(start, start.Add(moveAnimDuration), moveAnimDuration); !done {
		t.Fatal("t≥1 应权威结束")
	}
	// 掉帧场景：一帧跨度大幅超过 220ms 仍正确结束（POC-2 回填 R-G4）
	if _, done := animProgress(start, start.Add(2*time.Second), moveAnimDuration); !done {
		t.Fatal("掉帧后应权威结束")
	}
}

// spec: 08 §4——撤销/新局发生在动画中：CancelAnim 立即作废（active=false，
// 不落子），走对应取消链（#4）。
func TestBoardViewCancelAnimAborts(t *testing.T) {
	store := state.NewGameStore(state.GameStoreConfig{Mode: state.ModeHumanVsHuman})
	bv := NewBoardView(store)
	mover := store.VM.Board().PieceAtP(rules.Pos(7, 7))
	if mover == nil {
		t.Fatal("初始局面 (7,7) 应有红炮")
	}
	bv.startAnimation(mover, rules.Pos(7, 7), rules.Pos(4, 7))
	if !bv.Animating() {
		t.Fatal("startAnimation 后应处于动画中")
	}
	bv.CancelAnim()
	if bv.Animating() {
		t.Fatal("CancelAnim 后动画应作废")
	}
	// 动画作废 = 未落子：历史不变、棋子仍在起点
	if len(store.State().MoveHistory) != 0 {
		t.Fatal("动画作废不应落子")
	}
	if store.VM.Board().PieceAtP(rules.Pos(7, 7)) == nil {
		t.Fatal("动画作废后起点棋子应保持原位")
	}
}

// T3'.2：已落盘走法的视觉飞行层——AnimateMoveVisual 置 vis（终点棋子），
// CancelAnim 一并作废；不改变状态层（历史/盘面不变）。
func TestBoardViewAnimateMoveVisual(t *testing.T) {
	store := state.NewGameStore(state.GameStoreConfig{Mode: state.ModeHumanVsAi})
	bv := NewBoardView(store)

	m := rules.Move{From: rules.Pos(7, 7), To: rules.Pos(7, 4)}
	if !store.VM.PlayMove(m.From, m.To) {
		t.Fatal("预先落盘应成功")
	}
	bv.AnimateMoveVisual(m)
	if !bv.vis.active {
		t.Fatal("应启动视觉飞行层")
	}
	if bv.vis.piece == nil || bv.vis.piece.Kind != rules.Cannon {
		t.Fatalf("飞行棋子应为终点处的炮: %+v", bv.vis.piece)
	}
	history := len(store.State().MoveHistory)
	bv.CancelAnim()
	if bv.vis.active {
		t.Fatal("CancelAnim 应作废视觉飞行层")
	}
	if len(store.State().MoveHistory) != history {
		t.Fatal("视觉飞行层不改变状态层")
	}
	// 终点无子（防御路径）：不 panic、不置位
	store2 := state.NewGameStore(state.GameStoreConfig{Mode: state.ModeHumanVsAi})
	bv2 := NewBoardView(store2)
	bv2.AnimateMoveVisual(rules.Move{From: rules.Pos(7, 7), To: rules.Pos(7, 4)})
	if bv2.vis.active {
		t.Fatal("终点无子不应启动视觉飞行层")
	}
}
