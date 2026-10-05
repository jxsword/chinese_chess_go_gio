package parsers

import "testing"

// ParsedPuzzle 纯函数等价用例（puzzle_data.dart:51-109；Electron 版经由
// puzzleParser/corpusBrowser 用例间接覆盖，Go 侧直接锁定阈值与关键词口径）。

func TestDifficultyFromMoveCountThresholds(t *testing.T) {
	cases := map[int]int{0: 1, 20: 1, 21: 2, 40: 2, 41: 3, 80: 3, 81: 4, 150: 4, 151: 5, 300: 5}
	for moves, want := range cases {
		if got := DifficultyFromMoveCount(moves); got != want {
			t.Fatalf("DifficultyFromMoveCount(%d) = %d, 期望 %d", moves, got, want)
		}
	}
}

func TestDifficultyText(t *testing.T) {
	want := map[int]string{1: "入门", 2: "初级", 3: "中级", 4: "高级", 5: "职业", 0: "未知"}
	for d, s := range want {
		if got := DifficultyText(d); got != s {
			t.Fatalf("DifficultyText(%d) = %q, 期望 %q", d, got, s)
		}
	}
}

const standardFen = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

func TestIsEndgamePuzzleSourceKeywordFirst(t *testing.T) {
	// 来源关键词优先：残局/排局/杀势 → 是（即使盘面为标准开局）。
	for _, source := range []string{"残局/适情雅趣", "排局", "ChessQ-gamebooks/杀势集"} {
		if !IsEndgamePuzzle(source, standardFen) {
			t.Fatalf("IsEndgamePuzzle(%q) 应为 true", source)
		}
	}
	// 全局类关键词 → 否（即使盘面非标准开局，如让子局）。
	for _, source := range []string{"全局", "大师", "比赛", "布局", "中局", "名局", "让子/让左车"} {
		if IsEndgamePuzzle(source, "rnbakabn1/9/9/9/9/9/9/9/9/4K4 w - - 0 1") {
			t.Fatalf("IsEndgamePuzzle(%q) 应为 false", source)
		}
	}
}

func TestIsEndgamePuzzleByInitialBoard(t *testing.T) {
	// 无关键词：按初始盘面是否为标准开局判定。
	if IsEndgamePuzzle("其他", standardFen) {
		t.Fatal("标准开局应为 false")
	}
	if !IsEndgamePuzzle("其他", "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1") {
		t.Fatal("非标准开局应为 true")
	}
}

func TestKindLabel(t *testing.T) {
	if KindLabel("残局", standardFen) != "残局题" {
		t.Fatal("残局 → 残局题")
	}
	if KindLabel("其他", standardFen) != "全局对局" {
		t.Fatal("标准开局 → 全局对局")
	}
}

func TestCopyPuzzleWithOverrides(t *testing.T) {
	p := ParsedPuzzle{ID: "a", Source: "s", Format: "pgn", Difficulty: 1}
	id := "a#"
	diff := 3
	got := CopyPuzzleWith(p, PuzzleOverrides{ID: &id, Difficulty: &diff, SolutionMoves: []string{"h2e2"}})
	if got.ID != "a#" || got.Difficulty != 3 || len(got.SolutionMoves) != 1 {
		t.Fatalf("overrides 未生效: %+v", got)
	}
	if p.ID != "a" || p.Difficulty != 1 || len(p.SolutionMoves) != 0 {
		t.Fatalf("原值被修改: %+v", p)
	}
}
