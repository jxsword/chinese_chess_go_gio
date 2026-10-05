// Package ui Gio 自绘 UI 层：页面/组件/动画/绘制 ops（design_docs/00 §2）。
// 允许 import gioui.org（铁律 #G1）；禁止领域逻辑与 net/http；
// 后台 goroutine 禁止直写本包状态（铁律 #G3）。
package ui

import "gioui.org/layout"

// Page 可被 app 单事件循环驱动的最小页面契约（M0' 骨架形态；
// M1' 由 internal/app 页面路由栈接管，交互规格见 design_docs/08 §1）。
type Page interface {
	Layout(gtx layout.Context) layout.Dimensions
}
