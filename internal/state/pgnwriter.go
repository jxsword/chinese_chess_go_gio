package state

// 棋谱导出：标准 PGN 文本（ICCS 着法）+ 结果标记（翻译源 = 上游
// frontend/src/packages/storage-schema/pgnWriter.ts；对应 pgn_writer.dart，07 文档 §5 F2）。
// 协议面：输出经快照测试锁定（state/pgnwriter_test.go，基准与上游同源），改动须显式 review。
// 与 internal/parsers 的 PGN 解析器（导入方向）对偶：本文件只负责生成。

import (
	"fmt"
	"strings"
	"time"
)

// PgnResultTag PGN 结果标记（pgn_writer.dart:105-119）：残局按求解状态、对局按结果。
func PgnResultTag(record GameRecordData) string {
	if record.Mode == "endgame" {
		switch record.SolveStatus {
		case SolveSolved:
			return "1-0"
		case SolveNoSolution:
			return "0-1"
		default:
			return "*"
		}
	}
	switch deref(record.Result, "") {
	case "redWins":
		return "1-0"
	case "blackWins":
		return "0-1"
	case "draw":
		return "1/2-1/2"
	default:
		return "*"
	}
}

// WritePgn 生成 PGN（Seven Tag Roster + ICCS 着法序列，残局附 SetFen/FEN，
// pgn_writer.dart:11-50）。now 用于 CreatedAt 缺省。
func WritePgn(record GameRecordData, now time.Time) string {
	date := time.UnixMilli(record.CreatedAt)
	if record.CreatedAt == 0 {
		date = now
	}
	result := PgnResultTag(record)
	red := deref(record.RedName, "红方")
	black := deref(record.BlackName, "黑方")
	headers := []string{
		`[Event "中国象棋 Ultra"]`,
		`[Site "ChineseChessUltra"]`,
		fmt.Sprintf(`[Date "%d.%02d.%02d"]`, date.Year(), int(date.Month()), date.Day()),
		`[Round "-"]`,
		fmt.Sprintf(`[Red "%s"]`, red),
		fmt.Sprintf(`[Black "%s"]`, black),
		fmt.Sprintf(`[Result "%s"]`, result),
	}
	if record.SolveStatus != SolveNone {
		headers = append(headers, fmt.Sprintf(`[Annotator "%s"]`, SolveStatusLabel(record.SolveStatus)))
	}
	// 残局与初始局面不同时标注起始 FEN，导入方可据此复原。
	if !IsInitialBoardFen(record.InitialFen) {
		headers = append(headers,
			fmt.Sprintf(`[SetFen "%s"]`, record.InitialFen),
			fmt.Sprintf(`[FEN "%s"]`, record.InitialFen))
	}

	parts := []string{}
	for plies := 0; plies < len(record.Moves); plies++ {
		if plies%2 == 0 {
			parts = append(parts, fmt.Sprintf("%d.", plies/2+1))
		}
		parts = append(parts, IccsFallbackFormat(record.Moves[plies]))
	}
	body := ""
	if len(record.Moves) > 0 {
		body = fmt.Sprintf("%s %s", strings.Join(parts, " "), result)
	}
	return fmt.Sprintf("%s\n\n%s\n", strings.Join(headers, "\n"), body)
}

// SolveVerdictLines 分享文本的求解结论块（无解/超时文案，供详情页复用，
// pgn_writer.dart:88-92）。
func SolveVerdictLines(record GameRecordData) []string {
	solutions := record.Solutions
	if len(solutions) > 0 {
		unique := ""
		if HasUniqueSolution(record.SolveStatus, solutions) {
			unique = "，唯一解"
		}
		lines := []string{fmt.Sprintf("破解之法（%d 条%s）:", len(solutions), unique)}
		for i, solution := range solutions {
			lines = append(lines, fmt.Sprintf("解法%d: %s", i+1, strings.Join(solution, " ")))
		}
		return lines
	}
	if record.SolveStatus == SolveNoSolution {
		return []string{"求解结论: 无解（深度上界内已证明）"}
	}
	if record.SolveStatus == SolveTimeout {
		return []string{"求解结论: 限时内未找到解法"}
	}
	return []string{}
}
