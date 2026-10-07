package ui

// 大模型对战页测试（T4'.3，09 §4 状态级等价用例；翻译锚点 = 上游 LlmVsLlmPage.tsx）：
// 开始校验（DR-012 跟随/DR-014 内置侧免配置）、循环步进交替、暂停作废在途、
// 停止/新游戏代次收口、failed 判负终止（防错 #7）、双槽位保存回执。

import (
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// ---- fixture ----

type llvFixture struct {
	*pageEnvFixture
	store       *fakeLlmStore
	llm         *fakeLlmRunner
	page        *LlmVsLlmPage
	aiRev       *fakeRunner
	restoreDone bool
}

func newLlvFixture() *llvFixture {
	f := &llvFixture{pageEnvFixture: newPageEnvFixture(newPageRepo(), true), store: &fakeLlmStore{}, llm: newFakeLlmRunner()}
	var seq atomic.Int64
	f.env.NewRequestID = func(prefix string) string { return fmt.Sprintf("%s-%d", prefix, seq.Add(1)) }
	f.aiRev = newFakeRunner()
	f.page = newLlmVsLlmPage(llmEnvOf(f.pageEnvFixture.env, f.store), LlmVsLlmHooks{}, f.llm, f.aiRev)
	// 完成进页恢复（不自动续跑——上游语义）
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeLlmVsLlm})
	f.restoreDone = true
	return f
}

// loadConfigs 投递双槽位加载回执。
func (f *llvFixture) loadConfigs(red, black llm.LlmEndpointConfig) {
	f.store.scripts = map[string]*llm.LlmEndpointConfig{storage.SlotRed: &red, storage.SlotBlack: &black}
	f.store.LoadSlotAsync("t-red", storage.SlotRed)
	f.store.LoadSlotAsync("t-black", storage.SlotBlack)
	f.store.mu.Lock()
	pending := append([]SecureSlotLoaded(nil), f.store.pendingLoads...)
	f.store.pendingLoads = nil
	f.store.mu.Unlock()
	for _, ev := range pending {
		f.page.OnAppEvent(ev)
	}
}

// reply 回执当前在途请求（红/黑着法脚本）。
func (f *llvFixture) reply(move rules.Move) {
	id := f.page.moveRequest
	f.page.OnAppEvent(LlmMoveDone{RequestID: id, Result: engine.MoveSourceResult{Status: engine.StatusOK, Move: &move}})
}

// ---- 用例 ----

// 开始校验：双方全空 → toast 且不发起；红空黑有配置 → DR-012 红跟随黑可开局，
// 首手 spec 的 Endpoint=黑方配置、AuthSlot=黑槽。
func TestLlmVsLlmStartValidationAndMirror(t *testing.T) {
	f := newLlvFixture()
	f.loadConfigs(llm.LlmEndpointConfig{}, llm.LlmEndpointConfig{})
	f.page.start()
	if f.page.running || len(f.llm.moves) != 0 {
		t.Fatal("all-empty configs must not start")
	}
	if f.page.toastText == "" {
		t.Fatal("validation toast expected")
	}

	black := llm.LlmEndpointConfig{BaseURL: "https://b/v1", APIKey: "****blk1", Model: "black-model"}
	f.loadConfigs(llm.LlmEndpointConfig{}, black)
	f.page.start()
	if !f.page.running || len(f.llm.moves) != 1 {
		t.Fatal("red follows black (DR-012): start expected")
	}
	spec := f.llm.moves[0]
	if spec.Endpoint != black {
		t.Fatalf("endpoint = %+v, want black config", spec.Endpoint)
	}
	if spec.AuthSlot != storage.SlotBlack {
		t.Fatalf("authSlot = %q, want black slot", spec.AuthSlot)
	}
	if !f.page.moveActive || f.page.store.VM.IsFinished() {
		t.Fatal("move active + input locked expected")
	}
}

// 内置 AI 侧（DR-014）：一侧为内置 AI 时该侧不需要 LLM 配置。
func TestLlmVsLlmBuiltinSideSkipsLlmConfig(t *testing.T) {
	f := newLlvFixture()
	f.loadConfigs(llm.LlmEndpointConfig{}, llm.LlmEndpointConfig{})
	f.page.gameSettings.RedSideType = llm.SideEngineBuiltin
	f.page.gameSettings.BlackSideType = llm.SideEngineBuiltin
	f.page.start()
	if !f.page.running {
		t.Fatal("builtin red side must not require LLM config")
	}
	req := f.aiRev.lastRequest(t)
	if req.Type != engine.ReqFindBestMove {
		t.Fatalf("type = %q", req.Type)
	}
}

// 循环步进：红黑交替请模型应手，PlayMove 落盘后按间隔续跑（interval=0 立即）。
func TestLlmVsLlmLoopAlternates(t *testing.T) {
	f := newLlvFixture()
	f.page.gameSettings.IntervalSeconds = 0
	red := llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "red-model"}
	black := llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "black-model"}
	f.loadConfigs(red, black)
	f.page.start()

	// 首手（红）spec = 红方配置红槽。
	if got := f.llm.moves[0].Endpoint; got != red || f.llm.moves[0].AuthSlot != storage.SlotRed {
		t.Fatalf("first move spec = %+v", f.llm.moves[0])
	}
	f.reply(rules.Move{From: rules.Pos(7, 7), To: rules.Pos(7, 4)}) // 红中炮
	if len(f.page.store.State().MoveHistory) != 1 {
		t.Fatal("red move must be applied")
	}
	// interval=0：立即续跑黑方。
	if len(f.llm.moves) != 2 {
		t.Fatalf("black move should be requested immediately, got %d", len(f.llm.moves))
	}
	if got := f.llm.moves[1].Endpoint; got != black || f.llm.moves[1].AuthSlot != storage.SlotBlack {
		t.Fatalf("second move spec = %+v", f.llm.moves[1])
	}
	f.reply(rules.Move{From: rules.Pos(7, 2), To: rules.Pos(7, 3)}) // 黑应手
	if len(f.page.store.State().MoveHistory) != 2 {
		t.Fatal("black move must be applied")
	}
	if len(f.llm.moves) != 3 {
		t.Fatalf("loop must continue, got %d", len(f.llm.moves))
	}
	if f.page.lastMoveText == "" {
		t.Fatal("lastMoveText expected")
	}
}

// 间隔续跑：interval>0 时走子后等待 LlmLoopTick（代次匹配）。
func TestLlmVsLlmIntervalTick(t *testing.T) {
	f := newLlvFixture()
	f.page.gameSettings.IntervalSeconds = 2
	red := llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "m"}
	f.loadConfigs(red, red)
	f.page.start()
	f.reply(rules.Move{From: rules.Pos(7, 7), To: rules.Pos(7, 4)})
	if len(f.llm.moves) != 1 {
		t.Fatal("interval wait: no immediate next request")
	}
	f.page.OnAppEvent(LlmLoopTick{Gen: f.page.loopGen})
	if len(f.llm.moves) != 2 {
		t.Fatal("tick must pump next move")
	}
	// 第二手续跑：当代 tick 命中
	f.reply(rules.Move{From: rules.Pos(7, 2), To: rules.Pos(7, 3)})
	f.page.OnAppEvent(LlmLoopTick{Gen: f.page.loopGen})
	if len(f.llm.moves) != 3 {
		t.Fatal("valid tick expected (gen matches)")
	}
	// 过期代次 tick 忽略
	f.page.OnAppEvent(LlmLoopTick{Gen: f.page.loopGen + 1})
	if len(f.llm.moves) != 3 {
		t.Fatal("stale tick must be ignored")
	}
}

// 暂停：在途回复作废（cancel + 代次递增），迟到回执丢弃；继续后重新思考。
func TestLlmVsLlmPauseDiscards(t *testing.T) {
	f := newLlvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "m"}
	f.loadConfigs(red, red)
	f.page.start()
	id := f.page.moveRequest
	f.page.togglePause() // 暂停
	if !f.page.paused || f.page.moveRequest != "" {
		t.Fatal("paused + in-flight discarded expected")
	}
	if len(f.llm.cancels) == 0 || f.llm.cancels[len(f.llm.cancels)-1] != id {
		t.Fatalf("pause must cancel in-flight, cancels = %v", f.llm.cancels)
	}
	// 迟到回执（已作废）丢弃
	f.page.OnAppEvent(LlmMoveDone{RequestID: id, Result: engine.MoveSourceResult{Status: engine.StatusOK, Move: &rules.Move{From: rules.Pos(7, 7), To: rules.Pos(7, 4)}}})
	if len(f.page.store.State().MoveHistory) != 0 {
		t.Fatal("late move after pause must be discarded")
	}
	// 继续：重新请求
	f.page.togglePause()
	if f.page.paused || len(f.llm.moves) != 2 {
		t.Fatalf("resume must re-request, moves = %d", len(f.llm.moves))
	}
}

// 停止/新游戏：循环终止 + 解锁 + 代次收口。
func TestLlmVsLlmStopAndNewGame(t *testing.T) {
	f := newLlvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "m"}
	f.loadConfigs(red, red)
	f.page.start()
	f.page.stop()
	if f.page.running || f.page.moveRequest != "" {
		t.Fatal("stopped state expected")
	}
	// 已锁定棋盘（自动对局）在停止时解锁——OnTap 应产生选中
	f.page.store.VM.OnTap(7, 7)
	if f.page.store.State().Selected == nil {
		t.Fatal("input must be unlocked after stop")
	}

	f.page.start()
	f.page.confirmingNewGame = true
	f.page.doNewGame()
	if f.page.running || f.page.moveRequest != "" || f.page.statusText != "等待开始" {
		t.Fatalf("new game must reset loop, status = %q", f.page.statusText)
	}
}

// failed（resign 策略）：显式判负终止循环（防错 #7）。
func TestLlmVsLlmSideFailedResign(t *testing.T) {
	f := newLlvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "m"}
	f.loadConfigs(red, red)
	f.page.start()
	id := f.page.moveRequest
	f.page.OnAppEvent(LlmMoveDone{RequestID: id, Result: engine.MoveSourceResult{Status: engine.StatusFailed, Note: "模型持续失败"}})
	if f.page.running {
		t.Fatal("loop must terminate on side failed")
	}
	if got := *f.page.store.State().Result; got != state.ResultBlackWins {
		t.Fatalf("result = %q, want blackWins (红方判负)", got)
	}
	if f.page.redNote == "" {
		t.Fatal("failed reason must be recorded")
	}
}

// 迟到回执（停止/新开后到达）按代次丢弃（K21 二次收口）。
func TestLlmVsLlmStaleMoveDropped(t *testing.T) {
	f := newLlvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "m"}
	f.loadConfigs(red, red)
	f.page.start()
	id := f.page.moveRequest
	f.page.stop()
	f.page.OnAppEvent(LlmMoveDone{RequestID: id, Result: engine.MoveSourceResult{Status: engine.StatusOK, Move: &rules.Move{From: rules.Pos(7, 7), To: rules.Pos(7, 4)}}})
	if len(f.page.store.State().MoveHistory) != 0 {
		t.Fatal("stale move must be dropped")
	}
}

// 立即保存：双槽位写入 + 两回执齐后汇总 toast；单侧失败立即报错。
func TestLlmVsLlmSaveNow(t *testing.T) {
	f := newLlvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://r/v1", APIKey: "****red1", Model: "m"}
	black := llm.LlmEndpointConfig{BaseURL: "https://b/v1", APIKey: "****blk1", Model: "m"}
	f.loadConfigs(red, black)
	f.page.saveNow()
	if len(f.store.saved) != 2 {
		t.Fatalf("saved = %d, want 2", len(f.store.saved))
	}
	if f.store.saved[0].slot != storage.SlotRed || f.store.saved[1].slot != storage.SlotBlack {
		t.Fatalf("slots = %q,%q", f.store.saved[0].slot, f.store.saved[1].slot)
	}
	f.page.OnAppEvent(SecureSlotSaved{Slot: storage.SlotRed, Stored: "encrypted"})
	if f.page.toastText != "" {
		t.Fatal("toast only after both receipts")
	}
	f.page.OnAppEvent(SecureSlotSaved{Slot: storage.SlotBlack, Stored: "encrypted"})
	if f.page.toastText != "双方模型配置已保存" {
		t.Fatalf("toast = %q", f.page.toastText)
	}
	// 失败路径
	f.page.saveNow()
	f.page.OnAppEvent(SecureSlotSaved{Slot: storage.SlotRed, Err: errStorageUnavailableLike()})
	if f.page.toastText != "保存失败：本地存储不可用" {
		t.Fatalf("toast = %q", f.page.toastText)
	}
}

func errStorageUnavailableLike() error { return &llm.LlmApiError{Message: "storage down"} }

// DR-012 镜像提示行状态（空侧跟随对方）。
func TestLlmVsLlmMirrorHints(t *testing.T) {
	f := newLlvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "m"}
	f.loadConfigs(red, llm.LlmEndpointConfig{})
	if !llm.IsEmptyLlmConfig(f.page.blackConfig) || !llm.IsConfigured(f.page.redConfig) {
		t.Fatal("fixture precondition")
	}
	// 黑空红有 → 黑侧提示跟随红方（提示文案状态由 layout 判定，此处锚定数据面）
	resolved := llm.ResolveLlmSideConfig(f.page.blackConfig, f.page.redConfig, storage.SlotBlack, storage.SlotRed)
	if resolved.Config != f.page.redConfig || resolved.AuthSlot != storage.SlotRed {
		t.Fatalf("resolved = %+v", resolved)
	}
}

// OnAttempt 竞态回归（#G3/#G5）：回调闭包按触发时 requestId 值捕获（同
// humanvsllm）——作废后旧回调仍携带旧 id，不读主 goroutine 可写的 moveRequest。
func TestLlmVsLlmAttemptIdCaptured(t *testing.T) {
	f := newLlvFixture()
	f.page.gameSettings.IntervalSeconds = 0
	f.loadConfigs(
		llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "red-model"},
		llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "black-model"},
	)
	f.page.start()
	firstID := f.page.moveRequest
	firstSpec := f.llm.moves[0]

	// 停止 → 再开始：第二次 pump 携带新 id
	f.page.stop()
	f.page.start()
	secondID := f.page.moveRequest
	if firstID == secondID {
		t.Fatal("second pump should carry a new requestId")
	}

	// 模拟后台 goroutine 持旧 spec 回调：emit 必须携带触发时捕获的旧 id
	done := make(chan struct{})
	go func() {
		defer close(done)
		firstSpec.OnAttempt(1, 3)
	}()
	<-done
	f.mu.Lock()
	defer f.mu.Unlock()
	last := f.emitted[len(f.emitted)-1]
	progress, ok := last.(LlmAttemptProgress)
	if !ok {
		t.Fatalf("last payload = %T", last)
	}
	if progress.RequestID != firstID {
		t.Fatalf("stale OnAttempt emitted requestId = %q, want captured %q", progress.RequestID, firstID)
	}
}

// LlmTestDone 双卡定向（#G5）：回执只回填对应卡片，另一卡不受串扰。
func TestLlmVsLlmTestResultRoutedToCard(t *testing.T) {
	f := newLlvFixture()
	f.loadConfigs(
		llm.LlmEndpointConfig{BaseURL: "https://r/v1", Model: "red-model"},
		llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "black-model"},
	)

	f.page.onRedTest(llm.LlmEndpointConfig{BaseURL: "https://r/v1"}, storage.SlotRed)
	f.page.onBlackTest(llm.LlmEndpointConfig{BaseURL: "https://b/v1"}, storage.SlotBlack)
	if f.page.testRedID == "" || f.page.testBlackID == "" {
		t.Fatal("both test requests should record their requestIds")
	}

	f.page.OnAppEvent(LlmTestDone{RequestID: f.page.testBlackID, OK: false, Message: "黑方失败"})
	if f.page.redCard.testResult != "" {
		t.Fatalf("black receipt must not touch red card, got %q", f.page.redCard.testResult)
	}
	if f.page.blackCard.testResult != "黑方失败" {
		t.Fatalf("black card should receive its receipt, got %q", f.page.blackCard.testResult)
	}
	f.page.OnAppEvent(LlmTestDone{RequestID: f.page.testRedID, OK: true, Message: "红方成功"})
	if f.page.redCard.testResult != "红方成功" {
		t.Fatalf("red card should receive its receipt, got %q", f.page.redCard.testResult)
	}
	if f.page.testRedID != "" || f.page.testBlackID != "" {
		t.Fatal("test ids should clear after matched receipts")
	}
}
