// POC-1 棋盘 demo 页（T0'.2）：全要素展示 + 点击选中/合法目标/落子最小交互。
// 交互规格锚点 = 08 §3.3（选中→高亮→落子）；正式里程碑 BoardView 重写不继承。
package ui

import (
	"image"
	"os"
	"time"

	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// PocBoard POC-1 页面状态（主 goroutine 独占，铁律 #G3）。
type PocBoard struct {
	board    *rules.Board
	grid     rules.BoardGrid // 复制物 Board 无导出 grid 访问器，POC 以 FEN 往返同步
	lastMove *rules.Move
	selected *rules.Position
	targets  []rules.Move
	checkPos *rules.Position

	click   gesture.Click
	toggle  widget.Clickable
	coord08 bool // 坐标口径：false=ICCS（Electron 锚点），true=08 §3.2 纵线号

	// 自动演示状态机（取证专用）：以首帧为 t0，步骤按 FrameEvent 时间触发。
	demoArmed    bool
	demoStart    time.Time // zero=未锚定；首帧设为 t0
	demoApplied  bool
	demoSelected bool
}

// NewPocBoard 构造 POC-1 页（标准初始局面）。
// 环境变量（POC 取证专用，正式版无此逻辑）：
//
//	POC_COORD=cn   默认启用 08 §3.2 纵线号口径（默认 ICCS=Electron 锚点）
//	POC_AUTODEMO=1 启动后自动演示：1s 红炮二平五 → 2s 选中黑炮 → 3s 恢复初始
func NewPocBoard() *PocBoard {
	grid, err := rules.ParseBoardFen(rules.FENInitial)
	if err != nil {
		panic("POC: FENInitial 非法: " + err.Error())
	}
	p := &PocBoard{board: rules.Initial(), grid: grid}
	if os.Getenv("POC_COORD") == "cn" {
		p.coord08 = true
	}
	if os.Getenv("POC_AUTODEMO") == "1" {
		p.demoArmed = true
	}
	return p
}

// Layout 工具行 + 棋盘区。
func (p *PocBoard) Layout(gtx layout.Context) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutToolbar),
		layout.Flexed(1, p.layoutBoard),
	)
}

func (p *PocBoard) layoutToolbar(gtx layout.Context) layout.Dimensions {
	if p.toggle.Clicked(gtx) {
		p.coord08 = !p.coord08
		gtx.Execute(op.InvalidateCmd{})
	}
	title := material.H5(pocTheme, "POC-1 棋盘自绘（08 §3.2 全要素）")
	title.Color = ThemeOnSurface
	hint := material.Body2(pocTheme, "点击己方棋子选中 → 绿点/绿环为合法目标 → 点目标落子；连续点击可对弈")
	hint.Color = ThemeOnSurface
	btn := material.Button(pocTheme, &p.toggle, "切换坐标口径")
	return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(title.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(24))
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Flexed(1, hint.Layout),
			layout.Rigid(btn.Layout),
		)
	})
}

func (p *PocBoard) layoutBoard(gtx layout.Context) layout.Dimensions {
	l := ComputeBoardLayout(float32(gtx.Constraints.Max.X), float32(gtx.Constraints.Max.Y))
	p.runDemo(gtx)

	// 指针区域 = 整个棋盘组件；点击位置反算交点（08 §3.1 命中换算）。
	defer clip.Rect{Max: image.Pt(int(l.Width), int(l.Height))}.Push(gtx.Ops).Pop()
	p.click.Add(gtx.Ops)
	for {
		ev, ok := p.click.Update(gtx.Source)
		if !ok {
			break
		}
		if ev.Kind == gesture.KindClick {
			p.onBoardClick(l, float32(ev.Position.X), float32(ev.Position.Y))
		}
	}

	// 绘制次序 = Electron BoardView：底色/线路(boardArt) → 高亮层 → 棋子层。
	targets := make([]rules.Position, 0, len(p.targets))
	for _, m := range p.targets {
		targets = append(targets, m.To)
	}
	state := BoardState{
		Grid:         p.grid,
		LastMove:     p.lastMove,
		Selected:     p.selected,
		LegalTargets: targets,
		CheckKingPos: p.checkPos,
	}
	DrawBoardArt(gtx, l, p.coord08)
	DrawHighlights(gtx, l, &state)
	DrawPieces(gtx, l, state.Grid, nil)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// onBoardClick 最小交互：选中→目标落子（动画属 POC-2）。
func (p *PocBoard) onBoardClick(l BoardLayout, x, y float32) {
	col, row, ok := HitTest(l, x, y)
	if !ok {
		p.clearSelection()
		return
	}
	// 点中合法目标 → 落子（08 §3.3）
	for _, m := range p.targets {
		if m.To.Col == col && m.To.Row == row {
			p.board.ApplyMove(m)
			from := m.From
			to := m.To
			p.lastMove = &rules.Move{From: from, To: to}
			p.syncGrid()
			p.refreshCheck()
			p.clearSelection()
			return
		}
	}
	if piece := p.board.PieceAt(col, row); piece != nil &&
		p.board.IsRedTurn() == rules.IsRedSide(piece.Side) {
		// 再点选中子=取消；点己方其他子=改选（08 §3.3）
		if p.selected != nil && p.selected.Col == col && p.selected.Row == row {
			p.clearSelection()
			return
		}
		p.selected = &rules.Position{Col: col, Row: row}
		p.targets = p.board.LegalMovesFor(*p.selected)
		return
	}
	p.clearSelection()
}

func (p *PocBoard) clearSelection() {
	p.selected = nil
	p.targets = nil
}

// syncGrid 落子后同步 POC 侧网格投影（复制物 Board 无导出 grid，FEN 往返）。
func (p *PocBoard) syncGrid() {
	grid, err := rules.ParseBoardFen(p.board.ToFen())
	if err != nil {
		panic("POC: ToFen→Parse 往返失败: " + err.Error())
	}
	p.grid = grid
}

// refreshCheck 将军提示（08 §3.2 #7）：被将方帅/将位置。
func (p *PocBoard) refreshCheck() {
	side := p.board.Turn()
	if p.board.IsCheck(side) {
		p.checkPos = p.board.KingPositionOf(side)
	} else {
		p.checkPos = nil
	}
}

// runDemo 自动演示状态机（POC_AUTODEMO=1 时启用，仅用于取证截图与手测引导）。
// 时间轴：0~1s 静置；1s 红炮二平五（lastMove 两圆出现，(7,7) 十字标记露出）；
// 2s 选中黑炮(1,2)（选中圈+合法目标绿点/绿环）；3s 恢复初始局面并停表。
func (p *PocBoard) runDemo(gtx layout.Context) {
	if !p.demoArmed {
		return
	}
	if p.demoStart.IsZero() {
		p.demoStart = gtx.Now
	}
	now := gtx.Now
	elapsed := now.Sub(p.demoStart)
	switch {
	case elapsed >= 2*time.Second && !p.demoSelected:
		p.demoSelected = true
		p.selected = &rules.Position{Col: 1, Row: 2}
		p.targets = p.board.LegalMovesFor(*p.selected)
	case elapsed >= 1*time.Second && !p.demoApplied:
		p.demoApplied = true
		var move *rules.Move
		for _, m := range p.board.AllLegalMoves(rules.Red) {
			if m.From.Col == 7 && m.From.Row == 7 && m.To.Col == 4 && m.To.Row == 7 {
				move = &m
				break
			}
		}
		if move != nil {
			p.board.ApplyMove(*move)
			from, to := move.From, move.To
			p.lastMove = &rules.Move{From: from, To: to}
			p.syncGrid()
			p.refreshCheck()
		}
	}
	if elapsed >= 3*time.Second { // 停在终态（落子+选中），停止排帧——供取证截图
		p.demoArmed = false
		return
	}
	gtx.Execute(op.InvalidateCmd{})
}
