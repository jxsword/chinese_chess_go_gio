// 临时占位页：仅证明窗口/事件循环装配可用（T0'.1 验收）。
// M1' 主页落地后整文件删除；不属于 POC 也不继承。
package ui

import (
	"fmt"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// Placeholder M0' 骨架占位页。
type Placeholder struct {
	Version string
}

// Layout 绘制状态文本。
func (p *Placeholder) Layout(gtx layout.Context) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, PocBackground)

	title := material.H3(pocTheme, fmt.Sprintf("M0' 骨架就绪 — 版本 %s", p.Version))
	title.Color = PocText
	body := material.Body1(pocTheme, "window.Event() 单事件循环运行中（本占位页将在 M1' 删除）")
	body.Color = PocText
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(title.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(12))
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Rigid(body.Layout),
		)
	})
}

// pocTheme POC/占位期临时主题；正式里程碑换 internal/ui/theme.go（08 §1）。
var pocTheme = material.NewTheme()
