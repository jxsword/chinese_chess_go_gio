//go:build !windows

package ui

// 非 Windows 平台：无控制台宿主窗概念（no-op）。

import "syscall"

func hideWindowAttr() *syscall.SysProcAttr { return nil }
