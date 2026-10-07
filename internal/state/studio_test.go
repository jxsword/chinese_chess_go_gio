package state

// 工作室摆盘规则/整体校验单测（T6'.2，翻译锚点 = setupRules.ts /
// studioValidate.ts 行为；上游无独立 spec，用例按 TS 实现逐分支直译 + 金标准
// 求解 FEN 联动锚定）。

import (
	"strings"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

func mustGrid(t *testing.T, fen string) rules.BoardGrid {
	t.Helper()
	grid, err := rules.ParseBoardFen(fen)
	if err != nil {
		t.Fatalf("ParseBoardFen(%q): %v", fen, err)
	}
	return grid
}

// spec: PlacementIssue——九宫/士斜线点/象田字点/兵卒底线逐分支。
func TestPlacementIssue(t *testing.T) {
	redKing := rules.Piece{Kind: rules.King, Side: rules.Red}
	redAdvisor := rules.Piece{Kind: rules.Advisor, Side: rules.Red}
	redMinister := rules.Piece{Kind: rules.Minister, Side: rules.Red}
	redPawn := rules.Piece{Kind: rules.Pawn, Side: rules.Red}
	blackPawn := rules.Piece{Kind: rules.Pawn, Side: rules.Black}

	cases := []struct {
		name  string
		piece rules.Piece
		col   int
		row   int
		want  string // 空串 = 合法
	}{
		{"帅出九宫", redKing, 4, 5, "帅/将只能放在九宫内的 9 个位置"},
		{"帅在九宫", redKing, 4, 9, ""},
		{"士非斜线点", redAdvisor, 4, 9, "士/仕只能放在九宫的 5 个斜线位置上"},
		{"士在斜线点", redAdvisor, 3, 9, ""},
		{"黑士斜线点", rules.Piece{Kind: rules.Advisor, Side: rules.Black}, 3, 0, ""},
		{"象过河", redMinister, 2, 4, "相/象不能摆到对方半场"},
		{"相奇数列", redMinister, 3, 9, "相/象只能落在偶数列的田字点上"},
		{"相偶数行", redMinister, 2, 8, "相只能放在己方半场 5/7/9 排的田字点上"},
		{"相合法", redMinister, 2, 9, ""},
		{"黑象合法", rules.Piece{Kind: rules.Minister, Side: rules.Black}, 2, 0, ""},
		{"红兵底线", redPawn, 0, 8, "兵/卒不能放在本方底线三排"},
		{"红兵过河前", redPawn, 0, 6, ""},
		{"红兵到底线", redPawn, 0, 3, ""},
		{"黑卒底线", blackPawn, 0, 1, "兵/卒不能放在本方底线三排"},
		{"黑卒过河", blackPawn, 0, 5, ""},
		{"车无限制", rules.Piece{Kind: rules.Rook, Side: rules.Red}, 4, 4, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PlacementIssue(c.piece, c.col, c.row); got != c.want {
				t.Fatalf("PlacementIssue(%v,%d,%d) = %q, 欲 %q", c.piece, c.col, c.row, got, c.want)
			}
		})
	}
}

// spec: CountIssue / CountIssueForPlacement——数量上限与同格替换豁免。
func TestCountIssue(t *testing.T) {
	redRook := rules.Piece{Kind: rules.Rook, Side: rules.Red}
	if got := CountIssue(map[rules.Piece]int{redRook: 3}); got != "红方车最多 2 枚（当前 3 枚）" {
		t.Fatalf("CountIssue 超限 = %q", got)
	}
	if got := CountIssue(map[rules.Piece]int{redRook: 2}); got != "" {
		t.Fatalf("CountIssue 合法 = %q", got)
	}
	// 放置第 3 枚车 → 超限；同格替换 → 豁免。
	if got := CountIssueForPlacement(redRook, 2, nil); got != "红方车最多 2 枚" {
		t.Fatalf("放置超限 = %q", got)
	}
	if got := CountIssueForPlacement(redRook, 2, &redRook); got != "" {
		t.Fatalf("同格替换应豁免 = %q", got)
	}
}

// spec: ValidateStudioPosition——整体校验五条逐条（含求解金标准 FEN-A 通过）。
func TestValidateStudioPosition(t *testing.T) {
	t.Run("初始局面通过", func(t *testing.T) {
		grid := mustGrid(t, rules.FENInitial)
		if got := ValidateStudioPosition(grid, true); len(got) != 0 {
			t.Fatalf("初始局面应通过: %v", got)
		}
	})
	t.Run("缺王拦截", func(t *testing.T) {
		grid := mustGrid(t, "9/9/9/9/9/9/9/9/9/4K4 w") // 黑方无王
		got := ValidateStudioPosition(grid, true)
		if len(got) == 0 || !strings.Contains(got[0], "双方必须各有一个将/帅") {
			t.Fatalf("缺王应拦截: %v", got)
		}
	})
	t.Run("位置非法逐格标注", func(t *testing.T) {
		// 红兵放在红方底线（row 9）→ 位置非法（双王齐全，先报位置）。
		grid := mustGrid(t, "3k5/9/9/9/9/9/9/9/9/PK5R1 w")
		got := ValidateStudioPosition(grid, true)
		found := false
		for _, p := range got {
			if strings.Contains(p, "兵/卒不能放在本方底线三排") {
				found = true
			}
		}
		if !found {
			t.Fatalf("应含兵底线问题: %v", got)
		}
	})
	t.Run("轮走方对手正被将军拦截", func(t *testing.T) {
		// 红车直照黑将（同列相邻）且轮红走 → 黑方（对手）正被将军 → 非法。
		grid := mustGrid(t, "3k5/3R5/9/9/9/9/9/9/9/4K4 w")
		got := ValidateStudioPosition(grid, true)
		joined := strings.Join(got, "；")
		if !strings.Contains(joined, "轮走方行棋前对方已被将军，局面非法") {
			t.Fatalf("应含对手被将军问题: %v", got)
		}
	})
	t.Run("轮走方无着可走拦截", func(t *testing.T) {
		// 双车闷杀（黑王四格全被控、未正被将军、红方子力数量合法）且轮黑走。
		grid := mustGrid(t, "4k4/3R1R3/9/9/9/9/9/9/9/3K5 b")
		got := ValidateStudioPosition(grid, false)
		joined := strings.Join(got, "；")
		if !strings.Contains(joined, "轮走方已无着可走") {
			t.Fatalf("应含无着可走问题: %v", got)
		}
	})
	t.Run("FEN-A（金标准多解残局）通过", func(t *testing.T) {
		grid := mustGrid(t, "3k5/9/9/9/R8/8R/9/9/9/4K4 w")
		if got := ValidateStudioPosition(grid, true); len(got) != 0 {
			t.Fatalf("FEN-A 应通过: %v", got)
		}
	})
}

// spec: 标题与标注——solveLabelOf / studioRecordTitle / studioUnsolvedTitle。
func TestStudioTitles(t *testing.T) {
	if got := SolveLabelOf(SolveSolved, 1); got != StudioLabelUnique {
		t.Fatalf("唯一解 = %q", got)
	}
	if got := SolveLabelOf(SolveSolved, 2); got != StudioLabelMulti {
		t.Fatalf("多解 = %q", got)
	}
	if got := SolveLabelOf(SolveNoSolution, 0); got != StudioLabelNoSolution {
		t.Fatalf("无解 = %q", got)
	}
	if got := SolveLabelOf(SolveTimeout, 0); got != StudioLabelUndecided {
		t.Fatalf("未决 = %q", got)
	}
	now := time.Date(2026, 10, 7, 0, 0, 0, 0, time.Local)
	if got := StudioRecordTitle(StudioLabelUnique, true, now); got != "10-07 红方残局（唯一解）" {
		t.Fatalf("求解标题 = %q", got)
	}
	if got := StudioUnsolvedTitle(false, now); got != "10-07 黑方残局（未求解）" {
		t.Fatalf("未求解标题 = %q", got)
	}
}
