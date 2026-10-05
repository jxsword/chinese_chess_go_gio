package parsers

import (
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// ICCS 等价用例集（test/features/puzzle/model/iccs_test.dart 7 条，06 文档 §2）。

func TestParseIccsCompactLowercase(t *testing.T) {
	r := ParseIccs("h3e3")
	if r == nil {
		t.Fatal("h3e3 应可解析")
	}
	if r.From != rules.Pos(7, 6) || r.To != rules.Pos(4, 6) {
		t.Fatalf("h3e3 = %v, 期望 from(7,6) to(4,6)", r)
	}
}

func TestParseIccsDelimiterUppercase(t *testing.T) {
	r := ParseIccs("H3-E3")
	if r == nil {
		t.Fatal("H3-E3 应可解析")
	}
	if r.From != rules.Pos(7, 6) || r.To != rules.Pos(4, 6) {
		t.Fatalf("H3-E3 = %v, 期望 from(7,6) to(4,6)", r)
	}
}

func TestParseIccsRowZeroIsRedBaseline(t *testing.T) {
	// 行 0 为红方底线（a0 → (0,9)），行 9 为黑方底线（a9 → (0,0)）。
	r := ParseIccs("a0a9")
	if r == nil || r.From != rules.Pos(0, 9) || r.To != rules.Pos(0, 0) {
		t.Fatalf("a0a9 = %v, 期望 from(0,9) to(0,0)", r)
	}
}

func TestParseIccsBlackBaselineAs10(t *testing.T) {
	// 兼容黑方底线写成 10 的情况（a10a0 → from (0,0)）。
	r := ParseIccs("a10a0")
	if r == nil || r.From != rules.Pos(0, 0) {
		t.Fatalf("a10a0 = %v, 期望 from(0,0)", r)
	}
	r = ParseIccs("h10g8")
	if r == nil || r.From != rules.Pos(7, 0) || r.To != rules.Pos(6, 1) {
		t.Fatalf("h10g8 = %v, 期望 from(7,0) to(6,1)", r)
	}
}

func TestParseIccsInvalidReturnsNil(t *testing.T) {
	for _, input := range []string{"", "h3", "z3e3", "炮二平五", "h30e3"} {
		if r := ParseIccs(input); r != nil {
			t.Fatalf("ParseIccs(%q) = %v, 期望 nil", input, r)
		}
	}
}

func TestFormatIccsParseRoundTrip(t *testing.T) {
	from := rules.Pos(7, 6)
	to := rules.Pos(4, 6)
	if got := FormatIccs(from, to); got != "h3e3" {
		t.Fatalf("FormatIccs = %q, 期望 h3e3", got)
	}
	if r := ParseIccs(FormatIccs(from, to)); r == nil || r.From != from || r.To != to {
		t.Fatalf("format 与 parse 不互逆: %v", r)
	}
}

func TestFormatIccsOutOfRangeReturnsEmpty(t *testing.T) {
	if got := FormatIccs(rules.Pos(-1, 0), rules.Pos(0, 0)); got != "" {
		t.Fatalf("FormatIccs 越界 = %q, 期望空串", got)
	}
	if got := FormatIccs(rules.Pos(0, 0), rules.Pos(9, 0)); got != "" {
		t.Fatalf("FormatIccs 越界 = %q, 期望空串", got)
	}
}
