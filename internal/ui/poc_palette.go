// POC 专用色板与颜色助手：色值锚点 = Electron 版 src/renderer/app/global.css
// （design_docs/08 §1；正式里程碑锁定进 internal/ui/theme.go，本文件随 POC 删除）。
package ui

import "image/color"

// rgb 构造不透明色。
func rgb(v uint32) color.NRGBA {
	return color.NRGBA{
		R: uint8(v >> 16),
		G: uint8(v >> 8),
		B: uint8(v),
		A: 0xff,
	}
}

// rgba 构造带透明度（0~1）的色。
func rgba(v uint32, alpha float64) color.NRGBA {
	c := rgb(v)
	c.A = uint8(alpha*255 + 0.5)
	return c
}

// Electron 版棋盘色板（global.css :root）。
var (
	PocBackground = rgb(0xf6f2ea) // 占位页底色（POC 自定）
	PocText       = rgb(0x3a3a3a) // 占位页正文色（POC 自定）

	PocBoardBg    = rgb(0xf3d9a6)         // --cc-board-bg 木色底
	PocBoardLine  = rgb(0x8a6a3f)         // --cc-board-line 线路
	PocRiverText  = rgb(0x8a6a3f)         // --cc-river-text 楚河汉界/坐标文字
	PocPieceRed   = rgb(0xb71c1c)         // --cc-piece-red
	PocPieceBlack = rgb(0x212121)         // --cc-piece-black
	PocPieceFace  = rgb(0xfbefd0)         // --cc-piece-face 棋子盘面
	PocSelected   = rgba(0x1565c0, 0.416) // --cc-selected 选中圈
	PocLegalHint  = rgba(0x2e7d32, 0.416) // --cc-legal-hint 合法目标
	PocLastMove   = rgba(0xf9a825, 0.333) // --cc-last-move 最近着法
	PocCheckWarn  = rgba(0xb71c1c, 0.6)   // 将军提示环（08 §3.2 #7；无 Electron 锚点，POC 自定）
)
