// POC-2 220ms 两阶段飞行动画帧循环（T0'.3，design_docs/08 §4）：
// 发起→每帧按 now 重算 easeOutCubic 插值并排帧→帧时间戳 t≥1 权威结束→
// 才真正落子（被吃子随之消失）；动画窗口吞掉棋盘点击（防错 #1）。
// 正式里程碑 BoardView 重写，本文件不继承。
package ui

import (
	"image"
	"log"
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

// PocAnim POC-2 页面状态（主 goroutine 独占，铁律 #G3）。
type PocAnim struct {
	board *rules.Board
	grid  rules.BoardGrid

	anim struct {
		active   bool
		move     rules.Move
		start    time.Time
		captured *rules.Piece // 飞行中不消失的被吃子
	}
	duration time.Duration

	scenarios []rules.Move // 循环演示着法（合法着法，来自 rules 白名单）
	nextIdx   int
	landUntil time.Time // 落定后的停顿点

	frames    int
	fpsMark   time.Time
	fps       int
	swallowed int           // 动画窗口内被吞掉的点击数（防错 #1 计数）
	lastEnd   time.Duration // 权威结束实测时长（回填数据）

	click     gesture.Click
	slowBtn   widget.Clickable
	normBtn   widget.Clickable
	replayBtn widget.Clickable
}

// NewPocAnim 构造 POC-2 页：初始局面 + 三条循环演示着法
// （炮二平五/车吃卒/马二进三——覆盖空位落点、吃子落点、马腿校验）。
// POC_ANIM_SLOW=1 以 1s 慢速启动（观察两阶段与吞输入）。
func NewPocAnim() *PocAnim {
	grid, err := rules.ParseBoardFen(rules.FENInitial)
	if err != nil {
		panic("POC: FENInitial 非法: " + err.Error())
	}
	p := &PocAnim{
		board:    rules.Initial(),
		grid:     grid,
		duration: moveAnimDuration,
		scenarios: []rules.Move{
			{From: rules.Pos(7, 7), To: rules.Pos(4, 7)}, // 红：炮二平五（空位落点）
			{From: rules.Pos(7, 0), To: rules.Pos(6, 2)}, // 黑：马8进7（空位落点）
			{From: rules.Pos(4, 7), To: rules.Pos(4, 3)}, // 红：炮五进四 隔屏吃卒（阶段二被吃子消失）
		},
	}
	if os.Getenv("POC_ANIM_SLOW") == "1" {
		p.duration = time.Second
	}
	return p
}

// Layout 工具行（FPS/吞输入计数/时长切换/重放）+ 棋盘。
func (p *PocAnim) Layout(gtx layout.Context) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutToolbar),
		layout.Flexed(1, p.layoutBoard),
	)
}

func (p *PocAnim) layoutToolbar(gtx layout.Context) layout.Dimensions {
	for i, b := range []*widget.Clickable{&p.normBtn, &p.slowBtn} {
		if b.Clicked(gtx) {
			if i == 0 {
				p.duration = moveAnimDuration
			} else {
				p.duration = time.Second
			}
			p.nextIdx = 0
			p.resetBoard()
		}
	}
	if p.replayBtn.Clicked(gtx) {
		p.nextIdx = 0
		p.resetBoard()
	}
	title := material.H5(pocTheme, "POC-2 220ms 两阶段飞行动画")
	title.Color = ThemeOnSurface
	fps := material.Body2(pocTheme, "FPS: ")
	fps.Color = ThemeOnSurface
	stats := material.Body2(pocTheme, " 吞输入: ")
	stats.Color = ThemeOnSurface
	slow := material.Button(pocTheme, &p.slowBtn, "1000ms 慢速")
	norm := material.Button(pocTheme, &p.normBtn, "220ms 标准")
	replay := material.Button(pocTheme, &p.replayBtn, "重放")
	return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(title.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(16))
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Rigid(fps.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(pocTheme, itoa(p.fps))
				l.Color = ThemeOnSurface
				return l.Layout(gtx)
			}),
			layout.Rigid(stats.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(pocTheme, itoa(p.swallowed))
				l.Color = ThemeOnSurface
				return l.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Dimensions{}
			}),
			layout.Rigid(slow.Layout),
			layout.Rigid(norm.Layout),
			layout.Rigid(replay.Layout),
		)
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func (p *PocAnim) layoutBoard(gtx layout.Context) layout.Dimensions {
	l := ComputeBoardLayout(float32(gtx.Constraints.Max.X), float32(gtx.Constraints.Max.Y))

	// FPS 统计（每帧 +1，1s 窗口）。
	p.frames++
	if p.fpsMark.IsZero() {
		p.fpsMark = gtx.Now
	}
	if el := gtx.Now.Sub(p.fpsMark); el >= time.Second {
		p.fps = p.frames
		p.frames = 0
		p.fpsMark = gtx.Now
	}

	// 点击：动画窗口内一律吞掉（防错 #1），仅计数。
	defer clip.Rect{Max: image.Pt(int(l.Width), int(l.Height))}.Push(gtx.Ops).Pop()
	p.click.Add(gtx.Ops)
	for {
		ev, ok := p.click.Update(gtx.Source)
		if !ok {
			break
		}
		if ev.Kind == gesture.KindClick && p.anim.active {
			p.swallowed++
		}
	}

	// 帧循环推进动画（08 §4 状态机）。
	p.runAnim(gtx)

	// 绘制：静态层 → 高亮（lastMove）→ 棋子层（飞行中跳过起点格）→ 飞行棋子。
	skipFrom := (*rules.Position)(nil)
	if p.anim.active {
		skipFrom = &p.anim.move.From
	}
	lastMove := (*rules.Move)(nil)
	if !p.anim.active && p.nextIdx > 0 {
		lastMove = &p.scenarios[(p.nextIdx-1+len(p.scenarios))%len(p.scenarios)]
	}
	DrawBoardArt(gtx, l, false)
	DrawHighlights(gtx, l, &BoardState{Grid: p.grid, LastMove: lastMove})
	DrawPieces(gtx, l, p.grid, skipFrom)
	if p.anim.active {
		t, _ := animProgress(p.anim.start, gtx.Now, p.duration)
		e := easeOutCubic(t)
		x1, y1 := OffsetOf(l, p.anim.move.From.Col, p.anim.move.From.Row)
		x2, y2 := OffsetOf(l, p.anim.move.To.Col, p.anim.move.To.Row)
		piece := p.board.PieceAtP(p.anim.move.From)
		if piece != nil {
			DrawPiece(gtx, l, piece,
				x1+(x2-x1)*e, y1+(y2-y1)*e)
		}
		// 阶段二语义可视化：飞行中被吃子保持可见（到达 t≥1 才消失）。
		if p.anim.captured != nil {
			cx, cy := OffsetOf(l, p.anim.move.To.Col, p.anim.move.To.Row)
			DrawPiece(gtx, l, p.anim.captured, cx, cy)
		}
	}

	// 动画进行中或处于落定停顿期 → 持续排帧。
	if p.anim.active || !p.landUntil.IsZero() {
		gtx.Execute(op.InvalidateCmd{})
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// runAnim 08 §4 帧循环状态机。
func (p *PocAnim) runAnim(gtx layout.Context) {
	now := gtx.Now
	if !p.anim.active {
		// 落定停顿期 → 到点后发起下一循环。
		if !p.landUntil.IsZero() {
			if now.Before(p.landUntil) {
				return
			}
			p.landUntil = time.Time{}
		}
		p.startScenario(now)
		return
	}
	_, done := animProgress(p.anim.start, now, p.duration)
	if !done {
		return // t<1：下一帧继续（本帧已请求排帧）
	}
	// 权威结束（帧时间戳 t≥1）：此时才真正落子（防错 #2），被吃子消失。
	measured := now.Sub(p.anim.start)
	p.lastEnd = measured
	log.Printf("poc2: 权威结束 请求=%v 实测=%v（帧时间戳判定 t≥1）", p.duration, measured)
	p.board.ApplyMove(p.anim.move)
	p.syncGrid()
	p.anim.active = false
	p.landUntil = now.Add(900 * time.Millisecond)
}

// startScenario 发起下一个演示动画（08 §4：记录 anim{from,to,start,active} + 排帧）。
func (p *PocAnim) startScenario(now time.Time) {
	if p.nextIdx == 0 {
		p.resetBoard()
	}
	move := p.scenarios[p.nextIdx]
	p.nextIdx = (p.nextIdx + 1) % len(p.scenarios)
	// 校验场景着法在当前局面合法（演示板随循环复位，恒应合法）。
	legal := false
	for _, m := range p.board.LegalMovesFor(move.From) {
		if m.To == move.To {
			move = m
			legal = true
			break
		}
	}
	if !legal {
		log.Println("poc2: 场景着法非法，跳过")
		p.landUntil = now.Add(time.Second)
		return
	}
	p.anim.active = true
	p.anim.move = move
	p.anim.start = now
	p.anim.captured = p.board.PieceAtP(move.To)
}

func (p *PocAnim) resetBoard() {
	p.board = rules.Initial()
	p.syncGrid()
	p.anim.active = false
	p.anim.captured = nil
}

// syncGrid 落子后同步 POC 侧网格投影（同 POC-1，FEN 往返）。
func (p *PocAnim) syncGrid() {
	grid, err := rules.ParseBoardFen(p.board.ToFen())
	if err != nil {
		panic("POC: ToFen→Parse 往返失败: " + err.Error())
	}
	p.grid = grid
}
