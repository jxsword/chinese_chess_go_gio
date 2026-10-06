package app

// 页面路由（design_docs/08 §1）：internal/app 维护页面栈（主页 + 7 入口 + 弹窗层）；
// 每页一个 state struct（铁律 #G3 独占）；页面切换触发旧页 dispose
//（自动保存/输入锁解锁挂接点，07 §2——M2' 接入 state）。

import (
	"errors"
	"fmt"

	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// Route 应用路由（主页 + 08 §2 七入口）。
type Route string

const (
	RouteHome          Route = "home"
	RouteEndgameSelect Route = "endgameSelect" // 残局选关
	RouteHumanVsAi     Route = "humanVsAi"     // 人机对战
	RouteHumanVsLlm    Route = "humanVsLlm"    // 人机 LLM
	RouteLlmVsLlm      Route = "llmVsLlm"      // LLM vs LLM
	RouteHumanVsHuman  Route = "humanVsHuman"  // 双人对弈
	RouteStudio        Route = "studio"        // 残局工作室
	RouteCorpus        Route = "corpus"        // 棋谱库
)

// ErrRouteNotRegistered 导航到未注册路由。
var ErrRouteNotRegistered = errors.New("app: 路由未注册")

// Disposer 页面可选实现：页面切换离开时调用（07 §2 dispose 挂接点）。
type Disposer interface {
	Dispose()
}

// Router 页面路由表（M1' 单层切换；弹窗层为页面内 Stack，M2' 落地）。
type Router struct {
	current Route
	pages   map[Route]ui.Page
}

// NewRouter 创建路由（初始无当前页）。
func NewRouter() *Router {
	return &Router{pages: map[Route]ui.Page{}}
}

// Register 注册路由页面（重复注册覆盖）。
func (r *Router) Register(rt Route, p ui.Page) {
	r.pages[rt] = p
}

// Current 当前路由。
func (r *Router) Current() Route { return r.current }

// Page 当前页面（未导航时为 nil）。
func (r *Router) Page() ui.Page { return r.pages[r.current] }

// Navigate 切换路由：旧页实现 Disposer 则调用其 Dispose；
// 未注册路由返回 ErrRouteNotRegistered 且状态不变。
func (r *Router) Navigate(rt Route) error {
	if rt == r.current {
		return nil
	}
	target, ok := r.pages[rt]
	if !ok {
		return fmt.Errorf("%w: %q", ErrRouteNotRegistered, rt)
	}
	if old, ok := r.pages[r.current]; ok {
		if d, ok := old.(Disposer); ok {
			d.Dispose()
		}
	}
	r.current = rt
	_ = target
	return nil
}
