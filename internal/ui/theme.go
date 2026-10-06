package ui

// 主题色板与字体表（design_docs/08 §1）。
//
// 色值锚点 = Electron 版 src/app/global.css :root（Material 3 棕色系，对齐
// lib/shared/constants.dart AppColors）；全仓禁散落硬编码色值，一律引用本表。
// M0' POC 过渡色板 poc_palette.go 已由本文件取代删除（08 §1 预设项）。
//
// 字体（08 §1 POC-3 回填结论）：go-text/typesetting 整形 + gio 内建 fontscan
// 系统回退；显式注册 Noto CJK 四件（Sans/Serif × Regular/Bold）双保险；
// 棋子字用 Noto Serif CJK SC 近似 Electron 楷体意图（Linux 无 KaiTi）。
// 字体集为不可变资源，启动注册一次全局复用（不属对局状态，不受 #G4 约束）。

import (
	"image/color"
	"os"

	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/text"
	"gioui.org/widget/material"
)

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

// 主题色板（global.css :root 逐项对应）。
var (
	ThemeSeed         = rgb(0x8d6e63)         // --cc-seed 主色
	ThemeSeedDark     = rgb(0x6d4c41)         // --cc-seed-dark 主色（深）
	ThemeBoardBg      = rgb(0xf3d9a6)         // --cc-board-bg 木色棋盘底
	ThemeBoardLine    = rgb(0x8a6a3f)         // --cc-board-line 线路
	ThemeRiverText    = rgb(0x8a6a3f)         // --cc-river-text 楚河汉界/坐标文字
	ThemePieceRed     = rgb(0xb71c1c)         // --cc-piece-red
	ThemePieceBlack   = rgb(0x212121)         // --cc-piece-black
	ThemePieceFace    = rgb(0xfbefd0)         // --cc-piece-face 棋子盘面
	ThemeSelected     = rgba(0x1565c0, 0.416) // --cc-selected 选中圈
	ThemeLegalHint    = rgba(0x2e7d32, 0.416) // --cc-legal-hint 合法目标提示
	ThemeLastMove     = rgba(0xf9a825, 0.333) // --cc-last-move 最近着法高亮
	ThemeSurface      = rgb(0xfaf7f2)         // --cc-surface 页面底
	ThemeSurfaceDim   = rgb(0xefe8de)         // --cc-surface-dim 次级面（卡片/侧板）
	ThemeOnSurface    = rgb(0x2b2320)         // --cc-on-surface 正文
	ThemeError        = rgb(0xb3261e)         // --cc-error 错误
	ThemeCheckWarn    = rgba(0xb71c1c, 0.6)   // 将军提示环（08 §3.2 #7；无 :root 锚点，POC 沿用）
	ThemeHoverOverlay = rgba(0x8d6e63, 0.12)  // 控件 hover 叠色（global.css .cc-btn:hover）
)

// themeFontFiles Linux 系统字体路径候选（WSLg Ubuntu 实测路径；缺文件时依赖
// gio 内建系统回退——08 §1 POC-3 双保险口径）。
var themeFontFiles = []string{
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
	"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
	"/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc",
}

// FontFaceSerifSC 棋子字引用的衬线面名（对齐 Electron cc-piece-text 的楷体意图；
// Linux 无 KaiTi，取 Noto Serif CJK SC 为最接近锚点，差异已记 POC 回填）。
const FontFaceSerifSC = "Noto Serif CJK SC"

// fontCollection gofont 内建 + 系统 Noto CJK 显式注册（08 §1：启动注册一次）。
func fontCollection() []text.FontFace {
	faces := gofont.Collection()
	for _, p := range themeFontFiles {
		src, err := os.ReadFile(p)
		if err != nil {
			continue // 平台差异容忍：缺文件时依赖 gio 内建系统回退
		}
		ff, err := opentype.ParseCollection(src)
		if err != nil {
			continue
		}
		faces = append(faces, ff...)
	}
	return faces
}

// PageTheme 正式页面共用主题（Shaper 携带显式字体集；与 POC 的 pocTheme 独立，
// POC 文件不继承本主题）。
var PageTheme = func() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(fontCollection()))
	return th
}()

// fontSerifBold 棋子字面（bold 衬线，模拟 Electron cc-piece-text 的 KaiTi+bold；
// Linux 无 KaiTi，取 FontFaceSerifSC 为最接近锚点——08 §1 POC-3 回填）。
var fontSerifBold = font.Font{Typeface: FontFaceSerifSC, Weight: font.Bold}

// fontMedium 河界/坐标文字面（Electron cc-river-text：weight 500=Medium，默认族）。
var fontMedium = font.Font{Weight: font.Medium}
