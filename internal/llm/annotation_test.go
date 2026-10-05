package llm

// MoveAnnotation 用例（T4.2，09 §2 move_annotation 6 用例；annotation.spec.ts 移植）。

import (
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

func mvCode(code string) rules.Move {
	from := DecodeCell(code[:2])
	to := DecodeCell(code[3:5])
	return rules.Move{From: *from, To: *to}
}

// mustBoard 由 FEN 构造盘面（失败即 Fatal）。
func mustBoard(t *testing.T, fen string) *rules.Board {
	t.Helper()
	b, err := rules.FromFen(fen)
	if err != nil {
		t.Fatalf("FEN 非法 %q：%v", fen, err)
	}
	return b
}

func TestAnnotateMove(t *testing.T) {
	// 吃子注解：车吃卒（被吃子取棋盘实际局面）
	board := mustBoard(t, "4k4/9/9/9/9/p8/9/9/9/R2K5 w - - 0 1")
	if got := AnnotateMove(board, mvCode("a9-a5")); got != "a9-a5(车九进四,吃卒)" {
		t.Fatalf("吃子注解 = %q", got)
	}

	// 将军注解：走后对方被将军（probe 用走后轮走方判定）
	board2 := mustBoard(t, "4k4/9/9/9/9/9/9/9/9/3K4R w - - 0 1")
	if got := AnnotateMove(board2, mvCode("i9-i0")); got != "i9-i0(车一进九,将军)" {
		t.Fatalf("将军注解 = %q", got)
	}

	// 无吃无将：仅中文记法
	board3 := mustBoard(t, "4k4/9/9/9/9/9/9/9/9/3KN4 w - - 0 1")
	if got := AnnotateMove(board3, mvCode("e9-g8")); got != "e9-g8(马五进三)" {
		t.Fatalf("无吃无将注解 = %q", got)
	}

	// 起点无棋子退化为纯坐标
	board4 := mustBoard(t, "4k4/9/9/9/9/9/9/9/9/4K4 w - - 0 1")
	if got := AnnotateMove(board4, mvCode("a4-a5")); got != "a4-a5" {
		t.Fatalf("空起点注解 = %q", got)
	}
}

func TestScoreBucket(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "最佳/均势"}, {30, "最佳/均势"}, {31, "略亏"}, {100, "略亏"},
		{101, "明显亏（约半子）"}, {250, "明显亏（约半子）"}, {251, "大亏（丢一马/一炮级）"},
		{600, "大亏（丢一马/一炮级）"}, {601, "致命（丢车/被将杀级）"}, {-50, "最佳/均势"},
	}
	for _, c := range cases {
		if got := ScoreBucket(c.in); got != c.want {
			t.Errorf("ScoreBucket(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAnnotatedWithBucket(t *testing.T) {
	board := mustBoard(t, "4k4/9/9/9/9/p8/9/9/9/R2K5 w - - 0 1")
	want := "a9-a5(车九进四,吃卒) — 大亏（丢一马/一炮级）"
	if got := AnnotatedWithBucket(board, mvCode("a9-a5"), 300); got != want {
		t.Fatalf("候选行 = %q, want %q", got, want)
	}
}

func TestAsciiBoard(t *testing.T) {
	want := "    a b c d e f g h i\n" +
		"0  r n b a k a b n r\n" +
		"1  . . . . . . . . .\n" +
		"2  . c . . . . . c .\n" +
		"3  p . p . p . p . p\n" +
		"4  . . . . . . . . .\n" +
		"5  . . . . . . . . .\n" +
		"6  P . P . P . P . P\n" +
		"7  . C . . . . . C .\n" +
		"8  . . . . . . . . .\n" +
		"9  R N B A K A B N R\n"
	if got := AsciiBoard(mustBoard(t, rules.FENInitial)); got != want {
		t.Fatalf("初始局面 ASCII 图不符：\n%s", got)
	}
}
