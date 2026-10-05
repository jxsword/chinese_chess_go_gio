package rules

// 中国象棋标准局面 FEN 编解码（对应 fen.dart / Electron 版 fen.ts，参考 XQFEN）。
//
// 棋盘从黑方底线（row 0）写到红方底线（row 9），每行 9 列从左到右，
// 数字表示连续空位，红方大写、黑方小写。
// halfMove 恒 0、fullMove 恒 1（原版未维护半回合计数，保持一致，02 文档 §1.4/§6）。
import (
	"fmt"
	"strconv"
	"strings"
)

// FENInitial 标准初始局面（fen.dart:11-12）。
const FENInitial = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

// BoardGrid 棋盘矩阵：[row][col] 10×9，空格为 nil。
type BoardGrid [][]*Piece

// FenFormatError FEN 格式异常（Dart FormatException 等价，fen.dart:41/56/63）。
type FenFormatError struct{ Message string }

func (e *FenFormatError) Error() string { return e.Message }

// splitFields 等价于 JS fen.trim().split(/\s+/)：空串得 [""]（长度 1），
// 与 JS split 行为一致（后续以 rows!=10 报错/校验失败收口）。
func splitFields(fen string) []string {
	trimmed := strings.TrimSpace(fen)
	if trimmed == "" {
		return []string{""}
	}
	return strings.Fields(trimmed)
}

// IsValidFen 校验是否为合法 FEN（粗校：10 行、每行合计 9 列、
// 字符 ∈ {1-9, KABNRCPkabnrcp}；fen.dart:15-34）。
func IsValidFen(fen string) bool {
	parts := splitFields(fen)
	if len(parts) == 0 {
		return false
	}
	rows := strings.Split(parts[0], "/")
	if len(rows) != 10 {
		return false
	}
	for _, rowStr := range rows {
		sum := 0
		for i := 0; i < len(rowStr); i++ {
			ch := rowStr[i]
			if ch >= '0' && ch <= '9' {
				sum += int(ch - '0')
			} else if PieceFromFenChar(ch) != nil {
				sum++
			} else {
				return false
			}
		}
		if sum != 9 {
			return false
		}
	}
	return true
}

// ParseBoardFen 解析 FEN 棋盘部分，返回 10 行 × 9 列矩阵，空格为 nil。
// 矩阵下标为 [row][col]，row 0 为黑方底线（FEN 第一行），row 9 为红方底线。
// 行数错 / 行长≠9 / 非法字符返回 *FenFormatError（fen.dart:38-68）。
func ParseBoardFen(fen string) (BoardGrid, error) {
	parts := splitFields(fen)
	rows := strings.Split(parts[0], "/")
	if len(rows) != 10 {
		return nil, &FenFormatError{fmt.Sprintf("Invalid FEN board rows: %d", len(rows))}
	}
	result := make(BoardGrid, 10)
	for r := range result {
		result[r] = make([]*Piece, 9)
	}
	for r := 0; r < 10; r++ {
		col := 0
		for i := 0; i < len(rows[r]); i++ {
			ch := rows[r][i]
			if ch >= '0' && ch <= '9' {
				col += int(ch - '0')
				continue
			}
			piece := PieceFromFenChar(ch)
			if piece == nil {
				return nil, &FenFormatError{fmt.Sprintf("Invalid FEN char: %c", ch)}
			}
			// JS 数组越界会静默扩容、最终由行长检查抛错；Go 定长数组需先守卫
			//（同为行超长，错误类别一致）。
			if col >= 9 {
				return nil, &FenFormatError{fmt.Sprintf("Invalid FEN row length at %d: expected 9 got %d", r, col+1)}
			}
			result[r][col] = piece
			col++
		}
		if col != 9 {
			return nil, &FenFormatError{fmt.Sprintf("Invalid FEN row length at %d: expected 9 got %d", r, col)}
		}
	}
	return result, nil
}

// ParseTurnFen 解析 FEN 中"轮走方"字段，true=红方；解析到 w 或 r 都算红（fen.dart:71-75）。
func ParseTurnFen(fen string) bool {
	parts := splitFields(fen)
	if len(parts) < 2 {
		return true
	}
	turn := strings.ToLower(parts[1])
	return turn == "w" || turn == "r"
}

// BoardGridToFen 将棋盘矩阵序列化为 FEN 棋盘部分（fen.dart:78-99）。
func BoardGridToFen(grid BoardGrid) string {
	rows := make([]string, 10)
	for r := 0; r < 10; r++ {
		var buf strings.Builder
		empty := 0
		for c := 0; c < 9; c++ {
			p := grid[r][c]
			if p == nil {
				empty++
				continue
			}
			if empty > 0 {
				buf.WriteString(strconv.Itoa(empty))
				empty = 0
			}
			buf.WriteString(PieceFenChar(p))
		}
		if empty > 0 {
			buf.WriteString(strconv.Itoa(empty))
		}
		rows[r] = buf.String()
	}
	return strings.Join(rows, "/")
}

// BuildFen 拼装完整 FEN（含走子方/回合数等扩展字段，fen.dart:102-109）。
// 原版 halfMove 恒 0、fullMove 恒 1（全部调用点均未传其他值），Go 版收拢为固定输出。
func BuildFen(board BoardGrid, isRedTurn bool) string {
	turn := "b"
	if isRedTurn {
		turn = "w"
	}
	return fmt.Sprintf("%s %s - - %d %d", BoardGridToFen(board), turn, 0, 1)
}
