package parsers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// PGN 解析等价用例集（test/features/puzzle/model/parsers/pgn_parser_test.dart 12 条，06 文档 §4）。

const initialFen = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"

// fsSource 文件源（与 storage 层 corpus 服务同一形态的测试替身；打开的 fd 由测试收尾统一关闭，
// 09 §2.4：先关 fd 再删目录——Windows 句柄滞后教训）。
type fsSource struct {
	f    *os.File
	size int64
}

func openFsSource(t *testing.T, path string) *fsSource {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return &fsSource{f: f, size: st.Size()}
}

func (s *fsSource) ByteLength() int64 { return s.size }

func (s *fsSource) Read(offset int64, length int) []byte {
	if offset >= s.size {
		return []byte{}
	}
	if length > int(s.size-offset) {
		length = int(s.size - offset)
	}
	buf := make([]byte, length)
	n, err := s.f.ReadAt(buf, offset)
	if n < 0 {
		n = 0
	}
	if err != nil && n == 0 {
		return []byte{}
	}
	return buf
}

func parseGameText(t *testing.T, text, source string) *ParsedPuzzle {
	t.Helper()
	p, err := ParseGame(text, source)
	if err != nil {
		t.Fatalf("ParseGame: %v", err)
	}
	return p
}

func TestParseGameChineseNotationNoFen(t *testing.T) {
	// 标准初始局面 + 中文纵线记谱（无 FEN 标签）。
	text := "[Event \"测试局\"]\n" +
		"[Red \"红方\"]\n" +
		"[Black \"黑方\"]\n" +
		"\n" +
		"1. 炮二平五  马8进7\n" +
		"2. 马二进三  车9平8\n" +
		"*"
	p := parseGameText(t, text, "测试")
	if p.InitialFen != initialFen {
		t.Fatalf("initialFen = %q", p.InitialFen)
	}
	want := []string{"h2e2", "h9g7", "h0g2", "i9h9"}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(want, ",") {
		t.Fatalf("moves = %v, 期望 %v", p.SolutionMoves, want)
	}
	if p.Title == nil || *p.Title != "测试局" || p.Format != "pgn" || p.Difficulty != 1 {
		t.Fatalf("title/format/difficulty = %v/%s/%d", p.Title, p.Format, p.Difficulty)
	}
}

func TestParseGameIccsWithFen(t *testing.T) {
	text := "[FEN \"" + initialFen + "\"]\n\n1. C3-C4 C9-E7\n2. B2-D2 G6-G5\n"
	p := parseGameText(t, text, "pgn")
	want := []string{"c3c4", "c9e7", "b2d2", "g6g5"}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(want, ",") {
		t.Fatalf("moves = %v, 期望 %v", p.SolutionMoves, want)
	}
}

func TestParseGameSkipsCommentsVariationsNag(t *testing.T) {
	// 注释、行注释、变着、NAG 与步数序号被跳过。
	text := "1. 炮二平五 {好棋; 得中路} 马8进7 ; 行注释到行尾\n" +
		"2. 马二进三 (2. 卒3进1 3. 兵三进一) 车9平8 $1\n" +
		"3. 车一平二\n"
	p := parseGameText(t, text, "pgn")
	want := []string{"h2e2", "h9g7", "h0g2", "i9h9", "i0h0"}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(want, ",") {
		t.Fatalf("moves = %v, 期望 %v", p.SolutionMoves, want)
	}
}

func TestParseGameFullwidthDigits(t *testing.T) {
	// 全角数字（部分生成器黑方记谱）可解析。
	text := "1. 兵七进一  象３进５\n2. 炮八平六  卒７进１\n"
	p := parseGameText(t, text, "pgn")
	// 兵七进一 c3c4；象3进5 c9e7；炮八平六 b2d2；卒7进1 g6g5
	want := []string{"c3c4", "c9e7", "b2d2", "g6g5"}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(want, ",") {
		t.Fatalf("moves = %v, 期望 %v", p.SolutionMoves, want)
	}
}

func TestParseGameFrontModifierDisambiguation(t *testing.T) {
	// 前/后修饰消解同列多子：红方双炮同在五路（col 4），红方"前"为 row 较小者。
	fen := "4k4/9/9/9/9/9/4C4/9/4C4/4K4 w - - 0 1"
	text := "[FEN \"" + fen + "\"]\n\n1. 前炮进二\n"
	p := parseGameText(t, text, "pgn")
	// 前炮 (4,6)=e3，直进两格 → (4,4)=e5
	want := []string{"e3e5"}
	if strings.Join(p.SolutionMoves, ",") != strings.Join(want, ",") {
		t.Fatalf("moves = %v, 期望 %v", p.SolutionMoves, want)
	}
}

func TestParseGameUnresolvableMoveThrows(t *testing.T) {
	// 兵不可以在同一路平移五格。
	if _, err := ParseGame("1. 兵九平五\n", "pgn"); err == nil {
		t.Fatal("着法无法消解应报错")
	}
}

func TestParseGameNoMovesThrows(t *testing.T) {
	if _, err := ParseGame("[Event \"空局\"]\n*", "pgn"); err == nil {
		t.Fatal("无任何着法应报错")
	}
}

func TestParseGamesSplitsTwoGames(t *testing.T) {
	text := "[Event \"第一局\"]\n\n1. 炮二平五 马8进7\n\n[Event \"第二局\"]\n\n1. 兵七进一 卒7进1\n"
	games, err := ParseGames(text, "pgn")
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 2 || *games[0].Title != "第一局" || *games[1].Title != "第二局" {
		t.Fatalf("games = %d, titles %v/%v", len(games), games[0].Title, games[1].Title)
	}
	if games[1].SolutionMoves[0] != "c3c4" {
		t.Fatalf("第二局首着 = %s", games[1].SolutionMoves[0])
	}
}

func TestParseGamesSingleFailureDoesNotAffectOthers(t *testing.T) {
	text := "[Event \"坏局\"]\n\n1. 马九进九\n\n[Event \"好局\"]\n\n1. 炮二平五 马8进7\n"
	games, err := ParseGames(text, "pgn")
	if err != nil {
		t.Fatal(err)
	}
	if len(games) != 1 || *games[0].Title != "好局" {
		t.Fatalf("games = %d", len(games))
	}
}

func TestScanGameOffsetsIndexAndReadSlice(t *testing.T) {
	// 扫描偏移索引并按需读取单局。
	content := "[Event \"甲局\"]\n[Red \"红甲\"]\n[Black \"黑甲\"]\n\n1. 炮二平五 马8进7\n2. 马二进三 车9平8\n\n" +
		"[Event \"乙局\"]\n[Red \"红乙\"]\n[Black \"黑乙\"]\n\n1. 兵七进一 卒7进1\n"
	path := writeTemp(t, "multi.pgn", content)
	src := openFsSource(t, path)
	defer src.f.Close()

	index := ScanGameOffsets(src, -1)
	if len(index) != 2 {
		t.Fatalf("index = %d 局", len(index))
	}
	if index[0].Event == nil || *index[0].Event != "甲局" || index[1].Event == nil || *index[1].Event != "乙局" {
		t.Fatalf("events = %v/%v", index[0].Event, index[1].Event)
	}
	if index[1].Red == nil || *index[1].Red != "红乙" {
		t.Fatalf("red = %v", index[1].Red)
	}

	game1 := DecodeUtf8Lossy(src.Read(index[0].Offset, int(index[0].Length)))
	p1, err := ParseGame(game1, "pgn")
	if err != nil {
		t.Fatal(err)
	}
	if len(p1.SolutionMoves) != 4 {
		t.Fatalf("第一局着数 = %d", len(p1.SolutionMoves))
	}
	game2 := DecodeUtf8Lossy(src.Read(index[1].Offset, int(index[1].Length)))
	p2, err := ParseGame(game2, "pgn")
	if err != nil {
		t.Fatal(err)
	}
	if p2.SolutionMoves[0] != "c3c4" || p2.Title == nil || *p2.Title != "乙局" {
		t.Fatalf("第二局 = %v %v", p2.SolutionMoves[0], p2.Title)
	}
}

func TestScanGameOffsetsMaxGamesLimit(t *testing.T) {
	content := "[Event \"甲局\"]\n\n1. 炮二平五 马8进7\n\n[Event \"乙局\"]\n\n1. 兵七进一 卒7进1\n"
	path := writeTemp(t, "multi.pgn", content)
	src := openFsSource(t, path)
	defer src.f.Close()
	index := ScanGameOffsets(src, 1)
	if len(index) != 1 {
		t.Fatalf("maxGames=1 应只返回 1 局, got %d", len(index))
	}
}

func TestScanGameOffsetsOverlongLineTruncated(t *testing.T) {
	// 超长行按 moves 行处理，pending 不再无限累积（P2-5）：
	// 9MB 无换行的单行畸形文件（超过 8MB 阈值）+ 一个正常局。
	long := strings.Repeat("a", 9<<20)
	content := long + "\n[Event \"超长行后的一局\"]\n[Red \"红\"]\n\n1. 炮二平五\n"
	path := writeTemp(t, "long.pgn", content)
	src := openFsSource(t, path)
	defer src.f.Close()

	index := ScanGameOffsets(src, -1)
	// 超长行被计为 moves 行（gameStart=0），随后标签行开启第二局。
	if len(index) != 2 {
		t.Fatalf("index = %d 局", len(index))
	}
	if index[0].Offset != 0 {
		t.Fatalf("index[0].offset = %d", index[0].Offset)
	}
	if index[1].Event == nil || *index[1].Event != "超长行后的一局" || index[1].Red == nil || *index[1].Red != "红" {
		t.Fatalf("第二局摘要 = %v/%v", index[1].Event, index[1].Red)
	}
}

func TestScanGameOffsetsLargeFilePaginationBenchmark(t *testing.T) {
	// 99813 局大文件索引 + 分页切片（验收基准，Electron 版 R5 行为一致）：
	// 生成 99813 局单行紧凑 PGN（每局约 120 字节，文件约 12MB）。
	var b strings.Builder
	for i := 0; i < 99813; i++ {
		b.WriteString("[Event \"对局 ")
		b.WriteString(strings.TrimSpace(itoa(i)))
		b.WriteString("\"]\n[Red \"红")
		b.WriteString(itoa(i))
		b.WriteString("\"]\n[Black \"黑")
		b.WriteString(itoa(i))
		b.WriteString("\"]\n\n1. 炮二平五 马8进7\n2. 马二进三 车9平8\n")
	}
	path := writeTemp(t, "big.pgns", b.String())
	src := openFsSource(t, path)
	defer src.f.Close()

	index := ScanGameOffsets(src, -1)
	if len(index) != 99813 {
		t.Fatalf("index = %d 局", len(index))
	}
	if index[99812].Event == nil || *index[99812].Event != "对局 99812" {
		t.Fatalf("末局 = %v", index[99812].Event)
	}
	// 分页浏览：每页 50，第 1000 页切片可正常解析。
	const pageSize = 50
	page1000 := index[999*pageSize : 1000*pageSize]
	if len(page1000) != 50 {
		t.Fatalf("page1000 = %d", len(page1000))
	}
	if page1000[0].Event == nil || *page1000[0].Event != "对局 49950" {
		t.Fatalf("page1000[0] = %v", page1000[0].Event)
	}
}

func TestPgnGameIndexTitleFallback(t *testing.T) {
	idx := PgnGameIndex{Event: nil, Red: strPtr("红"), Black: nil}
	if PgnGameIndexTitle(idx) != "红 vs ?" {
		t.Fatalf("title = %q", PgnGameIndexTitle(idx))
	}
	idx.Event = strPtr("赛事")
	if PgnGameIndexTitle(idx) != "赛事" {
		t.Fatalf("title = %q", PgnGameIndexTitle(idx))
	}
}

func TestParseGameFenNormalizationTurn(t *testing.T) {
	// FEN 标签黑先行：解析后 initialFen 保留黑先，第一着由黑方消解。
	fen := "3k5/9/9/9/9/9/9/9/9/4K4 b - - 0 1"
	text := "[FEN \"" + fen + "\"]\n\n1. 将4进1\n"
	p := parseGameText(t, text, "pgn")
	if !strings.HasPrefix(p.InitialFen, "3k5/9/9/9/9/9/9/9/9/4K4 b") {
		t.Fatalf("initialFen = %q", p.InitialFen)
	}
	if len(p.SolutionMoves) != 1 || p.SolutionMoves[0] != "d9d8" {
		t.Fatalf("moves = %v", p.SolutionMoves)
	}
}

// ---- 测试辅助 ----

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func strPtr(s string) *string { return &s }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// 保持 rules 引用（部分用例直接断言 FEN 常量形态）。
var _ = rules.FENInitial
