package ui

// 基础绘制原语（boardart/对局页共用；T2'.3 起由 POC 文件上收为共享绘制库）。
// drawCenteredText 直接走 typesetting 整形 + 字形路径填充，绕开 label 布局包装，
// 位置完全由 (cx,cy) 决定（POC-1 实测 material.Label 宏重放定位不稳，08 §12 回填）。

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
)

// fillRect 填充矩形。
func fillRect(ops *op.Ops, r image.Rectangle, c color.NRGBA) {
	paint.FillShape(ops, c, clip.Rect(r).Op())
}

// strokeLine 线段（clip.Stroke）。
func strokeLine(ops *op.Ops, x1, y1, x2, y2, width float32, c color.NRGBA) {
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(x1, y1))
	p.LineTo(f32.Pt(x2, y2))
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

// fillCircle 填充圆。
func fillCircle(ops *op.Ops, cx, cy, r float32, c color.NRGBA) {
	paint.FillShape(ops, c, clip.Ellipse(image.Rect(
		int(cx-r), int(cy-r), int(cx+r), int(cy+r))).Op(ops))
}

// strokeRect 矩形描边。
func strokeRect(ops *op.Ops, x, y, w, h, width float32, c color.NRGBA) {
	var p clip.Path
	p.Begin(ops)
	p.MoveTo(f32.Pt(x, y))
	p.LineTo(f32.Pt(x+w, y))
	p.LineTo(f32.Pt(x+w, y+h))
	p.LineTo(f32.Pt(x, y+h))
	p.Close()
	paint.FillShape(ops, c, clip.Stroke{Path: p.End(), Width: width}.Op())
}

// strokeCircle 圆环描边（圆路径用三次贝塞尔逼近，同 clip.Ellipse 实现）。
func strokeCircle(ops *op.Ops, cx, cy, r, width float32, c color.NRGBA) {
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

// drawCenteredText 居中文字（等价 SVG text-anchor:middle + dominant-baseline:central）。
func drawCenteredText(gtx layout.Context, str string, cx, cy float32, px float32, c color.NRGBA, f font.Font) {
	ppem := fixed.Int26_6(px * 64)
	shaper := PageTheme.Shaper
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
