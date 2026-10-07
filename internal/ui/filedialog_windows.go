//go:build windows

package ui

// Windows 专用的 SysProcAttr（HideWindow 隐藏对话框控制台宿主窗）。

import (
	"syscall"
)

func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
