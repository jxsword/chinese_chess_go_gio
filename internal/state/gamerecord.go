package state

// 棋谱记录数据面（翻译源 = 上游 frontend/src/packages/storage-schema/gameRecord.ts
// 中 pgnWriter / 重放器消费的子集；对应 game_record.dart）。
// 纯 Go：仅依赖规则内核（铁律 #G1）。

import (
	"fmt"
	"strings"
	"time"

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

// GameRecordData 棋谱记录（gameRecord.ts GameRecordData，pgnWriter/分享文本/记录库消费面）。
type GameRecordData struct {
	ID         int64
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
	LlmNote     *string
	Note        *string
	CreatedAt   int64 // ms；0 = 写出时用当前时间
}

// ModeLabelOf 模式中文标签（gameRecord.ts modeLabelOf）。
func ModeLabelOf(mode string) string {
	switch mode {
	case "humanVsAi":
		return "人机对战"
	case "humanVsHuman":
		return "双人对弈"
	case "aiVsAi":
		return "机机对战"
	case "humanVsLlm":
		return "人机(大模型)"
	case "llmVsLlm":
		return "大模型对战"
	case "endgame":
		return "残局破解"
	}
	return mode
}

// ResultLabel 对局结果中文标签（gameRecord.ts resultLabel）。
func ResultLabel(result string) string {
	switch result {
	case "redWins":
		return "红方胜"
	case "blackWins":
		return "黑方胜"
	case "draw":
		return "和棋"
	}
	return result
}

// DefaultRecordTitle 标题缺省自动生成：`YYYY-MM-DD 模式名`（game_record.dart:181-185）。
func DefaultRecordTitle(mode string, now time.Time) string {
	return fmt.Sprintf("%d-%02d-%02d %s", now.Year(), int(now.Month()), now.Day(), ModeLabelOf(mode))
}

// RecordFromSessionInput 保存棋谱的会话输入（RecordSaveDialog → recordFromSession）。
type RecordFromSessionInput struct {
	Title     string // 空白 = 自动生成
	Mode      string
	FinalFen  string
	Moves     []rules.Move
	Result    *string
	RedName   *string
	BlackName *string
	Note      *string
}

// RecordFromSession 对局会话 → 棋谱记录（gameRecord.ts recordFromSession：
// 标题自动生成、initialFen 从终局反推逆序悔棋、solveStatus=none）。
func RecordFromSession(input RecordFromSessionInput, now time.Time) GameRecordData {
	title := input.Title
	if strings.TrimSpace(title) == "" {
		title = DefaultRecordTitle(input.Mode, now)
	}
	return GameRecordData{
		Title:       title,
		Mode:        input.Mode,
		InitialFen:  initialFenFromEnd(input.FinalFen, input.Moves),
		Moves:       append([]rules.Move(nil), input.Moves...),
		Result:      input.Result,
		RedName:     input.RedName,
		BlackName:   input.BlackName,
		SolveStatus: SolveNone,
		Solutions:   [][]string{},
		LlmNote:     nil,
		Note:        input.Note,
		CreatedAt:   now.UnixMilli(),
	}
}

// initialFenFromEnd 从终局反推初始 FEN：逆序悔棋；数据不一致时尽早止损
// （game_record.dart:188-195）。
func initialFenFromEnd(finalFen string, moves []rules.Move) string {
	board, err := rules.FromFen(finalFen)
	if err != nil {
		return finalFen
	}
	for i := len(moves) - 1; i >= 0; i-- {
		m := moves[i]
		if board.PieceAtP(m.To) == nil {
			break
		}
		board.UndoMove(rules.Move{From: m.From, To: m.To, Captured: m.Captured})
	}
	return board.ToFen()
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

// WriteShareText 分享文本（翻译源 = 上游 shareText.ts；对应 pgn_writer.dart
// writeShareText，07 文档 §5 F3。协议面：输出经快照测试锁定，改动须显式 review）。
func WriteShareText(record GameRecordData) string {
	lines := []string{}
	lines = append(lines, fmt.Sprintf("【中国象棋 Ultra 棋谱】%s", record.Title))
	redName := ""
	if record.RedName != nil {
		redName = "  红方: " + *record.RedName
	}
	blackName := ""
	if record.BlackName != nil {
		blackName = "  黑方: " + *record.BlackName
	}
	lines = append(lines, fmt.Sprintf("模式: %s%s%s", ModeLabelOf(record.Mode), redName, blackName))
	if record.Result != nil {
		lines = append(lines, fmt.Sprintf("结果: %s", ResultLabel(*record.Result)))
	}
	if !IsInitialBoardFen(record.InitialFen) {
		lines = append(lines, fmt.Sprintf("起始 FEN: %s", record.InitialFen))
	}
	if record.Note != nil && strings.TrimSpace(*record.Note) != "" {
		lines = append(lines, fmt.Sprintf("备注: %s", *record.Note))
	}
	if len(record.Moves) > 0 {
		lines = append(lines, "着法（中文记谱）:")
		notations := ChineseNotations(record.InitialFen, record.Moves)
		for i := 0; i < len(notations); i += 2 {
			round := i/2 + 1
			red := notations[i]
			black := ""
			if i+1 < len(notations) {
				black = notations[i+1]
			}
			lines = append(lines, fmt.Sprintf("%d. %s  %s", round, red, black))
		}
	}
	solutions := record.Solutions
	if len(solutions) > 0 {
		unique := ""
		if HasUniqueSolution(record.SolveStatus, solutions) {
			unique = "，唯一解"
		}
		lines = append(lines, fmt.Sprintf("破解之法（%d 条%s）:", len(solutions), unique))
		for i, solution := range solutions {
			lines = append(lines, fmt.Sprintf("解法%d: %s", i+1, strings.Join(solution, " ")))
		}
	} else if record.SolveStatus == SolveNoSolution {
		lines = append(lines, "求解结论: 无解（深度上界内已证明）")
	} else if record.SolveStatus == SolveTimeout {
		lines = append(lines, "求解结论: 限时内未找到解法")
	}
	if record.LlmNote != nil && strings.TrimSpace(*record.LlmNote) != "" {
		lines = append(lines, fmt.Sprintf("大模型注释: %s", *record.LlmNote))
	}
	return strings.Join(lines, "\n")
}
