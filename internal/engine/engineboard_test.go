package engine

// 引擎内部棋盘与搜索内核单测（03 文档 §3/§4；Electron 版 engineBoard.spec.ts 的 Go 对应）。
//
// 快速层（EngineBoard）与 rules.Board 逐项对拍锁定语义 1:1；
// 评估/排序用固定 FEN 手算分数断言；negamax 用金标准局面固定分。
import (
	"fmt"
	"sort"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 对拍用局面集：覆盖将军/照面/蹩腿/炮架/过河兵/沉底车/将死/困毙各形态。
var boardProbeFens = []string{
	"rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1",    // 初始
	"3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1",                                      // 白吃车
	"3k5/9/9/9/R8/8R/9/9/9/4K4 w - - 0 1",                                      // 一步杀
	"R3k4/9/9/9/9/4R4/9/9/9/4K4 b - - 0 1",                                     // 黑被将死
	"4k4/9/9/9/4r4/4P4/9/4C4/9/4K4 w - - 0 1",                                  // 兵炮对车
	"1rbakab1r/9/1c4nc1/p1p1p1p1p/9/9/P1P1P1P1P/1C2C1N2/9/RNBAKAB1R w - - 0 1", // 中炮局
	"4k4/9/9/9/4p4/9/9/9/9/4K4 b - - 0 1",                                      // 过河卒
	"2bak1b2/9/1cn4nc/9/9/9/9/9/9/1NBK1B1N1 w - - 0 1",                         // 士象活动
}

// pieceCodeOf 规则层棋子 → 引擎编码（nil → 0）。
func pieceCodeOf(p *rules.Piece) int8 {
	if p == nil {
		return 0
	}
	code := kindCode[p.Kind]
	if p.Side != rules.Red {
		code = -code
	}
	return code
}

func mustBoard(t *testing.T, fen string) *rules.Board {
	t.Helper()
	b, err := rules.FromFen(fen)
	if err != nil {
		t.Fatalf("rules.FromFen(%q) 报错: %v", fen, err)
	}
	return b
}

func mustEngineBoard(t *testing.T, fen string) *EngineBoard {
	t.Helper()
	b, err := FromFen(fen)
	if err != nil {
		t.Fatalf("FromFen(%q) 报错: %v", fen, err)
	}
	return b
}

// 与 rules.Board 对拍：fromFen 的棋盘内容与轮走方逐格一致。
func TestEngineBoardFromFenMatchesRules(t *testing.T) {
	for _, fen := range boardProbeFens {
		rulesBoard := mustBoard(t, fen)
		eb := mustEngineBoard(t, fen)
		if eb.isRedTurn != rulesBoard.IsRedTurn() {
			t.Errorf("%q 轮走方不一致: engine %v vs rules %v", fen, eb.isRedTurn, rulesBoard.IsRedTurn())
		}
		for sq := 0; sq < 90; sq++ {
			col, row := sq%9, sq/9
			want := pieceCodeOf(rulesBoard.PieceAt(col, row))
			if got := eb.PieceAt(sq); got != want {
				t.Fatalf("%q 格 (%d,%d) 编码不一致: got %d, want %d", fen, col, row, got, want)
			}
		}
	}
}

// isCheck 双方判定一致。
func TestEngineBoardIsCheckMatchesRules(t *testing.T) {
	for _, fen := range boardProbeFens {
		rulesBoard := mustBoard(t, fen)
		eb := mustEngineBoard(t, fen)
		for _, isRed := range []bool{true, false} {
			side := rules.Red
			if !isRed {
				side = rules.Black
			}
			if eb.IsCheck(isRed) != rulesBoard.IsCheck(side) {
				t.Errorf("%q IsCheck(%v) 不一致: engine %v vs rules %v", fen, isRed, eb.IsCheck(isRed), rulesBoard.IsCheck(side))
			}
		}
	}
}

// 伪合法走法集合一致（from/to/captured 逐条）。
func TestEngineBoardPseudoMovesMatchRules(t *testing.T) {
	for _, fen := range boardProbeFens {
		rulesBoard := mustBoard(t, fen)
		eb := mustEngineBoard(t, fen)
		buf := make([]int32, 128)
		for sq := 0; sq < 90; sq++ {
			p := rulesBoard.PieceAt(sq%9, sq/9)
			if p == nil {
				continue
			}
			ruleMoves := rulesBoard.PseudoMovesFor(rules.Pos(sq%9, sq/9))
			keys := make([]string, 0, len(ruleMoves))
			capturedBy := map[string]*rules.Piece{}
			for _, m := range ruleMoves {
				k := fmt.Sprintf("%d,%d->%d,%d", m.From.Col, m.From.Row, m.To.Col, m.To.Row)
				keys = append(keys, k)
				capturedBy[k] = m.Captured
			}
			n := eb.GenerateMovesFor(buf, 0, sq, false)
			ebKeys := make([]string, 0, n)
			for i := 0; i < n; i++ {
				m := buf[i]
				from, to := PackedFrom(m), PackedTo(m)
				k := fmt.Sprintf("%d,%d->%d,%d", from%9, from/9, to%9, to/9)
				ebKeys = append(ebKeys, k)
				// 位段 captured 与 rules 走法一致（TS 口径：编码 = cap≤7 ? cap : 8−cap）。
				cap := PackedCaptCode(m)
				want := capturedBy[k]
				wantCode := int8(0)
				if want != nil {
					wantCode = pieceCodeOf(want)
				}
				gotCode := int8(0)
				if cap != 0 {
					if cap <= 7 {
						gotCode = int8(cap)
					} else {
						gotCode = int8(8 - cap)
					}
				}
				switch {
				case cap == 0 && want != nil:
					t.Errorf("%q (%s) 位段无 captured 但 rules 有", fen, k)
				case cap != 0 && want == nil:
					t.Errorf("%q (%s) 位段有 captured=%d 但 rules 无", fen, k, cap)
				case cap != 0 && gotCode != wantCode:
					t.Errorf("%q (%s) captured 编码不一致: %d vs %d", fen, k, gotCode, wantCode)
				}
			}
			sort.Strings(keys)
			sort.Strings(ebKeys)
			if len(keys) != len(ebKeys) {
				t.Fatalf("%q 格 (%d,%d) 走法数不一致: rules %d vs engine %d", fen, sq%9, sq/9, len(keys), len(ebKeys))
			}
			for i := range keys {
				if keys[i] != ebKeys[i] {
					t.Errorf("%q 格 (%d,%d) 走法集合不一致: rules %q vs engine %q", fen, sq%9, sq/9, keys[i], ebKeys[i])
				}
			}
		}
	}
}

// capturesOnly 只保留吃子。
func TestEngineBoardCapturesOnly(t *testing.T) {
	eb := mustEngineBoard(t, "4k4/9/9/9/4r4/4P4/9/4C4/9/4K4 w - - 0 1")
	buf := make([]int32, 128)
	n := eb.GenerateMoves(buf, 0, true)
	if n <= 0 {
		t.Fatal("capturesOnly 应有吃子走法")
	}
	for i := 0; i < n; i++ {
		if PackedCaptCode(buf[i]) == 0 {
			t.Errorf("capturesOnly 输出含非吃子: %d", buf[i])
		}
	}
}

// applyMove/undoMove 往返与 rules 一致（全合法走法遍历）。
func TestEngineBoardApplyUndoRoundtrip(t *testing.T) {
	for _, fen := range boardProbeFens {
		rulesBoard := mustBoard(t, fen)
		eb := mustEngineBoard(t, fen)
		before := eb.data
		for _, move := range rulesBoard.AllLegalMoves(rulesBoard.Turn()) {
			from, to := PosToIndex(move.From), PosToIndex(move.To)
			cap := eb.ApplyMove(from, to)
			applied := rulesBoard.ApplyMove(move)
			if eb.isRedTurn != rulesBoard.IsRedTurn() {
				t.Fatalf("%q 走后轮走方不一致", fen)
			}
			for sq := 0; sq < 90; sq++ {
				want := pieceCodeOf(rulesBoard.PieceAt(sq%9, sq/9))
				if eb.PieceAt(sq) != want {
					t.Fatalf("%q 走 (%d,%d)->(%d,%d) 后盘面不一致 @%d", fen, move.From.Col, move.From.Row, move.To.Col, move.To.Row, sq)
				}
			}
			eb.UndoMove(from, to, cap)
			rulesBoard.UndoMove(applied)
		}
		if eb.data != before {
			t.Errorf("%q 全走法遍历后盘面未复原", fen)
		}
	}
}

// 将死/困毙局面判定一致。
func TestEngineBoardCheckmatePosition(t *testing.T) {
	mated := mustEngineBoard(t, "R3k4/9/9/9/9/4R4/9/9/9/4K4 b - - 0 1")
	if !mated.IsCheck(false) {
		t.Error("黑方应被将军")
	}
	stalemate := mustBoard(t, "R3k4/9/9/9/9/4R4/9/9/9/4K4 b - - 0 1")
	if !stalemate.IsCheck(rules.Black) {
		t.Error("rules 层黑方应被将军")
	}
}

// -----------------------------------------------------------------------------
// 静态评估（03 §4，固定 FEN 手算分数）
// -----------------------------------------------------------------------------

func TestEvaluateFixedScores(t *testing.T) {
	cases := []struct {
		name string
		fen  string
		want int
	}{
		{"初始局面红黑镜像对称，评估为 0", mustBoard(t, rules.FENInitial).ToFen(), 0},
		// 黑卒 (4,5) 已过河（row≥5）：colCenter=4 → 100 + 40 + 32 = 172；黑走 → +172。
		{"过河卒 172", "4k4/9/9/9/9/4p4/9/9/9/4K4 b - - 0 1", 172},
		// 黑卒 (4,3) 未过河：红视角 = -100，黑走 → +100。
		{"未过河卒无位置修正", "4k4/9/9/4p4/9/9/9/9/9/4K4 b - - 0 1", 100},
		// 红车 (0,0) 沉底（红方沉底 = row 0）：900 + 10 = 910。
		{"沉底车 +10", "R3k4/9/9/9/9/9/9/9/9/4K4 w - - 0 1", 910},
		// 炮 (4,7)：colCenter=4 → 450+16=466。
		{"居中炮 466", "3k5/9/9/9/9/9/9/4C4/9/3K5 w - - 0 1", 466},
		// 炮 (0,7)：colCenter=0 → 450。
		{"边炮 450", "3k5/9/9/9/9/9/9/C8/9/3K5 w - - 0 1", 450},
		// 评估按轮走方视角取正负。
		{"红走 +910", "R3k4/9/9/9/9/9/9/9/9/4K4 w - - 0 1", 910},
		{"黑走 -910", "R3k4/9/9/9/9/9/9/9/9/4K4 b - - 0 1", -910},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			eb := mustEngineBoard(t, c.fen)
			if got := eb.Evaluate(); got != c.want {
				t.Errorf("Evaluate = %d, want %d", got, c.want)
			}
		})
	}
}

// MVV-LVA 排序：马的多目标吃子按 victim 价值降序。
func TestMVVLVAOrdering(t *testing.T) {
	// 红马 (4,7) 可吃黑车 (5,5)（key=9000-400=8600）与黑卒 (3,5)（600）；
	// 等级在 packed 高位，升序排后等级小（优先级高）在前：吃车、吃卒、空格。
	eb := mustEngineBoard(t, "3k5/9/9/9/9/3p1r3/9/4N4/9/4K4 w - - 0 1")
	buf := make([]int32, 32)
	from := PosToIndex(rules.Pos(4, 7))
	n := eb.GenerateMovesFor(buf, 0, from, false)
	if n < 2 {
		t.Fatalf("马应至少有 2 个吃子目标，got %d", n)
	}
	sorted := make([]int32, n)
	copy(sorted, buf[:n])
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if got := PackedTo(sorted[0]); got != PosToIndex(rules.Pos(5, 5)) {
		t.Errorf("最高优先应吃车 (5,5)，got (%d,%d)", got%9, got/9)
	}
	if got := PackedTo(sorted[1]); got != PosToIndex(rules.Pos(3, 5)) {
		t.Errorf("次优先应吃卒 (3,5)，got (%d,%d)", got%9, got/9)
	}
}
