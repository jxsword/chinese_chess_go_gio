// BoardView 棋盘组件正式版（T2'.3，design_docs/08 §3/§4）：
// 布局口径 cell=min(w/9.6,h/10.6)（08 §3.1）+ 点击命中换算 + 选中/合法目标高亮
// （§3.3：点目标→飞行动画→动画结束才真正落子）+ 220ms 两阶段飞行动画
// （§4 帧时间戳权威结束）。
//
// 竞态防护逐条对齐上游 BoardView.tsx（防错 #1/#2，08 §11）：
//  1. 动画期间忽略新点击（吞掉，BoardView.anim.active 守卫）；
//  2. 飞行中棋子层隐藏起点格、被吃子保持可见（局面未应用，FEN 不变）；
//  3. 动画结束才调 vm.OnTap(to) 落子（帧时间戳 t≥1 权威，§4）；
//  4. 仅当 moveHistory 确实增长才触发 OnMoved（动画窗口内被清选/终局时
//     OnTap 未产生走子，不得触发 AI 应手——M3' 人机页接线点）。
//
// 悔棋/新局/离页发生在动画中：页面调 CancelAnim 立即作废（不落子，#4）。
package ui

import (
	"image"
	"time"

	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// moveAnimDuration 走子动画标准时长（08 §4：220ms；上游"220ms 定时器权威"语义）。
const moveAnimDuration = 220 * time.Millisecond

// easeOutCubic 缓动（08 §4：easeOutCubic）。
func easeOutCubic(t float32) float32 {
	if t >= 1 {
		return 1
	}
	if t < 0 {
		t = 0
	}
	u := 1 - t
	return 1 - u*u*u
}

// animProgress 帧时间戳权威判定：t=(now-start)/dur；t≥1 即权威结束
// （等价上游"220ms 定时器权威、transitionend 不可靠"语义，08 §4）。
func animProgress(start, now time.Time, dur time.Duration) (t float32, done bool) {
	el := now.Sub(start)
	if el >= dur {
		return 1, true
	}
	return float32(el) / float32(dur), false
}

// BoardView 棋盘组件（主 goroutine 独占，铁律 #G3）。
type BoardView struct {
	store *state.GameStore
	click gesture.Click

	// Blocked 页面注入的禁手谓词（弹窗打开等页面级守卫；nil = 不禁）。
	// 终局/输入锁守卫由 vm.OnTap 内部承担（防错 #6）。
	Blocked func() bool

	// OnMoved 走子完成回调（仅历史增长时触发；M3' 人机页挂 AI 应手）。
	OnMoved func()

	anim struct {
		active        bool
		move          rules.Move
		piece         *rules.Piece
		start         time.Time
		historyBefore int // 动画起点历史长度（OnMoved 增长守卫，board_widget.dart:96）
	}
}

// NewBoardView 创建棋盘组件（绑定对局 store）。
func NewBoardView(store *state.GameStore) *BoardView {
	return &BoardView{store: store}
}

// CancelAnim 作废进行中的动画（不落子）——悔棋/新局/离页时调用（08 §4，#4）。
func (b *BoardView) CancelAnim() { b.anim.active = false }

// Animating 动画是否进行中（页面禁手判定用，#1）。
func (b *BoardView) Animating() bool { return b.anim.active }

// Layout 一帧：处理点击 → 推进动画 → 绘制（静态层/高亮层/棋子层/飞行层）。
func (b *BoardView) Layout(gtx layout.Context) layout.Dimensions {
	l := ComputeBoardLayout(float32(gtx.Constraints.Max.X), float32(gtx.Constraints.Max.Y))

	// 指针区域 = 整个棋盘组件；点击位置反算交点（08 §3.1 命中换算）。
	defer clip.Rect{Max: image.Pt(int(l.Width), int(l.Height))}.Push(gtx.Ops).Pop()
	b.click.Add(gtx.Ops)
	for {
		ev, ok := b.click.Update(gtx.Source)
		if !ok {
			break
		}
		if ev.Kind != gesture.KindClick {
			continue
		}
		// ① 动画期间忽略新点击（board_widget.dart:42，防错 #1）
		if b.anim.active || (b.Blocked != nil && b.Blocked()) {
			continue
		}
		col, row, ok := HitTest(l, float32(ev.Position.X), float32(ev.Position.Y))
		if !ok {
			continue
		}
		b.handleClick(col, row)
	}

	b.runAnim(gtx)
	b.draw(gtx, l)

	// 动画进行中 → 持续排帧（帧循环，08 §4）。
	if b.anim.active {
		gtx.Execute(op.InvalidateCmd{})
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// handleClick 点击处理（BoardView.tsx handleClick 映照）：
// 选中 + 合法目标 → 先动画后落子；其余交由 vm.OnTap（选中/换选/取消）。
func (b *BoardView) handleClick(col, row int) {
	snap := b.store.State()
	hit := rules.Pos(col, row)
	if snap.Selected != nil && containsPos(snap.LegalTargets, hit) {
		// ② 合法目标：先动画后落子
		mover := b.store.VM.Board().PieceAtP(*snap.Selected)
		if mover != nil {
			b.startAnimation(mover, *snap.Selected, hit)
			return
		}
		// 防御：选中棋子意外丢失时退化为普通点击（上游同款）
		b.store.VM.OnTap(col, row)
		return
	}
	b.store.VM.OnTap(col, row)
}

// startAnimation 发起飞行动画（08 §4：记录 anim{from,to,start,active} + 排帧）。
func (b *BoardView) startAnimation(mover *rules.Piece, from, to rules.Position) {
	b.anim.active = true
	b.anim.move = rules.Move{From: from, To: to}
	b.anim.piece = mover
	b.anim.start = time.Now()
	// 历史长度在动画起点捕获（board_widget.dart:96）
	b.anim.historyBefore = len(b.store.State().MoveHistory)
}

// runAnim 08 §4 帧循环状态机。
func (b *BoardView) runAnim(gtx layout.Context) {
	if !b.anim.active {
		return
	}
	_, done := animProgress(b.anim.start, gtx.Now, moveAnimDuration)
	if !done {
		return // t<1：下一帧继续
	}
	// 权威结束（帧时间戳 t≥1）：此时才真正落子（board_widget.dart:99，防错 #2）。
	to := b.anim.move.To
	b.anim.active = false
	b.anim.piece = nil
	b.store.VM.OnTap(to.Col, to.Row)
	// 仅当历史确实增长才通知（board_widget.dart:104，防重复触发 AI）
	if len(b.store.State().MoveHistory) > b.anim.historyBefore && b.OnMoved != nil {
		b.OnMoved()
	}
}

// draw 三层绘制（boardArt.tsx 自底向上）+ 飞行棋子层。
func (b *BoardView) draw(gtx layout.Context, l BoardLayout) {
	snap := b.store.State()
	grid, err := rules.ParseBoardFen(snap.Fen)
	if err != nil {
		return // FEN 异常帧：跳过绘制（vm 侧保证 FEN 合法，防御性）
	}
	DrawBoardArt(gtx, l, true) // 纵线号口径（08 §3.2 #4，D-003）

	skipFrom := (*rules.Position)(nil)
	if b.anim.active {
		skipFrom = &b.anim.move.From // 飞行棋子 layer 接管（board_painter.dart:266-270）
	}
	// 高亮层随快照实时绘制（动画期间选中/目标高亮保持显示，上游同款）
	state := BoardState{
		Grid:         grid,
		LastMove:     snap.LastMove,
		Selected:     snap.Selected,
		LegalTargets: snap.LegalTargets,
		CheckKingPos: checkKingPos(grid, snap),
	}
	DrawHighlights(gtx, l, &state)
	DrawPieces(gtx, l, grid, skipFrom)

	// 飞行棋子层（动画期间渲染，忽略指针）：easeOutCubic 插值。
	if b.anim.active && b.anim.piece != nil {
		t, _ := animProgress(b.anim.start, gtx.Now, moveAnimDuration)
		e := easeOutCubic(t)
		x1, y1 := OffsetOf(l, b.anim.move.From.Col, b.anim.move.From.Row)
		x2, y2 := OffsetOf(l, b.anim.move.To.Col, b.anim.move.To.Row)
		DrawPiece(gtx, l, b.anim.piece, x1+(x2-x1)*e, y1+(y2-y1)*e)
	}
}

// checkKingPos 将军提示（08 §3.2 #7）：被将方（轮走方）帅/将位置。
func checkKingPos(grid rules.BoardGrid, snap state.GameSnapshot) *rules.Position {
	if !snap.IsCheck || snap.Result != nil {
		return nil
	}
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			if p := grid[r][c]; p != nil && p.Kind == rules.King && rules.IsRedSide(p.Side) == snap.IsRedTurn {
				pos := rules.Pos(c, r)
				return &pos
			}
		}
	}
	return nil
}

func containsPos(ps []rules.Position, p rules.Position) bool {
	for _, q := range ps {
		if q == p {
			return true
		}
	}
	return false
}
