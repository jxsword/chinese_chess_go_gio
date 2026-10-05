package rules

// 金标准对拍测试（09 §2.1，Electron 版 test/rules/golden.spec.ts 的 Go 对应）：
// 1. fen.json      —— FEN 逐条往返 FromFen(f).ToFen() == f（T1.1）；
// 2. moves.json    —— AllLegalMoves 输出与期望走法集合相等，顺序无关（T1.2）；
// 3. notation.json —— 中文记法逐字一致（T1.4）。
// 数据落盘于 testdata/golden/，与 TS 版共用 Dart 提取的期望值，跨语言逐位可比。
import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func loadGolden(t *testing.T, name string, v any) {
	t.Helper()
	data, err := os.ReadFile("../../testdata/golden/" + name)
	if err != nil {
		t.Fatalf("读取金标准 %s 失败: %v", name, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("解析金标准 %s 失败: %v", name, err)
	}
}

type goldenFenFile struct {
	Comment string   `json:"comment"`
	Fens    []string `json:"fens"`
}

type goldenStatus struct {
	InCheck   bool `json:"inCheck"`
	Checkmate bool `json:"checkmate"`
	Stalemate bool `json:"stalemate"`
}

type goldenMoveCase struct {
	Name   string       `json:"name"`
	Fen    string       `json:"fen"`
	Side   Side         `json:"side"`
	Status goldenStatus `json:"status"`
	Moves  [][]int      `json:"moves"`
}

type goldenMovesFile struct {
	Comment string           `json:"comment"`
	Cases   []goldenMoveCase `json:"cases"`
}

// 金标准 FEN 往返（tools/golden/fen.json）：isValidFen 全通过 + fromFen→toFen 逐条恒等。
func TestGoldenFenRoundTrip(t *testing.T) {
	var g goldenFenFile
	loadGolden(t, "fen.json", &g)
	for _, fen := range g.Fens {
		if !IsValidFen(fen) {
			t.Errorf("IsValidFen(%q) = false, 期望 true", fen)
			continue
		}
		b, err := FromFen(fen)
		if err != nil {
			t.Errorf("FromFen(%q) 报错: %v", fen, err)
			continue
		}
		if got := b.ToFen(); got != fen {
			t.Errorf("FEN 往返不一致: got %q, want %q", got, fen)
		}
	}
}

// moveKey 走法 → 比较键（四元组字符串，顺序无关比较用）。
func moveKey(m Move) string {
	return fmt.Sprintf("%d,%d,%d,%d", m.From.Col, m.From.Row, m.To.Col, m.To.Row)
}

// 金标准走法对拍（tools/golden/moves.json）：AllLegalMoves(side) 输出与 moves
// 逐条集合相等（数量一致 + 每条期望走法都在实际输出中，顺序无关），
// 并断言该局面下 side 方的终局 status（isCheck/isCheckmate/isStalemate）。
func TestGoldenMoves(t *testing.T) {
	var g goldenMovesFile
	loadGolden(t, "moves.json", &g)
	for _, c := range g.Cases {
		t.Run(c.Name, func(t *testing.T) {
			b, err := FromFen(c.Fen)
			if err != nil {
				t.Fatalf("FromFen(%q) 报错: %v", c.Fen, err)
			}
			actual := b.AllLegalMoves(c.Side)
			actualKeys := make(map[string]bool, len(actual))
			for _, m := range actual {
				actualKeys[moveKey(m)] = true
			}
			if len(actualKeys) != len(c.Moves) {
				t.Fatalf("着法数量不一致: got %d, want %d", len(actualKeys), len(c.Moves))
			}
			for _, q := range c.Moves {
				key := fmt.Sprintf("%d,%d,%d,%d", q[0], q[1], q[2], q[3])
				if !actualKeys[key] {
					t.Errorf("缺少期望着法 (%s)", key)
				}
			}
			// 终局状态期望。
			if got := b.IsCheck(c.Side); got != c.Status.InCheck {
				t.Errorf("IsCheck(%s) = %v, 期望 %v", c.Side, got, c.Status.InCheck)
			}
			if got := b.IsCheckmate(c.Side); got != c.Status.Checkmate {
				t.Errorf("IsCheckmate(%s) = %v, 期望 %v", c.Side, got, c.Status.Checkmate)
			}
			if got := b.IsStalemate(c.Side); got != c.Status.Stalemate {
				t.Errorf("IsStalemate(%s) = %v, 期望 %v", c.Side, got, c.Status.Stalemate)
			}
		})
	}
}

type goldenNotationCase struct {
	Name     string `json:"name"`
	Piece    string `json:"piece"`
	From     []int  `json:"from"`
	To       []int  `json:"to"`
	Expected string `json:"expected"`
}

type goldenNotationFile struct {
	Comment string               `json:"comment"`
	Cases   []goldenNotationCase `json:"cases"`
}

// 金标准中文记法对拍（tools/golden/notation.json）：ChineseNotation(piece, from, to)
// 输出与 expected 逐字一致（09 §2.1 之 3）。
func TestGoldenNotation(t *testing.T) {
	var g goldenNotationFile
	loadGolden(t, "notation.json", &g)
	for _, c := range g.Cases {
		t.Run(c.Name, func(t *testing.T) {
			piece := PieceFromFenChar(c.Piece[0])
			if piece == nil {
				t.Fatalf("非法棋子字符: %s", c.Piece)
			}
			got := ChineseNotation(piece, Pos(c.From[0], c.From[1]), Pos(c.To[0], c.To[1]))
			if got != c.Expected {
				t.Errorf("ChineseNotation = %q, 期望 %q", got, c.Expected)
			}
		})
	}
}
