package parsers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// XQF 解析等价用例集（test/features/puzzle/model/parsers/xqf_parser_test.dart 等价，06 文档 §3）。

// sampleBytes 语料联接缺失时的真实格式锚点：仓库自带样例（v0x0D，位置置换加密）。
func sampleBytes(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "xqf", "sample_xqf.xqf"))
	if err != nil {
		t.Fatalf("样例缺失: %v", err)
	}
	return b
}

// replayApplied 用规则内核重放走法，返回成功应用的手数（xqf_parser_test.dart 重放校验同款）。
func replayApplied(t *testing.T, initialFen string, moves []string) int {
	t.Helper()
	board, err := rules.FromFen(initialFen)
	if err != nil {
		t.Fatalf("FEN 非法: %v", err)
	}
	applied := 0
	for _, iccs := range moves {
		p := ParseIccs(iccs)
		if p == nil {
			break
		}
		legal := false
		for _, m := range board.LegalMovesFor(p.From) {
			if m.From == p.From && m.To == p.To {
				legal = true
				break
			}
		}
		if !legal {
			break
		}
		board.ApplyMove(rules.Move{From: p.From, To: p.To})
		applied++
	}
	return applied
}

func TestParseXqfBadMagicThrows(t *testing.T) {
	bad := make([]byte, 1100)
	bad[0] = 0x58
	bad[1] = 0x58
	bad[2] = 0x0a
	if _, err := ParseXqf(bad, "xqf"); err == nil {
		t.Fatal("坏魔数应报错")
	}
}

func TestParseXqfTooShortThrows(t *testing.T) {
	if _, err := ParseXqf([]byte{0x58, 0x51, 0x0a, 1, 2, 3}, "xqf"); err == nil {
		t.Fatal("文件过短应报错")
	}
}

func TestParseXqfMissingKingThrows(t *testing.T) {
	// 无黑将（k 缺失）的 FEN 构造 v0x0A 文件。
	bytes := buildXqf(t, xqfBuildOptions{
		version: 0x0a,
		fen:     "rnba1abnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1",
		moves:   []string{"h2e2"},
	})
	_, err := ParseXqf(bytes, "xqf")
	if err == nil || !strings.Contains(err.Error(), "缺少将/帅") {
		t.Fatalf("缺将帅应报错, got %v", err)
	}
}

func TestParseXqfRealSampleEncryptedV12(t *testing.T) {
	// 真语料样例（v0x0D，位置置换加密）：FEN/元数据/全量重放合法。
	p, err := ParseXqf(sampleBytes(t), "样例")
	if err != nil {
		t.Fatal(err)
	}
	if p.Format != "xqf" {
		t.Fatalf("format = %s", p.Format)
	}
	if p.Title == nil || *p.Title != "挺兵对卒底炮" {
		t.Fatalf("title = %v", p.Title)
	}
	if p.Description == nil || !strings.Contains(*p.Description, "96全国象棋锦标赛") || !strings.Contains(*p.Description, "1996.5.20") {
		t.Fatalf("description = %v", p.Description)
	}
	if p.InitialFen != rules.FENInitial {
		t.Fatalf("initialFen = %q", p.InitialFen)
	}
	if len(p.SolutionMoves) != 77 {
		t.Fatalf("着数 = %d, 期望 77", len(p.SolutionMoves))
	}
	want := []string{"c3c4", "b7c7", "h2e2", "c9e7", "h0g2"}
	if strings.Join(p.SolutionMoves[:5], ",") != strings.Join(want, ",") {
		t.Fatalf("前 5 着 = %v", p.SolutionMoves[:5])
	}
	if p.Difficulty != 3 {
		t.Fatalf("difficulty = %d", p.Difficulty)
	}
	// 全部走法在规则内核上重放合法（真实加密格式的金标准锚点）。
	if applied := replayApplied(t, p.InitialFen, p.SolutionMoves); applied != 77 {
		t.Fatalf("重放合法着数 = %d, 期望 77", applied)
	}
}

func TestParseXqfRoundTripLegacyV10(t *testing.T) {
	// 往返：旧格式 v0x0A（无加密）。
	moves := []string{"h2e2", "h9g7", "h0g2", "i9h9", "c3c4", "c6c5"}
	bytes := buildXqf(t, xqfBuildOptions{
		version: 0x0a,
		fen:     rules.FENInitial,
		moves:   moves,
		title:   "中炮对屏风马",
		event:   "测试赛事",
		date:    "2024.1.1",
		red:     "红方选手",
		black:   "黑方选手",
	})
	p, err := ParseXqf(bytes, "往返")
	if err != nil {
		t.Fatal(err)
	}
	if p.InitialFen != rules.FENInitial {
		t.Fatalf("initialFen = %q", p.InitialFen)
	}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(moves, ",") {
		t.Fatalf("moves = %v", p.SolutionMoves)
	}
	if p.Title == nil || *p.Title != "中炮对屏风马" {
		t.Fatalf("title = %v", p.Title)
	}
	if p.Description == nil || *p.Description != "测试赛事 · 2024.1.1 · 红方选手 vs 黑方选手" {
		t.Fatalf("description = %v", p.Description)
	}
	if p.Source != "往返" {
		t.Fatalf("source = %q", p.Source)
	}
}

func TestParseXqfRoundTripEncryptedV12NoPermutation(t *testing.T) {
	// 往返：加密版本 v0x0C（无布局置换）。
	moves := []string{"c3c4", "b7c7", "h2e2"}
	bytes := buildXqf(t, xqfBuildOptions{version: 0x0c, fen: rules.FENInitial, moves: moves, title: "加密局"})
	p, err := ParseXqf(bytes, "xqf")
	if err != nil {
		t.Fatal(err)
	}
	if p.InitialFen != rules.FENInitial {
		t.Fatalf("initialFen = %q", p.InitialFen)
	}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(moves, ",") {
		t.Fatalf("moves = %v", p.SolutionMoves)
	}
	if p.Title == nil || *p.Title != "加密局" {
		t.Fatalf("title = %v", p.Title)
	}
}

func TestParseXqfRoundTripHighVersionPermutationHandicap(t *testing.T) {
	// 往返：高版本 v0x12（布局位置置换）+ 让子盘面：红让左车（a 路车缺失）→ 0xFF 让子路径。
	fen := "rnbakabn1/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"
	moves := []string{"h2e2", "h9g7"}
	bytes := buildXqf(t, xqfBuildOptions{version: 0x12, fen: fen, moves: moves, title: "让车局"})
	p, err := ParseXqf(bytes, "xqf")
	if err != nil {
		t.Fatal(err)
	}
	if p.InitialFen != fen {
		t.Fatalf("initialFen = %q, 期望 %q", p.InitialFen, fen)
	}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(moves, ",") {
		t.Fatalf("moves = %v", p.SolutionMoves)
	}
	if p.Title == nil || *p.Title != "让车局" {
		t.Fatalf("title = %v", p.Title)
	}
}

func TestParseXqfRoundTripBlackFirstInference(t *testing.T) {
	// 往返：残局盘面 + 黑先行棋方推断：首着起点为黑子 → isRedTurn=false（xqf_parser.dart:133-140）。
	fen := "3k5/9/9/9/9/9/9/9/9/4K4 b - - 0 1"
	moves := []string{"d9e8"}
	bytes := buildXqf(t, xqfBuildOptions{version: 0x0b, fen: fen, moves: moves})
	p, err := ParseXqf(bytes, "xqf")
	if err != nil {
		t.Fatal(err)
	}
	if p.InitialFen != fen {
		t.Fatalf("initialFen = %q", p.InitialFen)
	}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(moves, ",") {
		t.Fatalf("moves = %v", p.SolutionMoves)
	}
}

func TestXqfHeaderSizeConstant(t *testing.T) {
	if XQFHeaderSize != 0x400 {
		t.Fatalf("XQFHeaderSize = %#x", XQFHeaderSize)
	}
}
