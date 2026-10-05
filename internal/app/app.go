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

// App 装配对象：窗口 + 事件总线 + 当前页面。
// Events 通道仅由主 goroutine 消费（单事件循环，铁律 #G3）。
type App struct {
	Window  *app.Window
	Version string

	events chan AppEvent
	page   ui.Page
}

// 事件通道容量：主循环每帧非阻塞 drain，正常不触顶；触顶按丢帧语义丢弃并记日志。
const eventsBuffer = 256

// New 创建装配对象并打开主窗口。
func New(cfg Config, page ui.Page) *App {
	w := new(app.Window)
	w.Option(app.Title("中国象棋 Ultra（Gio 版）"), app.Size(unit.Dp(1024), unit.Dp(768)))
	return &App{
		Window:  w,
		Version: cfg.Version,
		events:  make(chan AppEvent, eventsBuffer),
		page:    page,
	}
}

// Emit 供后台 goroutine 提交事件并排帧（Invalidate 线程安全）。
// 非阻塞：通道满时丢弃并记日志（00 §3 后台 goroutine 禁止阻塞等待主循环）。
func (a *App) Emit(ev AppEvent) {
	select {
	case a.events <- ev:
	default:
		log.Println("app: 事件通道满，事件被丢弃（requestId=", ev.RequestID, "）")
	}
	a.Window.Invalidate()
}

// Run 主 goroutine 单事件循环（00 §3）：FrameEvent 构造 gtx → drain 事件总线 →
// 当前页面 Layout → 提交帧。窗口生命周期事件（DestroyEvent）在此收口。
func (a *App) Run() error {
	var ops op.Ops
	for {
		switch e := a.Window.Event().(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			a.drainEvents()
			a.page.Layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

// drainEvents 非阻塞消费事件总线。骨架阶段仅统计；M1' 起按 requestId 分发到页面。
func (a *App) drainEvents() {
	for {
		select {
		case <-a.events:
		default:
			return
		}
	}
}

// Run 组装默认主页并进入事件循环（main.go 调用）。
func Run(cfg Config) error {
	a := New(cfg, &ui.Placeholder{Version: cfg.Version})
	return a.Run()
}
