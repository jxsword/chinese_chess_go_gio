// Package app 装配层：窗口创建、单事件循环、事件总线（design_docs/00 §2/§3）。
// 允许 import gioui.org 的包之一（铁律 #G1）；禁止承载领域逻辑。
package app

import (
	"log"

	"gioui.org/app"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// Config 启动配置。
type Config struct {
	Version string
}

// AppEvent 事件总线条目：后台 goroutine 的结果/进度/错误统一经此回主循环
// （00 §3：禁止后台 goroutine 直写 UI/state）。RequestID 供迟到结果丢弃
// （铁律 #G5，M1' 起接线）；本骨架仅保证通道与排帧语义。
type AppEvent struct {
	RequestID string
	Err       error
	Payload   any
}

// eventsBuffer 事件通道容量：主循环每帧非阻塞 drain，正常不触顶；
// 触顶按丢弃语义处理并记日志（后台 goroutine 禁止阻塞等待主循环）。
const eventsBuffer = 256

// WindowConfig 窗口参数。
type WindowConfig struct {
	Title         string
	Width, Height unit.Dp
}

// Window 装配一个 gio 窗口 + 事件总线。
// events 通道仅由主 goroutine 消费（单事件循环，铁律 #G3）。
type Window struct {
	*app.Window

	events chan AppEvent
}

// OpenWindow 创建窗口。
func OpenWindow(cfg WindowConfig) *Window {
	w := new(app.Window)
	w.Option(app.Title(cfg.Title), app.Size(cfg.Width, cfg.Height))
	return &Window{Window: w, events: make(chan AppEvent, eventsBuffer)}
}

// Emit 供后台 goroutine 提交事件并排帧（Invalidate 线程安全）。
// 非阻塞：通道满时丢弃并记日志。
func (w *Window) Emit(ev AppEvent) {
	select {
	case w.events <- ev:
	default:
		log.Println("app: 事件通道满，事件被丢弃（requestId=", ev.RequestID, "）")
	}
	w.Invalidate()
}

// Drain 主循环每帧非阻塞取走全部待处理事件（M1' 起按 requestId 分发到页面）。
func (w *Window) Drain() []AppEvent {
	var out []AppEvent
	for {
		select {
		case ev := <-w.events:
			out = append(out, ev)
		default:
			return out
		}
	}
}

// EventTarget 页面可选实现：接收事件总线负载（Window.Run 每帧分发；
// 页面侧在主 goroutine 内消费——铁律 #G3 的正方向）。
type EventTarget interface {
	OnAppEvent(payload any)
}

// Run 主 goroutine 单事件循环（00 §3）：FrameEvent 构造 gtx → drain 事件总线 →
// 页面事件分发 → 页面 Layout → 提交帧。窗口生命周期事件（DestroyEvent）在此收口。
func (w *Window) Run(page ui.Page) error {
	var ops op.Ops
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			if t, ok := page.(EventTarget); ok {
				for _, ev := range w.Drain() {
					t.OnAppEvent(ev.Payload)
				}
			} else {
				w.Drain()
			}
			page.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// Run 组装默认主页并进入事件循环（main.go 调用）。
func Run(cfg Config) error {
	w := OpenWindow(WindowConfig{
		Title:  "中国象棋 Ultra（Gio 版）",
		Width:  unit.Dp(1024),
		Height: unit.Dp(768),
	})
	return w.Run(&ui.Placeholder{Version: cfg.Version})
}
