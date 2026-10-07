package ui

// "保存为棋谱"对话框（翻译源 = 上游 frontend/src/features/record/RecordSaveDialog.tsx；
// 对应 record_saver.dart saveCurrentGame，07 文档 §5）。
// 四对战模式页面共用：标题/备注输入 → RecordFromSession 落 game_records。
// 与自动存档（saved_games，每模式一局）互不影响，棋谱库保留全部历史。

import (
	"fmt"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// RecordSaveDialog "保存为棋谱"对话框 state struct（主 goroutine 独占）。
type RecordSaveDialog struct {
	env   GameEnv
	mode  string
	snap  state.GameSnapshot
	close func()

	titleEditor widget.Editor
	noteEditor  widget.Editor
	saveBtn     widget.Clickable
	cancelBtn   widget.Clickable
	maskClick   widget.Clickable

	message string // 保存失败提示
	saving  bool
	saveID  string
}

// OpenRecordSaveDialog 构造对话框（页面在事件循环内调用——快照取当前局面）。
func OpenRecordSaveDialog(env GameEnv, mode string, snap state.GameSnapshot, onClose func()) *RecordSaveDialog {
	d := &RecordSaveDialog{env: env, mode: mode, snap: snap, close: onClose}
	d.titleEditor.SingleLine = true
	d.noteEditor.SingleLine = false
	return d
}

// handleEvents 输入消费（主 goroutine）。
func (d *RecordSaveDialog) handleEvents(gtx layout.Context) {
	if d.cancelBtn.Clicked(gtx) && !d.saving {
		d.Close()
		return
	}
	if d.saveBtn.Clicked(gtx) && !d.saving {
		d.save()
	}
}

// Close 关闭弹层（发起保存期间不可关——上游 disabled saving）。
func (d *RecordSaveDialog) Close() {
	if d.saveID != "" && d.env.Cancel != nil {
		d.env.Cancel(d.saveID)
	}
	d.close()
}

// Dispose 离页取消（#G5）。
func (d *RecordSaveDialog) Dispose() {
	if d.saveID != "" && d.env.Cancel != nil {
		d.env.Cancel(d.saveID)
	}
}

// save 上游 save()：RecordFromSession → recordsSave（异步）。
func (d *RecordSaveDialog) save() {
	var result *string
	if d.snap.Result != nil {
		v := string(*d.snap.Result)
		result = &v
	}
	var note *string
	if n := strings.TrimSpace(d.noteEditor.Text()); n != "" {
		note = &n
	}
	record := state.RecordFromSession(state.RecordFromSessionInput{
		Title:    strings.TrimSpace(d.titleEditor.Text()),
		Mode:     d.mode,
		FinalFen: d.snap.Fen,
		Moves:    append([]rules.Move(nil), d.snap.MoveHistory...),
		Result:   result,
		Note:     note,
	}, time.Now())
	d.saving = true
	d.message = ""
	d.saveID = d.newID("record-save")
	if d.env.Records == nil {
		d.saving = false
		d.message = "保存失败：本地存储不可用"
		return
	}
	d.env.Records.RecordsSaveAsync(d.saveID, record)
}

// newID 请求 ID（env 注入；测试容错缺省固定前缀）。
func (d *RecordSaveDialog) newID(prefix string) string {
	if d.env.NewRequestID != nil {
		return d.env.NewRequestID(prefix)
	}
	return prefix + "-test"
}

// OnSaved 保存回执（页面 OnAppEvent 转发；成功关闭，失败提示——上游 then/catch）。
func (d *RecordSaveDialog) OnSaved(err error) {
	if d.saveID == "" {
		return // 过期回执（弹层已重置）
	}
	d.saveID = ""
	d.saving = false
	if err != nil {
		d.message = "保存失败：本地存储不可用"
		return
	}
	d.close()
}

// Open 弹层是否打开（页面 modalOpen 纳入禁手守卫）。
func (d *RecordSaveDialog) Open() bool { return d != nil }

// Layout 对话框呈现（页面 Stack 顶层调用；KG-009 口径——宏量测内容尺寸 +
// op.Offset 手动居中，不用 layout.Center）。
func (d *RecordSaveDialog) Layout(gtx layout.Context) layout.Dimensions {
	d.handleEvents(gtx)
	// 半透明遮罩（拦截底层输入）
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgba(0x000000, 0.4))

	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	panelW := gtx.Dp(unit.Dp(420))
	if W < panelW {
		panelW = W
	}
	// 量测遍：内容区宽度=面板宽，高度自由
	macro := op.Record(gtx.Ops)
	mctx := gtx
	mctx.Constraints = layout.Constraints{Max: image.Point{X: panelW, Y: H}}
	dims := layout.UniformInset(unit.Dp(16)).Layout(mctx, d.panel)
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
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

func (d *RecordSaveDialog) panel(gtx layout.Context) layout.Dimensions {
	defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body1(PageTheme, "保存为棋谱")
				l.Color = ThemeOnSurface
				return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, "棋谱标题（留空自动生成）")
				l.Color = ThemeSeedDark
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutEditorBox(gtx, &d.titleEditor, "对局测试")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, "备注（可选）")
				l.Color = ThemeSeedDark
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(72))
				return layoutEditorBox(gtx, &d.noteEditor, "可留空")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if d.message == "" {
					return layout.Dimensions{}
				}
				l := material.Body2(PageTheme, d.message)
				l.Color = ThemeError
				return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(14)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							btn := material.Button(PageTheme, &d.cancelBtn, "取消")
							btn.Background = ThemeSurfaceDim
							btn.Color = ThemeSeedDark
							return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Dp(unit.Dp(96))
								return btn.Layout(gtx)
							})
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							btn := material.Button(PageTheme, &d.saveBtn, fmt.Sprintf("保存（共 %d 着）", len(d.snap.MoveHistory)))
							return btn.Layout(gtx)
						}),
					)
				})
			}),
		)
	})
}
