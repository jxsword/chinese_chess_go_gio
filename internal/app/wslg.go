package app

// WSLg 运行环境检测（KG-005 处置，决策记录 docs/decision_log.md D-002）。
// WSLg 的 RDP 桥接对窗口标题 _NET_WM_NAME（UTF-8 中文）解码错误（M0' xprop
// 核实应用侧正确，应用侧不可修）——检测命中时窗口标题改用 ASCII 绕过，
// 其余平台保持中文标题。

import (
	"os"
	"strings"
)

// envProbe 环境探测函数集（可注入供单测）。
type envProbe struct {
	stat        func(string) (os.FileInfo, error)
	procVersion func() ([]byte, error)
}

// realEnvProbe 真实环境探测：/mnt/wslg 目录存在性 + /proc/version 内核串。
var realEnvProbe = envProbe{
	stat: os.Stat,
	procVersion: func() ([]byte, error) {
		return os.ReadFile("/proc/version")
	},
}

// underWSLg 是否运行在 WSLg 下：
// ① /mnt/wslg 存在（微软官方 WSLg 标记目录）；② 兜底 /proc/version 含
// "microsoft"（WSL 内核标识，无 GUI 支持的 WSL 也命中，误报无害——仅影响标题语言）。
func underWSLg(p envProbe) bool {
	if p.stat != nil {
		if _, err := p.stat("/mnt/wslg"); err == nil {
			return true
		}
	}
	if p.procVersion != nil {
		if b, err := p.procVersion(); err == nil {
			return strings.Contains(strings.ToLower(string(b)), "microsoft")
		}
	}
	return false
}

// windowTitle 按运行环境返回窗口标题（KG-005：WSLg 内 ASCII 绕过桥接解码缺陷）。
func windowTitle(p envProbe) string {
	if underWSLg(p) {
		return "Chinese Chess Ultra (Gio)"
	}
	return "中国象棋 Ultra（Gio 版）"
}
