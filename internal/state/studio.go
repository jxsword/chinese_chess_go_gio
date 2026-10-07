package state

// 残局工作室摆盘合法性与整体校验（T6'.2，翻译锚点 =
// 上游 frontend/src/features/studio/setupRules.ts + studioValidate.ts，
// dart 原版 board_setup_rules.dart / endgame_studio_page.dart _validate 1:1）。
//
// 摆放即时校验（九宫/士象斜线田字/兵卒底线/数量上限）逐条对齐上游；
// 整体校验五条（08 文档 §4/§7）：FEN 格式 / 双王各一 / 位置合法 / 数量上限 /
// 对手不可正被将军 + 轮走方有着可走——FEN 导入与识图载入的棋盘同样过此门。
// 纯 Go：无 Gio / 网络（铁律 #G1）。

import (
	"fmt"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// MaxCountPerKind 每种棋子每方的数量上限（象棋标准配置，board_setup_rules.dart:14-22）。
var MaxCountPerKind = map[rules.Kind]int{
	rules.King:     1,
	rules.Advisor:  2,
	rules.Minister: 2,
	rules.Knight:   2,
	rules.Rook:     2,
	rules.Cannon:   2,
	rules.Pawn:     5,
}

func sideName(piece rules.Piece) string {
	if piece.Side == rules.Red {
		return "红方"
	}
	return "黑方"
}

// PlacementIssue 棋子放到 (col,row) 是否合法；不合法返回给用户看的原因，
// 合法返回空串（board_setup_rules.dart:25-68）。
func PlacementIssue(piece rules.Piece, col, row int) string {
	switch piece.Kind {
	case rules.King:
		if !rules.InPalace(col, row, piece.Side) {
			return "帅/将只能放在九宫内的 9 个位置"
		}
	case rules.Advisor:
		if !rules.InPalace(col, row, piece.Side) {
			return "士/仕只能放在己方九宫内"
		}
		// 士走斜线：只能在九宫的 5 个斜线点。
		// 黑方九宫斜线点满足 (col+row) 为奇数，红方为偶数。
		parity := (col + row) % 2
		ok := parity == 1
		if piece.Side == rules.Red {
			ok = parity == 0
		}
		if !ok {
			return "士/仕只能放在九宫的 5 个斜线位置上"
		}
	case rules.Minister:
		if !rules.InOwnHalf(row, piece.Side) {
			return "相/象不能摆到对方半场"
		}
		// 象走田字（列行各 ±2），(col+row) 奇偶性永不改变：
		// 红相起点 (2,9)/(6,9) 为奇数和 → 只能落在奇数行（5/7/9 排）；
		// 黑象起点 (2,0)/(6,0) 为偶数和 → 只能落在偶数行（0/2/4 排）。
		if col%2 != 0 {
			return "相/象只能落在偶数列的田字点上"
		}
		rowParityOk := row%2 == 0
		if piece.Side == rules.Red {
			rowParityOk = row%2 == 1
		}
		if !rowParityOk {
			if piece.Side == rules.Red {
				return "相只能放在己方半场 5/7/9 排的田字点上"
			}
			return "象只能放在己方半场 0/2/4 排的田字点上"
		}
	case rules.Pawn:
		// 兵/卒只进不退（过河后可横走）：红兵不可能出现在 row 7~9，
		// 黑卒不可能出现在 row 0~2。
		ok := row >= 3
		if piece.Side == rules.Red {
			ok = row <= 6
		}
		if !ok {
			return "兵/卒不能放在本方底线三排"
		}
	case rules.Rook, rules.Knight, rules.Cannon:
		// 无位置限制
	}
	return ""
}

// CountIssue 数量映射整体是否合法；返回首个超限原因，合法返回空串
// （board_setup_rules.dart:72-81）。
func CountIssue(counts map[rules.Piece]int) string {
	for piece, count := range counts {
		limit := MaxCountPerKind[piece.Kind]
		if count > limit {
			return fmt.Sprintf("%s%s最多 %d 枚（当前 %d 枚）", sideName(piece), rules.PieceLabel(&piece), limit, count)
		}
	}
	return ""
}

// CountIssueForPlacement 放置前的数量校验：若目标格已有同种棋子则替换不算新增
// （board_setup_rules.dart:84-99）。currentCount 为该棋子当前已有数量。
func CountIssueForPlacement(piece rules.Piece, currentCount int, occupant *rules.Piece) string {
	replacesSame := occupant != nil && occupant.Kind == piece.Kind && occupant.Side == piece.Side
	if replacesSame {
		return ""
	}
	limit := MaxCountPerKind[piece.Kind]
	if currentCount+1 > limit {
		return fmt.Sprintf("%s%s最多 %d 枚", sideName(piece), rules.PieceLabel(&piece), limit)
	}
	return ""
}

// ValidateStudioPosition 工作室当前局面的整体校验：返回全部问题（空切片 = 通过）
// （studioValidate.ts validateStudioPosition 逐字）。
func ValidateStudioPosition(grid rules.BoardGrid, redTurn bool) []string {
	var problems []string
	fen := rules.BuildFen(grid, redTurn)
	if !rules.IsValidFen(fen) {
		return append(problems, "FEN 格式非法（每行列数必须为 9）")
	}
	redKing, blackKing := 0, 0
	for _, row := range grid {
		for _, piece := range row {
			if piece != nil && piece.Kind == rules.King {
				if piece.Side == rules.Red {
					redKing++
				} else {
					blackKing++
				}
			}
		}
	}
	if redKing != 1 || blackKing != 1 {
		problems = append(problems, fmt.Sprintf("双方必须各有一个将/帅（红 %d / 黑 %d）", redKing, blackKing))
	}
	if len(problems) > 0 {
		return problems
	}

	// 全盘棋子位置与数量合法性（FEN 导入/识图载入的棋盘同样校验）。
	counts := map[rules.Piece]int{}
	for row := 0; row < 10; row++ {
		for col := 0; col < 9; col++ {
			piece := grid[row][col]
			if piece == nil {
				continue
			}
			counts[*piece]++
			if issue := PlacementIssue(*piece, col, row); issue != "" {
				problems = append(problems, fmt.Sprintf("(%d,%d) %s：%s", col, row, rules.PieceLabel(piece), issue))
			}
		}
	}
	if msg := CountIssue(counts); msg != "" {
		problems = append(problems, msg)
	}
	if len(problems) > 0 {
		return problems
	}

	board, err := rules.FromFen(fen)
	if err != nil {
		return append(problems, "FEN 格式非法（每行列数必须为 9）")
	}
	turn := board.Turn()
	// 轮走方的对手不应正被将军（否则说明上一手未解除将军，局面非法）。
	if board.IsCheck(rules.OpponentOf(turn)) {
		problems = append(problems, "轮走方行棋前对方已被将军，局面非法")
	}
	if !board.HasAnyLegalMoveFor(turn) {
		problems = append(problems, "轮走方已无着可走（该局面已分胜负）")
	}
	return problems
}

// StudioSolveLabel 求解结论的入库标注（唯一解/多解/无解/未决）。
type StudioSolveLabel string

const (
	StudioLabelUnique     StudioSolveLabel = "唯一解"
	StudioLabelMulti      StudioSolveLabel = "多解"
	StudioLabelNoSolution StudioSolveLabel = "无解"
	StudioLabelUndecided  StudioSolveLabel = "未决"
)

// SolveLabelOf 求解状态与解法数 → 入库标注（studioValidate.ts solveLabelOf）。
func SolveLabelOf(status SolveStatus, solutionCount int) StudioSolveLabel {
	switch status {
	case SolveSolved:
		if solutionCount == 1 {
			return StudioLabelUnique
		}
		return StudioLabelMulti
	case SolveNoSolution:
		return StudioLabelNoSolution
	default:
		return StudioLabelUndecided
	}
}

func pad2(v int) string { return fmt.Sprintf("%02d", v) }

// StudioRecordTitle 自动生成棋谱标题：`M-D 红方残局（唯一解/多解/无解/未决）`
// （endgame_studio_page.dart:726-735）。
func StudioRecordTitle(label StudioSolveLabel, redTurn bool, now time.Time) string {
	side := "黑"
	if redTurn {
		side = "红"
	}
	return fmt.Sprintf("%d-%s %s方残局（%s）", now.Month(), pad2(now.Day()), side, label)
}

// StudioUnsolvedTitle 未求解保存的标题：`M-D 红方残局（未求解）`
// （endgame_studio_page.dart:526-528）。
func StudioUnsolvedTitle(redTurn bool, now time.Time) string {
	side := "黑"
	if redTurn {
		side = "红"
	}
	return fmt.Sprintf("%d-%s %s方残局（未求解）", now.Month(), pad2(now.Day()), side)
}
