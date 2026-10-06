package app

// 页面路由单测（design_docs/09 §4：路由切换纯 Go 表驱动，不渲染）。
// 语义锚点 = 08 §1：internal/app 维护页面栈；页面切换触发旧页 dispose
//（自动保存/输入锁解锁挂接点，07 §2——M2' 接入 state）。

import (
	"errors"
	"testing"

	"gioui.org/layout"

	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// fakePage 记录 dispose 次数。
type fakePage struct {
	disposed int
}

func (f *fakePage) Layout(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }
func (f *fakePage) Dispose()                                    { f.disposed++ }

// 未实现 Disposer 的页面：切换不应 panic。
type plainPage struct{}

func (plainPage) Layout(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }

// 主页 → 入口页 → 返回主页：旧页 dispose 恰一次，当前页正确。
func TestRouter_NavigateDisposesOldPage(t *testing.T) {
	router := NewRouter()
	home := &fakePage{}
	entry := &fakePage{}
	router.Register(RouteHome, home)
	router.Register(RouteHumanVsHuman, entry)

	router.Navigate(RouteHome)
	router.Navigate(RouteHumanVsHuman)
	if router.Current() != RouteHumanVsHuman {
		t.Fatalf("current = %q, want humanVsHuman", router.Current())
	}
	if entry.disposed != 0 {
		t.Fatalf("在页 dispose = %d, want 0", entry.disposed)
	}
	if home.disposed != 1 {
		t.Fatalf("主页（离开时）dispose = %d, want 1", home.disposed)
	}
	router.Navigate(RouteHome)
	if entry.disposed != 1 {
		t.Fatalf("入口页（离开时）dispose = %d, want 1", entry.disposed)
	}
	if home.disposed != 1 {
		t.Fatalf("切回主页不应再次 dispose 主页 = %d, want 1", home.disposed)
	}
	if router.Current() != RouteHome || router.Page() != ui.Page(home) {
		t.Fatal("切回主页后当前页应为主页")
	}
}

// 重复导航同一路由：不重复 dispose（无页面切换）。
func TestRouter_NavigateSameRouteNoop(t *testing.T) {
	router := NewRouter()
	home := &fakePage{}
	router.Register(RouteHome, home)
	router.Navigate(RouteHome)
	router.Navigate(RouteHome)
	if home.disposed != 0 {
		t.Fatalf("同路由重复导航 dispose = %d, want 0", home.disposed)
	}
}

// 未注册路由：报错且当前页不变。
func TestRouter_NavigateUnregisteredFails(t *testing.T) {
	router := NewRouter()
	home := &fakePage{}
	router.Register(RouteHome, home)
	router.Navigate(RouteHome)
	err := router.Navigate(RouteCorpus)
	if err == nil {
		t.Fatal("未注册路由应返回错误")
	}
	if !errors.Is(err, ErrRouteNotRegistered) {
		t.Fatalf("err = %v, want ErrRouteNotRegistered", err)
	}
	if router.Current() != RouteHome {
		t.Fatal("导航失败后当前页应不变")
	}
}

// 非 Disposer 页面切换不 panic 且正常切换。
func TestRouter_PlainPageSwitch(t *testing.T) {
	router := NewRouter()
	router.Register(RouteHome, plainPage{})
	router.Register(RouteStudio, plainPage{})
	router.Navigate(RouteHome)
	router.Navigate(RouteStudio)
	if router.Current() != RouteStudio {
		t.Fatal("普通页面应可正常切换")
	}
}

// 主页 7 入口路由全部可注册可导航（08 §2 入口清单锚定）。
func TestRouter_HomeSevenEntriesNavigable(t *testing.T) {
	router := NewRouter()
	pages := map[Route]ui.Page{}
	for _, rt := range []Route{RouteHome, RouteEndgameSelect, RouteHumanVsAi, RouteHumanVsLlm, RouteLlmVsLlm, RouteHumanVsHuman, RouteStudio, RouteCorpus} {
		p := &fakePage{}
		pages[rt] = p
		router.Register(rt, p)
	}
	for _, rt := range []Route{RouteEndgameSelect, RouteHumanVsAi, RouteHumanVsLlm, RouteLlmVsLlm, RouteHumanVsHuman, RouteStudio, RouteCorpus} {
		router.Navigate(RouteHome)
		router.Navigate(rt)
		if router.Current() != rt {
			t.Fatalf("导航到 %q 失败，current = %q", rt, router.Current())
		}
	}
	// 每轮"离开主页→进入入口页"各 dispose 主页一次（07 §2 离开即 dispose）。
	if got := pages[RouteHome].(*fakePage).disposed; got != 7 {
		t.Fatalf("主页 dispose = %d, want 7（每轮离开一次）", got)
	}
	// 已返回过主页的入口页各 dispose 一次；最后停留的棋谱库页仍在页（0 次）。
	for _, rt := range []Route{RouteEndgameSelect, RouteHumanVsAi, RouteHumanVsLlm, RouteLlmVsLlm, RouteHumanVsHuman, RouteStudio} {
		if got := pages[rt].(*fakePage).disposed; got != 1 {
			t.Fatalf("%q dispose = %d, want 1", rt, got)
		}
	}
	if got := pages[RouteCorpus].(*fakePage).disposed; got != 0 {
		t.Fatalf("在页（棋谱库）dispose = %d, want 0", got)
	}
}
