// POC 棋盘几何单元测试（T0'.2）：锚点 = Electron 版 boardLayout.ts 的公式与
// board_widget.ts 的命中换算。表驱动，覆盖公式两分支与边界钳制。
// 测试尺寸 960×1060 → cell = min(960/9.6, 1060/10.6) = 100（float32 下 ≈99.999994）。
package ui

import (
	"math"
	"testing"
)

func almostEq(a, b float32) bool {
	const eps = 1e-3
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

// spec: boardLayout.ts computeBoardLayout——cell 取两方向约束的较小者。
func TestComputeBoardLayoutCellBranches(t *testing.T) {
	// 宽受限：960×1920 → cell=960/9.6=100
	l := ComputeBoardLayout(960, 1920)
	if !almostEq(l.Cell, 100) {
		t.Fatalf("width-bound cell=%v want 100", l.Cell)
	}
	// 高受限：1920×960 → cell=960/10.6≈90.566
	l = ComputeBoardLayout(1920, 960)
	if !almostEq(l.Cell, 960/10.6) {
		t.Fatalf("height-bound cell=%v want %v", l.Cell, 960/10.6)
	}
}

// spec: computeBoardLayout——origin 居中、pieceRadius=cell*0.86/2、borderMargin=cell*0.5。
func TestComputeBoardLayoutDerived(t *testing.T) {
	l := ComputeBoardLayout(960, 1060) // cell=100 → 板 800×900，余量 160/160
	if !almostEq(l.OriginX, 80) || !almostEq(l.OriginY, 80) {
		t.Fatalf("origin=(%v,%v) want (80,80)", l.OriginX, l.OriginY)
	}
	if !almostEq(l.PieceRadius, 43) {
		t.Fatalf("pieceRadius=%v want 43", l.PieceRadius)
	}
	if !almostEq(l.BorderMargin, 50) {
		t.Fatalf("borderMargin=%v want 50", l.BorderMargin)
	}
}

// spec: offsetOf——交点坐标 = origin + col/row×cell。
func TestOffsetOf(t *testing.T) {
	l := ComputeBoardLayout(960, 1060)
	x, y := OffsetOf(l, 4, 5)
	if !almostEq(x, 480) || !almostEq(y, 580) {
		t.Fatalf("offsetOf(4,5)=(%v,%v) want (480,580)", x, y)
	}
	x, y = OffsetOf(l, 8, 9)
	if !almostEq(x, 880) || !almostEq(y, 980) {
		t.Fatalf("offsetOf(8,9)=(%v,%v) want (880,980)", x, y)
	}
}

// spec: hitTest（board_widget.ts:47-49）——四舍五入取最近交点；越界 false。
func TestHitTest(t *testing.T) {
	l := ComputeBoardLayout(960, 1060) // origin≈(80,80), cell≈100
	cases := []struct {
		x, y     float32
		col, row int
		ok       bool
	}{
		{80, 80, 0, 0, true},     // 交点上
		{135, 85, 1, 0, true},    // 0.55→1（最近交点）
		{480, 580, 4, 5, true},   // 中腹
		{880, 980, 8, 9, true},   // 右下角交点
		{20, 85, 0, 0, false},    // x 反算 -0.6 越界
		{880, 1035, 8, 9, false}, // row 反算 9.55→10 越界
	}
	for _, c := range cases {
		col, row, ok := HitTest(l, c.x, c.y)
		if ok != c.ok || (ok && (col != c.col || row != c.row)) {
			t.Fatalf("hitTest(%v,%v)=(%v,%v,%v) want (%v,%v,%v)",
				c.x, c.y, col, row, ok, c.col, c.row, c.ok)
		}
	}
}

// round 边界：Go math.Round 半值远离零，与 Electron Math.round 同向。
func TestHitTestRounding(t *testing.T) {
	if got := int(math.Round(0.5)); got != 1 {
		t.Fatalf("round(0.5)=%d want 1", got)
	}
}
