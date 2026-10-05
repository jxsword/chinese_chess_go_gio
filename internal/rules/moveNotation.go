package rules

// 中文纵线记法（对应 move_notation.dart / Electron 版 moveNotation.ts，02 文档 §4）。
//
// 形如 "炮二平五" / "马8进7"：红方用汉字数字（从红方视角右→左），
// 黑方用阿拉伯数字。三分支：同列直线（进/退+步数）、同行平移（平+目标列号）、
// 斜走（进/退+目标列号）。进/退方向：红方 row 减小为进，黑方 row 增大为进。
//
// 已知局限（02 §6 保持一致）：无"前/后/中"同列多子消歧——正向记谱不输出，
// 该消歧仅在 PGN 解析器（06 文档 §3.3）中反向实现。
import "strconv"

// han 红方列号汉字：han[col]，col 0 → '九'、col 8 → '一'（move_notation.dart:11）。
var han = [9]string{"九", "八", "七", "六", "五", "四", "三", "二", "一"}

// stepLabel 同列直线时红方步数取 han[9-steps]（move_notation.dart:28）。
func stepLabel(steps int) string { return han[9-steps] }

// ChineseNotation 把 (piece, from, to) 序列化为带颜色与中文坐标的记法字符串
// （move_notation.dart:9-39）。前置：from ≠ to（真实走法恒成立；退化输入
// 未定义——原版同场景产出含 undefined 的废串）。
func ChineseNotation(piece *Piece, from, to Position) string {
	red := IsRedSide(piece.Side)
	colLabel := func(c int) string {
		if red {
			return han[c]
		}
		return strconv.Itoa(c + 1)
	}

	fromCol := colLabel(from.Col)
	toCol := colLabel(to.Col)
	sameCol := from.Col == to.Col
	forwardDelta := to.Row - from.Row
	isForward := (red && forwardDelta < 0) || (!red && forwardDelta > 0)

	var action string
	var target string
	switch {
	case sameCol:
		// 同列直线：进/退 + 步数（红方步数取 han[9-steps]）。
		if isForward {
			action = "进"
		} else {
			action = "退"
		}
		steps := forwardDelta
		if steps < 0 {
			steps = -steps
		}
		if red {
			target = stepLabel(steps)
		} else {
			target = strconv.Itoa(steps)
		}
	case from.Row == to.Row:
		// 同行平移：平 + 目标列号。
		action = "平"
		target = toCol
	default:
		// 斜走（马/象/士）：进/退 + 目标列号。
		if isForward {
			action = "进"
		} else {
			action = "退"
		}
		target = toCol
	}
	return PieceLabel(piece) + fromCol + action + target
}
