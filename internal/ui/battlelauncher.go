package ui

// 进入对战启动器（共用组件，T5'.3 验收反馈统一两处界面；翻译锚点 = 上游
// frontend/src/features/record/RecordLauncherDialog.tsx：模式选择 → 人机 AI
// 附执方选择 → 回调 launch）。
//
// 布局口径：KG-009——Stack Stacked 子节点内 layout.Center 退化为左上角，
// 必须"宏量测内容尺寸 + op.Offset 手动居中"（记录库启动器同款，实测可点；
// 语料重放器旧版 Center 实测错位+点击失效，已统一本组件）。

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// BattleLauncher 启动器 state struct（主 goroutine 独占）。
type BattleLauncher struct {
	open     bool
	sideStep bool // 人机 AI：执方选择步
	target   any  // 调用方上下文（启动时原样回传）

	modeClicks   [4]widget.Clickable
	sideRedBtn   widget.Clickable
	sideBlackBtn widget.Clickable
	cancelBtn    widget.Clickable
}

// Open 打开弹层（target 为调用方上下文，launch 时原样回传）。
func (l *BattleLauncher) Open(target any) { l.open, l.sideStep, l.target = true, false, target }

// Close 关闭弹层。
func (l *BattleLauncher) Close() { l.open, l.sideStep, l.target = false, false, nil }

// Opened 弹层是否打开。
func (l *BattleLauncher) Opened() bool { return l.open }

// handleEvents 输入消费；选择模式/执方后以 onLaunch(mode, side, target) 回调
// （side 仅 humanVsAi 携带 "red"/"black"，其余空串——上游 battleRouteFor 口径）。
func (l *BattleLauncher) handleEvents(gtx layout.Context, onLaunch func(mode BattleMode, side string, target any)) {
	if !l.open {
		return
	}
	for i := range l.modeClicks {
		if l.modeClicks[i].Clicked(gtx) {
			opt := BattleModeOptions[i]
			if opt.ID == BattleHumanVsAi && !l.sideStep {
				l.sideStep = true // 人机 AI：附执方选择
				return
			}
			mode, side := opt.ID, ""
			l.Close()
			onLaunch(mode, side, l.target)
			return
		}
	}
	if l.sideStep {
		switch {
		case l.sideRedBtn.Clicked(gtx):
			l.Close()
			onLaunch(BattleHumanVsAi, "red", l.target)
		case l.sideBlackBtn.Clicked(gtx):
			l.Close()
			onLaunch(BattleHumanVsAi, "black", l.target)
		}
	}
	if l.cancelBtn.Clicked(gtx) {
		if l.sideStep {
			l.sideStep = false // 返回模式选择步（上游同款）
		} else {
			l.Close()
		}
	}
}

// Layout 弹层呈现（遮罩 + 面板；KG-009 居中口径）。
func (l *BattleLauncher) Layout(gtx layout.Context, onLaunch func(mode BattleMode, side string, target any)) {
	if !l.open {
		return
	}
	l.handleEvents(gtx, onLaunch)
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgba(0x000000, 0.4))

	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	panelW := gtx.Dp(unit.Dp(380))
	if W < panelW {
		panelW = W
	}
	// 量测遍：内容区宽度=面板宽，高度自由
	macro := op.Record(gtx.Ops)
	mctx := gtx
	mctx.Constraints = layout.Constraints{Max: image.Point{X: panelW, Y: H}}
	dims := layout.UniformInset(unit.Dp(16)).Layout(mctx, l.panel)
	call := macro.Stop()

	panelH := dims.Size.Y
	if panelH > H {
		panelH = H
	}
	tr := op.Offset(image.Pt((W-panelW)/2, (H-panelH)/2)).Push(gtx.Ops)
	defer tr.Pop()
	defer clip.UniformRRect(image.Rectangle{Max: image.Point{X: panelW, Y: panelH}}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	call.Add(gtx.Ops)
}

// panel 启动器内容（标题 / 模式清单或执方选择 / 取消-返回）。
func (l *BattleLauncher) panel(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			title := "选择对战模式"
			if l.sideStep {
				title = "人机对战（内置 AI） · 选择执方（AI 执另一方）"
			}
			lb := material.Body1(PageTheme, title)
			lb.Color = ThemeOnSurface
			return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, lb.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if l.sideStep {
				btn := material.Button(PageTheme, &l.sideRedBtn, "玩家执红")
				btn.Background = ThemeSurfaceDim
				btn.Color = ThemeSeedDark
				b2 := material.Button(PageTheme, &l.sideBlackBtn, "玩家执黑")
				b2.Background = ThemeSurfaceDim
				b2.Color = ThemeSeedDark
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(btn.Layout),
					layout.Rigid(b2.Layout),
				)
			}
			children := make([]layout.FlexChild, 0, len(BattleModeOptions))
			for i, opt := range BattleModeOptions {
				i, opt := i, opt
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return l.modeClicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
							paint.Fill(gtx.Ops, ThemeSurfaceDim)
							return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										lb := material.Body2(PageTheme, opt.Label)
										lb.Color = ThemeOnSurface
										return lb.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										sub := material.Body2(PageTheme, opt.Subtitle)
										sub.TextSize = unit.Sp(12)
										sub.Color = ThemeSeedDark
										return sub.Layout(gtx)
									}),
								)
							})
						})
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cancel := material.Button(PageTheme, &l.cancelBtn, l.cancelLabel())
				cancel.Background = ThemeSurfaceDim
				cancel.Color = ThemeSeedDark
				return cancel.Layout(gtx)
			})
		}),
	)
}

// cancelLabel 取消按钮文案（执方选择步=返回，首屏=取消——上游同款）。
func (l *BattleLauncher) cancelLabel() string {
	if l.sideStep {
		return "返回"
	}
	return "取消"
}
