package parsers

import (
	"fmt"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 棋谱解析门面（对应 puzzle_parser.dart，06 文档 §4.5；逐行翻译 puzzleParser.ts）。
//
// 按文件扩展名分发到 ParseXqf / PGN 解析，并对解析结果做
// 重放校验（用规则内核 Board 逐着验证合法性，遇到非法着即截断），
// 保证进入 UI 的棋谱可以在演示器中完整播放。

// StreamImportThresholdBytes 大文件导入阈值：超过此大小的多局 PGN 整读内存代价过高（如 101MB 的 .pgns）。
const StreamImportThresholdBytes = 8 * 1024 * 1024

// ShouldStreamImport 判断导入是否应走按局索引流式路径（仅多局 PGN 大文件，puzzle_parser.dart:55-59）。
func ShouldStreamImport(fileName string, byteLength int64) bool {
	ext := extensionOf(fileName)
	return (ext == "pgn" || ext == "pgns") && byteLength > StreamImportThresholdBytes
}

func extensionOf(fileName string) string {
	lower := strings.ToLower(fileName)
	dot := strings.LastIndex(lower, ".")
	if dot < 0 {
		return ""
	}
	return lower[dot+1:]
}

// ParsePuzzleFile 解析棋谱字节流为棋局列表（XQF 单局；PGN 可多局）（puzzle_parser.dart:30-48）。
// fileName 仅用于判断格式；source 作为来源标注（如语料分类）。
// 不认识的扩展名返回错误。
func ParsePuzzleFile(fileName string, data []byte, source string) ([]*ParsedPuzzle, error) {
	ext := extensionOf(fileName)
	var parsed []*ParsedPuzzle
	switch ext {
	case "xqf":
		p, err := ParseXqf(data, source)
		if err != nil {
			return nil, err
		}
		parsed = []*ParsedPuzzle{p}
	case "pgn", "pgns":
		games, err := ParseGames(DecodeUtf8Lossy(data), source)
		if err != nil {
			return nil, err
		}
		parsed = games
	default:
		return nil, fmt.Errorf("不支持的棋谱格式: .%s（支持 .xqf / .pgn）", ext)
	}
	return validateAndDedupe(parsed), nil
}

// validateAndDedupe 逐局重放校验；非法着截断（至少保留 1 着，否则丢弃该局），
// 并保证 id 唯一（puzzle_parser.dart:62-101）。
func validateAndDedupe(input []*ParsedPuzzle) []*ParsedPuzzle {
	result := make([]*ParsedPuzzle, 0)
	seenIDs := make(map[string]bool)
	for _, puzzle := range input {
		board, err := rules.FromFen(puzzle.InitialFen)
		if err != nil {
			continue // FEN 无效等异常：丢弃该局（puzzle_parser.dart:96-98）
		}
		valid := make([]string, 0, len(puzzle.SolutionMoves))
		for _, iccs := range puzzle.SolutionMoves {
			pos := ParseIccs(iccs)
			if pos == nil || board.PieceAtP(pos.From) == nil {
				break
			}
			legal := false
			for _, m := range board.LegalMovesFor(pos.From) {
				if m.From == pos.From && m.To == pos.To {
					legal = true
					break
				}
			}
			if !legal {
				break // 非法着：截断
			}
			board.ApplyMove(rules.Move{From: pos.From, To: pos.To})
			valid = append(valid, iccs)
		}
		if len(valid) == 0 {
			continue // 无可演示走法，丢弃
		}
		id := puzzle.ID
		for seenIDs[id] {
			id += "#" // id 冲突加 # 后缀（puzzle_parser.dart:88-90）
		}
		seenIDs[id] = true
		difficulty := DifficultyFromMoveCount(len(valid))
		out := CopyPuzzleWith(*puzzle, PuzzleOverrides{ID: &id, SolutionMoves: valid, Difficulty: &difficulty})
		result = append(result, &out)
	}
	return result
}
