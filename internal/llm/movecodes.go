package llm

// 坐标编码（Electron 版 moveCodes.ts 1:1 移植；move_source.dart:1-28 同源）：
// 列 a-i（对应 col 0-8），行 0-9（0 为黑方底线/棋盘顶部，9 为红方底线）。
// 这是与 LLM 交互的着法文本格式，例如 "h7-e7"。
// 纯 Go：禁止 import Wails / net/http / frontend（铁律 #1）。

import (
	"fmt"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// EncodeCell 单格坐标编码（move_source.dart:11-12）。
func EncodeCell(p rules.Position) string {
	return fmt.Sprintf("%c%d", rune('a'+p.Col), p.Row)
}

// DecodeCell 解析单格坐标（如 "b2"）；格式非法/越界返回 nil（move_source.dart:14-21）。
func DecodeCell(cell string) *rules.Position {
	if len(cell) != 2 {
		return nil
	}
	col := int(cell[0]) - 97
	row := int(cell[1]) - 48
	if col < 0 || col > 8 || row < 0 || row > 9 {
		return nil
	}
	p := rules.Pos(col, row)
	return &p
}

// EncodeMove 把走法编码为 "起点-终点" 文本（move_source.dart:23）。
func EncodeMove(m rules.Move) string {
	return EncodeCell(m.From) + "-" + EncodeCell(m.To)
}
