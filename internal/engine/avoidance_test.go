package engine

// L2 根节点历史回避单测（03 文档 §6.2 / 09 §2.2；Electron 版 repetitionAvoidance.spec.ts 的 Go 对应）：
//   - PickAvoidanceMove 纯函数：阈值内随机换着 / 长将强制变着（底线 −500）/ 保留原着；
//   - FindBestMove 集成：不传 HistoryFens 与参谋一致（向后兼容）；命中历史时返回
//     非重复着法；无合理替代时保留原着（将杀优先于回避）。
import (
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

const l2FenCannonEnd = "4k4/9/9/9/4r4/4P4/9/4C4/9/4K4 w - - 0 1" // 参谋分差 72 < 阈值 100
const l2FenMate = "3k5/9/9/9/9/9/9/9/9/R3K4 w - - 0 1"           // 一步杀，分差 ≫ 任何阈值

// avoidanceCtx 测试辅助：repeated/checks 列出重复与将军的 packed 着法。
func avoidanceCtx(scored []ScoredMove, best int32, opts struct {
	repeated []int32
	checks   []int32
	base     int
	random   func() float64
}) AvoidanceContext {
	return AvoidanceContext{
		Scored: scored,
		Best:   best,
		ThresholdBase: func() int {
			if opts.base != 0 {
				return opts.base
			}
			return 100
		}(),
		PostCount: func(m int32) int {
			for _, r := range opts.repeated {
				if r == m {
					return 1
				}
			}
			return 0
		},
		IsCheckMove: func(m int32) bool {
			for _, c := range opts.checks {
				if c == m {
					return true
				}
			}
			return false
		},
		Random: opts.random,
	}
}

// 阈值内存在非重复候选 → 随机取一（注入固定随机源可断言）。
func TestPickAvoidanceMoveRandomCandidate(t *testing.T) {
	scored := []ScoredMove{{11, 500}, {22, 480}, {33, 460}}
	// 阈值 = 100（500 不 >200 → 系数 1）；11 重复被排除，22/33 均为候选。
	// 固定随机源恒取 0.999 → Fisher-Yates 每步 j=i 不换序 → 取首个（22）。
	picked := PickAvoidanceMove(avoidanceCtx(scored, 11, struct {
		repeated []int32
		checks   []int32
		base     int
		random   func() float64
	}{repeated: []int32{11}, random: func() float64 { return 0.999 }}))
	if picked != 22 {
		t.Errorf("picked = %d, want 22", picked)
	}
}

// 优劣势系数：大优势阈值 ×0.5，大劣势 ×2.0。
func TestPickAvoidanceMoveCoefficients(t *testing.T) {
	alwaysZero := func() float64 { return 0 }
	scored := []ScoredMove{{11, 500}, {22, 470}} // 差 30
	// 大优势（500>200）：阈值 = 100×0.5 = 50 → 470 ≥ 450 → 是候选
	if got := PickAvoidanceMove(avoidanceCtx(scored, 11, struct {
		repeated []int32
		checks   []int32
		base     int
		random   func() float64
	}{repeated: []int32{11}, random: alwaysZero})); got != 22 {
		t.Errorf("大优势阈值内应换着: got %d, want 22", got)
	}
	// 大劣势（−500 < −200）：阈值 = 100×2 = 200
	losing := []ScoredMove{{11, -500}, {22, -400}}
	if got := PickAvoidanceMove(avoidanceCtx(losing, 11, struct {
		repeated []int32
		checks   []int32
		base     int
		random   func() float64
	}{repeated: []int32{11}, random: alwaysZero})); got != 22 {
		t.Errorf("大劣势阈值内应换着: got %d, want 22", got)
	}
	// 大优势下差 60 超过阈值 50 → 无候选
	tight := []ScoredMove{{11, 500}, {22, 440}}
	if got := PickAvoidanceMove(avoidanceCtx(tight, 11, struct {
		repeated []int32
		checks   []int32
		base     int
		random   func() float64
	}{repeated: []int32{11}, random: alwaysZero})); got != 11 {
		t.Errorf("超阈值应保留原着: got %d, want 11", got)
	}
}

// 长将形态：无阈值内候选且最佳为将军 → 强制选非将军非重复最高分（底线 −500）。
func TestPickAvoidanceMoveForcedChange(t *testing.T) {
	scored := []ScoredMove{
		{11, 300},  // 最佳：将军且重复
		{22, -100}, // 非将军非重复，劣化 400 ≤ 500 底线 → 被选中
		{33, -700}, // 劣化 1000 > 底线 → 排除
	}
	picked := PickAvoidanceMove(avoidanceCtx(scored, 11, struct {
		repeated []int32
		checks   []int32
		base     int
		random   func() float64
	}{repeated: []int32{11}, checks: []int32{11}, random: func() float64 { return 0 }}))
	if picked != 22 {
		t.Errorf("picked = %d, want 22", picked)
	}
}

// 长将形态底线内无候选 → 保留原着（宁可重复交规则裁决）。
func TestPickAvoidanceMoveForcedFloorExceeded(t *testing.T) {
	scored := []ScoredMove{
		{11, 300},
		{22, -400}, // 劣化 700 > 500 底线
	}
	picked := PickAvoidanceMove(avoidanceCtx(scored, 11, struct {
		repeated []int32
		checks   []int32
		base     int
		random   func() float64
	}{repeated: []int32{11}, checks: []int32{11}, random: func() float64 { return 0 }}))
	if picked != 11 {
		t.Errorf("picked = %d, want 11", picked)
	}
}

// 最佳非将军且无候选 → 保留原着（闲着重复交 L3）。
func TestPickAvoidanceMoveQuietRepeatKeepsBest(t *testing.T) {
	scored := []ScoredMove{{11, 100}, {22, -900}}
	picked := PickAvoidanceMove(avoidanceCtx(scored, 11, struct {
		repeated []int32
		checks   []int32
		base     int
		random   func() float64
	}{repeated: []int32{11}, random: func() float64 { return 0 }}))
	if picked != 11 {
		t.Errorf("picked = %d, want 11", picked)
	}
}

// 向后兼容：不传 HistoryFens 时与参谋报告最佳一致。
func TestFindBestMoveNoHistoryMatchesEx(t *testing.T) {
	fens := []string{
		l2FenCannonEnd,
		"1rbakab1r/9/1c4nc1/p1p1p1p1p/9/9/P1P1P1P1P/1C2C1N2/9/RNBAKAB1R w - - 0 1",
	}
	for _, fen := range fens {
		best, err := FindBestMove(fen, FindBestMoveOptions{Difficulty: 3})
		if err != nil {
			t.Fatalf("FindBestMove 报错: %v", err)
		}
		report, err := FindBestMoveEx(fen, FindBestMoveExOptions{Depth: 4})
		if err != nil {
			t.Fatalf("FindBestMoveEx 报错: %v", err)
		}
		if report == nil {
			t.Fatalf("%q 参谋报告为空", fen)
		}
		if best == nil || moveKey(*best) != moveKey(report.Best) {
			t.Errorf("%q 不传 HistoryFens 的 best 应与参谋一致", fen)
		}
	}
}

// 最佳着法命中历史（count=1）→ 返回落子后非重复的着法。
func TestFindBestMoveHistoryAvoidance(t *testing.T) {
	fen := l2FenCannonEnd
	report, err := FindBestMoveEx(fen, FindBestMoveExOptions{Depth: 4})
	if err != nil || report == nil {
		t.Fatalf("参谋报告为空: %v", err)
	}
	best := report.Best
	// 构造历史：当前局面 + 最佳着法落子后局面（各出现 1 次）。
	b := mustBoard(t, fen)
	b.ApplyMove(rules.Move{From: best.From, To: best.To})
	historyFens := []string{fen, b.ToFen()}
	result, err := FindBestMove(fen, FindBestMoveOptions{Difficulty: 3, HistoryFens: historyFens})
	if err != nil {
		t.Fatalf("FindBestMove 报错: %v", err)
	}
	if result == nil {
		t.Fatal("应返回着法")
	}
	if moveKey(*result) == moveKey(best) {
		t.Fatalf("分差 72 < 阈值 100，阈值内必有非重复候选，仍返回原着")
	}
	// 落子后局面不在历史中（否则仍在重复）——直接比键。
	eb := mustEngineBoard(t, fen)
	eb.ApplyMove(PosToIndex(result.From), PosToIndex(result.To))
	for _, hf := range historyFens {
		hb := mustEngineBoard(t, hf)
		if hb.ZobristKey() == eb.ZobristKey() {
			t.Errorf("返回着法落子后局面仍在历史中")
		}
	}
}

// 将杀优先于回避：无合理替代时保留原着（一步杀不因历史回避放弃）。
func TestFindBestMoveMatePriorityOverAvoidance(t *testing.T) {
	fen := l2FenMate
	report, err := FindBestMoveEx(fen, FindBestMoveExOptions{Depth: 4})
	if err != nil || report == nil {
		t.Fatalf("参谋报告为空: %v", err)
	}
	best := report.Best
	b := mustBoard(t, fen)
	_ = b.ApplyMove(rules.Move{From: best.From, To: best.To})
	historyFens := []string{fen, b.ToFen()}
	result, err := FindBestMove(fen, FindBestMoveOptions{Difficulty: 3, HistoryFens: historyFens})
	if err != nil {
		t.Fatalf("FindBestMove 报错: %v", err)
	}
	// 将杀分 29999 与次选 910 分差 ≫ 阈值与 −500 底线 → 保留将杀。
	if result == nil || moveKey(*result) != moveKey(best) {
		t.Errorf("将杀应优先于回避")
	}
}

// PosToIndex 与 packed 坐标一致（协议换算健全性）。
func TestPosToIndexSanity(t *testing.T) {
	if got := PosToIndex(rules.Pos(0, 9)); got != 81 {
		t.Errorf("PosToIndex(0,9) = %d, want 81", got)
	}
	if got := PosToIndex(rules.Pos(4, 0)); got != 4 {
		t.Errorf("PosToIndex(4,0) = %d, want 4", got)
	}
}
