// POC 字体链（T0'.2/T0'.4）：显式注册系统 Noto CJK 到 Gio 字体集，
// 棋子/河界文字可按名字引用 Serif 面；默认西文仍走 gofont。
// 正式里程碑按 08 §1 结论（POC-3 回填）重建注册路径，本文件不继承。
package ui

import (
	"os"

	"gioui.org/font/gofont"
	"gioui.org/font/opentype"
	"gioui.org/text"
	"gioui.org/widget/material"
)

// pocFontFiles Linux 系统字体路径候选（WSLg Ubuntu 实测路径；POC-3 结论回填 08 §1）。
var pocFontFiles = []string{
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
	"/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc",
	"/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
	"/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc",
}

// pocFontFaceSerifSC 棋子字引用的衬线面名（对齐 Electron cc-piece-text 的楷体意图：
// Linux 无 KaiTi，取 Noto Serif CJK SC 为最接近锚点，差异记 POC 回填）。
const pocFontFaceSerifSC = "Noto Serif CJK SC"

// pocFontCollection gofont + 系统 Noto CJK（启动时注册一次，08 §1）。
func pocFontCollection() []text.FontFace {
	faces := gofont.Collection()
	for _, p := range pocFontFiles {
		src, err := os.ReadFile(p)
		if err != nil {
			continue // 平台差异容忍：缺文件时依赖 gio 内建系统回退（POC-3 记录）
		}
		ff, err := opentype.ParseCollection(src)
		if err != nil {
			continue
		}
		faces = append(faces, ff...)
	}
	return faces
}

// pocTheme POC 专用主题：Shaper 携带显式字体集。
var pocTheme = func() *material.Theme {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(pocFontCollection()))
	return th
}()
