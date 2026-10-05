package parsers

import (
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 解析门面等价用例集（test/features/puzzle/model/puzzle_parser_test.dart 7 条，06 文档 §4.5）。

func bytesOf(text string) []byte { return []byte(text) }

func TestParsePuzzleFileDispatchesPgn(t *testing.T) {
	// 按扩展名分发：.pgn 走 PGN 解析。
	puzzles, err := ParsePuzzleFile("对局.pgn", bytesOf("1. 炮二平五 马8进7\n"), "测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(puzzles) != 1 || puzzles[0].Format != "pgn" {
		t.Fatalf("puzzles = %d", len(puzzles))
	}
	if strings.Join(puzzles[0].SolutionMoves, ",") != "h2e2,h9g7" {
		t.Fatalf("moves = %v", puzzles[0].SolutionMoves)
	}
}

func TestParsePuzzleFilePgnsExtension(t *testing.T) {
	// .pgns 扩展名同样分发到 PGN 解析。
	puzzles, err := ParsePuzzleFile("合集.pgns", bytesOf("1. 兵七进一 卒7进1\n"), "测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(puzzles) != 1 || puzzles[0].SolutionMoves[0] != "c3c4" {
		t.Fatalf("puzzles = %d", len(puzzles))
	}
}

func TestParsePuzzleFileUnknownExtension(t *testing.T) {
	if _, err := ParsePuzzleFile("棋局.cbf", bytesOf(""), "测试"); err == nil || !strings.Contains(err.Error(), "不支持的棋谱格式") {
		t.Fatalf("未知扩展名应报错, got %v", err)
	}
}

func TestParsePuzzleFileTruncatesIllegalTail(t *testing.T) {
	// "炮二平五 马8进7 h1h9" 前两着合法；追加一着故意非法的 ICCS（起点无子）触发截断。
	puzzles, err := ParsePuzzleFile("截断.pgn", bytesOf("1. 炮二平五 马8进7 h1h9\n"), "测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(puzzles) != 1 {
		t.Fatalf("puzzles = %d", len(puzzles))
	}
	if strings.Join(puzzles[0].SolutionMoves, ",") != "h2e2,h9g7" {
		t.Fatalf("moves = %v", puzzles[0].SolutionMoves)
	}
}

func TestParsePuzzleFileDropsAllIllegal(t *testing.T) {
	// 全部着法非法时丢弃该局。
	puzzles, err := ParsePuzzleFile("空.pgn", bytesOf("1. 马九进九\n"), "测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(puzzles) != 0 {
		t.Fatalf("puzzles = %d", len(puzzles))
	}
}

func TestParsePuzzleFileDedupesIDs(t *testing.T) {
	// 重复 id 自动去重。
	one := "1. 炮二平五 马8进7\n"
	multi := "[Event \"同一标题\"]\n\n" + one + "[Event \"同一标题\"]\n\n" + one
	puzzles, err := ParsePuzzleFile("重复.pgn", bytesOf(multi), "测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(puzzles) != 2 {
		t.Fatalf("puzzles = %d", len(puzzles))
	}
	if puzzles[0].ID == puzzles[1].ID {
		t.Fatal("重复 id 未去重")
	}
	if !strings.HasPrefix(puzzles[1].ID, puzzles[0].ID) {
		t.Fatalf("id 后缀缺失: %q vs %q", puzzles[1].ID, puzzles[0].ID)
	}
}

func TestParsePuzzleFileDispatchesXqf(t *testing.T) {
	// .xqf 扩展名分发到 XQF 解析（含重放校验）。
	bytes := buildXqf(t, xqfBuildOptions{
		version: 0x0a,
		fen:     rules.FENInitial,
		moves:   []string{"h2e2", "h9g7"},
	})
	puzzles, err := ParsePuzzleFile("残局.xqf", bytes, "测试")
	if err != nil {
		t.Fatal(err)
	}
	if len(puzzles) != 1 || puzzles[0].Format != "xqf" {
		t.Fatalf("puzzles = %d", len(puzzles))
	}
	if strings.Join(puzzles[0].SolutionMoves, ",") != "h2e2,h9g7" {
		t.Fatalf("moves = %v", puzzles[0].SolutionMoves)
	}
	if !strings.HasPrefix(puzzles[0].ID, "xqf/测试/") {
		t.Fatalf("id = %q", puzzles[0].ID)
	}
}

func TestShouldStreamImportThreshold(t *testing.T) {
	// 大文件流式导入判定：多局 PGN 大文件走流式路径，其余走整读。
	const mb = 1024 * 1024
	cases := []struct {
		name string
		size int64
		want bool
	}{
		{"合集.pgns", 9 * mb, true},
		{"对局.pgn", 9 * mb, true},
		{"合集.pgns", 8 * mb, false}, // 等于阈值不流式
		{"对局.pgn", 1024, false},
		{"残局.xqf", 9 * mb, false}, // XQF 单文件不大，始终整读
	}
	for _, c := range cases {
		if got := ShouldStreamImport(c.name, c.size); got != c.want {
			t.Fatalf("ShouldStreamImport(%q, %d) = %v, 期望 %v", c.name, c.size, got, c.want)
		}
	}
}
