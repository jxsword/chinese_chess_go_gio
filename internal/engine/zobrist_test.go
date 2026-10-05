package engine

// Zobrist 哈希单测（03 文档 §4 测试口径 / 09 §2.2 L0；Electron 版 zobrist.spec.ts 的 Go 对应）：
// ① 初始局面键确定性快照（固定种子 PRNG，跨进程可复现）；
// ② FromFen 全量键 === rebuildZobrist 重建键；
// ③ 随机对局每步增量键 === 全量重建键，undo 后键复原；
// ④ 轮走方参与键：同局面异轮走方键不同；⑤ 吃子改变键。
import "testing"

var zobristProbeFens = []string{
	"rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1",
	"1rbakab1r/9/1c4nc1/p1p1p1p1p/9/9/P1P1P1P1P/1C2C1N2/9/RNBAKAB1R w - - 0 1",
	"3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1",
	"4k4/9/9/9/4r4/4P4/9/4C4/9/4K4 w - - 0 1",
}

// ① 初始局面键对跨进程确定（快照；换种子/移位须同步改此值并 review）。
func TestZobristInitialKeySnapshot(t *testing.T) {
	for i, fen := range zobristProbeFens {
		b, err := FromFen(fen)
		if err != nil {
			t.Fatalf("FromFen(%q) 报错: %v", fen, err)
		}
		var want uint64
		switch i {
		case 0:
			want = 0xaa6de09ebd0fc8d9
		case 1:
			want = 0xcebb39404a33f2a4
		case 2:
			want = 0x8bcd9a6dec60a6f4
		case 3:
			want = 0x3560571aa8840be1
		}
		if b.ZobristKey() != want {
			t.Errorf("FEN%d 键快照不符: got %#016x, want %#016x", i, b.ZobristKey(), want)
		}
	}
}

// ② FromFen 全量键 === rebuildZobrist 重建键。
func TestZobristRebuildMatches(t *testing.T) {
	for _, fen := range zobristProbeFens {
		b, err := FromFen(fen)
		if err != nil {
			t.Fatalf("FromFen(%q) 报错: %v", fen, err)
		}
		if want := rebuildZobrist(&b.data, b.isRedTurn); b.ZobristKey() != want {
			t.Errorf("增量键 %#016x ≠ 重建键 %#016x (%q)", b.ZobristKey(), want, fen)
		}
	}
}

// lcg 测试内固定种子 LCG，保证随机对局可复现（对齐 TS lcg）。
func lcg(seed uint32) func() float64 {
	s := seed
	return func() float64 {
		s = s*1664525 + 1013904223
		return float64(s) / 0x100000000
	}
}

// ③ 随机对局每步：增量键 === 全量重建键；undo 后键复原。
func TestZobristRandomGameIncrementalEqualsRebuild(t *testing.T) {
	rand := lcg(0x5eed1234)
	for _, fen := range zobristProbeFens {
		b, err := FromFen(fen)
		if err != nil {
			t.Fatalf("FromFen(%q) 报错: %v", fen, err)
		}
		lo0, hi0 := b.ZobristKey(), b.ZobristKey()
		for step := 0; step < 120; step++ {
			var buf [128]int32
			n := b.GenerateMoves(buf[:], 0, false)
			if n == 0 {
				break
			}
			// 收集合法（不吃王）走法，随机取一。
			type cand struct{ from, to int }
			var candidates []cand
			for i := 0; i < n; i++ {
				from := PackedFrom(buf[i])
				to := PackedTo(buf[i])
				target := b.PieceAt(to)
				if target == 1 || target == -1 {
					continue // 不吃王，保持王位缓存有效
				}
				candidates = append(candidates, cand{from, to})
			}
			if len(candidates) == 0 {
				break
			}
			c := candidates[int(rand()*float64(len(candidates)))]
			keyBefore := b.ZobristKey()
			captured := b.ApplyMove(c.from, c.to)
			if want := rebuildZobrist(&b.data, b.isRedTurn); b.ZobristKey() != want {
				t.Fatalf("第 %d 步增量键 %#016x ≠ 重建键 %#016x", step, b.ZobristKey(), want)
			}
			b.UndoMove(c.from, c.to, captured)
			if b.ZobristKey() != keyBefore {
				t.Fatalf("undo 后键未复原: %#016x ≠ %#016x", b.ZobristKey(), keyBefore)
			}
		}
		if b.ZobristKey() != lo0 || b.ZobristKey() != hi0 {
			t.Errorf("对局结束键未回到初始键")
		}
	}
}

// ④ 同局面异轮走方键不同（轮走方参与键）。
func TestZobristTurnParticipates(t *testing.T) {
	fen := zobristProbeFens[1]
	redToMove, _ := FromFen(fen)
	blackToMove, err := FromFen(replaceTurnMarker(fen))
	if err != nil {
		t.Fatalf("FromFen 报错: %v", err)
	}
	if redToMove.ZobristKey() == blackToMove.ZobristKey() {
		t.Errorf("同局面异轮走方键应不同")
	}
}

// ⑤ 不同局面键不同（捕获改变键）。
func TestZobristCaptureChangesKey(t *testing.T) {
	before, _ := FromFen("3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1")
	after, _ := FromFen("3k5/9/9/9/9/9/R8/9/9/4K4 w - - 0 1")
	if before.ZobristKey() == after.ZobristKey() {
		t.Errorf("吃子前后键应不同")
	}
}

// replaceTurnMarker FEN 轮走方标记 w→b（测试辅助）。
func replaceTurnMarker(fen string) string {
	out := []byte(fen)
	for i := 0; i < len(out); i++ {
		if out[i] == ' ' {
			if i+1 < len(out) && out[i+1] == 'w' {
				out[i+1] = 'b'
			}
			break
		}
	}
	return string(out)
}
