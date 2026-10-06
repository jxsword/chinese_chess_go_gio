package state

// 棋谱记录数据面（翻译源 = 上游 frontend/src/packages/storage-schema/gameRecord.ts
// 中 pgnWriter / 重放器消费的子集；对应 game_record.dart）。
// 纯 Go：仅依赖规则内核（铁律 #G1）。

import (
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// SolveStatus 求解状态（game_record.dart SolveStatus）。
type SolveStatus string

const (
	SolveNone       SolveStatus = "none"
	SolveSolved     SolveStatus = "solved"
	SolveNoSolution SolveStatus = "noSolution"
	SolveTimeout    SolveStatus = "timeout"
)

// GameRecordData 棋谱记录（gameRecord.ts GameRecordData，pgnWriter/分享文本消费面）。
type GameRecordData struct {
	Title      string
	Mode       string // GameMode 字符串（'endgame' | 对局模式）
	InitialFen string
	// Moves 对局/主变走法，含棋子与吃子信息（FillMovePieces 补齐）。
	Moves       []rules.Move
	Result      *string // redWins | blackWins | draw | nil
	RedName     *string
	BlackName   *string
	SolveStatus SolveStatus
	Solutions   [][]string
	CreatedAt   int64 // ms；0 = 写出时用当前时间
}

// SolveStatusLabel 求解状态中文标签（gameRecord.ts solveStatusLabel）。
func SolveStatusLabel(status SolveStatus) string {
	switch status {
	case SolveNone:
		return "对局"
	case SolveSolved:
		return "已破解"
	case SolveNoSolution:
		return "无解"
	case SolveTimeout:
		return "未决(超时)"
	}
	return string(status)
}

// HasUniqueSolution 唯一解判定（gameRecord.ts hasUniqueSolution）。
func HasUniqueSolution(status SolveStatus, solutions [][]string) bool {
	return status == SolveSolved && len(solutions) == 1
}

// FinalFenOf 终局 FEN：从 initialFen 重放 moves；遇到与局面不符的走法即止损
// （game_record.dart:89-96）。
func FinalFenOf(initialFen string, moves []rules.Move) string {
	board, err := rules.FromFen(initialFen)
	if err != nil {
		return initialFen
	}
	for _, m := range moves {
		if board.PieceAtP(m.From) == nil {
			break
		}
		board.ApplyMove(rules.Move{From: m.From, To: m.To})
	}
	return board.ToFen()
}

// FillMovePieces 走法序列补棋子信息：以 initialFen 起重放（脏走法止损），
// piece 缺失的从盘面取（gameRecord.ts fillMovePieces）。
func FillMovePieces(initialFen string, moves []rules.Move) []rules.Move {
	board, err := rules.FromFen(initialFen)
	if err != nil {
		return append([]rules.Move(nil), moves...)
	}
	out := make([]rules.Move, 0, len(moves))
	for _, m := range moves {
		if m.Piece == nil {
			if p := board.PieceAtP(m.From); p != nil {
				piece := *p
				m.Piece = &piece
			}
		}
		out = append(out, m)
		if board.PieceAtP(m.From) == nil {
			break // 脏走法：与局面不符即止损（其余保持原样）
		}
		board.ApplyMove(rules.Move{From: m.From, To: m.To})
	}
	return out
}

// ChineseNotations 走法的中文记谱序列（gameRecord.ts chineseNotations）。
func ChineseNotations(initialFen string, moves []rules.Move) []string {
	filled := FillMovePieces(initialFen, moves)
	out := make([]string, 0, len(filled))
	for _, m := range filled {
		if m.Piece == nil {
			out = append(out, IccsFallbackFormat(m))
			continue
		}
		out = append(out, rules.ChineseNotation(m.Piece, m.From, m.To))
	}
	return out
}

// IccsFallbackFormat 坐标兜底格式（gameRecord.ts iccsFallbackFormat）：b2e2 形。
func IccsFallbackFormat(m rules.Move) string {
	cell := func(col, row int) string {
		return string(rune('a'+col)) + string(rune('0'+9-row))
	}
	return cell(m.From.Col, m.From.Row) + cell(m.To.Col, m.To.Row)
}

// IsInitialBoardFen 起始 FEN 是否为标准开局盘面（pgn_writer.dart:121-124）。
func IsInitialBoardFen(fen string) bool {
	return strings.SplitN(fen, " ", 2)[0] ==
		"rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR"
}
