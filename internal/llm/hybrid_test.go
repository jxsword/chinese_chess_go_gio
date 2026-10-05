package llm

// HybridLlmPlayer 等价集（T4.4，09 §2 hybrid.spec 7 用例 + 否决分支；
// test/llm/hybrid.spec.ts 移植，05 文档 §5 三模式决策流程）。

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// ---------------------------------------------------------------------------
// 可编程 fake（同 llmplayer_test）
// ---------------------------------------------------------------------------

// fakeAdvisor 可编程参谋引擎。
type fakeAdvisor struct {
	mu            sync.Mutex
	report        *engine.EngineReport
	evalMap       map[string]int
	lastExOptions [3]int // depth/topK/timeLimitMs；nil 报告不搜索时为未触碰标记
	exCalled      bool
}

func newFakeAdvisor() *fakeAdvisor {
	return &fakeAdvisor{evalMap: map[string]int{}}
}

func (f *fakeAdvisor) FindBestMoveEx(_ context.Context, _ string, depth, topK, timeLimitMs int) (*engine.EngineReport, error) {
	f.mu.Lock()
	f.lastExOptions = [3]int{depth, topK, timeLimitMs}
	f.exCalled = true
	report := f.report
	f.mu.Unlock()
	return report, nil
}

func (f *fakeAdvisor) EvaluateMove(_ context.Context, _ string, m rules.Move, _ int) (*int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := fmt.Sprintf("%d%d-%d%d", m.From.Col, m.From.Row, m.To.Col, m.To.Row)
	if cp, ok := f.evalMap[key]; ok {
		return &cp, nil
	}
	return nil, nil
}

func (f *fakeAdvisor) wasCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exCalled
}

// hybridReport 红车 a9、黑卒 a5 报告：best=a9-a5(吃) cp 300，Top-K 3 条。
func hybridReport() *engine.EngineReport {
	return &engine.EngineReport{
		Best:   mvWithCapture("a9-a5"),
		BestCp: 300,
		TopK: []engine.ReportEntry{
			{Move: mvWithCapture("a9-a5"), Cp: 300},
			{Move: mvCode("a9-a6"), Cp: 290},
			{Move: mvCode("d9-d8"), Cp: 50},
		},
	}
}

// mvWithCapture a9-a5 携带吃卒快照。
func mvWithCapture(code string) rules.Move {
	m := mvCode(code)
	m.Captured = &rules.Piece{Kind: rules.Pawn, Side: rules.Black}
	return m
}

type hybridOver struct {
	advisorMode       AdvisorMode
	strengthBlend     *int
	advisorDifficulty int
	maxAttempts       int
	fallback          LlmFallback
}

func makeHybrid(t *testing.T, ft *fakeTransport, adv *fakeAdvisor, over hybridOver) *HybridLlmPlayer {
	t.Helper()
	opts := HybridLlmPlayerOptions{
		AdvisorMode:       over.advisorMode,
		StrengthBlend:     50,
		AdvisorDifficulty: over.advisorDifficulty,
		MaxAttempts:       over.maxAttempts,
		Fallback:          over.fallback,
		BuiltinAiSource:   noopBuiltin,
	}
	if opts.AdvisorMode == "" {
		opts.AdvisorMode = AdvisorCandidate
	}
	if opts.Fallback == "" {
		opts.Fallback = FallbackBuiltinAI
	}
	if over.strengthBlend != nil {
		opts.StrengthBlend = *over.strengthBlend
	}
	if opts.MaxAttempts == 0 {
		opts.MaxAttempts = 3
	}
	i := 0
	return NewHybridLlmPlayer(playerCfg, ft, adv, opts, ChatClientOptions{
		NewID: func() string {
			i++
			return fmt.Sprintf("id-%d", i)
		},
	})
}

func blend(v int) *int { return &v }

// userOf 取第 call 次请求的 user 提示。
func userOf(t *testing.T, ft *fakeTransport, call int) string {
	t.Helper()
	_, user := chatContent(t, ft, call)
	return user
}

// ---------------------------------------------------------------------------
// 三模式（05 §5）
// ---------------------------------------------------------------------------

func TestHybridOffMode(t *testing.T) {
	// 01 off 模式：纯 Prompt v2 全量清单（无候选头/无分档），走 LlmPlayer 兜底链
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a5")}}
	adv := newFakeAdvisor()
	result, err := makeHybrid(t, ft, adv, hybridOver{advisorMode: AdvisorOff}).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK {
		t.Fatalf("status = %q", result.Status)
	}
	if result.Note != "" {
		t.Fatalf("off 模式成功即无参谋注解：%q", result.Note)
	}
	if adv.wasCalled() {
		t.Fatal("off 模式不做参谋搜索")
	}
	user := userOf(t, ft, 0)
	if !strings.Contains(user, "【合法着法清单（共") ||
		!strings.Contains(user, "括号内为中文记法/吃子/将军注解") {
		t.Fatalf("off 模式清单头不符：\n%s", user)
	}
	if strings.Contains(user, "【候选着法清单") || strings.Contains(user, " — ") {
		t.Fatalf("off 模式不应有候选头/分档：\n%s", user)
	}
}

func TestHybridCandidateMode(t *testing.T) {
	// 02 candidate 模式：Top-K 短名单附分档；池内命中 → note=参谋评分分桶
	ft := &fakeTransport{script: []scriptItem{scriptText("分析: 吃过河卒。\n着法: a9-a5")}}
	adv := newFakeAdvisor()
	adv.report = hybridReport()
	result, err := makeHybrid(t, ft, adv, hybridOver{strengthBlend: blend(0)}).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK || result.Note != "参谋评分: 最佳/均势" {
		t.Fatalf("结果不符：%+v", result) // bestCp 300 − 300 = 0
	}
	adv.mu.Lock()
	got := adv.lastExOptions
	adv.mu.Unlock()
	if got != [3]int{6, 3, 5000} {
		t.Fatalf("参谋搜索参数 = %v", got)
	}
	user := userOf(t, ft, 0)
	if !strings.Contains(user, "【候选着法清单（共 3 条，由本地引擎选出，必须从中选择一条；「—」后为引擎评估分档）】") {
		t.Fatalf("候选头不符：\n%s", user)
	}
	if !strings.Contains(user, "a9-a5(车九进四,吃卒) — 最佳/均势") ||
		!strings.Contains(user, "d9-d8(帅六进一) — 明显亏（约半子）") {
		t.Fatalf("分档行不符：\n%s", user)
	}
	if strings.Contains(user, "【合法着法清单（共") {
		t.Fatal("候选模式不应有全量清单头")
	}
}

func TestHybridCandidateNotInPool(t *testing.T) {
	// 03 candidate 编造非 Top-K → 重试（不在候选清单中）→ 二次池内命中
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a8"), scriptText("着法: a9-a6")}}
	adv := newFakeAdvisor()
	adv.report = hybridReport()
	result, err := makeHybrid(t, ft, adv, hybridOver{}).NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK || result.Note != "参谋评分: 最佳/均势" {
		t.Fatalf("结果不符：%+v", result) // 300 − 290 = 10
	}
	if !strings.Contains(userOf(t, ft, 1), "你上一次的回复的着法 a9-a8无效（着法 a9-a8 不在候选清单中）") {
		t.Fatalf("重试反馈不符：\n%s", userOf(t, ft, 1))
	}
}

func TestHybridGateOutsideTopK(t *testing.T) {
	// 04 gate 模式：全量清单自由选；选在 Top-K 外 → 用否决评估值补齐 note
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a8")}}
	adv := newFakeAdvisor()
	adv.report = hybridReport()
	adv.evalMap["09-08"] = 290 // a9-a8 → loss 10
	result, err := makeHybrid(t, ft, adv, hybridOver{advisorMode: AdvisorGate, strengthBlend: blend(0)}).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK || result.Note != "参谋评分: 最佳/均势" {
		t.Fatalf("结果不符：%+v", result)
	}
	user := userOf(t, ft, 0)
	if !strings.Contains(user, "【合法着法清单（共 7 条，必须从中选择一条）】") ||
		!strings.Contains(user, "a9-a8(车九进一)") {
		t.Fatalf("gate 清单不符：\n%s", user)
	}
	if strings.Contains(user, " — 最佳/均势") {
		t.Fatal("gate 无分档")
	}
}

func TestHybridGateVetoSecondPass(t *testing.T) {
	// 05 gate 否决 → 带理由再问 → 二次通过（复评 loss ≤ 阈值）
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a8"), scriptText("分析: 换一路。\n着法: a9-a7")}}
	adv := newFakeAdvisor()
	adv.report = hybridReport()
	adv.evalMap["09-08"] = 0   // a9-a8 亏损 300 > 阈值 80（blend 0）
	adv.evalMap["09-07"] = 295 // a9-a7 复评 loss 5 ≤ 80
	result, err := makeHybrid(t, ft, adv, hybridOver{advisorMode: AdvisorGate, strengthBlend: blend(0)}).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK || result.FromFallback {
		t.Fatalf("结果不符：%+v", result)
	}
	if result.Move == nil || result.Move.From != rules.Pos(0, 9) || result.Move.To != rules.Pos(0, 7) {
		t.Fatalf("应采用二次选择 a9-a7：%+v", result.Move)
	}
	if result.Note != "参谋评分: 最佳/均势" {
		t.Fatalf("note = %q", result.Note)
	}
	// 第二次调用的 user 追加了参谋否决文本（含分桶与厘兵损失）
	if !strings.Contains(userOf(t, ft, 1),
		"【参谋否决】你上一次选择的 a9-a8 会被引擎惩罚（大亏（丢一马/一炮级），相对最佳损失 300 厘兵）") {
		t.Fatalf("否决文本不符：\n%s", userOf(t, ft, 1))
	}
}

func TestHybridGateVetoOverridden(t *testing.T) {
	// 06 gate 二次违抗 → 引擎最佳代走（参谋职责，不走 resign 分支）
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a8"), scriptText("着法: a9-a7")}}
	adv := newFakeAdvisor()
	adv.report = hybridReport()
	adv.evalMap["09-08"] = 0 // 首选 loss 300 > 80
	adv.evalMap["09-07"] = 0 // 二次仍 loss 300 > 80
	result, err := makeHybrid(t, ft, adv, hybridOver{
		advisorMode:   AdvisorGate,
		strengthBlend: blend(0),
		fallback:      FallbackResign, // 即使 resign 设置，否决代走也不走该分支
	}).NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK || !result.FromFallback {
		t.Fatalf("结果不符：%+v", result)
	}
	if result.Move == nil || result.Move.To != rules.Pos(0, 5) {
		t.Fatalf("应采用 report.best：%+v", result.Move)
	}
	wantNote := "已由参谋否决（两次选择均造成最佳/均势的损失），改为引擎最佳着法"
	if result.Note != wantNote {
		t.Fatalf("note = %q, want %q", result.Note, wantNote)
	}
}

func TestHybridModelFailureFallback(t *testing.T) {
	// 07 模型真失败 → builtinAi：report.best 代走 note 注明；resign → failed
	ftBuiltin := &fakeTransport{script: []scriptItem{
		scriptText("不好意思"), scriptText("不好意思"), scriptText("不好意思"),
	}}
	eBuiltin := newFakeAdvisor()
	eBuiltin.report = hybridReport()
	r1, err := makeHybrid(t, ftBuiltin, eBuiltin, hybridOver{}).NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != engine.StatusOK || !r1.FromFallback {
		t.Fatalf("结果不符：%+v", r1)
	}
	if r1.Move == nil || r1.Move.To != rules.Pos(0, 5) {
		t.Fatalf("应代走 report.best：%+v", r1.Move)
	}
	wantNote := "模型未给出有效着法（无法从回复中解析出着法），已由参谋（内置引擎）代走"
	if r1.Note != wantNote {
		t.Fatalf("note = %q, want %q", r1.Note, wantNote)
	}

	ftResign := &fakeTransport{script: []scriptItem{scriptError("HTTP 500: boom")}}
	eResign := newFakeAdvisor()
	eResign.report = hybridReport()
	r2, err := makeHybrid(t, ftResign, eResign, hybridOver{maxAttempts: 1, fallback: FallbackResign}).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Status != engine.StatusFailed {
		t.Fatalf("status = %q", r2.Status)
	}
	wantNote2 := "模型未给出有效着法（第 1 次调用失败：HTTP 500: boom），按判负处理"
	if r2.Note != wantNote2 {
		t.Fatalf("note = %q, want %q", r2.Note, wantNote2)
	}
}

// ---------------------------------------------------------------------------
// 旋钮与边界（hybrid_llm_move_source.dart:74-78）
// ---------------------------------------------------------------------------

func TestShortlistSize(t *testing.T) {
	// 08 shortlistSize：blend 0→3 / 40→5 / 100→8；越界 clamp
	cases := []struct{ in, want int }{{0, 3}, {40, 5}, {100, 8}, {-20, 3}, {200, 8}}
	for _, c := range cases {
		if got := ShortlistSize(c.in); got != c.want {
			t.Errorf("ShortlistSize(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestVetoThreshold(t *testing.T) {
	// 09 vetoThresholdCp：blend 0→80 / 50→240 / 100→400
	cases := []struct{ in, want int }{{0, 80}, {50, 240}, {100, 400}}
	for _, c := range cases {
		if got := VetoThresholdCp(c.in); got != c.want {
			t.Errorf("VetoThresholdCp(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestHybridBlend100NoVeto(t *testing.T) {
	// 10 blend=100 时 gate 不否决（阈值 400，loss 300 不触发）
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a8")}}
	adv := newFakeAdvisor()
	adv.report = hybridReport()
	result, err := makeHybrid(t, ft, adv, hybridOver{advisorMode: AdvisorGate, strengthBlend: blend(100)}).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK {
		t.Fatalf("status = %q", result.Status)
	}
	if ft.callCount() != 1 {
		t.Fatalf("没有第二次否决再问，调用次数 = %d", ft.callCount())
	}
	if result.Note != "参谋评分: 未单独评估" {
		t.Fatalf("不做否决评估，Top-K 外无分：note = %q", result.Note)
	}
}

func TestHybridNoLegalMoveOrNullReport(t *testing.T) {
	// 11 棋盘无合法着法 / 引擎报告 nil → noLegalMove，不发起调用
	ft := &fakeTransport{}
	adv := newFakeAdvisor()
	adv.report = nil
	r1, err := makeHybrid(t, ft, adv, hybridOver{}).
		NextMove(t.Context(), mustBoard(t, "k8/1P7/9/9/9/R8/9/9/9/2K6 b - - 0 1"), nil)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := makeHybrid(t, ft, adv, hybridOver{advisorMode: AdvisorGate}).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Status != engine.StatusNoLegalMove || r2.Status != engine.StatusNoLegalMove {
		t.Fatalf("statuses = %q / %q", r1.Status, r2.Status)
	}
	if ft.callCount() != 0 {
		t.Fatal("不应发起调用")
	}
}

func TestHybridGateEvalNullDefensive(t *testing.T) {
	// 12 gate 评估返回 null（理论上池内均合法的防御路径）→ pick 置空走兜底链
	ft := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a8")}}
	adv := newFakeAdvisor()
	adv.report = hybridReport()
	// evaluateMove 对 a9-a8 返回 null → pick = null → 按模型失败兜底（Dart :173-175）
	result, err := makeHybrid(t, ft, adv, hybridOver{advisorMode: AdvisorGate, strengthBlend: blend(0)}).
		NextMove(t.Context(), simpleBoard(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != engine.StatusOK || !result.FromFallback {
		t.Fatalf("结果不符：%+v", result)
	}
	if result.Move == nil || result.Move.To != rules.Pos(0, 5) {
		t.Fatalf("应代走 report.best：%+v", result.Move)
	}
	if result.Note != "模型未给出有效着法，已由参谋（内置引擎）代走" {
		t.Fatalf("note = %q", result.Note)
	}
}

func TestHybridOffModeCancel(t *testing.T) {
	// 13 off 模式取消：cancelCurrent 覆盖委托链路
	ft := &fakeTransport{script: []scriptItem{scriptHang()}}
	adv := newFakeAdvisor()
	player := makeHybrid(t, ft, adv, hybridOver{advisorMode: AdvisorOff})

	type res struct {
		err error
	}
	done := make(chan res, 1)
	go func() {
		_, err := player.NextMove(t.Context(), simpleBoard(t), nil)
		done <- res{err: err}
	}()
	sleep(10)
	player.CancelCurrent()
	out := <-done
	if out.err != ErrCanceled {
		t.Fatalf("应以取消结算：err=%v", out.err)
	}
	if ids := ft.cancelled(); len(ids) != 1 {
		t.Fatalf("cancelledIds = %v", ids)
	}
}

// 复审修复回归（R1-P2）：引擎参谋调用错误按 TS 语义上抛（不吞为终局状态）。
func TestHybridEngineErrorPropagates(t *testing.T) {
	// findBestMoveEx 非取消错误 → NextMove 返回 error（页面 onSideFailed 收口）。
	ft := &fakeTransport{}
	adv := &errAdvisor{exErr: fmt.Errorf("engine worker crashed")}
	player1 := NewHybridLlmPlayer(playerCfg, ft, adv, HybridLlmPlayerOptions{
		AdvisorMode:     AdvisorCandidate,
		StrengthBlend:   50,
		MaxAttempts:     1,
		Fallback:        FallbackBuiltinAI,
		BuiltinAiSource: noopBuiltin,
	}, ChatClientOptions{NewID: func() string { return "x" }})
	_, err := player1.NextMove(t.Context(), simpleBoard(t), nil)
	if err == nil || err.Error() != "engine worker crashed" {
		t.Fatalf("findBestMoveEx 错误应上抛，got %v", err)
	}

	// gate 首评错误 → 上抛。
	ft2 := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a8")}}
	adv2 := &errAdvisor{report: hybridReport(), evalErr: fmt.Errorf("eval failed")}
	player2 := NewHybridLlmPlayer(playerCfg, ft2, adv2, HybridLlmPlayerOptions{
		AdvisorMode:     AdvisorGate,
		StrengthBlend:   0,
		MaxAttempts:     1,
		Fallback:        FallbackBuiltinAI,
		BuiltinAiSource: noopBuiltin,
	}, ChatClientOptions{NewID: func() string { return "x" }})
	_, err2 := player2.NextMove(t.Context(), simpleBoard(t), nil)
	if err2 == nil || err2.Error() != "eval failed" {
		t.Fatalf("evaluateMove 错误应上抛，got %v", err2)
	}

	// 复评错误 → 上抛（askAgainWithVeto 内）。
	ft3 := &fakeTransport{script: []scriptItem{scriptText("着法: a9-a8"), scriptText("着法: a9-a7")}}
	adv3 := &errAdvisor{report: hybridReport(), firstEvalCp: 0, secondEvalErr: fmt.Errorf("re-eval failed")}
	// 首评 a9-a8 = 0 → loss 300 > 阈值 80 触发否决；复评注入错误。
	player3 := NewHybridLlmPlayer(playerCfg, ft3, adv3, HybridLlmPlayerOptions{
		AdvisorMode:     AdvisorGate,
		StrengthBlend:   0,
		MaxAttempts:     1,
		Fallback:        FallbackBuiltinAI,
		BuiltinAiSource: noopBuiltin,
	}, ChatClientOptions{NewID: func() string { return "x" }})
	_, err3 := player3.NextMove(t.Context(), simpleBoard(t), nil)
	if err3 == nil || err3.Error() != "re-eval failed" {
		t.Fatalf("复评错误应上抛，got %v", err3)
	}
}

// errAdvisor 错误注入参谋：EvaluateMove 第 evalCalls 次调用可分别注入
// （首评给分触发否决、复评注入错误）。
type errAdvisor struct {
	report        *engine.EngineReport
	exErr         error
	evalErr       error
	firstEvalCp   int
	secondEvalErr error
	evalCalls     int
}

func (f *errAdvisor) FindBestMoveEx(context.Context, string, int, int, int) (*engine.EngineReport, error) {
	return f.report, f.exErr
}

func (f *errAdvisor) EvaluateMove(_ context.Context, _ string, m rules.Move, _ int) (*int, error) {
	f.evalCalls++
	if f.evalCalls == 1 {
		if f.evalErr != nil {
			return nil, f.evalErr
		}
		cp := f.firstEvalCp
		return &cp, nil
	}
	if f.secondEvalErr != nil {
		return nil, f.secondEvalErr
	}
	cp := 295
	return &cp, nil
}
