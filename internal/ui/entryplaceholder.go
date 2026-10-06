package ui

// 入口占位页：M1' 保证主页 7 入口可导航；各正式页面 M2' 起逐个替换本页。

import (
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// EntryPlaceholder 占位页 state struct。
type EntryPlaceholder struct {
	Title  string
	OnBack func()
	back   widget.Clickable
}

// NewEntryPlaceholder 创建占位页。
func NewEntryPlaceholder(cfg EntryPlaceholder) *EntryPlaceholder {
	return &EntryPlaceholder{Title: cfg.Title, OnBack: cfg.OnBack}
}

// Layout 绘制占位页。
func (p *EntryPlaceholder) Layout(gtx layout.Context) layout.Dimensions {
	if p.back.Clicked(gtx) && p.OnBack != nil {
		p.OnBack()
	}
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				title := material.H4(PageTheme, p.Title)
				title.Color = ThemeOnSurface
				return title.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				body := material.Body1(PageTheme, "本页面将在后续里程碑落地")
				body.Color = ThemeSeedDark
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(24)}.Layout(gtx, body.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				btn := material.Button(PageTheme, &p.back, "返回主页")
				btn.Background = ThemeSurfaceDim
				btn.Color = ThemeSeedDark
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(160))
				return btn.Layout(gtx)
			}),
		)
	})
}
