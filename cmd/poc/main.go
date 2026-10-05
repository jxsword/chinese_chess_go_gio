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
	page, title := demo(os.Args[1])
	if page == nil {
		fmt.Fprintf(os.Stderr, "poc: 未知 demo %q\n", os.Args[1])
		usage()
	}
	w := app.OpenWindow(app.WindowConfig{Title: title, Width: unit.Dp(720), Height: unit.Dp(920)})
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
