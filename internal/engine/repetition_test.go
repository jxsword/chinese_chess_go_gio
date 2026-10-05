package engine

// L1 搜索内重复检测单测（03 文档 §6.1 / 09 §2.2；Electron 版 searchRepetition.spec.ts 的 Go 对应）：
//   - 向后兼容：HistoryCounts 缺省（nil）= 旧行为（两次运行 best/分数/nodeCount 逐位一致）；
//   - 全局历史惩罚：count≥2 的局面在 ply≤3 命中即剪枝，被标记着法分数精确无搜索噪声；
//   - 路径重复：搜索分支内自循环被惩罚（空表即启用路径检测）；
//   - 性能门：开/关检测 nodeCount 差 ≤5%（显式大超时，防 CI 负载假超时——Electron 版教训）。
import (
	"math"
	"testing"
	"time"
)

// 车马对弱方等基准局面（金标准既有期望覆盖，不传 historyCounts 分数不变）。
var l1ProbeFens = []string{
	"3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1",                                      // 白吃车
	"1rbakab1r/9/1c4nc1/p1p1p1p1p/9/9/P1P1P1P1P/1C2C1N2/9/RNBAKAB1R w - - 0 1", // 中炮局
	"4k4/9/9/9/4r4/4P4/9/4C4/9/4K4 w - - 0 1",                                  // 兵炮对车
}

// 车在底线的摇摆局面：红可 Ra1-a10 将军后摇摆回原位，形成搜索路径内循环。
const l1SwingFen = "4k4/9/9/9/9/9/9/9/9/R2K5 w - - 0 1"

func mustEngineBoardT(t *testing.T, fen string) *EngineBoard {
	t.Helper()
	b, err := FromFen(fen)
	if err != nil {
		t.Fatalf("FromFen(%q) 报错: %v", fen, err)
	}
	return b
}

func runScoredWithCounts(t *testing.T, fen string, depth int, historyCounts map[uint64]int) []ScoredMove {
	s := NewSearch(mustEngineBoardT(t, fen), SearchConfig{
		MaxDepth:      depth,
		DeadlineMs:    time.Now().UnixMilli() + 10_000,
		Randomness:    0,
		HistoryCounts: historyCounts,
	})
	return s.RunScored()
}

// 向后兼容：缺省 historyCounts 两次运行 best/分数/nodeCount 逐位一致。
func TestL1DefaultOffDeterministic(t *testing.T) {
	for _, fen := range l1ProbeFens {
		s1 := NewSearch(mustEngineBoardT(t, fen), SearchConfig{MaxDepth: 4, DeadlineMs: time.Now().UnixMilli() + 10_000})
		best1, _ := s1.Run()
		n1 := s1.NodeCount()
		s2 := NewSearch(mustEngineBoardT(t, fen), SearchConfig{MaxDepth: 4, DeadlineMs: time.Now().UnixMilli() + 10_000})
		best2, _ := s2.Run()
		if best1 != best2 {
			t.Errorf("%q 两次运行 best 不一致: %d vs %d", fen, best1, best2)
		}
		if n1 != s2.NodeCount() {
			t.Errorf("%q 两次运行 nodeCount 不一致: %d vs %d", fen, n1, s2.NodeCount())
		}
	}
}

// 全局历史惩罚：count=2/3 的惩罚值精确（重复方 −100/−200，命中即剪枝）。
func TestL1GlobalPenaltyExact(t *testing.T) {
	fen := l1ProbeFens[1]
	baseline := runScoredWithCounts(t, fen, 4, nil)
	bestPacked := baseline[0].Move
	from, to := PackedFrom(bestPacked), PackedTo(bestPacked)
	// 落子后局面（红方视角静态分）作为惩罚基准。
	probe := mustEngineBoardT(t, fen)
	probe.ApplyMove(from, to)
	evalAfterMove := -probe.Evaluate() // Evaluate() 为走子方（黑）视角 → 取反得红方视角
	for _, tc := range []struct {
		count   int
		penalty int
	}{{2, 100}, {3, 200}} {
		counts := map[uint64]int{probe.ZobristKey(): tc.count}
		scored := runScoredWithCounts(t, fen, 4, counts)
		var same *ScoredMove
		for i := range scored {
			if scored[i].Move == bestPacked {
				same = &scored[i]
				break
			}
		}
		if same == nil {
			t.Fatalf("count=%d：最佳着法不在评分表中", tc.count)
		}
		// ply1 节点命中即剪枝：返回值 = 落子后静态评估 − 100×(count−1)，精确无搜索噪声。
		if same.Score != evalAfterMove-tc.penalty {
			t.Errorf("count=%d 分数 = %d, want %d（评估 %d − 惩罚 %d）",
				tc.count, same.Score, evalAfterMove-tc.penalty, evalAfterMove, tc.penalty)
		}
	}
}

// 路径检测启用（空表）：搜索确定且分数有限（摇摆局面冒烟）。
func TestL1PathDetectionSmoke(t *testing.T) {
	a := runScoredWithCounts(t, l1SwingFen, 6, map[uint64]int{})
	b := runScoredWithCounts(t, l1SwingFen, 6, map[uint64]int{})
	if len(a) != len(b) {
		t.Fatalf("两次运行评分表长度不一致: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Errorf("第 %d 项不一致: %+v vs %+v", i, a[i], b[i])
		}
		if math.IsNaN(float64(a[i].Score)) || math.IsInf(float64(a[i].Score), 0) {
			t.Errorf("第 %d 项分数非有限值: %d", i, a[i].Score)
		}
	}
	if len(a) == 0 {
		t.Fatal("摇摆局面应有评分表")
	}
}

// 性能：检测的节点数开销 ≤5%（惩罚改变分数窗口可轻微改变剪枝形态）。
// 显式大超时防 CI 负载假超时（Electron 版 CI 教训，09 §3）。
func TestL1NodeCountOverhead(t *testing.T) {
	runCounted := func(fen string, depth int, counts map[uint64]int) int {
		s := NewSearch(mustEngineBoardT(t, fen), SearchConfig{
			MaxDepth:      depth,
			DeadlineMs:    time.Now().UnixMilli() + 60_000,
			Randomness:    0,
			HistoryCounts: counts,
		})
		_, _ = s.Run()
		return s.NodeCount()
	}
	for _, fen := range l1ProbeFens {
		baseline := runCounted(fen, 6, nil)
		withDetection := runCounted(fen, 6, map[uint64]int{})
		limit := int(math.Ceil(float64(baseline) * 1.05))
		if withDetection > limit {
			t.Errorf("%q 检测开销超 5%%: %d > %d（baseline %d）", fen, withDetection, limit, baseline)
		}
		t.Logf("%q nodeCount: baseline=%d withDetection=%d", fen, baseline, withDetection)
	}
}
