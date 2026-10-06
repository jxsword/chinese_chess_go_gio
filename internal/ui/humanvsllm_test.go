package ui

// 人机 LLM 页测试（T4'.3，09 §4 状态级等价用例；翻译锚点 = 上游 HumanVsLlmPage.tsx）：
// DR-014 配置加载/镜像、触发守卫（防错 #4）、应手结算（ok/failed/迟到丢弃）、
// 取消链（#G5）、流式消息区 K21 二次收口、立即保存（镜像拦截/掩码回执）、
// dispose 取消。

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// ---- fakes ----

// fakeLlmStore ui.LlmStore fake：记录调用；槽位配置可脚本化回放
// （加载回执由测试手动经 OnAppEvent 投递——模拟事件总线）。
type fakeLlmStore struct {
	mu           sync.Mutex
	loaded       []string
	pendingLoads []SecureSlotLoaded
	saved        []fakeSlotSave
	settings     []int
	resolveKey   string
	scripts      map[string]*llm.LlmEndpointConfig // slot → LoadSlotAsync 回执配置
}

type fakeSlotSave struct {
	slot string
	cfg  llm.LlmEndpointConfig
}

func (s *fakeLlmStore) LoadSlotAsync(_, slot string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loaded = append(s.loaded, slot)
	if cfg, ok := s.scripts[slot]; ok {
		cfgCopy := *cfg
		s.pendingLoads = append(s.pendingLoads, SecureSlotLoaded{Slot: slot, Config: &cfgCopy})
	} else {
		s.pendingLoads = append(s.pendingLoads, SecureSlotLoaded{Slot: slot})
	}
}

func (s *fakeLlmStore) SaveSlotAsync(_ string, slot string, cfg llm.LlmEndpointConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saved = append(s.saved, fakeSlotSave{slot: slot, cfg: cfg})
}

func (s *fakeLlmStore) SaveSettings(settings state.LlmGameSettings, _ ...state.LlmSettingsField) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = append(s.settings, settings.TimeoutSeconds)
}

func (s *fakeLlmStore) ResolveAPIKey(string) string { return s.resolveKey }

// ---- fixture ----

type llmEnvFixture struct {
	*pageEnvFixture
	store    *fakeLlmStore
	llm      *fakeLlmRunner
	page     *HumanVsLlmPage
	aiRunner *fakeRunner
}

func newLlmEnvFixture() *llmEnvFixture {
	f := &llmEnvFixture{pageEnvFixture: newPageEnvFixture(newPageRepo(), true), store: &fakeLlmStore{}, llm: newFakeLlmRunner()}
	env := f.pageEnvFixture.env
	var seq atomic.Int64
	env.NewRequestID = func(prefix string) string { return fmt.Sprintf("%s-%d", prefix, seq.Add(1)) }
	f.aiRunner = newFakeRunner()
	f.page = newHumanVsLlmPage(llmEnvOf(env, f.store), HumanVsLlmHooks{}, f.llm, f.aiRunner)
	return f
}

func llmEnvOf(env GameEnv, store LlmStore) LlmEnv {
	return LlmEnv{GameEnv: env, Store: store, Settings: nil}
}

// reload 设置脚本后重新发起两槽位加载（构造期加载先于脚本设置）。
func (f *llmEnvFixture) reload() {
	f.store.LoadSlotAsync("t-black", storage.SlotBlack)
	f.store.LoadSlotAsync("t-red", storage.SlotRed)
}

// deliverLoad 投递槽位加载回执 + 完成进页恢复（模拟事件总线）。
func (f *llmEnvFixture) deliverLoad(t *testing.T) {
	t.Helper()
	f.store.mu.Lock()
	pending := append([]SecureSlotLoaded(nil), f.store.pendingLoads...)
	f.store.pendingLoads = nil
	f.store.mu.Unlock()
	for _, ev := range pending {
		f.page.OnAppEvent(ev)
	}
	f.page.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsLlm})
}

// blackReady 投递"黑=配置/红=空"加载回执（非镜像态）。
func (f *llmEnvFixture) blackReady(t *testing.T, black llm.LlmEndpointConfig) {
	t.Helper()
	f.store.scripts = map[string]*llm.LlmEndpointConfig{
		storage.SlotBlack: &black,
	}
	f.reload()
	f.deliverLoad(t)
}

// playPlayerMove 玩家走红炮（7,7）→（7,4）。
func (f *llmEnvFixture) playPlayerMove() bool {
	f.page.store.VM.OnTap(7, 7)
	snap := f.page.store.State()
	if snap.Selected == nil {
		return false
	}
	for _, p := range snap.LegalTargets {
		if p.Col == 7 && p.Row == 4 {
			f.page.store.VM.OnTap(7, 4)
			return len(f.page.store.State().MoveHistory) == len(snap.MoveHistory)+1
		}
	}
	return false
}

// ---- fake LlmRunner ----

type fakeLlmRunner struct {
	mu       sync.Mutex
	moves    []LlmMoveSpec
	moveIDs  []string
	tests    []string // authSlot
	testCfgs []llm.LlmEndpointConfig
	cancels  []string
}

func newFakeLlmRunner() *fakeLlmRunner { return &fakeLlmRunner{} }

func (r *fakeLlmRunner) MoveAsync(id string, spec LlmMoveSpec) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.moves = append(r.moves, spec)
	r.moveIDs = append(r.moveIDs, id)
}

func (r *fakeLlmRunner) TestConnectionAsync(_ string, cfg llm.LlmEndpointConfig, authSlot string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tests = append(r.tests, authSlot)
	r.testCfgs = append(r.testCfgs, cfg)
}

func (r *fakeLlmRunner) Cancel(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels = append(r.cancels, id)
}

func (r *fakeLlmRunner) lastMoveID(t *testing.T) string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.moveIDs) == 0 {
		t.Fatal("无在途 LLM 走子请求")
	}
	return r.moveIDs[len(r.moveIDs)-1]
}

// ---- 用例 ----

// 防错 #4 + DR-014：配置未加载完成不触发；黑槽全空且红槽有配置 → 镜像视图。
func TestHumanVsLlmConfigLoadMirror(t *testing.T) {
	f := newLlmEnvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://red.example/v1", APIKey: "****red1", Model: "red-model", Preset: "智谱 GLM"}

	// 黑=空、红=配置 → 镜像
	f.store.scripts = map[string]*llm.LlmEndpointConfig{storage.SlotRed: &red}
	f.reload()
	f.deliverLoad(t)
	if !f.page.configReady() {
		t.Fatal("config should be ready after both slots loaded")
	}
	if !f.page.mirrored {
		t.Fatal("empty black + configured red must mirror")
	}
	if f.page.config != red {
		t.Fatalf("config = %+v, want red view", f.page.config)
	}
	// 玩家走子后（轮黑）不得触发：镜像但触发守卫与普通一致——此处直接验证触发。
	if !f.playPlayerMove() {
		t.Fatal("player move failed")
	}
	f.page.onPlayerMoved()
	if f.page.llmRequestID == "" {
		t.Fatal("black to move after config ready: LLM should be triggered")
	}

	// 黑方有配置 → 非镜像
	f2 := newLlmEnvFixture()
	black := llm.LlmEndpointConfig{BaseURL: "https://black.example/v1", APIKey: "****blk1", Model: "black-model"}
	f2.blackReady(t, black)
	if f2.page.mirrored {
		t.Fatal("configured black must not mirror")
	}
	if f2.page.config != black {
		t.Fatalf("config = %+v, want black", f2.page.config)
	}
}

// 触发载荷：AuthSlot 随来源（镜像=红槽——DR-010 闭环）。
func TestHumanVsLlmTriggerSpec(t *testing.T) {
	f := newLlmEnvFixture()
	black := llm.LlmEndpointConfig{BaseURL: "https://b/v1", APIKey: "****1234", Model: "glm-4-flash"}
	f.blackReady(t, black)
	if !f.playPlayerMove() {
		t.Fatal("player move failed")
	}
	f.page.onPlayerMoved()
	spec := f.llm.moves[len(f.llm.moves)-1]
	if spec.Endpoint != black {
		t.Fatalf("endpoint = %+v", spec.Endpoint)
	}
	if spec.AuthSlot != storage.SlotBlack {
		t.Fatalf("authSlot = %q", spec.AuthSlot)
	}
	if spec.AdvisorMode != f.page.gameSettings.AdvisorMode || spec.MaxAttempts != f.page.gameSettings.MaxAttempts {
		t.Fatalf("settings not propagated: %+v", spec)
	}
	if !f.page.llmThinking {
		t.Fatal("thinking state expected")
	}
	if !f.page.store.VM.IsFinished() && f.page.store.State().Selected != nil {
		t.Fatal("input should be locked during thinking")
	}
}

// 应手结算：ok 落盘 + 解锁；迟到/作废 id 丢弃（K21 二次收口）。
func TestHumanVsLlmMoveDoneOkAndStale(t *testing.T) {
	f := newLlmEnvFixture()
	f.blackReady(t, llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "m"})
	if !f.playPlayerMove() {
		t.Fatal("player move failed")
	}
	f.page.onPlayerMoved()
	id := f.llm.lastMoveID(t)
	stale := id + "-stale"
	f.page.OnAppEvent(LlmMoveDone{RequestID: stale, Result: engine.MoveSourceResult{Status: engine.StatusOK, Move: &rules.Move{From: rules.Pos(7, 2), To: rules.Pos(7, 3)}}})
	if !f.page.llmThinking {
		t.Fatal("stale move done must be ignored")
	}
	reply := rules.Move{From: rules.Pos(7, 2), To: rules.Pos(7, 3)}
	f.page.OnAppEvent(LlmMoveDone{RequestID: id, Result: engine.MoveSourceResult{Status: engine.StatusOK, Move: &reply}})
	if f.page.llmThinking || f.page.llmRequestID != "" {
		t.Fatal("must settle and unlock")
	}
	if len(f.page.store.State().MoveHistory) != 2 {
		t.Fatalf("history = %d, want 2", len(f.page.store.State().MoveHistory))
	}
}

// failed（resign 策略）显式判负防软死锁（防错 #7）。
func TestHumanVsLlmMoveFailedResign(t *testing.T) {
	f := newLlmEnvFixture()
	f.blackReady(t, llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "m"})
	if !f.playPlayerMove() {
		t.Fatal("player move failed")
	}
	f.page.onPlayerMoved()
	id := f.llm.lastMoveID(t)
	f.page.OnAppEvent(LlmMoveDone{RequestID: id, Result: engine.MoveSourceResult{Status: engine.StatusFailed, Note: "模型持续失败"}})
	if f.page.store.State().Result == nil {
		t.Fatal("failed must resign and finish game")
	}
	if got := *f.page.store.State().Result; got != state.ResultRedWins {
		t.Fatalf("result = %q, want redWins", got)
	}
}

// 取消链：悔棋/新游戏/离页 → Cancel（#G5）+ 解锁。
func TestHumanVsLlmCancelChain(t *testing.T) {
	f := newLlmEnvFixture()
	f.blackReady(t, llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "m"})
	if !f.playPlayerMove() {
		t.Fatal("player move failed")
	}
	f.page.onPlayerMoved()
	id := f.llm.lastMoveID(t)
	f.page.undoMove()
	// 上游语义：思考中悔棋 = 作废在途应手（undo 因输入锁 no-op，轮黑重触发"中止重想"）。
	if len(f.llm.cancels) == 0 || f.llm.cancels[len(f.llm.cancels)-1] != id {
		t.Fatalf("undo must cancel in-flight llm request, cancels = %v", f.llm.cancels)
	}
	if !f.page.llmThinking || f.page.llmRequestID == "" || f.page.llmRequestID == id {
		t.Fatalf("abort-and-rethink expected: thinking=%v newID=%q old=%q", f.page.llmThinking, f.page.llmRequestID, id)
	}

	// dispose 取消（离页路径）：先结算"中止重想"的应手，回到玩家回合再触发。
	idRethink := f.page.llmRequestID
	reply := rules.Move{From: rules.Pos(7, 2), To: rules.Pos(7, 3)}
	f.page.OnAppEvent(LlmMoveDone{RequestID: idRethink, Result: engine.MoveSourceResult{Status: engine.StatusOK, Move: &reply}})
	// 红炮 (7,4) →（7,7）回撤（上一步已离开起点，helper 不适用）。
	f.page.store.VM.OnTap(7, 4)
	f.page.store.VM.OnTap(7, 7)
	if len(f.page.store.State().MoveHistory) != 3 {
		t.Fatalf("second player move failed, history = %d", len(f.page.store.State().MoveHistory))
	}
	f.page.onPlayerMoved()
	id2 := f.llm.lastMoveID(t)
	f.page.Dispose()
	found := false
	for _, c := range f.llm.cancels {
		if c == id2 {
			found = true
		}
	}
	if !found {
		t.Fatalf("dispose must cancel in-flight request, cancels = %v", f.llm.cancels)
	}
}

// 流式消息区：chunk 追加（K21：非当前 id 丢弃）、done 收口。
func TestHumanVsLlmStreamChunkK21(t *testing.T) {
	f := newLlmEnvFixture()
	f.blackReady(t, llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "m"})
	if !f.playPlayerMove() {
		t.Fatal("player move failed")
	}
	f.page.onPlayerMoved()
	id := f.llm.lastMoveID(t)
	content := "分析:中炮"
	f.page.OnAppEvent(LlmStreamChunk{RequestID: id, Delta: llm.Delta{Content: &content}})
	f.page.OnAppEvent(LlmStreamChunk{RequestID: "stale", Delta: llm.Delta{Content: &content}})
	lines := len(f.page.msgArea.lines)
	if lines != 1 {
		t.Fatalf("lines = %d, want 1（stale 必须丢弃）", lines)
	}
	if got := f.page.msgArea.lines[0]; got != "黑方 "+content {
		t.Fatalf("line = %q", got)
	}
	f.page.OnAppEvent(LlmStreamDone{RequestID: id, Text: "着法: h2-e2"})
	if f.page.msgArea.streaming {
		t.Fatal("stream must end on done")
	}
}

// 立即保存：镜像态拦截；正常态写黑槽 + 回执 toast（掩码合并由复制物 Set 收口）。
func TestHumanVsLlmSaveNow(t *testing.T) {
	f := newLlmEnvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://red/v1", APIKey: "****red1", Model: "red-model"}
	f.store.scripts = map[string]*llm.LlmEndpointConfig{storage.SlotRed: &red}
	f.reload()
	f.deliverLoad(t)
	f.page.saveNow()
	if len(f.store.saved) != 0 {
		t.Fatal("mirrored view must not write black slot")
	}
	if f.page.toastText == "" {
		t.Fatal("mirror save must toast hint")
	}

	// 正常态
	f2 := newLlmEnvFixture()
	black := llm.LlmEndpointConfig{BaseURL: "https://b/v1", APIKey: "****blk9", Model: "m", Preset: "DeepSeek"}
	f2.blackReady(t, black)
	f2.page.saveNow()
	if len(f2.store.saved) != 1 || f2.store.saved[0].slot != storage.SlotBlack {
		t.Fatalf("saved = %+v", f2.store.saved)
	}
	if f2.store.saved[0].cfg.APIKey != "****blk9" {
		t.Fatal("masked key must round-trip for storage merge")
	}
	f2.page.OnAppEvent(SecureSlotSaved{Slot: storage.SlotBlack, Stored: "encrypted"})
	if f2.page.toastText != "模型配置已保存" {
		t.Fatalf("toast = %q", f2.page.toastText)
	}
	// plainFallback 如实提示
	f2.page.saveNow()
	f2.page.OnAppEvent(SecureSlotSaved{Slot: storage.SlotBlack, Stored: "plainFallback"})
	if f2.page.toastText != "模型配置已保存（系统安全存储不可用，已明文保存到本地）" {
		t.Fatalf("toast = %q", f2.page.toastText)
	}
}

// 测试连接：镜像态测红方配置与红槽（DR-014 testOverride 同口径）。
func TestHumanVsLlmTestConnectionOverride(t *testing.T) {
	f := newLlmEnvFixture()
	red := llm.LlmEndpointConfig{BaseURL: "https://red/v1", APIKey: "****red1", Model: "red-model"}
	f.store.scripts = map[string]*llm.LlmEndpointConfig{storage.SlotRed: &red}
	f.reload()
	f.deliverLoad(t)
	f.page.onTestConnection(f.page.config, f.page.configCard.slot)
	if len(f.llm.tests) != 1 || f.llm.tests[0] != storage.SlotRed {
		t.Fatalf("tests = %v, want red slot", f.llm.tests)
	}
	if f.llm.testCfgs[0] != red {
		t.Fatalf("test cfg = %+v, want red config", f.llm.testCfgs[0])
	}
	// 回执回填
	f.page.configCard.SetTestResult(true, "连接成功，模型 red-model 响应正常")
	if !f.page.configCard.testOK {
		t.Fatal("test result should be applied")
	}
}

// 内置 AI 对手路径（DR-014）：引擎客户端应手，结算后解锁。
func TestHumanVsLlmBuiltinOpponent(t *testing.T) {
	f := newLlmEnvFixture()
	f.blackReady(t, llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "m"})
	f.page.opponentType = llm.SideEngineBuiltin
	if !f.playPlayerMove() {
		t.Fatal("player move failed")
	}
	f.page.onPlayerMoved()
	req := f.aiRunner.lastRequest(t)
	if req.Type != engine.ReqFindBestMove {
		t.Fatalf("type = %q", req.Type)
	}
	f.page.llmStartedAt = time.Now().Add(-minThinkDuration) // 快进 DR-G004 最小思考呈现
	m := rules.Move{From: rules.Pos(7, 2), To: rules.Pos(7, 3)}
	f.page.OnAppEvent(EngineMoveDone{RequestID: f.page.llmRequestID, Move: &m})
	if f.page.llmThinking {
		t.Fatal("must settle")
	}
	if len(f.page.store.State().MoveHistory) != 2 {
		t.Fatalf("history = %d", len(f.page.store.State().MoveHistory))
	}
}

// 防抖保存到点（LlmPersistTick 代次）+ 防错 #4：配置未加载不回写。
func TestHumanVsLlmPersist(t *testing.T) {
	f := newLlmEnvFixture()
	f.page.persistSeq = 1
	f.page.OnAppEvent(LlmPersistTick{Seq: 1})
	if len(f.store.saved) != 0 {
		t.Fatal("config not loaded: must not persist (防错 #4)")
	}
	f.blackReady(t, llm.LlmEndpointConfig{BaseURL: "https://b/v1", Model: "m"})
	f.page.OnAppEvent(LlmPersistTick{Seq: 1})
	if len(f.store.saved) != 1 {
		t.Fatal("loaded: persist expected")
	}
	// 过期代次
	f.page.OnAppEvent(LlmPersistTick{Seq: 999})
	if len(f.store.saved) != 1 {
		t.Fatal("stale persist tick must be ignored")
	}
}
