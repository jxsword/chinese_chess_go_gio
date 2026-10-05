package rules

// FEN 编解码测试（对齐原版 fen_test.dart 5 用例 + 02 §1.4 异常契约；
// Electron 版 test/rules/fen.spec.ts 的 Go 对应）。
import (
	"errors"
	"strings"
	"testing"
)

func TestIsValidFenInitial(t *testing.T) {
	if !IsValidFen(FENInitial) {
		t.Fatalf("IsValidFen(FENInitial) = false, 期望 true")
	}
}

func TestIsValidFenCases(t *testing.T) {
	cases := []struct {
		fen  string
		want bool
	}{
		{"", false},
		{"9/9/9/9/9/9/9/9/9/9 w - - 0 1", true},
		{"9/9/9/9/9/9/9/9/9/8 w - - 0 1", false},
		{"xxxxxxxxx/9/9/9/9/9/9/9/9/9 w - - 0 1", false},
		// 行数不对（只有 9 行）。
		{"rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/9/9/9 w - - 0 1", false},
	}
	for _, c := range cases {
		if got := IsValidFen(c.fen); got != c.want {
			t.Errorf("IsValidFen(%q) = %v, 期望 %v", c.fen, got, c.want)
		}
	}
}

func TestParseBoardAndBoardGridToFenRoundTrip(t *testing.T) {
	grid, err := ParseBoardFen(FENInitial)
	if err != nil {
		t.Fatalf("ParseBoardFen(FENInitial) 报错: %v", err)
	}
	want := strings.Split(FENInitial, " ")[0]
	if got := BoardGridToFen(grid); got != want {
		t.Errorf("BoardGridToFen(parseBoardFen) = %q, 期望 %q", got, want)
	}
}

func TestParseBoardInitialPositions(t *testing.T) {
	grid, err := ParseBoardFen(FENInitial)
	if err != nil {
		t.Fatalf("ParseBoardFen(FENInitial) 报错: %v", err)
	}
	if p := grid[9][0]; p == nil || p.Kind != Rook || p.Side != Red {
		t.Errorf("grid[9][0] = %v, 期望红车（row=9, col=0）", p)
	}
	if p := grid[0][0]; p == nil || p.Kind != Rook || p.Side != Black {
		t.Errorf("grid[0][0] = %v, 期望黑车", p)
	}
}

func TestParseTurnFen(t *testing.T) {
	if !ParseTurnFen(FENInitial) {
		t.Errorf("ParseTurnFen(FENInitial) = false, 期望 true（红方先行）")
	}
	if ParseTurnFen("9/9/9/9/9/9/9/9/9/9 b - - 0 1") {
		t.Errorf("ParseTurnFen(b) = true, 期望 false")
	}
}

// parseBoard 对行数错/行长≠9/非法字符抛 FormatException 等价异常（02 §1.4）。
func TestParseBoardFenErrors(t *testing.T) {
	cases := []string{
		"9/9/9/9/9/9/9/9/9 w - - 0 1",           // 行数错（9 行）
		"9/9/9/9/9/9/9/9/8 w - - 0 1",           // 行长≠9
		"xxxxxxxxx/9/9/9/9/9/9/9/9/9 w - - 0 1", // 非法字符
	}
	for _, fen := range cases {
		_, err := ParseBoardFen(fen)
		if err == nil {
			t.Errorf("ParseBoardFen(%q) 未报错, 期望 FenFormatError", fen)
			continue
		}
		var fenErr *FenFormatError
		if !errors.As(err, &fenErr) {
			t.Errorf("ParseBoardFen(%q) 错误类型 = %T, 期望 *FenFormatError", fen, err)
		}
	}
}
