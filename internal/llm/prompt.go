package llm

// Prompt 组装（纯函数，便于单测；Electron 版 prompt.ts 1:1 移植，
// llm_move_source.dart LlmPrompts 同源）。
//
// 协议铁律（05 文档 §2 / AGENTS 铁律 #2）：以下全部文本是五期实测调优结果，
// 逐字搬运、禁止意译改写或"优化"措辞；改动必须走快照测试显式 review。
// 纯 Go：禁止 import Wails / net/http / frontend（铁律 #1）。

import (
	"fmt"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

func sideName(side rules.Side) string {
	if rules.IsRedSide(side) {
		return "红方"
	}
	return "黑方"
}

// notationOf 记谱片段：Move 自带棋子用中文记法，缺棋子退化为坐标
// （llm_move_source.dart:51-56）。
func notationOf(m rules.Move) string {
	if m.Piece == nil {
		return EncodeMove(m)
	}
	return rules.ChineseNotation(m.Piece, m.From, m.To)
}

// joinNotations 一次性的中文记法历史文本（连接符为两个空格）。
func joinNotations(moves []rules.Move) string {
	parts := make([]string, len(moves))
	for i, m := range moves {
		parts[i] = notationOf(m)
	}
	return strings.Join(parts, "  ")
}

// ---------------------------------------------------------------------------
// Prompt v1（严格单行，能力评估基线保留）
// ---------------------------------------------------------------------------

// SystemV1 v1 系统提示（llm_move_source.dart:19-34，仅执方变化）。
func SystemV1(side rules.Side) string {
	return "你是中国象棋对弈引擎的着法接口，本局执" + sideName(side) + "。\n" +
		"坐标约定：列用字母 a-i（从左到右），行用数字 0-9" +
		"（0 为黑方底线、棋盘顶部，9 为红方底线、棋盘底部）。\n" +
		"你只能从用户提供的「合法着法清单」中选择一步，禁止编造清单之外的着法。\n" +
		"\n" +
		"【回复格式（唯一允许的格式，违反即视为无效）】\n" +
		"整个回复只包含一行，形式为：\n" +
		"着法: 起点-终点\n" +
		"示例：着法: b2-e2\n" +
		"\n" +
		"禁止输出：任何解释、推理过程、心理活动、道歉、开场白、" +
		"markdown、代码块、引号、多行文本。你的回复将被程序逐字解析，" +
		"任何多余字符都会导致这步棋作废。"
}

// UserV1 v1 用户提示（llm_move_source.dart:36-63）：FEN + 轮走方 + 最近 12 手 + 合法清单。
func UserV1(board *rules.Board, history []rules.Move, legalCodes []string) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "【当前局面 FEN】%s\n", board.ToFen())
	fmt.Fprintf(&buf, "【轮走方】%s（该方是你）\n", sideName(board.Turn()))
	if len(history) == 0 {
		buf.WriteString("【最近着法】（开局，暂无历史）\n")
	} else {
		recent := history
		if len(history) > 12 {
			recent = history[len(history)-12:]
		}
		fmt.Fprintf(&buf, "【最近着法（中文记法，最新在最后）】%s\n", joinNotations(recent))
	}
	fmt.Fprintf(&buf, "【合法着法清单（共 %d 条，必须从中选择一条）】\n", len(legalCodes))
	buf.WriteString(strings.Join(legalCodes, ", "))
	buf.WriteString("\n")
	buf.WriteString("【输出】仅一行，格式：着法: 起点-终点（起点与终点均取自上方清单）")
	return buf.String()
}

// RetryFeedback v1 非法回复后的追加反馈（llm_move_source.dart:66-69）。
func RetryFeedback(reason string) string {
	return "\n\n你上一次的回复无效（" +
		reason + "）。" +
		"请重新回答：整个回复只含一行「着法: 起点-终点」，" +
		"着法必须取自合法着法清单，不要输出任何其他文字。"
}

// ---------------------------------------------------------------------------
// Prompt v2（五期 P0）：棋盘图 + 着法注解 + 放开分析段
// ---------------------------------------------------------------------------

// SystemV2 v2 系统提示（llm_move_source.dart:79-99）：允许先输出分析段，
// 最后一行才是着法。解析器 ExtractMove 优先取「着法:」标记后的坐标，与本格式
// 天然兼容。withBucketGuide：候选模式清单带「—」分档时的引导文本（括号段）。
func SystemV2(side rules.Side, withBucketGuide bool) string {
	bucketGuide := "合法着法清单中每条着法附有括号注解（中文记法/吃子/将军）。"
	if withBucketGuide {
		bucketGuide = "合法着法清单中每条着法附有括号注解（中文记法/吃子/将军）" +
			"与「—」后的引擎评估分档，请优先考虑评估为「最佳/均势」的着法，" +
			"避免选择「大亏/致命」档的着法。"
	}
	return "你是中国象棋对弈引擎的着法接口，本局执" + sideName(side) + "。\n" +
		"坐标约定：列用字母 a-i（从左到右），行用数字 0-9" +
		"（0 为黑方底线、棋盘顶部，9 为红方底线、棋盘底部）。\n" +
		"你只能从用户提供的「合法着法清单」中选择一步，禁止编造清单之外的着法。\n" +
		"\n" +
		"【回复格式（唯一允许的格式，共两段）】\n" +
		"第一段以「分析:」开头，用一两句话（不超过 100 字）说明你的计划" +
		"（进攻目标、需要提防的威胁）。\n" +
		"最后一段为一行，形式为：\n" +
		"着法: 起点-终点\n" +
		"示例：着法: b2-e2\n" +
		"\n" +
		bucketGuide
}

// LooksLikeRepetition 检测历史末尾的来回重复（llm_move_source.dart:151-159）。
// 红黑交替下"同侧同 from 重复"在几何上不可能，必须比互逆：
// 红 A→B、黑 C→D、红 B→A、黑 D→C。
func LooksLikeRepetition(history []rules.Move) bool {
	if len(history) < 4 {
		return false
	}
	isInverse := func(a, b rules.Move) bool {
		return a.From.Col == b.To.Col && a.From.Row == b.To.Row &&
			a.To.Col == b.From.Col && a.To.Row == b.From.Row
	}
	n := len(history)
	at := func(i int) rules.Move { return history[n-i] }
	return isInverse(at(1), at(3)) && isInverse(at(2), at(4))
}

// HistoryTextV2 历史中文记法文本：v2 扩到整局（>60 着从最早截断，
// llm_move_source.dart:138-148）。
func HistoryTextV2(history []rules.Move) string {
	if len(history) == 0 {
		return "（开局，暂无历史）"
	}
	recent := history
	if len(history) > 60 {
		recent = history[len(history)-60:]
	}
	return joinNotations(recent)
}

// UserV2 v2 用户提示（llm_move_source.dart:103-127）：FEN + 棋盘 ASCII 图 +
// 整局历史（≤60 着）+ 逐条注解的合法清单；历史出现来回重复时附循环警示。
func UserV2(board *rules.Board, history []rules.Move, legalMoves []rules.Move) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "【当前局面 FEN】%s\n", board.ToFen())
	buf.WriteString("【棋盘图】\n")
	buf.WriteString(AsciiBoard(board))
	fmt.Fprintf(&buf, "【轮走方】%s（该方是你）\n", sideName(board.Turn()))
	fmt.Fprintf(&buf, "【对局着法（中文记法，最新在最后）】%s\n", HistoryTextV2(history))
	if LooksLikeRepetition(history) {
		buf.WriteString("【警示】最近着法出现来回重复。长将/长捉判负，" +
			"重复局面会被视为无效——请选择打破循环的着法。\n")
	}
	fmt.Fprintf(&buf,
		"【合法着法清单（共 %d 条，必须从中选择一条；括号内为中文记法/吃子/将军注解）】\n",
		len(legalMoves))
	for _, m := range legalMoves {
		buf.WriteString(AnnotateMove(board, m))
		buf.WriteString("\n")
	}
	buf.WriteString("【输出】先输出「分析:」段，最后一行输出「着法: 起点-终点」（起点与终点均取自上方清单）")
	return buf.String()
}

// RetryFeedbackV2 v2 非法回复反馈：回显上次着法与原因（llm_move_source.dart:130-136）。
// lastCode 为空串时不回显着法。
func RetryFeedbackV2(reason, lastCode string) string {
	last := ""
	if lastCode != "" {
		last = "的着法 " + lastCode
	}
	return "\n\n你上一次的回复" +
		last +
		"无效（" + reason + "）。着法必须取自合法着法清单。" +
		"请重新回答：先「分析:」一两句，最后一行「着法: 起点-终点」。"
}

// VetoFeedback 参谋否决再问的追加反馈（hybrid_llm_move_source.dart:291-295，
// gate 模式专用）。
func VetoFeedback(user, vetoedCode, bucket string, loss int) string {
	return user + "\n\n" +
		"【参谋否决】你上一次选择的 " + vetoedCode + " " +
		"会被引擎惩罚（" + bucket + "，" +
		"相对最佳损失 " + fmt.Sprintf("%d", loss) + " 厘兵）。请重新从候选清单中选择，" +
		"优先考虑「最佳/均势」档；先「分析:」一句，最后一行「着法: 起点-终点」。"
}
