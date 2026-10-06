// Package app 装配层：窗口创建、单事件循环、事件总线、页面路由、生命周期派发
// （design_docs/00 §2/§3、08 §1）。允许 import gioui.org 的包之一（铁律 #G1）；
// 禁止承载领域逻辑。
package app

import (
	"fmt"
	"log"
	"sync/atomic"

	"gioui.org/app"
	"gioui.org/io/key"
	"gioui.org/op"
	"gioui.org/unit"

	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// Config 启动配置。
type Config struct {
	Version string
}

// eventsBuffer 事件通道容量：主循环每帧非阻塞 drain，正常不触顶；
// 触顶按丢弃语义处理并记日志（后台 goroutine 禁止阻塞等待主循环）。
const eventsBuffer = 256

// WindowConfig 窗口参数。
type WindowConfig struct {
	Title         string
	Width, Height unit.Dp
}

// Window 装配一个 gio 窗口 + 事件总线 + 页面路由 + 生命周期派发。
type Window struct {
	*app.Window

	bus       *EventBus
	router    *Router
	lifecycle *LifecycleRouter
	// 存储装配（T2'.1，07 §1/§4/§5）：数据目录懒打开 + 全局设置单例
	//（铁律 #G4 例外面，上游同口径）。
	store    *DataStore
	settings *state.GlobalSettings
	// 自动保存生命周期总线（T2'.2，07 §2）：blur/minimize 相位 → 各页 GameAutoSave。
	autoSaveBus *state.LifecycleBus
	repo        *gameRepo
}

// Store 存储装配（M2' 起对局页经 repo 适配器使用）。
func (w *Window) Store() *DataStore { return w.store }

// Settings 全局设置单例。
func (w *Window) Settings() *state.GlobalSettings { return w.settings }

// emitFunc 供 repo 适配器等后台提交事件的回调面（ui 不 import app 的解耦点）。
func (w *Window) emitFunc() func(requestID string, payload any, err error) {
	return func(requestID string, payload any, err error) {
		w.Emit(AppEvent{RequestID: requestID, Payload: payload, Err: err})
	}
}

// OpenWindow 创建窗口。
func OpenWindow(cfg WindowConfig) *Window {
	w := new(app.Window)
	w.Option(app.Title(cfg.Title), app.Size(cfg.Width, cfg.Height))
	return &Window{
		Window:    w,
		bus:       NewEventBus(eventsBuffer),
		router:    NewRouter(),
		lifecycle: NewLifecycleRouter(),
	}
}

// openDataStore 数据目录装配（07 §1）；目录不可用时降级（log + 空目录）。
func openDataStore() *DataStore {
	dir, err := DataDir()
	if err != nil {
		log.Println("app: 数据目录不可用（本地存储降级）:", err)
		return OpenDataStore("")
	}
	return OpenDataStore(dir)
}

// Emit 供后台 goroutine 提交事件并排帧（Invalidate 线程安全）。
func (w *Window) Emit(ev AppEvent) {
	w.bus.Emit(ev)
	w.Invalidate()
}

// Cancel 取消异步请求：迟到结果按 requestId 丢弃（铁律 #G5）。
func (w *Window) Cancel(requestID string) { w.bus.Cancel(requestID) }

// Router 路由表（页面注册）。
func (w *Window) Router() *Router { return w.router }

// Lifecycle 生命周期派发器（M2' 自动保存状态机挂接）。
func (w *Window) Lifecycle() *LifecycleRouter { return w.lifecycle }

// Navigate 页面切换并排帧（旧页 dispose 由 Router 负责）。
func (w *Window) Navigate(rt Route) error {
	if err := w.router.Navigate(rt); err != nil {
		return err
	}
	w.Invalidate()
	return nil
}

// EventTarget 页面可选实现：接收事件总线负载（Window.Run 每帧分发；
// 页面侧在主 goroutine 内消费——铁律 #G3 的正方向）。
type EventTarget interface {
	OnAppEvent(payload any)
}

// Run 主 goroutine 单事件循环（00 §3）：FrameEvent 构造 gtx → drain 事件总线
// （requestId 过滤）→ 页面事件分发 → 页面 Layout → 提交帧。
// page 非空 = 单页模式（cmd/poc 使用）；nil = 走路由表当前页。
// 窗口生命周期事件在此翻译为 LifecycleRouter 派发（07 §2 表）：
// FocusEvent 失焦 → OnBlur；ClosingEvent → OnClose（M2' 自动保存后放行）。
func (w *Window) Run(page ui.Page) error {
	var ops op.Ops
	currentPage := func() ui.Page {
		if page != nil {
			return page
		}
		return w.router.Page()
	}
	for {
		switch e := w.Event().(type) {
		case app.DestroyEvent:
			if p := currentPage(); p != nil {
				if d, ok := p.(Disposer); ok {
					d.Dispose()
				}
			}
			return e.Err
		case *app.ClosingEvent:
			// 关闭请求：M2' 在此自动保存（有界等待 best-effort，07 §2）后放行；
			// M1' 直接放行退出。
			DispatchCloseRequest(w.lifecycle)
		case key.FocusEvent:
			DispatchFocusChanged(w.lifecycle, e.Focus)
		case app.FrameEvent:
			// 全局界面缩放（08 §9，Gio 版新增）：设置乘数叠加系统检测值
			//（X11 侧 gio 读 Xft.dpi，缺省 96dpi=1.0）。逐帧读取——设置弹窗
			// 调整即刻生效；仅影响 dp/sp 换算，指针命中两侧同为像素坐标不受影响。
			if w.settings != nil && w.settings.UIScale > 1.0 {
				e.Metric = unit.Metric{
					PxPerDp: e.Metric.PxPerDp * float32(w.settings.UIScale),
					PxPerSp: e.Metric.PxPerSp * float32(w.settings.UIScale),
				}
			}
			gtx := app.NewContext(&ops, e)
			cur := currentPage()
			for _, ev := range w.bus.Drain() {
				if t, ok := cur.(EventTarget); ok {
					t.OnAppEvent(ev.Payload)
				}
			}
			if cur != nil {
				cur.Layout(gtx)
			}
			e.Frame(gtx.Ops)
		}
	}
}

// requestSeq 异步请求 ID 序列（requestId 全局唯一，铁律 #G5）。
var requestSeq atomic.Int64

// newRequestID 生成页面异步请求 ID（前缀-序号）。
func newRequestID(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, requestSeq.Add(1))
}

// gameEnv 构造对局页环境（T2'.2：ui 不 import app 的解耦点）。
func (w *Window) gameEnv() ui.GameEnv {
	return ui.GameEnv{
		Settings:     w.settings,
		Bus:          w.autoSaveBus,
		Repo:         w.repo,
		DB:           w.repo,
		Emit:         w.emitFunc(),
		Cancel:       w.Cancel,
		NewRequestID: newRequestID,
	}
}

// llmEnv 构造 LLM 页环境（T4'.2：凭据槽位/设置异步代理 + 完整 Key 注入面）。
func (w *Window) llmEnv() ui.LlmEnv {
	return ui.LlmEnv{
		GameEnv:  w.gameEnv(),
		Store:    newLlmStore(w.store, w.emitFunc()),
		Settings: w.store.Settings(),
	}
}

// Run 组装主页 + 7 入口路由并进入事件循环（main.go 调用）。
func Run(cfg Config) error {
	w := OpenWindow(WindowConfig{
		Title:  windowTitle(realEnvProbe), // KG-005（D-002）：WSLg 内 ASCII 标题绕过
		Width:  unit.Dp(1024),
		Height: unit.Dp(768),
	})
	// 存储装配（T2'.1）：数据目录懒打开 + 全局设置单例加载（07 §5）。
	w.store = openDataStore()
	w.settings = state.NewGlobalSettings(w.store.Settings())
	w.settings.Load()
	defer w.store.Close()
	// 自动保存生命周期总线 + repo 异步代理（T2'.2，07 §2）。
	w.autoSaveBus = &state.LifecycleBus{}
	w.repo = newGameRepo(w.store, w.emitFunc())
	w.lifecycle.Add(&autosaveBridge{bus: w.autoSaveBus, window: w})
	w.Router().Register(RouteHome, ui.NewHomePage(ui.HomePageHooks{
		OnNavigate: func(id ui.EntryID) {
			if err := w.Navigate(routeOfEntry(id)); err != nil {
				log.Println("app: 导航失败:", err)
			}
		},
		Settings: w.settings,
	}))
	// 双人对弈页（T2'.4）：工厂页——每次导航进入创建新对局（铁律 #G4 每局一实例），
	// 离开 Dispose 触发离开保存（07 §2）。
	w.Router().RegisterFactory(RouteHumanVsHuman, func() ui.Page {
		return ui.NewHumanVsHumanPage(w.gameEnv(), ui.HumanVsHumanHooks{
			OnBack: func() {
				if err := w.Navigate(RouteHome); err != nil {
					log.Println("app: 返回主页失败:", err)
				}
			},
		})
	})
	// 人机对战页（T3'.2）：工厂页 + 引擎 Runner 直调（03 消费方式注记：结果经
	// 事件总线回主循环，requestId 取消与迟到丢弃语义不变）。
	w.Router().RegisterFactory(RouteHumanVsAi, func() ui.Page {
		return ui.NewHumanVsAiPage(w.gameEnv(), ui.HumanVsAiHooks{
			OnBack: func() {
				if err := w.Navigate(RouteHome); err != nil {
					log.Println("app: 返回主页失败:", err)
				}
			},
		})
	})
	// 人机 LLM 页（T4'.3）：工厂页 + LLM 流式客户端直调（05 附录消费方式注记：
	// chunk/done/error 经事件总线按 requestId 分发；凭据槽位经 llmStore 异步代理）。
	w.Router().RegisterFactory(RouteHumanVsLlm, func() ui.Page {
		return ui.NewHumanVsLlmPage(w.llmEnv(), ui.HumanVsLlmHooks{
			OnBack: func() {
				if err := w.Navigate(RouteHome); err != nil {
					log.Println("app: 返回主页失败:", err)
				}
			},
		})
	})
	// LLM vs LLM 页（T4'.3）：工厂页，双槽位配置 + 循环状态机。
	w.Router().RegisterFactory(RouteLlmVsLlm, func() ui.Page {
		return ui.NewLlmVsLlmPage(w.llmEnv(), ui.LlmVsLlmHooks{
			OnBack: func() {
				if err := w.Navigate(RouteHome); err != nil {
					log.Println("app: 返回主页失败:", err)
				}
			},
		})
	})
	// 语料库/棋谱库页（T5'.1）：工厂页——每次导航进入创建新实例并重扫
	//（上游 useEffect(load, []) 每挂载语义），离开 Dispose 取消在途解析/下载。
	w.Router().RegisterFactory(RouteCorpus, func() ui.Page {
		return ui.NewCorpusPage(w.corpusEnv(), ui.CorpusHooks{
			OnBack: func() {
				if err := w.Navigate(RouteHome); err != nil {
					log.Println("app: 返回主页失败:", err)
				}
			},
		})
	})
	// 其余 2 入口页后续里程碑逐个落地；先注册占位页保证主页可导航。
	for _, rt := range []Route{RouteEndgameSelect, RouteStudio} {
		rt := rt
		w.Router().Register(rt, ui.NewEntryPlaceholder(ui.EntryPlaceholder{
			Title: titleOfRoute(rt),
			OnBack: func() {
				if err := w.Navigate(RouteHome); err != nil {
					log.Println("app: 返回主页失败:", err)
				}
			},
		}))
	}
	if err := w.Navigate(RouteHome); err != nil {
		return err
	}
	return w.Run(nil)
}

// routeOfEntry 主页入口 ID → 路由。
func routeOfEntry(id ui.EntryID) Route {
	switch id {
	case ui.EntryEndgameSelect:
		return RouteEndgameSelect
	case ui.EntryHumanVsAi:
		return RouteHumanVsAi
	case ui.EntryHumanVsLlm:
		return RouteHumanVsLlm
	case ui.EntryLlmVsLlm:
		return RouteLlmVsLlm
	case ui.EntryHumanVsHuman:
		return RouteHumanVsHuman
	case ui.EntryStudio:
		return RouteStudio
	case ui.EntryCorpus:
		return RouteCorpus
	}
	return RouteHome
}

// titleOfRoute 占位页标题。
func titleOfRoute(rt Route) string {
	switch rt {
	case RouteEndgameSelect:
		return "残局选关"
	case RouteHumanVsAi:
		return "人机对战"
	case RouteHumanVsLlm:
		return "人机 LLM"
	case RouteLlmVsLlm:
		return "LLM vs LLM"
	case RouteHumanVsHuman:
		return "双人对弈"
	case RouteStudio:
		return "残局工作室"
	case RouteCorpus:
		return "棋谱库"
	}
	return string(rt)
}
