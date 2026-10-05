// Command poc M0' POC 排险演示入口（design_docs/11 §3 M0' 表 T0'.2~T0'.5）。
//
// 用法：go run ./cmd/poc <demo>
//
//	demo ∈ {board}（T0'.3~T0'.5 陆续追加 anim/ime/list）
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
	fmt.Fprintln(os.Stderr, "用法: go run ./cmd/poc <board>")
	os.Exit(2)
}

func demo(name string) (ui.Page, string) {
	switch name {
	case "board":
		return ui.NewPocBoard(), "POC-1 棋盘自绘"
	default:
		return nil, ""
	}
}
