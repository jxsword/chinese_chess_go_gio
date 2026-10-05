// POC-2 动画帧循环单元测试（T0'.3）：easeOutCubic 缓动与帧时间戳权威结束判定。
// 锚点 = design_docs/08 §4（220ms 两阶段，t≥1 权威结束）。
package ui

import (
	"testing"
	"time"
)

// spec: 08 §4——easeOutCubic(t)=1-(1-t)^3；端点与单调性。
func TestPocEaseOutCubic(t *testing.T) {
	cases := []struct {
		in, want float32
	}{
		{0, 0}, {0.25, 1 - 0.75*0.75*0.75}, {0.5, 1 - 0.125}, {1, 1},
	}
	for _, c := range cases {
		if got := pocEaseOutCubic(c.in); got != c.want {
			t.Fatalf("ease(%v)=%v want %v", c.in, got, c.want)
		}
	}
	// 越界钳制
	if pocEaseOutCubic(-0.5) != 0 || pocEaseOutCubic(1.5) != 1 {
		t.Fatal("ease 越界未钳制")
	}
	// 单调不减
	prev := float32(0)
	for i := 0; i <= 20; i++ {
		v := pocEaseOutCubic(float32(i) / 20)
		if v < prev {
			t.Fatalf("ease 在 %d 步递减", i)
		}
		prev = v
	}
}

// spec: 08 §4——帧时间戳权威结束：t=(now-start)/220ms，t≥1 即结束（不依赖帧数）。
func TestPocAnimProgressAuthoritativeEnd(t *testing.T) {
	start := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	dur := pocAnimDuration

	// 半程：未结束，t=0.5
	tHalf, done := pocAnimProgress(start, start.Add(110*time.Millisecond), dur)
	if done || tHalf < 0.49 || tHalf > 0.51 {
		t.Fatalf("half: t=%v done=%v", tHalf, done)
	}
	// 掉帧场景：一帧跨越 220ms（如 500ms 后的第一帧）→ 仍以时间戳判定结束
	tLate, done := pocAnimProgress(start, start.Add(500*time.Millisecond), dur)
	if !done || tLate != 1 {
		t.Fatalf("late: t=%v done=%v（帧时间戳权威要求 done=true）", tLate, done)
	}
	// 恰好 220ms → 结束
	if _, done := pocAnimProgress(start, start.Add(dur), dur); !done {
		t.Fatal("t=1 应判结束")
	}
	// 慢速模式同样成立
	if _, done := pocAnimProgress(start, start.Add(time.Second), time.Second); !done {
		t.Fatal("1s 慢速 t=1 应判结束")
	}
}
