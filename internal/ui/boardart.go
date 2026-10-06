// 棋盘静态绘制层（design_docs/08 §3.2 ops，T2'.3 正式版）：Electron 版
// boardArt.tsx 逐元素映照——外框/横线/纵线(河界断开)/九宫斜线/楚河汉界/
// 兵炮位标记/坐标标注/棋子/高亮层。绘制次序 = boardArt.tsx 自底向上。
// 几何与绘制公式经 M0' POC-1 与 Electron 参照逐元素对照验证（08 §12 回填），
// 本文件为正式共享绘制库（POC demo 页同源引用）。
package ui

import (
	"image"
	"image/color"

	"gioui.org/layout"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// BoardState 棋盘绘制入参（镜像 Electron BoardView 的层参数）。
type BoardState struct {
	Grid         rules.BoardGrid
	LastMove     *rules.Move      // 最近着法两圆（--cc-last-move，棋子层之下）
	Selected     *rules.Position  // 选中圈（--cc-selected）
	LegalTargets []rules.Position // 合法目标：空位=圆点/有子=外环（--cc-legal-hint）
	CheckKingPos *rules.Position  // 将军提示（08 §3.2 #7；Electron 无锚点，POC 以红环呈现）
}

// crossMarks 兵/炮位十字角标位置（boardArt.tsx CROSS_MARKS，board_painter.dart:111-116）。
var crossMarks = [][2]int{
	{1, 2}, {7, 2},
	{0, 3}, {2, 3}, {4, 3}, {6, 3}, {8, 3},
	{0, 6}, {2, 6}, {4, 6}, {6, 6}, {8, 6},
	{1, 7}, {7, 7},
}

// boardFiles ICCS 列标注 a-i（boardArt.tsx FILES）。
var boardFiles = [9]byte{'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i'}

// fileNumbersCN 纵线号：红方底部汉字一~九（右→左）（08 §3.2 #4，D-003）。
var fileNumbersCN = [9]string{"一", "二", "三", "四", "五", "六", "七", "八", "九"}

// DrawBoardArt 静态层：底色+外框+线路+河界+标记+坐标（boardArt.tsx BoardArt）。
// showFileNumbers=false → ICCS 坐标（Electron 锚点）；true → 纵线号（08 §3.2 #4，D-003）。
func DrawBoardArt(gtx layout.Context, l BoardLayout, showFileNumbers bool) {
	ops := gtx.Ops
	lineW := float32(gtx.Dp(2))  // boardArt strokeWidth=2（CSS px→dp 同标度）
	frameW := float32(gtx.Dp(4)) // 外框 strokeWidth=4

	// 1. 底色（BoardViewStatic:43 --cc-board-bg）
	fillRect(ops, image.Rect(0, 0, int(l.Width), int(l.Height)), ThemeBoardBg)

	at := func(c, r int) (float32, float32) { return OffsetOf(l, c, r) }
	riverY := l.OriginY + 4.5*l.Cell
	centerX := l.OriginX + 4*l.Cell
	gap := l.Cell * boardCoordGap

	// 2. 外框（boardArt.tsx:31-39）
	fx, fy := at(0, 0)
	strokeRect(ops, fx-l.BorderMargin, fy-l.BorderMargin,
		boardCols*l.Cell+2*l.BorderMargin, boardRows*l.Cell+2*l.BorderMargin, frameW, ThemeBoardLine)

	// 3. 横线 10 条
	for r := 0; r <= 9; r++ {
		x1, y1 := at(0, r)
		x2, _ := at(8, r)
		strokeLine(ops, x1, y1, x2, y1, lineW, ThemeBoardLine)
	}
	// 4. 竖线 9 条：中间 7 条被楚河汉界打断
	for c := 0; c <= 8; c++ {
		if c == 0 || c == 8 {
			x, y1 := at(c, 0)
			_, y2 := at(c, 9)
			strokeLine(ops, x, y1, x, y2, lineW, ThemeBoardLine)
			continue
		}
		x, t1 := at(c, 0)
		_, t2 := at(c, 4)
		_, b1 := at(c, 5)
		_, b2 := at(c, 9)
		strokeLine(ops, x, t1, x, t2, lineW, ThemeBoardLine)
		strokeLine(ops, x, b1, x, b2, lineW, ThemeBoardLine)
	}
	// 5. 九宫斜线 ×4
	for _, d := range [4][4]int{{3, 0, 5, 2}, {5, 0, 3, 2}, {3, 7, 5, 9}, {5, 7, 3, 9}} {
		x1, y1 := at(d[0], d[1])
		x2, y2 := at(d[2], d[3])
		strokeLine(ops, x1, y1, x2, y2, lineW, ThemeBoardLine)
	}
	// 6. 楚河汉界（boardArt.tsx:71-76："楚 河" / "漢 界"，字号 cell*0.55）
	riverPx := l.Cell * boardRiverFontSizeR
	drawCenteredText(gtx, "楚 河", centerX-l.Cell*2, riverY, riverPx, ThemeRiverText, fontMedium)
	drawCenteredText(gtx, "漢 界", centerX+l.Cell*2, riverY, riverPx, ThemeRiverText, fontMedium)
	// 7. 兵/炮位十字角标（boardArt.tsx:78-96）
	for _, m := range crossMarks {
		cx, cy := at(m[0], m[1])
		radius := l.Cell * boardCrossRadiusR
		length := radius * 1.4
		for _, d := range [4][2]int{{-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
			bx := cx + float32(d[0])*radius
			by := cy + float32(d[1])*radius
			strokeLine(ops, bx, by, bx, by+float32(d[1])*length, lineW, ThemeBoardLine)
			strokeLine(ops, bx, by, bx+float32(d[0])*length, by, lineW, ThemeBoardLine)
		}
	}
	// 8. 坐标标注（boardArt.tsx:97-118 / 08 §3.2 #4 双口径）
	coordPx := l.Cell * boardCoordFontSizeR
	topY := fy - gap
	bottomY := fy + boardRows*l.Cell + gap
	for c := 0; c <= 8; c++ {
		x, _ := at(c, 0)
		if showFileNumbers {
			// 纵线号（08 §3.2，D-003）：黑方顶部数字 1~9（左→右）、红方底部汉字一~九（右→左）
			drawCenteredText(gtx, string(rune('1'+c)), x, topY, coordPx, ThemeRiverText, fontMedium)
			drawCenteredText(gtx, fileNumbersCN[8-c], x, bottomY, coordPx, ThemeRiverText, fontMedium)
		} else {
			// Electron 锚点：ICCS files a-i 上下
			drawCenteredText(gtx, string(boardFiles[c]), x, topY, coordPx, ThemeRiverText, fontMedium)
			drawCenteredText(gtx, string(boardFiles[c]), x, bottomY, coordPx, ThemeRiverText, fontMedium)
		}
	}
	for row := 0; row <= 9; row++ {
		rank := 9 - row // 0=红方底线（board_painter.dart:129-172）
		_, y := at(0, row)
		drawCenteredText(gtx, string(rune('0'+rank)), fx-gap, y, coordPx, ThemeRiverText, fontMedium)
		drawCenteredText(gtx, string(rune('0'+rank)), fx+boardCols*l.Cell+gap, y, coordPx, ThemeRiverText, fontMedium)
	}
}

// DrawHighlights 高亮层（棋子层之下，boardArt.tsx HighlightsLayer）：
// lastMove 两圆 + 选中圈 + 合法目标（空位圆点/吃子外环）+ 将军提示环。
func DrawHighlights(gtx layout.Context, l BoardLayout, st *BoardState) {
	ops := gtx.Ops
	if st.LastMove != nil {
		for _, p := range []rules.Position{st.LastMove.From, st.LastMove.To} {
			cx, cy := OffsetOf(l, p.Col, p.Row)
			fillCircle(ops, cx, cy, l.PieceRadius, ThemeLastMove)
		}
	}
	if st.Selected != nil {
		cx, cy := OffsetOf(l, st.Selected.Col, st.Selected.Row)
		fillCircle(ops, cx, cy, l.PieceRadius, ThemeSelected)
	}
	for _, p := range st.LegalTargets {
		cx, cy := OffsetOf(l, p.Col, p.Row)
		if st.Grid[p.Row][p.Col] != nil {
			strokeCircle(ops, cx, cy, l.PieceRadius+l.Cell*boardHintRingExtraR,
				l.Cell*boardHintRingWidthR, ThemeLegalHint)
		} else {
			fillCircle(ops, cx, cy, l.Cell*boardHintDotR, ThemeLegalHint)
		}
	}
	if st.CheckKingPos != nil {
		cx, cy := OffsetOf(l, st.CheckKingPos.Col, st.CheckKingPos.Row)
		strokeCircle(ops, cx, cy, l.PieceRadius+l.Cell*boardHintRingExtraR,
			l.Cell*boardHintRingWidthR, ThemeCheckWarn)
	}
}

// DrawPiece 单枚棋子（boardArt.tsx PieceFigure：阴影/盘面/描边/文字）。
func DrawPiece(gtx layout.Context, l BoardLayout, p *rules.Piece, cx, cy float32) {
	ops := gtx.Ops
	r := l.PieceRadius
	sideCol := ThemePieceRed
	if !rules.IsRedSide(p.Side) {
		sideCol = ThemePieceBlack
	}
	// 阴影（cy+radius*0.08, r*1.02, rgba(0,0,0,.25)）
	fillCircle(ops, cx, cy+r*0.08, r*1.02, color.NRGBA{A: 0x40})
	// 盘面
	fillCircle(ops, cx, cy, r, ThemePieceFace)
	// 内环描边（r*0.92，宽 r*0.1）
	strokeCircle(ops, cx, cy, r*0.92, r*0.1, sideCol)
	// 汉字（radius*1.05，bold，衬线）
	drawCenteredText(gtx, rules.PieceLabel(p), cx, cy, r*boardPieceFontSizeR, sideCol, fontSerifBold)
}

// DrawPieces 棋子层（boardArt.tsx PiecesLayer；skipFrom/skipTo 非空时跳过
// 该格——动画飞行层接管，BoardView 复用：skipFrom=点击路径飞行起点、
// skipTo=已落盘走法视觉飞行终点）。
func DrawPieces(gtx layout.Context, l BoardLayout, grid rules.BoardGrid, skipFrom, skipTo *rules.Position) {
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			p := grid[r][c]
			if p == nil {
				continue
			}
			if skipFrom != nil && skipFrom.Col == c && skipFrom.Row == r {
				continue
			}
			if skipTo != nil && skipTo.Col == c && skipTo.Row == r {
				continue
			}
			x, y := OffsetOf(l, c, r)
			DrawPiece(gtx, l, p, x, y)
		}
	}
}
