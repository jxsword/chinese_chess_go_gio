package ui

// 主页（design_docs/08 §2）：标题区 + 居中列式 Flex 的 7 张入口卡 + 全局设置入口。
// 入口卡 = 自绘圆角矩形（clip.RRect）+ 标签文本 + 点击态（widget.Clickable，hover 高亮）。
// 残局选关卡副标题的语料 puzzles 分类计数在 M5' 语料对接后接入（state 预计算投影，
// 不触发解析）。

import (
	"image"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
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
	OnNavigate     func(EntryID)
	OnOpenSettings func()
}

// HomePage 主页 state struct（铁律 #G3：主 goroutine 独占）。
type HomePage struct {
	hooks       HomePageHooks
	clicks      map[EntryID]*widget.Clickable
	settingsBtn widget.Clickable
	list        layout.List
	inited      bool
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
	if p.settingsBtn.Clicked(gtx) && p.hooks.OnOpenSettings != nil {
		p.hooks.OnOpenSettings()
	}

	// 页面底色
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)

	inset := unit.Dp(24)
	return layout.Inset{Top: inset, Bottom: inset, Left: inset, Right: inset}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
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
