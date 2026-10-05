// Package parsers 棋谱解析（06 文档 §2~§4.5）：ICCS 坐标、PGN（双着法格式 +
// 中文记谱消解 + 大文件流式索引）、XQF 二进制（Dong Shiwei 解密）与解析门面。
// 逐行翻译 Electron 版 src/packages/parsers（iccs.ts/pgnParser.ts/xqfParser.ts/
// puzzleParser.ts/parsedPuzzle.ts），纯 Go：禁止 import Wails / net/http / 前端符号。
package parsers

import "strings"

// standardInitialBoard 标准开局盘面部分（用于区分全局对局与残局/排局，puzzle_data.dart:65-66）。
const standardInitialBoard = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR"

// ParsedPuzzle 解析后的棋谱（残局题或全局对局，puzzle_data.dart:8-48）。
type ParsedPuzzle struct {
	// ID 唯一标识符（`xqf/<source>/<title>` 或 `pgn/<source>/<title>/<n>`）。
	ID string `json:"id"`
	// InitialFen 初始局面 FEN。
	InitialFen string `json:"initialFen"`
	// SolutionMoves 破解/对局走法序列（ICCS 坐标）。
	SolutionMoves []string `json:"solutionMoves"`
	// Title 残局标题。
	Title *string `json:"title"`
	// Description 描述（对局双方/日期/赛事）。
	Description *string `json:"description"`
	// Source 来源（如"残局/适情雅趣"）。
	Source string `json:"source"`
	// Format 格式（"xqf" / "pgn"）。
	Format string `json:"format"`
	// Difficulty 难度等级（1-5）。
	Difficulty int `json:"difficulty"`
}

// MoveCountOf 残局步数（puzzle_data.dart:51）。
func MoveCountOf(p *ParsedPuzzle) int { return len(p.SolutionMoves) }

// HasSolution 是否有破解走法（puzzle_data.dart:92）。
func HasSolution(p *ParsedPuzzle) bool { return len(p.SolutionMoves) > 0 }

// DifficultyFromMoveCount 按总步数推算难度分档（XQF/PGN 棋谱本身没有难度字段，
// puzzle_data.dart:56-62）：≤20 步入门(1)、21-40 初级(2)、41-80 中级(3)、
// 81-150 高级(4)、>150 职业(5)。
func DifficultyFromMoveCount(moveCount int) int {
	if moveCount <= 20 {
		return 1
	}
	if moveCount <= 40 {
		return 2
	}
	if moveCount <= 80 {
		return 3
	}
	if moveCount <= 150 {
		return 4
	}
	return 5
}

// DifficultyText 难度显示文本（puzzle_data.dart:95-109）。
func DifficultyText(difficulty int) string {
	switch difficulty {
	case 1:
		return "入门"
	case 2:
		return "初级"
	case 3:
		return "中级"
	case 4:
		return "高级"
	case 5:
		return "职业"
	default:
		return "未知"
	}
}

// IsEndgamePuzzle 是否为残局/排局题（区别于全局对局，puzzle_data.dart:74-86）。
// 判定顺序：来源分类名关键词优先（"让子局"盘面非标准开局但属对局），
// 否则按初始盘面是否为标准开局。
func IsEndgamePuzzle(source string, initialFen string) bool {
	if containsAny(source, "残局", "排局", "杀势") {
		return true
	}
	if containsAny(source, "全局", "大师", "比赛", "布局", "中局", "名局", "让子") {
		return false
	}
	board := initialFen
	for i := 0; i < len(initialFen); i++ {
		if initialFen[i] == ' ' {
			board = initialFen[:i]
			break
		}
	}
	return board != standardInitialBoard
}

// KindLabel 类型标签文案（puzzle_data.dart:89）。
func KindLabel(source string, initialFen string) string {
	if IsEndgamePuzzle(source, initialFen) {
		return "残局题"
	}
	return "全局对局"
}

// PuzzleOverrides 门面校验允许覆盖的字段（puzzle_data.dart:118-138 copyWith 的覆盖面）。
type PuzzleOverrides struct {
	ID            *string
	SolutionMoves []string
	Difficulty    *int
}

// CopyPuzzleWith 创建副本并覆盖指定字段（puzzle_data.dart:118-138 copyWith）。
func CopyPuzzleWith(puzzle ParsedPuzzle, overrides PuzzleOverrides) ParsedPuzzle {
	if overrides.ID != nil {
		puzzle.ID = *overrides.ID
	}
	if overrides.SolutionMoves != nil {
		puzzle.SolutionMoves = overrides.SolutionMoves
	}
	if overrides.Difficulty != nil {
		puzzle.Difficulty = *overrides.Difficulty
	}
	return puzzle
}

// containsAny 子串包含判定（strings.Contains 的多关键词展开）。
func containsAny(s string, keywords ...string) bool {
	for _, k := range keywords {
		if k != "" && strings.Contains(s, k) {
			return true
		}
	}
	return false
}
