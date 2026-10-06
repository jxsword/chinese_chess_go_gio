package ui

// 语料库长列表性能实测（T5'.2 验证门，POC-4 口径；CC_GIO_SYNTH_SCROLL=1 启用）：
// 每帧统计布局耗时（64 帧环形缓冲）+ FPS；自动滚动直接推进 Position.First
//（KG-002 红线：不经 List.ScrollBy），每帧 Invalidate 排帧——最坏情况口径
//（每帧重绘+每帧排帧）。非交互路径，正式手测不启用。

import (
	"fmt"
	"image"
	"log"
	"time"

	"gioui.org/f32"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// framePerf 帧开销统计 + 自动滚动驱动。
type framePerf struct {
	costs [64]time.Duration
	idx   int

	frames int
	fps    int
	mark   time.Time
}

func newFramePerf() *framePerf { return &framePerf{} }

// frame 记录一帧布局耗时（defer 于 Layout 顶部），并排下一帧。
func (f *framePerf) frame(gtx layout.Context, start time.Time) {
	f.costs[f.idx] = time.Since(start)
	f.idx = (f.idx + 1) % len(f.costs)
	f.frames++
	if f.mark.IsZero() {
		f.mark = gtx.Now
	}
	if el := gtx.Now.Sub(f.mark); el >= time.Second {
		f.fps = f.frames
		f.frames = 0
		f.mark = gtx.Now
		// 每秒输出统计（性能实测取证；POC-4 口径的数值面）。
		var sum, mx time.Duration
		n := 0
		for _, c := range f.costs {
			if c > 0 {
				n++
				sum += c
				if c > mx {
					mx = c
				}
			}
		}
		avg := time.Duration(0)
		if n > 0 {
			avg = sum / time.Duration(n)
		}
		log.Printf("perf: FPS %d | frame avg %s / max %s", f.fps, avg.Round(time.Microsecond), mx.Round(time.Microsecond))
	}
	// 最坏情况：每帧强制排帧（与 POC-4 口径一致）。
	gtx.Execute(op.InvalidateCmd{})
}

// overlay 自动滚动 PGN 长列表并绘制统计浮层（FPS + 帧开销均值/峰值）。
func (f *framePerf) overlay(gtx layout.Context, l *layout.List, total int) {
	if total > 0 {
		l.Position.First += 2
		if l.Position.First >= total {
			l.Position.First = 0
		}
	}
	var sum, max time.Duration
	n := 0
	for _, c := range f.costs {
		if c > 0 {
			n++
			sum += c
			if c > max {
				max = c
			}
		}
	}
	avg := time.Duration(0)
	if n > 0 {
		avg = sum / time.Duration(n)
	}
	label := fmt.Sprintf("FPS %d | frame avg %s / max %s", f.fps, avg.Round(time.Microsecond), max.Round(time.Microsecond))

	// 顶部右侧浮层（测量遍取宽后右对齐；语义沿 poc_list layoutStats）。
	macro := op.Record(gtx.Ops)
	mctx := gtx
	mctx.Constraints = layout.Constraints{Max: image.Point{X: 1e6, Y: 1e6}}
	stats := material.Body2(PageTheme, label)
	stats.TextSize = unit.Sp(12)
	tdims := stats.Layout(mctx)
	macro.Stop()

	boxW := tdims.Size.X + gtx.Dp(unit.Dp(16))
	boxH := tdims.Size.Y + gtx.Dp(8)
	off := op.Affine(f32.Affine2D{}.Offset(f32.Point{X: float32(gtx.Constraints.Max.X - boxW), Y: 0})).Push(gtx.Ops)
	defer off.Pop()
	defer clip.Rect{Max: image.Point{X: boxW, Y: boxH}}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgb(0x2b2320))
	gtx.Constraints = layout.Exact(image.Point{X: boxW, Y: boxH})
	layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		s := material.Body2(PageTheme, label)
		s.TextSize = unit.Sp(12)
		s.Color = rgb(0xfaf7f2)
		return s.Layout(gtx)
	})
}
