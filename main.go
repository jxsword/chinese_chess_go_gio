// 薄入口（design_docs/00 §5）：仅启动装配层，不承载页面/领域逻辑。
package main

import (
	"fmt"
	"os"

	"github.com/jxsword/chinese_chess_go_gio/internal/app"
)

// version 由生产构建注入：go build -ldflags "-X main.version=..."（00 §6）。
var version = "dev"

func main() {
	if err := app.Run(app.Config{Version: version}); err != nil {
		fmt.Fprintln(os.Stderr, "chinese-chess-ultra-gio:", err)
		os.Exit(1)
	}
}
