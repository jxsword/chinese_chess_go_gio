package rules

// Package rules 中国象棋规则内核（对应 Electron 版 src/packages/rules，02 文档）。
//
// 纯 Go 包：禁止 import Wails / net/http / frontend / 任何 GUI 或运行时绑定符号
//（铁律 #1），可被 go test、CLI（cmd/eval）、Wails 后端三端直接调用。

// Position 坐标（对应 move.dart Position）：列 0..8（从左到右），行 0..9（红方在下）。
//
// 注意：作为方向向量使用时 Col/Row 可能为负数或越界（如马腿偏移），
// 因此不做范围断言，由调用方用 InBoard 校验（02 文档 §1.1）。
type Position struct {
	Col int
	Row int
}

// Pos 构造坐标。
func Pos(col, row int) Position { return Position{Col: col, Row: row} }

// AddPos 加法运算（结果可能越界，调用方需检查）。
func AddPos(a, b Position) Position {
	return Position{Col: a.Col + b.Col, Row: a.Row + b.Row}
}

// SamePos 值相等（move.dart operator==）。
func SamePos(a, b Position) bool { return a.Col == b.Col && a.Row == b.Row }

// InBoard 是否在棋盘内（board.dart:50-51）。
func InBoard(col, row int) bool {
	return col >= 0 && col < 9 && row >= 0 && row < 10
}
