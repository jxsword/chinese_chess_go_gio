package engine_test

// MatchRunner 金标准对拍 + 结算路径确定性断言（T7.1，09 §2.2 / 05 §9；
// Electron 版 test/engine/matchRunner.spec.ts 逐用例移植）。
//
// 测试口径对齐 Flutter 版 strength_evaluation_test.dart：
//   - 弱模型以脚本化 fake transport 驱动（09 §2.3 可编程 fake）；
//   - 对手内置 AI 用难度 3（无随机窗口，完全可复现）；
//   - 质量评估深度对齐（advisorDifficulty 1 → 引擎 depth 2）。
// 结算路径（resign/no-legal-move/illegal/超时/source-error/draw-limit）
// 为 Electron 版新增用例（09 §2.3 表的同类扩展），逐条移植。

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 大超时（CI 平台性教训：性能类用例显式大超时，09 §3）。
const testCtxTimeout = 120 * time.Second

func testCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), testCtxTimeout)
	t.Cleanup(cancel)
	return ctx
}

var config = llm.LlmEndpointConfig{BaseURL: "https://example.com/v1", Model: "fake", APIKey: "", Preset: ""}

// ---------------------------------------------------------------------------
// "弱模型"fake transport：从 prompt 的着法清单中选择（strength_evaluation_test.dart 同款）
// ---------------------------------------------------------------------------

// weakStrategy first/last：取清单首个/末个条目。
type weakStrategy string

var codeRe = regexp.MustCompile(`([a-i]\d)-([a-i]\d)`)

func pickWeak(strategy weakStrategy, userPrompt string) string {
	// 只取清单段落，避免棋盘图/分析文本中的坐标干扰。
	start := indexOfString(userPrompt, "着法清单")
	if start < 0 {
		return "着法: a0-a1" // 触发拒绝链路
	}
	section := userPrompt[start:]
	if outIdx := indexOfString(section, "【输出】"); outIdx >= 0 {
		section = section[:outIdx]
	}
	codes := codeRe.FindAllString(section, -1)
	if len(codes) == 0 {
		return "着法: a0-a1"
	}
	if strategy == "first" {
		return "着法: " + codes[0]
	}
	return "着法: " + codes[len(codes)-1]
}

// indexOfString strings.Index 的可读包装（TS String.indexOf 同义）。
func indexOfString(s, sub string) int { return strings.Index(s, sub) }

// weakTransport 解析 prompt 清单并按策略回复（09 §2.3）。
type weakTransport struct{ strategy weakStrategy }

func (f *weakTransport) Chat(_ context.Context, req llm.ChatRequest, handlers llm.ChatHandlers) error {
	var body struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(req.Body), &body); err != nil {
		handlers.OnError("fake transport: 请求体解析失败：" + err.Error())
		return nil
	}
	user := ""
	for _, m := range body.Messages {
		if m.Role == "user" {
			user = m.Content
		}
	}
	handlers.OnDone(pickWeak(f.strategy, user))
	return nil
}

func (f *weakTransport) Cancel(string) {}

// ---------------------------------------------------------------------------
// Node 直连引擎的构造件（spec 的 engineSource / advisorEngine 同形）
// ---------------------------------------------------------------------------

// aiSource 内置 AI 棋手（spec engineSource：镜像 ChessAiMoveSource(difficulty: 3)，
// 不传 HistoryFens——对局完全可复现；对齐 Electron eval.ts chessAiSource）。
type aiSource struct{ difficulty int }

func (s aiSource) DisplayName() string {
	return fmt.Sprintf("内置 AI（%s）", engine.DifficultyName(s.difficulty))
}

func (s aiSource) NextMove(ctx context.Context, b *rules.Board, _ []rules.Move) (engine.MoveSourceResult, error) {
	move, err := engine.FindBestMove(b.ToFen(), engine.FindBestMoveOptions{Difficulty: s.difficulty, Ctx: ctx})
	if err != nil {
		return engine.MoveSourceResult{}, err
	}
	if move == nil {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}
	return engine.MoveSourceResult{Status: engine.StatusOK, Move: move}, nil
}

func engineSource() engine.MoveSource { return aiSource{difficulty: 3} }

// nodeAdvisor Node 直连引擎的参谋适配器（EngineClient 同语义）。
type nodeAdvisor struct{}

func (nodeAdvisor) FindBestMoveEx(ctx context.Context, fen string, depth, topK, timeLimitMs int) (*engine.EngineReport, error) {
	return engine.FindBestMoveEx(fen, engine.FindBestMoveExOptions{
		Depth: depth, TopK: topK, TimeLimitMs: timeLimitMs, Ctx: ctx,
	})
}

func (nodeAdvisor) EvaluateMove(ctx context.Context, fen string, m rules.Move, depth int) (*int, error) {
	return engine.EvaluateMove(fen, m, engine.EvaluateMoveOptions{Depth: depth, Ctx: ctx})
}

func baselineSource() engine.MoveSource {
	return llm.NewLlmPlayer(config, &weakTransport{strategy: "last"}, llm.LlmPlayerOptions{
		UsePromptV2:     false,
		MaxAttempts:     2,
		Fallback:        llm.FallbackBuiltinAI,
		BuiltinAiSource: engineSource,
	}, llm.ChatClientOptions{})
}

func hybridCandidateSource() engine.MoveSource {
	return llm.NewHybridLlmPlayer(config, &weakTransport{strategy: "last"}, nodeAdvisor{}, llm.HybridLlmPlayerOptions{
		AdvisorMode:       llm.AdvisorCandidate,
		StrengthBlend:     0,
		AdvisorDifficulty: 1,
		MaxAttempts:       2,
		Fallback:          llm.FallbackBuiltinAI,
		BuiltinAiSource:   engineSource,
	}, llm.ChatClientOptions{})
}

// ---------------------------------------------------------------------------
// 脚本化棋手：按固定序列回复（结算路径确定性驱动）
// ---------------------------------------------------------------------------

type scriptItem struct {
	status engine.MoveSourceStatus
	move   *rules.Move
	err    error
	hang   bool
}

type scriptedSource struct {
	name   string
	script []scriptItem
}

func (s *scriptedSource) DisplayName() string { return s.name }

func (s *scriptedSource) NextMove(_ context.Context, _ *rules.Board, _ []rules.Move) (engine.MoveSourceResult, error) {
	if len(s.script) == 0 {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}
	item := s.script[0]
	s.script = s.script[1:]
	if item.hang {
		// 永不结算（模拟卡死；由 RunMatch 单手超时收口）
		<-make(chan struct{})
	}
	return engine.MoveSourceResult{Status: item.status, Move: item.move}, item.err
}

func moveOf(t *testing.T, iccs string) *rules.Move {
	t.Helper()
	if len(iccs) != 5 || iccs[2] != '-' {
		t.Fatalf("ICCS 形态非法：%q", iccs)
	}
	col := func(b byte) int {
		if b < 'a' || b > 'i' {
			t.Fatalf("ICCS 列非法：%q", iccs)
		}
		return int(b - 'a')
	}
	row := func(b byte) int {
		if b < '0' || b > '9' {
			t.Fatalf("ICCS 行非法：%q", iccs)
		}
		return int(b - '0')
	}
	return &rules.Move{
		From: rules.Pos(col(iccs[0]), row(iccs[1])),
		To:   rules.Pos(col(iccs[3]), row(iccs[4])),
	}
}

func okMove(t *testing.T, iccs string) scriptItem {
	return scriptItem{status: engine.StatusOK, move: moveOf(t, iccs)}
}

// ---------------------------------------------------------------------------
// 战术命中率（确定性，镜像原版）
// ---------------------------------------------------------------------------

// TestTacticalHitRate 固定战术局面集：候选模式命中 Top-3 = 100%，基线选尾严格更低。
func TestTacticalHitRate(t *testing.T) {
	tacticalFens := []string{
		"4k4/9/9/9/r8/9/R8/9/9/4K4 w",           // R×r 白吃车
		"3k5/9/9/9/R8/8R/9/9/9/4K4 w",           // 双车杀
		"4k4/9/9/9/9/4C4/9/4C4/9/4K4 w - - 0 1", // 双炮中线
	}
	hybridHits, baselineHits, total := 0, 0, 0
	for _, fen := range tacticalFens {
		board, err := rules.FromFen(fen)
		if err != nil {
			t.Fatalf("FEN 非法 %q: %v", fen, err)
		}
		// 与被测 source 同深度（advisorDifficulty 1 → 引擎 depth 2），
		// 否则两个深度的 Top-3 集合可能不一致，断言结构上不成立。
		report, err := engine.FindBestMoveEx(fen, engine.FindBestMoveExOptions{Depth: 2, TopK: 3})
		if err != nil || report == nil {
			t.Fatalf("FindBestMoveEx 失败 %q: %v", fen, err)
		}
		top3 := map[string]bool{}
		for _, entry := range report.TopK {
			key := fmt.Sprintf("%d,%d-%d,%d", entry.Move.From.Col, entry.Move.From.Row, entry.Move.To.Col, entry.Move.To.Row)
			top3[key] = true
		}
		total++

		// Hybrid 候选模式：池被限定在 Top-K 内，怎么选都命中。
		hybrid := hybridCandidateSource()
		hybridResult, err := hybrid.NextMove(testCtx(t), board, nil)
		if err != nil {
			t.Fatalf("hybrid NextMove: %v", err)
		}
		if hybridResult.Move != nil {
			key := fmt.Sprintf("%d,%d-%d,%d", hybridResult.Move.From.Col, hybridResult.Move.From.Row, hybridResult.Move.To.Col, hybridResult.Move.To.Row)
			if top3[key] {
				hybridHits++
			}
		}

		// 基线：弱模型在全量清单中选尾。
		baseline := baselineSource()
		baselineResult, err := baseline.NextMove(testCtx(t), board, nil)
		if err != nil {
			t.Fatalf("baseline NextMove: %v", err)
		}
		if baselineResult.Move != nil {
			key := fmt.Sprintf("%d,%d-%d,%d", baselineResult.Move.From.Col, baselineResult.Move.From.Row, baselineResult.Move.To.Col, baselineResult.Move.To.Row)
			if top3[key] {
				baselineHits++
			}
		}
	}
	if total != 3 {
		t.Fatalf("局面集应全部有效：total = %d", total)
	}
	if hybridHits != total {
		t.Fatalf("候选模式命中 Top-3 比例应为 100%%：hybridHits = %d / %d", hybridHits, total)
	}
	if baselineHits >= total {
		t.Fatalf("基线选尾命中率应更低：baselineHits = %d / %d", baselineHits, total)
	}
}

// ---------------------------------------------------------------------------
// 对局质量（MatchRunner，短局确定性对比，镜像原版）
// ---------------------------------------------------------------------------

func TestMatchQualityHybridVsBaseline(t *testing.T) {
	ctx := testCtx(t)
	// 基线（弱模型裸奔）。
	baselineReport, err := engine.RunMatch(ctx, baselineSource(), engineSource(), engine.MatchRunnerOptions{
		MaxPlies: 40, EvaluateQuality: true, QualityDepth: 2,
	})
	if err != nil {
		t.Fatalf("baseline RunMatch: %v", err)
	}
	// Hybrid 候选模式（同一弱模型 + 参谋）。
	hybridReport, err := engine.RunMatch(ctx, hybridCandidateSource(), engineSource(), engine.MatchRunnerOptions{
		MaxPlies: 40, EvaluateQuality: true, QualityDepth: 2,
	})
	if err != nil {
		t.Fatalf("hybrid RunMatch: %v", err)
	}

	if baselineReport.EvaluatedPlies <= 0 {
		t.Fatalf("baseline evaluatedPlies 应 > 0：%d", baselineReport.EvaluatedPlies)
	}
	if hybridReport.EvaluatedPlies <= 0 {
		t.Fatalf("hybrid evaluatedPlies 应 > 0：%d", hybridReport.EvaluatedPlies)
	}
	// Top-3 跟随率：Hybrid 候选模式被限定在引擎名单内（0 失随），
	// 基线选尾必然大量脱离引擎认可集合。
	if hybridReport.RedTop3Misses != 0 {
		t.Fatalf("候选模式所有选择都应在引擎 Top-3 内：redTop3Misses = %d", hybridReport.RedTop3Misses)
	}
	if baselineReport.RedTop3Misses <= 0 {
		t.Fatalf("基线选尾应有脱离引擎 Top-3 的着法：redTop3Misses = %d", baselineReport.RedTop3Misses)
	}
	// 阈值常量与 Dart 一致（docs/phase5/02 §4）。
	if engine.BlunderThresholdCp != 250 {
		t.Fatalf("BlunderThresholdCp 应为 250：%d", engine.BlunderThresholdCp)
	}
	if engine.MatchRunnerBlunderMate != 30000 {
		t.Fatalf("MatchRunnerBlunderMate 应为 30000：%d", engine.MatchRunnerBlunderMate)
	}
}

// ---------------------------------------------------------------------------
// "大模型对战"场景（镜像原版）
// ---------------------------------------------------------------------------

func TestHybridVsHybridMatch(t *testing.T) {
	hybridGate := func() engine.MoveSource {
		return llm.NewHybridLlmPlayer(config, &weakTransport{strategy: "first"}, nodeAdvisor{}, llm.HybridLlmPlayerOptions{
			AdvisorMode:       llm.AdvisorGate,
			StrengthBlend:     0,
			AdvisorDifficulty: 1,
			MaxAttempts:       2,
			Fallback:          llm.FallbackBuiltinAI,
			BuiltinAiSource:   engineSource,
		}, llm.ChatClientOptions{})
	}
	report, err := engine.RunMatch(testCtx(t), hybridCandidateSource(), hybridGate(), engine.MatchRunnerOptions{MaxPlies: 30})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	if report.Plies <= 0 {
		t.Fatalf("plies 应 > 0：%d", report.Plies)
	}
	validReasons := map[string]bool{
		"checkmate": true, "stalemate": true, "move-limit": true,
		"resign": true, "no-legal-move": true, "illegal-move": true,
	}
	if !validReasons[report.EndReason] {
		t.Fatalf("endReason 不在合法集内：%q", report.EndReason)
	}
	// 棋谱可追溯。
	if len(report.MovesIccs) <= 0 {
		t.Fatalf("movesIccs 应非空")
	}
	if len(report.MovesIccs) != report.Plies {
		t.Fatalf("movesIccs 长度应等于 plies：%d vs %d", len(report.MovesIccs), report.Plies)
	}
}

// ---------------------------------------------------------------------------
// 结算路径（脚本化确定性）
// ---------------------------------------------------------------------------

func TestSettleFailedAndNoLegalMove(t *testing.T) {
	// 失败/无着 = 当方认输（winner=失败方-resign / endReason）。
	blackFailed := &scriptedSource{name: "黑-失败", script: []scriptItem{{status: engine.StatusFailed}}}
	r1, err := engine.RunMatch(testCtx(t), &scriptedSource{name: "红", script: []scriptItem{okMove(t, "h7-e7")}}, blackFailed, engine.MatchRunnerOptions{})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	// Dart 语义：winner = sideResign(loser)，即失败方为 red-resign/black-resign。
	if r1.Winner != "black-resign" || r1.EndReason != "resign" || r1.Plies != 1 {
		t.Fatalf("失败认输结算不符：winner=%q endReason=%q plies=%d", r1.Winner, r1.EndReason, r1.Plies)
	}
	if len(r1.MovesIccs) != 1 || r1.MovesIccs[0] != "h7e7" {
		t.Fatalf("棋谱应为 [h7e7]：%v", r1.MovesIccs)
	}

	blackNoLegal := &scriptedSource{name: "黑-无着", script: []scriptItem{{status: engine.StatusNoLegalMove}}}
	r2, err := engine.RunMatch(testCtx(t), &scriptedSource{name: "红", script: []scriptItem{okMove(t, "h7-e7")}}, blackNoLegal, engine.MatchRunnerOptions{})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	if r2.Winner != "black-resign" || r2.EndReason != "no-legal-move" || r2.Plies != 1 {
		t.Fatalf("无着认输结算不符：winner=%q endReason=%q plies=%d", r2.Winner, r2.EndReason, r2.Plies)
	}
}

func TestSettleSourceError(t *testing.T) {
	// source 异常 = 当方认输（endReason source-error）。
	blackThrow := &scriptedSource{name: "黑-异常", script: []scriptItem{{err: fmt.Errorf("boom")}}}
	r, err := engine.RunMatch(testCtx(t), &scriptedSource{name: "红", script: []scriptItem{okMove(t, "h7-e7")}}, blackThrow, engine.MatchRunnerOptions{})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	if r.Winner != "black-resign" || r.EndReason != "source-error" || r.Plies != 1 {
		t.Fatalf("source-error 结算不符：winner=%q endReason=%q plies=%d", r.Winner, r.EndReason, r.Plies)
	}
}

func TestSettleIllegalMove(t *testing.T) {
	// 非法着法 = 当方认输（endReason illegal-move，合法性终审兜底）：
	// 黑方返回红方棋子的着法（起点非走子方棋子 → 非法）。
	blackIllegal := &scriptedSource{name: "黑-非法", script: []scriptItem{okMove(t, "h7-e7")}}
	r, err := engine.RunMatch(testCtx(t), &scriptedSource{name: "红", script: []scriptItem{okMove(t, "h7-e7")}}, blackIllegal, engine.MatchRunnerOptions{})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	if r.Winner != "black-resign" || r.EndReason != "illegal-move" || r.Plies != 1 {
		t.Fatalf("illegal-move 结算不符：winner=%q endReason=%q plies=%d", r.Winner, r.EndReason, r.Plies)
	}
}

func TestSettlePerMoveTimeout(t *testing.T) {
	// 单手超时 = 超时方认输（默认 5 分钟，可注入短超时）。
	blackStall := &scriptedSource{name: "黑-卡死", script: []scriptItem{{hang: true}}}
	r, err := engine.RunMatch(testCtx(t), &scriptedSource{name: "红", script: []scriptItem{okMove(t, "h7-e7")}}, blackStall, engine.MatchRunnerOptions{PerMoveTimeoutMs: 30})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	if r.Winner != "black-resign" || r.EndReason != "resign" {
		t.Fatalf("超时认输结算不符：winner=%q endReason=%q", r.Winner, r.EndReason)
	}
}

func TestDrawLimitFullMoves(t *testing.T) {
	// 双方都取第一个合法着法：确定且永不终局。
	firstLegal := func(name string) engine.MoveSource {
		return &firstLegalSource{name: name}
	}
	r, err := engine.RunMatch(testCtx(t), firstLegal("红"), firstLegal("黑"), engine.MatchRunnerOptions{MaxPlies: 40})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	if r.Winner != "draw-limit" || r.EndReason != "move-limit" || r.Plies != 40 {
		t.Fatalf("draw-limit 结算不符：winner=%q endReason=%q plies=%d", r.Winner, r.EndReason, r.Plies)
	}
	if len(r.MovesIccs) != 40 {
		t.Fatalf("着法序列应完整：len(movesIccs) = %d", len(r.MovesIccs))
	}
	if r.RedTimeMs < 0 || r.BlackTimeMs < 0 {
		t.Fatalf("用时不应为负：%d/%d", r.RedTimeMs, r.BlackTimeMs)
	}
}

// firstLegalSource 恒取首个合法着法（AllLegalMoves 顺序确定）。
type firstLegalSource struct{ name string }

func (s *firstLegalSource) DisplayName() string { return s.name }

func (s *firstLegalSource) NextMove(_ context.Context, b *rules.Board, _ []rules.Move) (engine.MoveSourceResult, error) {
	legal := b.AllLegalMoves(b.Turn())
	if len(legal) == 0 {
		return engine.MoveSourceResult{Status: engine.StatusNoLegalMove}, nil
	}
	move := legal[0]
	return engine.MoveSourceResult{Status: engine.StatusOK, Move: &move}, nil
}

func TestRunSeriesSwapSides(t *testing.T) {
	// runSeries 红黑换边：奇数局交换（消除执先偏差）。
	var mu sync.Mutex
	var calls []string
	tag := func(name string) func(rules.Side) engine.MoveSource {
		return func(side rules.Side) engine.MoveSource {
			mu.Lock()
			calls = append(calls, fmt.Sprintf("%s:%s", name, string(side)))
			mu.Unlock()
			return &scriptedSource{name: fmt.Sprintf("%s-%s", name, side), script: []scriptItem{{status: engine.StatusNoLegalMove}}}
		}
	}
	reports, err := engine.RunMatchSeries(testCtx(t), tag("R"), tag("B"), engine.RunSeriesOptions{
		MatchRunnerOptions: engine.MatchRunnerOptions{MaxPlies: 4},
		Games:              2,
	})
	if err != nil {
		t.Fatalf("RunMatchSeries: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("应产出 2 局报告：%d", len(reports))
	}
	// 局 0：红先（R:red）；局 1：换边（R:black）——双方 builder 每局都构造（Dart 同款）。
	want := []string{"R:red", "B:black", "B:black", "R:red"}
	mu.Lock()
	got := append([]string(nil), calls...)
	mu.Unlock()
	if len(got) != len(want) {
		t.Fatalf("builder 调用序列不符：%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("builder 调用序列不符：want %v got %v", want, got)
		}
	}
	// 两局都是"红侧无着认输"：局 0 R 执红先手即无着；局 1 换边后 B 执红、
	// B 的脚本同样首轮即无着 → 认输方均为红侧（red-resign）。
	if reports[0].Winner != "red-resign" || reports[1].Winner != "red-resign" {
		t.Fatalf("换边局结算不符：%q / %q", reports[0].Winner, reports[1].Winner)
	}
}

// ---------------------------------------------------------------------------
// MatchReport JSON 协议面（05 §9 字段；Dart toJson 逐字一致）
// ---------------------------------------------------------------------------

func TestMatchReportJSONFields(t *testing.T) {
	r, err := engine.RunMatch(testCtx(t),
		&scriptedSource{name: "红", script: []scriptItem{okMove(t, "h7-e7")}},
		&scriptedSource{name: "黑", script: []scriptItem{{status: engine.StatusFailed}}},
		engine.MatchRunnerOptions{})
	if err != nil {
		t.Fatalf("RunMatch: %v", err)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	wantKeys := []string{
		"blackBlunders", "blackFallbacks", "blackTimeMs", "blackTop3Hits", "blackTop3Misses",
		"endReason", "evaluatedPlies", "movesIccs", "plies",
		"redBlunders", "redFallbacks", "redTimeMs", "redTop3Hits", "redTop3Misses", "winner",
	}
	if len(m) != len(wantKeys) {
		t.Fatalf("JSON 键集应逐字一致（%d 键）：got %v", len(wantKeys), m)
	}
	for _, key := range wantKeys {
		if _, ok := m[key]; !ok {
			t.Fatalf("缺键 %q：%v", key, m)
		}
	}
	if m["winner"] != "black-resign" || m["plies"] != float64(1) {
		t.Fatalf("结算字段不符：%v", m)
	}
	// movesIccs（Dart toJson 的 moves 映射在 CLI 报告组装层 reportToJson）。
	if fmt.Sprint(m["movesIccs"]) != "[h7e7]" {
		t.Fatalf("movesIccs 应为 [h7e7]：%v", m["movesIccs"])
	}
}
