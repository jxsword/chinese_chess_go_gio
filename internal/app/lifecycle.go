package app

// 生命周期事件派发（design_docs/07 §2 事件源映射表）：上游 WebView app:lifecycle
// 事件消失，替代为 gio 窗口事件 → 本派发器 → state 自动保存挂接（M2' 接线）。
//
// v0.10.3 实测口径（07 §2 表修订，随 M1' 落档）：
//   - blur ← key.FocusEvent{Focus:false}（窗口焦点变化经 ConfigEvent 转
//     FocusEvent 入队；字段名为 Focus 而非文档预设的 Focused）
//   - close ← *app.ClosingEvent（无 system.CommandClose；可 Abort 挂起关闭，
//     M2' 自动保存有界等待后放行）
//   - minimize ← gio 无直接窗口事件（system.ActionMinimize 仅为发起动作），
//     各平台覆盖面留 M2' POC 核实；派发通道先就位

import (
	"gioui.org/io/key"

	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// LifecycleHandler 生命周期挂接点（M2' 由 state 自动保存状态机实现）。
type LifecycleHandler interface {
	OnBlur()
	OnMinimize()
	OnClose()
}

// LifecycleRouter 生命周期 handler 注册表与派发器。
type LifecycleRouter struct {
	handlers []LifecycleHandler
}

// NewLifecycleRouter 创建派发器。
func NewLifecycleRouter() *LifecycleRouter { return &LifecycleRouter{} }

// Add 注册 handler（按注册顺序派发）。
func (lr *LifecycleRouter) Add(h LifecycleHandler) { lr.handlers = append(lr.handlers, h) }

// DispatchFocusChanged 窗口焦点变化翻译：失焦派发 OnBlur，获焦不派发。
func DispatchFocusChanged(lr *LifecycleRouter, focused bool) {
	if focused {
		return
	}
	for _, h := range lr.handlers {
		h.OnBlur()
	}
}

// DispatchCloseRequest 关闭请求翻译（ClosingEvent → OnClose；M2' 有界等待后放行）。
func DispatchCloseRequest(lr *LifecycleRouter) {
	for _, h := range lr.handlers {
		h.OnClose()
	}
}

// DispatchMinimize 最小化翻译（事件源 M2' 核实：gio 无直接窗口事件；
// WSLg 实测最小化伴随失焦，blur 相位覆盖，07 §2 表）。
func DispatchMinimize(lr *LifecycleRouter) {
	for _, h := range lr.handlers {
		h.OnMinimize()
	}
}

// autosaveBridge 生命周期 → state 自动保存桥接（07 §2 落地口径）：
// blur/minimize → LifecycleBus.Notify()（广播给当前页 GameAutoSave）；
// close → 当前页 ui.CloseHandler.OnClose()（同步保存 best-effort 后放行，
// 不走总线——总线写入为 fire-and-forget，退出前可能未落盘）。
type autosaveBridge struct {
	bus    *state.LifecycleBus
	window *Window
}

func (b *autosaveBridge) OnBlur() { b.bus.Notify() }

// OnMinimize gio 无直接窗口事件源（07 §2 minimize 行：system.ActionMinimize 仅为
// 发起动作；WSLg 实测最小化伴随失焦，blur 相位覆盖）。保留相位派发点，直接并入
// 总线广播。
func (b *autosaveBridge) OnMinimize() { b.bus.Notify() }
func (b *autosaveBridge) OnClose() {
	if b.window == nil {
		return
	}
	if p, ok := b.window.router.Page().(ui.CloseHandler); ok {
		p.OnClose()
	}
}

// focusLost 供 Window.Run 事件循环翻译 key.FocusEvent。
func (lr *LifecycleRouter) focusLost(e key.FocusEvent) {
	DispatchFocusChanged(lr, e.Focus)
}
