package engine

// 搜索内核固定分与中断语义单测（03 文档 §5；Electron 版 engineBoard.spec.ts
// negamax/quiescence 段的 Go 对应，随 Search 落地）。
import (
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// -----------------------------------------------------------------------------
// negamax/quiescence 固定分（金标准局面，03 §5）
// -----------------------------------------------------------------------------

// 一步杀局面：全窗口深度 1 分 = MATE_SCORE − 1。
func TestSearchMateInOne(t *testing.T) {
	eb := mustEngineBoard(t, "3k5/9/9/9/R8/8R/9/9/9/4K4 w - - 0 1")
	search := NewSearch(eb, SearchConfig{MaxDepth: 1, DeadlineMs: time.Now().UnixMilli() + 10_000})
	scored := search.RunScored()
	if len(scored) == 0 {
		t.Fatal("评分表不应为空")
	}
	if scored[0].Score != MATE_SCORE-1 {
		t.Errorf("一步杀分 = %d, want %d", scored[0].Score, MATE_SCORE-1)
	}
}

// 被将死局面：RunScored 空表、Run 返回 false。
func TestSearchDeadPosition(t *testing.T) {
	eb := mustEngineBoard(t, "R3k4/9/9/9/9/4R4/9/9/9/4K4 b - - 0 1")
	search := NewSearch(eb, SearchConfig{MaxDepth: 2, DeadlineMs: time.Now().UnixMilli() + 10_000})
	if scored := search.RunScored(); len(scored) != 0 {
		t.Errorf("被将死评分表应为空，got %d 项", len(scored))
	}
	eb2 := mustEngineBoard(t, "R3k4/9/9/9/9/4R4/9/9/9/4K4 b - - 0 1")
	if _, ok := NewSearch(eb2, SearchConfig{MaxDepth: 2, DeadlineMs: time.Now().UnixMilli() + 10_000}).Run(); ok {
		t.Error("被将死 Run 应返回 false")
	}
}

// 白吃车局面深度 2 全窗口分 = 638（Dart 参考值）。
func TestSearchWhiteTakesRookDepth2(t *testing.T) {
	eb := mustEngineBoard(t, "4k4/9/9/9/4r4/4P4/9/4C4/9/4K4 w - - 0 1")
	search := NewSearch(eb, SearchConfig{MaxDepth: 2, DeadlineMs: time.Now().UnixMilli() + 10_000})
	scored := search.RunScored()
	if len(scored) == 0 || scored[0].Score != 638 {
		t.Fatalf("深度 2 首分 = %v, want 638", scored)
	}
}

// deadline 已过时优雅中断并标记 interrupted（对齐 Dart 捕获语义）。
func TestSearchDeadlineInterrupted(t *testing.T) {
	eb := mustEngineBoard(t, rules.FENInitial)
	search := NewSearch(eb, SearchConfig{MaxDepth: 6, DeadlineMs: time.Now().UnixMilli() - 1})
	best, _ := search.Run()
	if search.Interrupted != interruptedTimeout {
		t.Errorf("Interrupted = %q, want %q", search.Interrupted, interruptedTimeout)
	}
	// 返回的是某一完整层的确定结果或 false。
	if search.hasBest && (PackedFrom(best) < 0 || PackedFrom(best) > 89) {
		t.Errorf("best 走法 from 越界: %d", PackedFrom(best))
	}
}

// INFINITY 与 mate 分档不越界（杀分 < mateScore）。
func TestSearchScoreConstants(t *testing.T) {
	if MATE_SCORE != 30000 {
		t.Errorf("MATE_SCORE = %d, want 30000", MATE_SCORE)
	}
	if INFINITY != 100000 {
		t.Errorf("INFINITY = %d, want 100000", INFINITY)
	}
}
