// POC-1 棋盘自绘（T0'.2）：Electron 版 boardArt.tsx 逐元素映照——
// 外框/横线/纵线(河界断开)/九宫斜线/楚河汉界/兵炮位标记/坐标标注/棋子/高亮层。
// 绘制次序 = boardArt.tsx 自底向上。正式里程碑 BoardView 重写，本文件不继承。
package ui

import (
	"image"
	"image/color"
	"math"

	"gioui.org/f32"
	"gioui.org/font"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	fixed "golang.org/x/image/math/fixed"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// PocBoardState 棋盘绘制入参（镜像 Electron BoardView 的层参数）。
type PocBoardState struct {
	Grid            rules.BoardGrid
	LastMove        *rules.Move     // 最近着法两圆（--cc-last-move，棋子层之下）
	Selected        *rules.Position // 选中圈（--cc-selected）
	LegalTargets    []rules.Move    // 合法目标：空位=圆点/有子=外环（--cc-legal-hint）
	CheckKingPos    *rules.Position // 将军提示（08 §3.2 #7；Electron 无锚点，POC 以红环呈现）
	ShowFileNumbers bool            // false=ICCS 坐标（Electron 锚点）；true=08 §3.2 纵线号
}

// 兵/炮位十字角标位置（boardArt.tsx CROSS_MARKS，board_painter.dart:111-116）。
var pocCrossMarks = [][2]int{
	{1, 2}, {7, 2},
	{0, 3}, {2, 3}, {4, 3}, {6, 3}, {8, 3},
	{0, 6}, {2, 6}, {4, 6}, {6, 6}, {8, 6},
	{1, 7}, {7, 7},
}

// pocFiles ICCS 列标注 a-i（boardArt.tsx FILES）。
var pocFiles = [9]byte{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i'}

// pocFileNumbers 08 §3.2 纵线号：红方底部汉字一~九（右→左）。
var pocFileNumbers = [9]string{"一", "二", "三", "四", "五", "六", "七", "八", "九"}

// ---- 基础绘制助手（POC 各 demo 共用） ----

// pocFillRect 填充矩形。
func pocFillRect(ops *op.Ops, r image.Rectangle, c color.NRGBA) {
	paint.FillShape(ops, c, clip.Rect(r).Op())
}

// pocStrokeLine 线段（clip.Stroke）。
func pocStrokeLine(ops *op.Ops, x1, y1, x2, y2, width float32, c color.NRGBA) {
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(x1, y1))
	p.LineTo(f32.Pt(x2, y2))
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

// pocFillCircle 填充圆。
func pocFillCircle(ops *op.Ops, cx, cy, r float32, c color.NRGBA) {
	paint.FillShape(ops, c, clip.Ellipse(image.Rect(
		int(cx-r), int(cy-r), int(cx+r), int(cy+r))).Op(ops))
}

// pocStrokeCircle 圆环描边（圆路径用三次贝塞尔逼近，同 clip.Ellipse 实现）。
func pocStrokeCircle(ops *op.Ops, cx, cy, r, width float32, c color.NRGBA) {
	var p clip.Path
	p.Begin(ops)
	const q = 4 * (math.Sqrt2 - 1) / 3
	curve := r * q
	p.MoveTo(f32.Pt(cx, cy-r))
	p.CubeTo(f32.Pt(cx+curve, cy-r), f32.Pt(cx+r, cy-curve), f32.Pt(cx+r, cy))
	p.CubeTo(f32.Pt(cx+r, cy+curve), f32.Pt(cx+curve, cy+r), f32.Pt(cx, cy+r))
	p.CubeTo(f32.Pt(cx-curve, cy+r), f32.Pt(cx-r, cy+curve), f32.Pt(cx-r, cy))
	p.CubeTo(f32.Pt(cx-r, cy-curve), f32.Pt(cx-curve, cy-r), f32.Pt(cx, cy-r))
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

// pocText 居中文字（等价 SVG text-anchor:middle + dominant-baseline:central）。
// 直接走 typesetting 整形 + 字形路径填充，绕开 label 布局包装，位置完全由
// (cx,cy) 决定（POC-1 实测 material.Label 宏重放定位不稳，见回填记录）。
func pocText(gtx layout.Context, str string, cx, cy float32, px float32, c color.NRGBA, f font.Font) {
	ppem := fixed.Int26_6(px * 64)
	shaper := pocTheme.Shaper
	shaper.LayoutString(text.Parameters{
		Font:     f,
		PxPerEm:  ppem,
		MaxLines: 1,
		MaxWidth: math.MaxInt32,
	}, str)
	var glyphs []text.Glyph
	var ascent, descent fixed.Int26_6
	for {
		g, ok := shaper.NextGlyph()
		if !ok {
			break
		}
		ascent, descent = g.Ascent, g.Descent
		glyphs = append(glyphs, g)
	}
	if len(glyphs) == 0 {
		return
	}
	last := glyphs[len(glyphs)-1]
	width := last.X + last.Advance - glyphs[0].X
	// 基线 y：使字形盒在 cy 垂直居中（dominant-baseline:central 的近似）。
	baseY := cy + fixedToFloat(ascent-descent)/2
	x0 := cx - fixedToFloat(width)/2
	spec := shaper.Shape(glyphs)
	ops := gtx.Ops
	tr := op.Offset(image.Pt(int(x0), int(baseY))).Push(ops)
	paint.FillShape(ops, c, clip.Outline{Path: spec}.Op())
	tr.Pop()
}

// fixedToFloat 26.6 定点 → 浮点。
func fixedToFloat(v fixed.Int26_6) float32 {
	return float32(v) / 64
}

// ---- boardArt 映照 ----

// PocDrawBoardArt 静态层：底色+外框+线路+河界+标记+坐标（boardArt.tsx BoardArt）。
// showFileNumbers=false → ICCS 坐标（Electron 锚点）；true → 08 §3.2 纵线号。
func PocDrawBoardArt(gtx layout.Context, l PocBoardLayout, showFileNumbers bool) {
	ops := gtx.Ops
	lineW := float32(gtx.Dp(2))  // boardArt strokeWidth=2（CSS px→dp 同标度）
	frameW := float32(gtx.Dp(4)) // 外框 strokeWidth=4

	// 1. 底色（BoardViewStatic:43 --cc-board-bg）
	pocFillRect(ops, image.Rect(0, 0, int(l.Width), int(l.Height)), ThemeBoardBg)

	at := func(c, r int) (float32, float32) { return PocOffsetOf(l, c, r) }
	riverY := l.OriginY + 4.5*l.Cell
	centerX := l.OriginX + 4*l.Cell
	gap := l.Cell * pocCoordGap

	// 2. 外框（boardArt.tsx:31-39）
	fx, fy := at(0, 0)
	pocStrokeRect(ops, fx-l.BorderMargin, fy-l.BorderMargin,
		pocBoardCols*l.Cell+2*l.BorderMargin, pocBoardRows*l.Cell+2*l.BorderMargin, frameW, ThemeBoardLine)

	// 3. 横线 10 条
	for r := 0; r <= 9; r++ {
		x1, y1 := at(0, r)
		x2, _ := at(8, r)
		pocStrokeLine(ops, x1, y1, x2, y1, lineW, ThemeBoardLine)
	}
	// 4. 竖线 9 条：中间 7 条被楚河汉界打断
	for c := 0; c <= 8; c++ {
		if c == 0 || c == 8 {
			x, y1 := at(c, 0)
			_, y2 := at(c, 9)
			pocStrokeLine(ops, x, y1, x, y2, lineW, ThemeBoardLine)
			continue
		}
		x, t1 := at(c, 0)
		_, t2 := at(c, 4)
		_, b1 := at(c, 5)
		_, b2 := at(c, 9)
		pocStrokeLine(ops, x, t1, x, t2, lineW, ThemeBoardLine)
		pocStrokeLine(ops, x, b1, x, b2, lineW, ThemeBoardLine)
	}
	// 5. 九宫斜线 ×4
	for _, d := range [4][4]int{{3, 0, 5, 2}, {5, 0, 3, 2}, {3, 7, 5, 9}, {5, 7, 3, 9}} {
		x1, y1 := at(d[0], d[1])
		x2, y2 := at(d[2], d[3])
		pocStrokeLine(ops, x1, y1, x2, y2, lineW, ThemeBoardLine)
	}
	// 6. 楚河汉界（boardArt.tsx:71-76："楚 河" / "漢 界"，字号 cell*0.55）
	riverPx := l.Cell * pocRiverFontSizeR
	pocText(gtx, "楚 河", centerX-l.Cell*2, riverY, riverPx, ThemeRiverText, pocFontMedium)
	pocText(gtx, "漢 界", centerX+l.Cell*2, riverY, riverPx, ThemeRiverText, pocFontMedium)
	// 7. 兵/炮位十字角标（boardArt.tsx:78-96）
	for _, m := range pocCrossMarks {
		cx, cy := at(m[0], m[1])
		radius := l.Cell * pocCrossRadiusR
		length := radius * 1.4
		for _, d := range [4][2]int{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
			bx := cx + float32(d[0])*radius
			by := cy + float32(d[1])*radius
			pocStrokeLine(ops, bx, by, bx, by+float32(d[1])*length, lineW, ThemeBoardLine)
			pocStrokeLine(ops, bx, by, bx+float32(d[0])*length, by, lineW, ThemeBoardLine)
		}
	}
	// 8. 坐标标注（boardArt.tsx:97-118 / 08 §3.2 #4 双口径切换）
	coordPx := l.Cell * pocCoordFontSizeR
	topY := fy - gap
	bottomY := fy + pocBoardRows*l.Cell + gap
	for c := 0; c <= 8; c++ {
		x, _ := at(c, 0)
		if showFileNumbers {
			// 08 §3.2：黑方顶部数字 1~9（左→右）、红方底部汉字一~九（右→左）
			pocText(gtx, string(rune('1'+c)), x, topY, coordPx, ThemeRiverText, pocFontMedium)
			pocText(gtx, pocFileNumbers[8-c], x, bottomY, coordPx, ThemeRiverText, pocFontMedium)
		} else {
			// Electron 锚点：ICCS files a-i 上下
			pocText(gtx, string(pocFiles[c]), x, topY, coordPx, ThemeRiverText, pocFontMedium)
			pocText(gtx, string(pocFiles[c]), x, bottomY, coordPx, ThemeRiverText, pocFontMedium)
		}
	}
	for row := 0; row <= 9; row++ {
		rank := 9 - row // 0=红方底线（board_painter.dart:129-172）
		_, y := at(0, row)
		pocText(gtx, string(rune('0'+rank)), fx-gap, y, coordPx, ThemeRiverText, pocFontMedium)
		pocText(gtx, string(rune('0'+rank)), fx+pocBoardCols*l.Cell+gap, y, coordPx, ThemeRiverText, pocFontMedium)
	}
}

// pocStrokeRect 矩形描边。
func pocStrokeRect(ops *op.Ops, x, y, w, h, width float32, c color.NRGBA) {
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(x, y))
	p.LineTo(f32.Pt(x+w, y))
	p.LineTo(f32.Pt(x+w, y+h))
	p.LineTo(f32.Pt(x, y+h))
	p.Close()
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

// PocDrawHighlights 高亮层（棋子层之下，boardArt.tsx HighlightsLayer）：
// lastMove 两圆 + 选中圈 + 合法目标（空位圆点/吃子外环）+ 将军提示环。
func PocDrawHighlights(gtx layout.Context, l PocBoardLayout, st *PocBoardState) {
	ops := gtx.Ops
	if st.LastMove != nil {
		for _, p := range []rules.Position{st.LastMove.From, st.LastMove.To} {
			cx, cy := PocOffsetOf(l, p.Col, p.Row)
			pocFillCircle(ops, cx, cy, l.PieceRadius, ThemeLastMove)
		}
	}
	if st.Selected != nil {
		cx, cy := PocOffsetOf(l, st.Selected.Col, st.Selected.Row)
		pocFillCircle(ops, cx, cy, l.PieceRadius, ThemeSelected)
	}
	for _, m := range st.LegalTargets {
		cx, cy := PocOffsetOf(l, m.To.Col, m.To.Row)
		if st.Grid[m.To.Row][m.To.Col] != nil {
			pocStrokeCircle(ops, cx, cy, l.PieceRadius+l.Cell*pocHintRingExtraR,
				l.Cell*pocHintRingWidthR, ThemeLegalHint)
		} else {
			pocFillCircle(ops, cx, cy, l.Cell*pocHintDotR, ThemeLegalHint)
		}
	}
	if st.CheckKingPos != nil {
		cx, cy := PocOffsetOf(l, st.CheckKingPos.Col, st.CheckKingPos.Row)
		pocStrokeCircle(ops, cx, cy, l.PieceRadius+l.Cell*pocHintRingExtraR,
			l.Cell*pocHintRingWidthR, ThemeCheckWarn)
	}
}

// PocDrawPiece 单枚棋子（boardArt.tsx PieceFigure：阴影/盘面/描边/文字）。
func PocDrawPiece(gtx layout.Context, l PocBoardLayout, p *rules.Piece, cx, cy float32) {
	ops := gtx.Ops
	r := l.PieceRadius
	sideCol := ThemePieceRed
	if !rules.IsRedSide(p.Side) {
		sideCol = ThemePieceBlack
	}
	// 阴影（cy+radius*0.08, r*1.02, rgba(0,0,0,.25)）
	pocFillCircle(ops, cx, cy+r*0.08, r*1.02, color.NRGBA{A: 0x40})
	// 盘面
	pocFillCircle(ops, cx, cy, r, ThemePieceFace)
	// 内环描边（r*0.92，宽 r*0.1）
	pocStrokeCircle(ops, cx, cy, r*0.92, r*0.1, sideCol)
	// 汉字（radius*1.05，bold，衬线）
	pocText(gtx, rules.PieceLabel(p), cx, cy, r*pocPieceFontSizeR, sideCol, pocFontSerifBold)
}

// PocDrawPieces 棋子层（boardArt.tsx PiecesLayer；skipFrom 非空时跳过该格——
// 动画飞行层接管，POC-2 复用）。
func PocDrawPieces(gtx layout.Context, l PocBoardLayout, grid rules.BoardGrid, skipFrom *rules.Position) {
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			p := grid[r][c]
			if p == nil {
				continue
			}
			if skipFrom != nil && skipFrom.Col == c && skipFrom.Row == r {
				continue
			}
			x, y := PocOffsetOf(l, c, r)
			PocDrawPiece(gtx, l, p, x, y)
		}
	}
}
