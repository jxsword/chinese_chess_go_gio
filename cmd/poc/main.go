// Command poc M0' POC 排险演示入口（design_docs/11 §3 M0' 表 T0'.2~T0'.5）。
//
// 用法：go run ./cmd/poc <demo>
//
//	demo ∈ {board, anim, ime, list}
//
// 各 demo 独立开窗，互不依赖正式页面；POC 实现位于 internal/ui/poc_*.go，
// 正式里程碑重写不继承。
package main

import (
	"fmt"
	"os"

	"gioui.org/unit"

	"github.com/jxsword/chinese_chess_go_gio/internal/app"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	if os.Args[1] == "corpus" {
		demoCorpus()
		return
	}
	page, title := demo(os.Args[1])
	if page == nil {
		fmt.Fprintf(os.Stderr, "poc: 未知 demo %q\n", os.Args[1])
		usage()
	}
	w := app.OpenWindow(app.WindowConfig{Title: title, Width: unit.Dp(720), Height: unit.Dp(920)})
	if binder, ok := page.(interface{ BindEvents(func(any)) }); ok {
		binder.BindEvents(func(ev any) { w.Emit(app.AppEvent{Payload: ev}) })
	}
	if err := w.Run(page); err != nil {
		fmt.Fprintln(os.Stderr, "poc:", err)
		os.Exit(1)
	}
}

// demoCorpus M5' 语料库页渲染冒烟（POC 专用，正式页面走 go run .）：
// CC_CORPUS_ROOT 指向语料根目录（空 = 未找到本地语料的下载引导分支）。
func demoCorpus() {
	w := app.OpenWindow(app.WindowConfig{Title: "M5' corpus", Width: unit.Dp(1024), Height: unit.Dp(768)})
	seq := 0
	page := ui.NewCorpusPage(ui.CorpusEnv{
		Emit: func(id string, payload any, err error) {
			w.Emit(app.AppEvent{RequestID: id, Payload: payload, Err: err})
		},
		Cancel:       w.Cancel,
		NewRequestID: func(prefix string) string { seq++; return fmt.Sprintf("%s-%d", prefix, seq) },
		Root:         func() string { return os.Getenv("CC_CORPUS_ROOT") },
	}, ui.CorpusHooks{})
	if err := w.Run(page); err != nil {
		fmt.Fprintln(os.Stderr, "poc:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: go run ./cmd/poc <board|anim|ime|list>")
	os.Exit(2)
}

func demo(name string) (ui.Page, string) {
	switch name {
	case "board":
		return ui.NewPocBoard(), "POC-1 棋盘自绘"
	case "anim":
		return ui.NewPocAnim(), "POC-2 220ms 飞行动画帧循环"
	case "ime":
		return ui.NewPocIme(), "POC-3 中文 IME + 字体回退链"
	case "list":
		return ui.NewPocList(), "POC-4 长列表虚拟化（14 万局）"
	default:
		return nil, ""
	}
}
