// POC 棋盘几何（T0'.2，逐行映照 Electron 版 boardLayout.ts —— 正式里程碑
// 由 internal/ui BoardView 重写，本文件不继承）。
//
// 尺寸推导（board_layout.dart:13-45）：画布四周留白比例 0.8（外框外扩 0.5
// + 边框线宽与坐标余量 0.3），故 cell = min(w/9.6, h/10.6)（08 §3.1）。
// 注：08 文档曾写作 min(w/8.6, h/9.6) 系笔误，以对齐 board_layout.dart
// 的 9.6/10.6 为准（Electron boardLayout.ts 同注）。
package ui

import "math"

const (
	pocPieceRatio     = 0.86 // 棋子直径/格边长（constants.dart:15）
	pocBorderMargin   = 0.5  // 外框外扩（cell 倍数）
	pocCanvasPadding  = pocBorderMargin + 0.3
	pocBoardCols      = 8.0 // col∈[0,8]
	pocBoardRows      = 9.0 // row∈[0,9]
	pocCoordGap       = 0.65
	pocPieceFontSizeR = 1.05 // 棋子字号 = radius × 1.05
	pocRiverFontSizeR = 0.55 // 楚河汉界字号 = cell × 0.55
	pocCoordFontSizeR = 0.28 // 坐标字号 = cell × 0.28
	pocCrossRadiusR   = 0.08 // 兵炮位标记半径 = cell × 0.08
	pocHintDotR       = 0.16 // 空位目标点 = cell × 0.16
	pocHintRingExtraR = 0.05 // 吃子外环半径增量 = cell × 0.05
	pocHintRingWidthR = 0.08 // 吃子外环线宽 = cell × 0.08
)

// PocBoardLayout 棋盘几何：绘制/命中/动画共用（Electron boardLayout.ts 同构）。
type PocBoardLayout struct {
	Cell         float32
	OriginX      float32
	OriginY      float32
	Width        float32
	Height       float32
	PieceRadius  float32
	BorderMargin float32
}

// PocComputeBoardLayout 由组件尺寸推导布局（boardLayout.ts computeBoardLayout）。
// 入参为组件约束的像素尺寸（Gio 约束=物理像素，与 Electron CSS px×dpr 等价）。
func PocComputeBoardLayout(width, height float32) PocBoardLayout {
	byWidth := width / (pocBoardCols + 2*pocCanvasPadding)
	byHeight := height / (pocBoardRows + 2*pocCanvasPadding)
	cell := math.Min(float64(byWidth), float64(byHeight))
	return PocBoardLayout{
		Cell:         float32(cell),
		OriginX:      (width - pocBoardCols*float32(cell)) / 2,
		OriginY:      (height - pocBoardRows*float32(cell)) / 2,
		Width:        width,
		Height:       height,
		PieceRadius:  float32(cell * pocPieceRatio / 2),
		BorderMargin: float32(cell * pocBorderMargin),
	}
}

// PocOffsetOf 网格交点 (col,row) 的组件坐标（boardLayout.ts offsetOf）。
func PocOffsetOf(l PocBoardLayout, col, row int) (float32, float32) {
	return l.OriginX + float32(col)*l.Cell, l.OriginY + float32(row)*l.Cell
}

// PocHitTest 组件坐标 → 最近交点；越界返回 ok=false（boardLayout.ts hitTest，
// 命中阈值=四舍五入取最近交点，08 §3.1）。
func PocHitTest(l PocBoardLayout, x, y float32) (col, row int, ok bool) {
	col = int(math.Round(float64((x - l.OriginX) / l.Cell)))
	row = int(math.Round(float64((y - l.OriginY) / l.Cell)))
	if col < 0 || col > 8 || row < 0 || row > 9 {
		return 0, 0, false
	}
	return col, row, true
}
