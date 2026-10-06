package ui

// 共享 UI 组件（design_docs/08 §5）：模态弹窗（遮罩+面板+按钮组）、
// 非阻塞 toast 浮层、终局结算横幅。
// 新游戏确认框/终局结算弹窗全页共用；toast 不注册输入事件（防错 #11）。

import (
	"image"
	"image/color"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// ModalDialog 模态确认框（上游 ConfirmDialog；AlertDialog 最小实现）。
// LayoutFull 绘制整页遮罩 + 居中面板，返回本帧按钮边沿 (confirmed, canceled)，
// 调用方据此执行回调并关闭（立即模式无内部状态残留）。
type ModalDialog struct {
	Title        string
	Content      string
	ConfirmLabel string
	CancelLabel  string

	confirm widget.Clickable
	cancel  widget.Clickable
}

// NewModalDialog 创建确认框（缺省按钮文案=确定/取消）。
func NewModalDialog(title, content string) *ModalDialog {
	return &ModalDialog{Title: title, Content: content, ConfirmLabel: "确定", CancelLabel: "取消"}
}

// LayoutFull 整页遮罩（.cc-dialog-mask，半透明黑；无 :root 锚点取通用 0.45）
// + 居中面板 + 按钮行。
func (d *ModalDialog) LayoutFull(gtx layout.Context) (confirmed, canceled bool) {
	fillRect(gtx.Ops, image.Rectangle{Max: gtx.Constraints.Max}, rgba(0x000000, 0.45))
	// 边沿检测先于绘制（Click 在本帧布局中注册，下一帧回执）
	confirmed = d.confirm.Clicked(gtx)
	canceled = d.cancel.Clicked(gtx)
	d.drawPanel(gtx)
	return confirmed, canceled
}

// drawPanel 居中面板：先量内容尺寸（宽度=面板宽、高度自适应），再手动偏移到
// 窗口正中绘制。不走 layout.Center——gio 的 Center 是 Direction，按 Min 约束
// 空间居中，而 Stack 的 Stacked 子节点 Min={0,0} 会退化到左上角（KG-009）。
func (d *ModalDialog) drawPanel(gtx layout.Context) {
	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	panelW := gtx.Dp(unit.Dp(380))
	if W < panelW {
		panelW = W
	}

	// 量测：内容区宽度=面板宽，高度自由；面板底色在最终偏移后绘制
	macro := op.Record(gtx.Ops)
	mctx := gtx
	mctx.Constraints = layout.Constraints{Max: image.Point{X: panelW, Y: H}}
	dims := layout.UniformInset(unit.Dp(20)).Layout(mctx, d.panelContent)
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

// panelContent 标题/内容/按钮行（约束=面板内容区）。
func (d *ModalDialog) panelContent(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			t := material.H6(PageTheme, d.Title)
			t.Color = ThemeOnSurface
			return t.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			c := material.Body2(PageTheme, d.Content)
			c.Color = ThemeOnSurface
			return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(16)}.Layout(gtx, c.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
				layout.Rigid(d.button(&d.cancel, d.CancelLabel, ThemeSurfaceDim, ThemeSeedDark, unit.Dp(8))),
				layout.Rigid(d.button(&d.confirm, d.ConfirmLabel, ThemeSeed, ThemeSurface, 0)),
			)
		}),
	)
}

// button 对话框按钮（定宽，主/次配色由入参决定）。
func (d *ModalDialog) button(c *widget.Clickable, label string, bg, fg color.NRGBA, right unit.Dp) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(PageTheme, c, label)
		btn.Background = bg
		btn.Color = fg
		return layout.Inset{Right: right}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(110))
			return btn.Layout(gtx)
		})
	}
}

// DrawToast 非阻塞 toast 浮层（.cc-snackbar；Stack 顶层，不注册输入事件——防错 #11）。
// 底部居中胶囊（上游 .cc-snackbar 固定底部）；手动偏移定位（KG-009 同 drawPanel）。
func DrawToast(gtx layout.Context, text string) {
	if text == "" {
		return
	}
	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	w := gtx.Dp(unit.Dp(320))
	if W < w {
		w = W
	}
	h := gtx.Dp(unit.Dp(40))
	bottom := gtx.Dp(unit.Dp(24))
	tr := op.Offset(image.Pt((W-w)/2, H-h-bottom)).Push(gtx.Ops)
	defer tr.Pop()
	defer clip.UniformRRect(image.Rectangle{Max: image.Point{X: w, Y: h}}, gtx.Dp(unit.Dp(20))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgba(0x2b2320, 0.92))
	// 胶囊内文字居中（Center 按 Exact Min=Max 空间居中，此处语义正确）
	gtx.Constraints = layout.Exact(image.Point{X: w, Y: h})
	layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(PageTheme, text)
		l.Color = ThemeSurface
		return l.Layout(gtx)
	})
}

// DrawResultBanner 终局结算横幅（side_panel.dart:142-175；result 为 nil 不显示）。
// k=3 判负等经 vm.Resign/AgreeDraw 复用本收尾（防错 #12）。
func DrawResultBanner(gtx layout.Context, result *state.GameResult) layout.Dimensions {
	if result == nil {
		return layout.Dimensions{}
	}
	bg := ThemeSeed
	switch *result {
	case state.ResultRedWins:
		bg = ThemePieceRed
	case state.ResultBlackWins:
		bg = ThemePieceBlack
	}
	size := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(unit.Dp(44))}
	defer clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, bg)
	gtx.Constraints = layout.Exact(size)
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.H6(PageTheme, resultText(*result))
		l.Color = ThemeSurface
		return l.Layout(gtx)
	})
}

func resultText(result state.GameResult) string {
	switch result {
	case state.ResultRedWins:
		return "红方胜！"
	case state.ResultBlackWins:
		return "黑方胜！"
	}
	return "和棋"
}

// formatMoveText 走法格式化（side_panel.dart:210-222）：走法记录恒有棋子信息
// （executeMove 记录），直接中文记谱；防御退化占位符。
func formatMoveText(m rules.Move) string {
	if m.Piece != nil {
		return rules.ChineseNotation(m.Piece, m.From, m.To)
	}
	return "?"
}

// chipOpt 选项 chip（选择类控件；selected=当前项高亮）。
type chipOpt struct {
	click    *widget.Clickable
	label    string
	selected bool
}

// layoutOptionChips 一行选项 chips（人机页/LLM 配置卡共用；width=单 chip 最小宽 dp，
// 需经 Clickable.Layout 注册指针区——M3' 验收修复口径）。
func layoutOptionChips(gtx layout.Context, width int, opts ...chipOpt) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(opts))
	for _, o := range opts {
		o := o
		w := gtx.Dp(unit.Dp(width))
		if w < gtx.Dp(unit.Dp(42)) {
			w = gtx.Dp(unit.Dp(42))
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			bg := ThemeSurface
			fg := ThemeSeedDark
			if o.selected {
				bg = ThemeSeed
				fg = ThemeSurface
			}
			return layout.Inset{Right: unit.Dp(5), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return o.click.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					gtx.Constraints.Min.X = w
					gtx.Constraints.Max.X = w
					gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(30))
					gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(30))
					defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(15))).Push(gtx.Ops).Pop()
					paint.Fill(gtx.Ops, bg)
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						l := material.Body2(PageTheme, o.label)
						l.Color = fg
						l.TextSize = unit.Sp(15)
						return l.Layout(gtx)
					})
				})
			})
		}))
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}
