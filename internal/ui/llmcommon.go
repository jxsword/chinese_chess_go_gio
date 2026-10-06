package ui

// LLM 两页共享组件（T4'.3，design_docs/08 §5）：
//   - streamMessageArea 流式消息区：chunk 追加文本渲染 + 备注/否决链条目，
//     滚动区随新条目置底（立即模式全帧重绘，chunk 事件经总线回主循环后排帧）；
//   - choiceRow 标签+chips 选择行（上游 <select> 的 Gio 呈现，M3' chips 先例）；
//   - thinkingSuffix 思考状态后缀（已思考秒数 + 第 N/M 次尝试）。

import (
	"fmt"
	"image"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
)

// ---- 流式消息区 ----

// streamMessageArea 流式消息区 state（每页一实例；主 goroutine 独占）。
// 条目两段式：chunk 追加进当前流式行（requestId 匹配由页面收口——K21 防线），
// 完成后固化为整行；备注/否决链为独立条目。
// **不内嵌 layout.List**（M4' 验收反馈第 7 轮修正）：内嵌列表会在消息卡片
// 区域盗走全部滚轮/拖拽事件（自己无内容可滚也不放行），外层侧板列表在
// 消息区上方永远滚不动——消息区改为普通列随侧板单列滚动。
type streamMessageArea struct {
	lines     []string
	streaming bool // 有流式行在追加（行号 = len(lines)-1）
}

func newStreamMessageArea() *streamMessageArea {
	return &streamMessageArea{}
}

// begin 开启一条流式行（label 为前缀，如"黑方"）。
func (a *streamMessageArea) begin(label string) {
	a.lines = append(a.lines, label+" ")
	a.streaming = true
}

// appendChunk 追加增量（仅 Content 正文——思维链恒关，DR-005；Reasoning 不呈现）。
func (a *streamMessageArea) appendChunk(delta llm.Delta) {
	if delta.Content == nil || !a.streaming || len(a.lines) == 0 {
		return
	}
	a.lines[len(a.lines)-1] += *delta.Content
}

// endStream 流式行收尾：text 为空则原样固化（done 的全量文本与 chunk 累积一致，
// 以 chunk 累积为准——done 只负责关闭流式态）。
func (a *streamMessageArea) endStream() {
	a.streaming = false
}

// appendLine 追加备注/否决链条目（独立行；超上限从最旧侧丢弃）。
func (a *streamMessageArea) appendLine(line string) {
	a.lines = append(a.lines, line)
	if len(a.lines) > streamMessageMaxLines {
		a.lines = a.lines[len(a.lines)-streamMessageMaxLines:]
	}
}

// clear 清空（新游戏路径）。
func (a *streamMessageArea) clear() {
	a.lines = nil
	a.streaming = false
}

// streamMessageMaxLines 消息区留存上限（超出从最旧侧丢弃——消息区是
// 流式过程/否决链的临时呈现，完整 note 仍在状态条与走法注记）。
const streamMessageMaxLines = 60

// layout 渲染消息区：**固定高度、只显示最新若干行**（tail）——不无限增高
// （无限增高会把配置/设置区挤出视口——M4' 验收反馈第 8 轮实证），也不内嵌
// 列表（嵌套列表盗取滚轮事件——第 7 轮实证）。文本按面板宽度自动换行。
func (a *streamMessageArea) layout(gtx layout.Context) layout.Dimensions {
	n := len(a.lines)
	if n == 0 {
		l := material.Body2(PageTheme, "消息区：模型回复将在此流式显示")
		l.Color = ThemeSeedDark
		l.TextSize = unit.Sp(15)
		return l.Layout(gtx)
	}
	// tail：从最新行倒序取，直到超过可见预算（~10 行）。
	const maxVisible = 10
	start := 0
	if n > maxVisible {
		start = n - maxVisible
	}
	children := make([]layout.FlexChild, 0, n-start)
	for _, line := range a.lines[start:] {
		line := line
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(PageTheme, line)
			l.Color = ThemeOnSurface
			l.TextSize = unit.Sp(15)
			return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
		}))
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
}

// ---- 选择 chips 行 ----

// choiceRow 标签 + chips 选择行（上游 <select> 的 Gio 呈现）。
type choiceRow struct {
	values []int
	labels []string
	clicks []widget.Clickable
}

func newChoiceRow(values []int, labels []string) *choiceRow {
	return &choiceRow{values: values, labels: labels, clicks: make([]widget.Clickable, len(values))}
}

// consume 消费本帧点击边沿（页面 handleEvents 内调用；返回新选中值，
// 无点击 = current）。与 draw 分离：消费面每帧全量、绘制面仅可见行
// （virtualized list 行投影——humanvsai handleEvents/layout 两段同构）。
func (c *choiceRow) consume(gtx layout.Context, current int) int {
	for i := range c.clicks {
		if c.clicks[i].Clicked(gtx) {
			return c.values[i]
		}
	}
	return current
}

// draw 渲染标签 + chips 行（selected 高亮当前值）。
func (c *choiceRow) draw(gtx layout.Context, title string, current int, titleWidth int) layout.Dimensions {
	children := []layout.FlexChild{
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(titleWidth))
			l := material.Body2(PageTheme, title)
			l.Color = ThemeOnSurface
			l.TextSize = unit.Sp(16)
			return l.Layout(gtx)
		}),
	}
	for i := range c.clicks {
		i := i
		selected := c.values[i] == current
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutOptionChips(gtx, 44, chipOpt{&c.clicks[i], c.labels[i], selected})
		}))
	}
	return layout.Inset{Top: unit.Dp(5), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// ---- 状态后缀 ----

// thinkingSuffix 思考状态后缀："（已思考 Ns）" 或 "（已思考 Ns，第 M/K 次尝试）"
// （上游 useThinkingStatus.formatThinkingSuffix 同语义）。
func thinkingSuffix(startedAt time.Time, attemptN, attemptTotal int) string {
	secs := int(time.Since(startedAt).Seconds())
	if attemptN > 0 {
		return fmt.Sprintf("（已思考 %ds，第 %d/%d 次尝试）", secs, attemptN, attemptTotal)
	}
	return fmt.Sprintf("（已思考 %ds）", secs)
}

// sectionTitle 小节标题行。
func sectionTitle(gtx layout.Context, s string) layout.Dimensions {
	l := material.Body2(PageTheme, s)
	l.Color = ThemeSeedDark
	l.TextSize = unit.Sp(17)
	return layout.Inset{Top: unit.Dp(10), Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
}

// hintLine 弱化提示行（镜像提示/固定提示语）。
func hintLine(gtx layout.Context, s string) layout.Dimensions {
	l := material.Body2(PageTheme, s)
	l.Color = ThemeSeedDark
	l.TextSize = unit.Sp(15)
	return layout.Inset{Top: unit.Dp(3)}.Layout(gtx, l.Layout)
}

// messageAreaCard 流式消息区卡片容器（随内容自然增高——不设定高，
// 随侧板单列滚动；M4' 验收反馈第 7 轮）。
func messageAreaCard(gtx layout.Context, area *streamMessageArea, title string) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return sectionTitle(gtx, title)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Constraints.Max.X
			defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
			paint.Fill(gtx.Ops, ThemeSurface)
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, area.layout)
		}),
	)
}
