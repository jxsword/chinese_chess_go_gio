// Package state 状态层（DR-G002）：以上游 frontend/src/stores 为行为锚点翻译为纯 Go。
// 禁止 import gioui.org / net-http / 任何 GUI 符号（铁律 #G1）；
// 对局状态由工厂创建，禁止全局单例（铁律 #G4）。
//
// M0' 仅目录占位；M1'（T1'.1/T1'.2）填充 gameVm/gameStore/globalSettings 骨架。
package state
