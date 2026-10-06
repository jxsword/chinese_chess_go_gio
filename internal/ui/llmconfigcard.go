package ui

// LLM 配置卡（T4'.2；翻译锚点 = 上游 features/settings/LlmConfigCard.tsx，
// design_docs/08 §5、05 附录）：
//   - 预设 chips / 端点地址 / API Key（掩码回显）/ 模型 ID（Editor+IME）；
//   - 【上游-DR-005 硬性要求】无思维链开关——关闭参数由复制物请求构造层
//     （internal/llm/config.go）按预设恒发，卡内仅显示固定提示语；
//   - 测试连接（DR-014 镜像 testOverride 由页面决定）；
//   - KG-004 可靠粘贴按钮（powershell 管道异步化，铁律 #G3）；
//   - 持久化不在此落盘：onChange 由页面防抖保存（掩码合并在 storage.Set）。

import (
	"image"
	"strings"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
)

// colorWhite 编辑框底色（白底描边，POC-3 layoutEditor 同款）。
var colorWhite = rgb(0xffffff)

// sanitizeField 表单输入清洗：剥离全部控制字符（NUL/制表/回车等 C0 与 DEL）
// 及首尾空白。M4' 验收反馈实证：百炼端点 404 回显的模型名尾部夹带 4 个 NUL
// （HTTP 404: The model `qwen3.8-max:\u0000\u0000\u0000\u0000` does not exist）——
// 控制字符经键入/剪贴板路径进入 Editor 后原样进入请求体。UI 层在表单边界
// 清洗，不触碰复制物协议构造（#G2）。
func sanitizeField(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}

// 配置卡文本字段目标编号（PasteTextDone.Target 定向回填）。
const (
	PasteTargetBaseURL = iota
	PasteTargetAPIKey
	PasteTargetModel
)

// LlmConfigCard LLM 配置卡 state struct（每页每槽位一实例；铁律 #G3 主 goroutine 独占）。
type LlmConfigCard struct {
	title    string
	slot     string
	presets  []llm.LlmPreset
	onChange func(llm.LlmEndpointConfig)

	// OnPaste "从 Windows 剪贴板粘贴"请求（KG-004；页面负责异步取词并回填）。
	OnPaste func(target int)
	// OnTestConnection 测试连接请求（页面负责发起与回执回填 testResult）。
	OnTestConnection func(cfg llm.LlmEndpointConfig, slot string)

	config     llm.LlmEndpointConfig
	testing    bool
	testResult string
	testOK     bool

	presetBtns []widget.Clickable
	baseURL    widget.Editor
	apiKey     widget.Editor
	model      widget.Editor
	pasteBtns  [3]widget.Clickable
	testBtn    widget.Clickable
}

// NewLlmConfigCard 构造配置卡（config 为掩码回读配置；onChange 每次编辑回调整体配置）。
func NewLlmConfigCard(title, slot string, presets []llm.LlmPreset, config llm.LlmEndpointConfig, onChange func(llm.LlmEndpointConfig)) *LlmConfigCard {
	if presets == nil {
		presets = llm.LlmPresets
	}
	c := &LlmConfigCard{title: title, slot: slot, presets: presets, config: config, onChange: onChange}
	c.baseURL.SingleLine = true
	c.apiKey.SingleLine = true
	c.model.SingleLine = true
	c.baseURL.SetText(config.BaseURL)
	c.apiKey.SetText(config.APIKey)
	c.model.SetText(config.Model)
	c.presetBtns = make([]widget.Clickable, len(presets))
	return c
}

// SetConfig 覆盖配置（加载回执/预设切换/页面镜像时调用；同步编辑器文本并清洗）。
func (c *LlmConfigCard) SetConfig(cfg llm.LlmEndpointConfig) {
	cfg.BaseURL = sanitizeField(cfg.BaseURL)
	cfg.APIKey = sanitizeField(cfg.APIKey)
	cfg.Model = sanitizeField(cfg.Model)
	c.config = cfg
	c.baseURL.SetText(cfg.BaseURL)
	c.apiKey.SetText(cfg.APIKey)
	c.model.SetText(cfg.Model)
}

// Config 当前配置（编辑器文本为准——ChangeEvent 异步于回读；控制字符在
// 表单边界清洗，sanitizeField）。
func (c *LlmConfigCard) Config() llm.LlmEndpointConfig {
	return llm.LlmEndpointConfig{
		BaseURL: sanitizeField(c.baseURL.Text()),
		APIKey:  sanitizeField(c.apiKey.Text()),
		Model:   sanitizeField(c.model.Text()),
		Preset:  c.config.Preset,
	}
}

// SetTestResult 回填测试连接结果（页面在 LlmTestDone 回执时调用）。
func (c *LlmConfigCard) SetTestResult(ok bool, message string) {
	c.testing = false
	c.testOK = ok
	c.testResult = message
}

// SetTesting 测试中状态置位（页面发起测试连接时调用）。
func (c *LlmConfigCard) SetTesting() {
	c.testing = true
	c.testResult = ""
}

// ApplyPaste 回填粘贴文本（页面在 PasteTextDone 回执时调用；失败以错误后缀显示在目标字段）。
func (c *LlmConfigCard) ApplyPaste(target int, text string, err error) {
	if err != nil {
		return // 粘贴失败静默（非 Windows 环境按钮本就少用；不干扰表单）
	}
	text = sanitizeField(text)
	if text == "" {
		return
	}
	switch target {
	case PasteTargetBaseURL:
		c.baseURL.SetText(text)
	case PasteTargetAPIKey:
		c.apiKey.SetText(text)
	case PasteTargetModel:
		c.model.SetText(text)
	}
	c.notifyChange()
}

// currentPreset 当前预设：优先按配置记录的预设名，回退按端点地址匹配（旧存档兼容；
// 上游 LlmConfigCard presetFor 翻译）。
func (c *LlmConfigCard) currentPreset() llm.LlmPreset {
	for _, p := range c.presets {
		if p.Name == c.config.Preset {
			return p
		}
	}
	for _, p := range c.presets {
		if p.Name != llm.LlmPresetCustom.Name && p.BaseURL == strings.TrimSpace(c.config.BaseURL) {
			return p
		}
	}
	return llm.LlmPresetCustom
}

func (c *LlmConfigCard) selectPreset(p llm.LlmPreset) {
	// 自定义也记录预设名（兜底关闭参数形态）；非自定义回填端点与示例模型
	//（上游 onChange 语义）。
	if p.Name == llm.LlmPresetCustom.Name {
		c.config.Preset = p.Name
	} else {
		c.config.Preset = p.Name
		c.config.BaseURL = p.BaseURL
		c.config.Model = p.ExampleModel
	}
	c.SetConfig(c.config)
	c.notifyChange()
}

func (c *LlmConfigCard) notifyChange() {
	c.config = c.Config()
	if c.onChange != nil {
		c.onChange(c.config)
	}
}

// Layout 渲染并消费本帧事件（title 行 + 预设 chips + 三字段 + 提示 + 测试连接）。
func (c *LlmConfigCard) Layout(gtx layout.Context) layout.Dimensions {
	c.handleEvents(gtx)
	return c.draw(gtx)
}

func (c *LlmConfigCard) handleEvents(gtx layout.Context) {
	for i := range c.presetBtns {
		if c.presetBtns[i].Clicked(gtx) {
			c.selectPreset(c.presets[i])
		}
	}
	editorChanged := func(ed *widget.Editor) {
		for {
			_, ok := ed.Update(gtx)
			if !ok {
				break
			}
		}
		if ed.Text() != c.configFieldOf(ed) {
			c.notifyChange()
		}
	}
	editorChanged(&c.baseURL)
	editorChanged(&c.apiKey)
	editorChanged(&c.model)

	for i := range c.pasteBtns {
		if c.pasteBtns[i].Clicked(gtx) && c.OnPaste != nil {
			c.OnPaste(PasteTargetBaseURL + i)
		}
	}
	if c.testBtn.Clicked(gtx) && !c.testing && c.OnTestConnection != nil {
		c.SetTesting()
		c.OnTestConnection(c.Config(), c.slot)
	}
}

// configFieldOf 编辑器对应的配置字段当前值（变更检测基线）。
func (c *LlmConfigCard) configFieldOf(ed *widget.Editor) string {
	switch ed {
	case &c.baseURL:
		return c.config.BaseURL
	case &c.apiKey:
		return c.config.APIKey
	case &c.model:
		return c.config.Model
	}
	return ""
}

func (c *LlmConfigCard) draw(gtx layout.Context) layout.Dimensions {
	preset := c.currentPreset()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(PageTheme, c.title)
			l.Color = ThemeSeedDark
			l.TextSize = unit.Sp(15)
			return l.Layout(gtx)
		}),
		// 预设 chips（6 项 → 每行 2 项折行；Gio 无 <select>，chips 为同语义呈现）
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				children := []layout.FlexChild{}
				const perRow = 2
				for start := 0; start < len(c.presets); start += perRow {
					end := min(perRow, len(c.presets)-start)
					start, end := start, end
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						opts := make([]chipOpt, 0, end)
						for i := 0; i < end; i++ {
							p := c.presets[start+i]
							opts = append(opts, chipOpt{&c.presetBtns[start+i], p.Name, p.Name == preset.Name})
						}
						return layoutOptionChips(gtx, 150, opts...)
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			})
		}),
		layout.Rigid(c.fieldRow(&c.baseURL, "端点地址（Base URL）", "https://…/v4 或 https://…/v1", 0)),
		layout.Rigid(c.fieldRow(&c.apiKey, "API Key", "留空表示本地网关；已保存的 Key 以掩码回显", 1)),
		layout.Rigid(c.fieldRow(&c.model, "模型 ID", "如 glm-4-flash / deepseek-chat", 2)),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// DR-005：思维链强制关闭固定提示（无开关路径）。
			l := material.Body2(PageTheme, "思维链已强制关闭（按端点预设发送关闭参数，无需配置）。")
			l.Color = ThemeSeedDark
			l.TextSize = unit.Sp(13)
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			label := "测试连接"
			if c.testing {
				label = "测试中…"
			}
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(110))
				btn := material.Button(PageTheme, &c.testBtn, label)
				btn.Background = ThemeSurfaceDim
				btn.Color = ThemeSeedDark
				return btn.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if c.testResult == "" {
				return layout.Dimensions{}
			}
			l := material.Body2(PageTheme, c.testResult)
			if c.testOK {
				l.Color = ThemeSeedDark
			} else {
				l.Color = ThemeError
			}
			l.TextSize = unit.Sp(13)
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, l.Layout)
		}),
	)
}

// fieldRow 标签 + 输入框（Editor）+ 粘贴按钮（KG-004）。
func (c *LlmConfigCard) fieldRow(ed *widget.Editor, label, hint string, pasteIdx int) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Body2(PageTheme, label)
							l.Color = ThemeOnSurface
							l.TextSize = unit.Sp(14)
							return l.Layout(gtx)
						}),
						layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							size := image.Point{X: gtx.Dp(unit.Dp(44)), Y: gtx.Dp(unit.Dp(22))}
							return c.pasteBtns[pasteIdx].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								gtx.Constraints = layout.Exact(size)
								defer clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(unit.Dp(11))).Push(gtx.Ops).Pop()
								fillRect(gtx.Ops, image.Rectangle{Max: size}, ThemeSurface)
								return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									l := material.Body2(PageTheme, "粘贴")
									l.Color = ThemeSeedDark
									l.TextSize = unit.Sp(11)
									return l.Layout(gtx)
								})
							})
						}),
					)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutEditorBox(gtx, ed, hint)
				}),
			)
		})
	}
}

// layoutEditorBox 白底描边输入框（单行；POC-3 layoutEditor 的正式版收编）。
func layoutEditorBox(gtx layout.Context, ed *widget.Editor, hint string) layout.Dimensions {
	gtx.Constraints.Min.X = gtx.Constraints.Max.X
	gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(34))
	gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(34))
	return layout.Stack{Alignment: layout.NW}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
			fillRect(gtx.Ops, image.Rectangle{Max: gtx.Constraints.Max}, colorWhite)
			return layout.Dimensions{Size: gtx.Constraints.Max}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6), Top: unit.Dp(4), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				e := material.Editor(PageTheme, ed, hint)
				e.Color = ThemeOnSurface
				e.TextSize = unit.Sp(14)
				return e.Layout(gtx)
			})
		}),
	)
}
