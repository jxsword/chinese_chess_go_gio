package ui

// 主页（design_docs/08 §2）：标题区 + 居中列式 Flex 的 7 张入口卡 + 全局设置入口。
// 入口卡 = 自绘圆角矩形（clip.RRect）+ 标签文本 + 点击态（widget.Clickable，hover 高亮）。
// 残局选关卡副标题的语料 puzzles 分类计数在 M5' 语料对接后接入（state 预计算投影，
// 不触发解析）。

import (
	"fmt"
	"image"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// EntryID 主页入口标识（app 层映射到路由）。
type EntryID string

const (
	EntryEndgameSelect EntryID = "endgameSelect" // 残局选关
	EntryHumanVsAi     EntryID = "humanVsAi"     // 人机对战
	EntryHumanVsLlm    EntryID = "humanVsLlm"    // 人机 LLM
	EntryLlmVsLlm      EntryID = "llmVsLlm"      // LLM vs LLM
	EntryHumanVsHuman  EntryID = "humanVsHuman"  // 双人对弈
	EntryStudio        EntryID = "studio"        // 残局工作室
	EntryCorpus        EntryID = "corpus"        // 棋谱库
)

// entryDesc 入口卡静态描述（标题 + 副标题）。
type entryDesc struct {
	id       EntryID
	title    string
	subtitle string
}

// homeEntries 08 §2 七入口（顺序即页面列示顺序）。
var homeEntries = []entryDesc{
	{EntryEndgameSelect, "残局选关", "关卡挑战 · 演示播放"},
	{EntryHumanVsAi, "人机对战", "内置 AI 引擎 · 三档难度"},
	{EntryHumanVsLlm, "人机 LLM", "与大模型对弈"},
	{EntryLlmVsLlm, "LLM vs LLM", "双大模型对战观演"},
	{EntryHumanVsHuman, "双人对弈", "同屏轮流走子"},
	{EntryStudio, "残局工作室", "摆盘校验 · 求解 · 识图"},
	{EntryCorpus, "棋谱库", "14 万局棋谱浏览"},
}

// HomePageHooks 主页回调（app 装配层接线；ui 不 import app）。
type HomePageHooks struct {
	OnNavigate func(EntryID)
	// Settings 全局设置（非 nil 时"全局设置"按钮打开页内弹窗——08 §9；
	// OnOpenSettings 为无 Settings 时的兼容回调）。
	Settings       *state.GlobalSettings
	OnOpenSettings func()
}

// HomePage 主页 state struct（铁律 #G3：主 goroutine 独占）。
type HomePage struct {
	hooks            HomePageHooks
	clicks           map[EntryID]*widget.Clickable
	settingsBtn      widget.Clickable
	settingsCloseBtn widget.Clickable
	list             layout.List
	inited           bool
	settingsOpen     bool
	autoSave         widget.Bool
	scaleClicks      []widget.Clickable
}

// NewHomePage 创建主页。
func NewHomePage(hooks HomePageHooks) *HomePage {
	return &HomePage{hooks: hooks, list: layout.List{Axis: layout.Vertical}}
}

func (p *HomePage) init() {
	if p.inited {
		return
	}
	p.clicks = make(map[EntryID]*widget.Clickable, len(homeEntries))
	for _, e := range homeEntries {
		p.clicks[e.id] = &widget.Clickable{}
	}
	p.inited = true
}

// Layout 绘制主页。
func (p *HomePage) Layout(gtx layout.Context) layout.Dimensions {
	p.init()
	// 点击派发（Layout 中消费 Clickable 状态——立即模式约定）
	for _, e := range homeEntries {
		if p.clicks[e.id].Clicked(gtx) && p.hooks.OnNavigate != nil {
			p.hooks.OnNavigate(e.id)
		}
	}
	if p.settingsBtn.Clicked(gtx) {
		if p.hooks.Settings != nil {
			p.settingsOpen = !p.settingsOpen
		} else if p.hooks.OnOpenSettings != nil {
			p.hooks.OnOpenSettings()
		}
	}

	// 页面底色
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)

	inset := unit.Dp(24)
	dims := layout.Inset{Top: inset, Bottom: inset, Left: inset, Right: inset}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// 标题区（居中：根约束 Min=Max=窗宽，须用 Center 清 Min 再居中）
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				title := material.H3(PageTheme, "中国象棋 Ultra")
				title.Color = ThemeOnSurface
				return layout.Center.Layout(gtx, title.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				sub := material.Body2(PageTheme, "Gio 全自绘单二进制桌面版")
				sub.Color = ThemeSeedDark
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, sub.Layout)
				})
			}),
			// 7 张入口卡（居中列式；超高窗口时列表可滚动）
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				cardH := gtx.Dp(unit.Dp(76))
				gtx.Constraints.Min.Y = cardH * len(homeEntries)
				return p.list.Layout(gtx, len(homeEntries), func(gtx layout.Context, i int) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						// 行宽=窗宽（Min 继承），Center 清 Min 后按卡宽水平居中
						return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return p.drawEntryCard(gtx, homeEntries[i])
						})
					})
				})
			}),
			// 全局设置入口（水平居中，按钮定宽）
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				btn := material.Button(PageTheme, &p.settingsBtn, "全局设置")
				btn.Background = ThemeSurfaceDim
				btn.Color = ThemeSeedDark
				return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(160))
						return btn.Layout(gtx)
					})
				})
			}),
		)
	})
	if p.settingsOpen && p.hooks.Settings != nil {
		p.layoutSettingsModal(gtx)
	}
	return dims
}

// layoutSettingsModal 全局设置弹窗（08 §9）：遮罩+居中面板（KG-009 模式），
// autoSave 开关 + 界面缩放档位 chips（改动即生效/落盘）。
func (p *HomePage) layoutSettingsModal(gtx layout.Context) {
	fillRect(gtx.Ops, image.Rectangle{Max: gtx.Constraints.Max}, rgba(0x000000, 0.45))
	if len(p.scaleClicks) != len(state.UIScaleOptions) {
		p.scaleClicks = make([]widget.Clickable, len(state.UIScaleOptions))
	}
	gs := p.hooks.Settings
	closeClicked := func() bool {
		if p.autoSave.Update(gtx) {
			gs.SetAutoSave(p.autoSave.Value)
		}
		for i := range p.scaleClicks {
			if p.scaleClicks[i].Clicked(gtx) {
				gs.SetUIScale(state.UIScaleOptions[i])
			}
		}
		return false
	}
	content := func(gtx layout.Context) layout.Dimensions {
		p.autoSave.Value = gs.AutoSave
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := material.H6(PageTheme, "全局设置")
				t.Color = ThemeOnSurface
				return t.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(12), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					cb := material.CheckBox(PageTheme, &p.autoSave, "自动保存棋局（离开页面/切后台时）")
					cb.Color = ThemeSeed
					return cb.Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, "界面缩放（跟随系统为基础，调整后立即生效）")
				l.Color = ThemeOnSurface
				return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				opts := make([]chipOpt, 0, len(state.UIScaleOptions))
				for i, v := range state.UIScaleOptions {
					label := fmt.Sprintf("%d%%", int(v*100+0.5))
					opts = append(opts, chipOpt{&p.scaleClicks[i], label, gs.UIScale == v})
				}
				return layoutOptionChips(gtx, 64, opts...)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							btn := material.Button(PageTheme, &p.settingsCloseBtn, "关闭")
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints.Min.X = gtx.Dp(unit.Dp(110))
								return btn.Layout(gtx)
							})
						}),
					)
				})
			}),
		)
	}
	// 关闭路径：遮罩点击或"关闭"按钮
	if p.settingsCloseBtn.Clicked(gtx) {
		p.settingsOpen = false
	}
	_ = closeClicked
	closeClicked()
	// 面板（KG-009 手动居中模式，同 widgets.drawPanel）
	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	panelW := gtx.Dp(unit.Dp(460))
	if W < panelW {
		panelW = W
	}
	macro := op.Record(gtx.Ops)
	mctx := gtx
	mctx.Constraints = layout.Constraints{Max: image.Point{X: panelW, Y: H}}
	dims := layout.UniformInset(unit.Dp(20)).Layout(mctx, content)
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

// drawEntryCard 单张入口卡：圆角矩形 + hover 高亮 + 标题/副标题（可点击区域=整卡）。
func (p *HomePage) drawEntryCard(gtx layout.Context, e entryDesc) layout.Dimensions {
	c := p.clicks[e.id]
	return c.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		bg := ThemeSurfaceDim
		if c.Hovered() {
			bg = ThemeHoverOverlay
		}
		w := gtx.Dp(unit.Dp(420))
		if gtx.Constraints.Max.X < w {
			w = gtx.Constraints.Max.X
		}
		h := gtx.Dp(unit.Dp(64))
		gtx.Constraints = layout.Exact(image.Point{X: w, Y: h})
		defer clip.UniformRRect(image.Rectangle{Max: image.Point{X: w, Y: h}}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, bg)
		// 文本块整块居中（Center 清 Min 使文本按内容取宽，再在卡内两轴居中）
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					t := material.H6(PageTheme, e.title)
					t.Color = ThemeOnSurface
					return t.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					s := material.Body2(PageTheme, e.subtitle)
					s.Color = ThemeSeedDark
					return s.Layout(gtx)
				}),
			)
		})
	})
}
