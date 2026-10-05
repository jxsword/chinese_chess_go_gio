package llm

// 着法注解与引擎分数分桶（05 文档 §2.2 清单行规则；Electron 版 annotation.ts
// 1:1 移植，move_annotation.dart 同源）。
//
// 所有标注信息（棋子/中文记法/吃子/将军/分数分桶）均由本地规则引擎
// 与搜索结果生成，零成本、零幻觉——给 LLM 的"战术眼镜"。
// 纯 Go：禁止 import Wails / net/http / frontend（铁律 #1）。

import (
	"fmt"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// AnnotateMove 生成带注解的着法文本：`b2-e2(炮二平五,吃卒,将军)`
// （move_annotation.dart:17-31）。board 为走子前的局面（move 的起点须有棋子）；
// 起点无棋子时退化为纯坐标。
func AnnotateMove(board *rules.Board, m rules.Move) string {
	piece := board.PieceAtP(m.From)
	if piece == nil {
		return EncodeMove(m)
	}

	parts := []string{rules.ChineseNotation(piece, m.From, m.To)}
	// 优先用棋盘实际局面取被吃子（手工构造的 Move 不带 Captured）。
	captured := m.Captured
	if captured == nil {
		captured = board.PieceAtP(m.To)
	}
	if captured != nil {
		parts = append(parts, "吃"+rules.PieceLabel(captured))
	}

	probe := board.Copy()
	probe.ApplyMove(rules.Move{From: m.From, To: m.To})
	if probe.IsCheck(probe.Turn()) {
		parts = append(parts, "将军")
	}

	return fmt.Sprintf("%s(%s)", EncodeMove(m), strings.Join(parts, ","))
}

// ScoreBucket 相对最佳分的损失 → 分桶文字（给 LLM 的可读评估，
// move_annotation.dart:36-42）。cpDiff 为厘兵（正数越大亏损越多）。
func ScoreBucket(cpDiff int) string {
	switch {
	case cpDiff <= 30:
		return "最佳/均势"
	case cpDiff <= 100:
		return "略亏"
	case cpDiff <= 250:
		return "明显亏（约半子）"
	case cpDiff <= 600:
		return "大亏（丢一马/一炮级）"
	default:
		return "致命（丢车/被将杀级）"
	}
}

// AnnotatedWithBucket 候选清单的一行文本：`b2-e2(炮二平五,吃卒,将军) — 均势`
// （move_annotation.dart:44-46）。
func AnnotatedWithBucket(board *rules.Board, m rules.Move, cpDiff int) string {
	return fmt.Sprintf("%s — %s", AnnotateMove(board, m), ScoreBucket(cpDiff))
}

// AsciiBoard 棋盘 ASCII 图（原 llm_solve_assist._asciiBoard，
// move_annotation.dart:50-62）：10 行文本，大写红方/小写黑方，
// 行号 0-9（0 为黑方底线）、列标 a-i。行首 "row  两空格"，列间单空格，
// 列标行 "    a b c d e f g h i"。
func AsciiBoard(board *rules.Board) string {
	var buf strings.Builder
	buf.WriteString("    a b c d e f g h i\n")
	for row := 0; row < 10; row++ {
		cells := make([]string, 0, 9)
		for col := 0; col < 9; col++ {
			piece := board.PieceAt(col, row)
			if piece == nil {
				cells = append(cells, ".")
			} else {
				cells = append(cells, rules.PieceFenChar(piece))
			}
		}
		fmt.Fprintf(&buf, "%d  %s\n", row, strings.Join(cells, " "))
	}
	return buf.String()
}
