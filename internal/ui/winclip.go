package ui

// KG-004 可靠粘贴路径（POC-3 端到端实证，M4' 配置卡沿用——08 §10 回填注记）：
// WSLg 剪贴板桥接对 CJK 乱码且有损（Windows→WSL 方向），Ctrl+V 仅适用于 ASCII；
// 可靠路径 = 后台 goroutine 经 powershell.exe Get-Clipboard 管道读取 Windows
// 剪贴板（UTF-8 正确），结果经回调送事件总线（铁律 #G3：I/O 不进主循环）。

import (
	"errors"
	"os/exec"
	"strings"
)

// pasteFromWindowsAsync 异步读取 Windows 剪贴板文本（仅 WSL 环境；done 在后台
// goroutine 调用一次——实现须只发事件/加锁，不得直写 UI）。
func pasteFromWindowsAsync(done func(text string, err error)) {
	if done == nil {
		return
	}
	go func() {
		if !inWSL() {
			done("", errPasteUnavailable)
			return
		}
		candidates := []string{
			"/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe", // interop PATH 未注入时的兜底
			"powershell.exe",
		}
		var out []byte
		var err error
		for _, exe := range candidates {
			out, err = exec.Command(exe, "-NoProfile", "-Command",
				"[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; Get-Clipboard").Output()
			if err == nil {
				break
			}
		}
		done(strings.TrimSpace(string(out)), err)
	}()
}

// errPasteUnavailable 非 WSL 环境（无 Windows 剪贴板可读）。
var errPasteUnavailable = errors.New("ui: 当前环境无 Windows 剪贴板")
