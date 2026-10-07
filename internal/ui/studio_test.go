package ui

// 工作室页状态机单测（T6'.2，09 §4：纯 Go 表驱动，不渲染）。
// 锚点 = 08 §7（摆盘校验/求解/入库/进入对战联动）+ 08 §5（非阻塞悬浮条
// 防重复发起、结果面板置顶关闭）+ 上游 EndgameStudioPage 行为。

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/solver"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// stubStudioRecords 记录库 fake（保存回调同步触发）。
type stubStudioRecords struct {
	saved  []state.GameRecordData
	errs   []error // 逐次保存回执错误（nil = 成功）
	calls  int
	onDone func(err error)
}

func (s *stubStudioRecords) RecordsListAsync(requestID string)             {}
func (s *stubStudioRecords) RecordsGetAsync(requestID string, id int64)    {}
func (s *stubStudioRecords) RecordsDeleteAsync(requestID string, id int64) {}
func (s *stubStudioRecords) RecordsSaveAsync(requestID string, record state.GameRecordData) {
	s.saved = append(s.saved, record)
	var err error
	if s.calls < len(s.errs) {
		err = s.errs[s.calls]
	}
	s.calls++
	if s.onDone != nil {
		s.onDone(err)
	}
}

// studioEventQueue 事件队列：后台 goroutine emit 入队，测试按序回投
// OnAppEvent（模拟事件总线→页面方向，-race 安全）。
type studioEventQueue struct {
	ch chan any
}

const studioQueueCap = 64

func newStudioEventQueue() *studioEventQueue {
	return &studioEventQueue{ch: make(chan any, studioQueueCap)}
}

func (q *studioEventQueue) emitFunc() func(string, any, error) {
	return func(_ string, payload any, _ error) {
		select {
		case q.ch <- payload:
		default:
			// 队列满：非阻塞丢弃（KG-010 同语义；测试中不触顶）
		}
	}
}

// pump 按序回投队列中全部事件到页面（模拟主循环 Drain→OnAppEvent）。
func (q *studioEventQueue) pump(p *StudioPage) {
	for {
		select {
		case ev := <-q.ch:
			p.OnAppEvent(ev)
		default:
			return
		}
	}
}

// waitEvent 等待一个事件（求解响应 goroutine 异步 emit）。
func (q *studioEventQueue) waitEvent(t *testing.T) any {
	t.Helper()
	select {
	case ev := <-q.ch:
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("等待事件超时")
		return nil
	}
}

func newTestStudioPage(t *testing.T) (*StudioPage, *studioEventQueue, *stubStudioRecords, *[]string) {
	t.Helper()
	q := newStudioEventQueue()
	records := &stubStudioRecords{}
	env := LlmEnv{
		GameEnv: GameEnv{
			Emit:         q.emitFunc(),
			Cancel:       func(string) {},
			NewRequestID: func(prefix string) string { return prefix + "-1" },
			Records:      records,
		},
	}
	launched := &[]string{}
	page := NewStudioPage(env, StudioHooks{
		OnBack: func() {},
		OnBattle: func(mode BattleMode, fen, side string) {
			*launched = append(*launched, string(mode)+"|"+fen+"|"+side)
		},
	})
	t.Cleanup(page.Dispose)
	return page, q, records, launched
}

// loadFen 页面载入局面（测试注入；生产路径为 FEN 导入/摆盘/识图）。
func (p *StudioPage) loadFen(t *testing.T, fen string) {
	t.Helper()
	grid, err := rules.ParseBoardFen(fen)
	if err != nil {
		t.Fatalf("ParseBoardFen(%q): %v", fen, err)
	}
	p.grid = grid
	p.touchGrid()
}

// spec: 摆盘编辑——放置即时校验（位置/数量）与橡皮清除（setupRules 行为）。
func TestStudioSetupEditing(t *testing.T) {
	page, _, _, _ := newTestStudioPage(t)

	redRook := 4 // paletteKinds: king advisor minister knight rook cannon pawn
	page.selectPiece(rules.Red, redRook)
	page.onCellTap(0, 5) // 放第 1 枚车
	page.onCellTap(1, 5) // 第 2 枚
	if page.grid[5][0] == nil || page.grid[5][1] == nil {
		t.Fatal("车应已放置")
	}
	page.onCellTap(2, 5) // 第 3 枚：数量超限 → 拒绝 + toast
	if page.grid[5][2] != nil {
		t.Fatal("第 3 枚车应被数量上限拒绝")
	}
	if page.toastText != "红方车最多 2 枚" {
		t.Fatalf("toast = %q", page.toastText)
	}

	page.selectPiece(rules.Red, 0) // 帅
	page.onCellTap(4, 5)           // 九宫外 → 拒绝
	if page.grid[5][4] != nil {
		t.Fatal("帅放九宫外应被拒绝")
	}
	if page.toastText != "帅/将只能放在九宫内的 9 个位置" {
		t.Fatalf("toast = %q", page.toastText)
	}
	page.onCellTap(4, 9) // 九宫内 → 放置
	if page.grid[9][4] == nil || page.grid[9][4].Kind != rules.King {
		t.Fatal("帅应已放置")
	}

	page.eraser = true
	page.onCellTap(4, 9) // 橡皮清除
	if page.grid[9][4] != nil {
		t.Fatal("橡皮应清除棋子")
	}
}

// spec: 整体校验守卫——空盘/缺王不允许求解与保存（toast 报因，不发起请求）。
func TestStudioValidationGuard(t *testing.T) {
	page, _, records, _ := newTestStudioPage(t)
	fake := newFakeSolver()
	page.solver = NewSolverClient(page.env.GameEnv, fake)

	page.startSolve()
	if len(fake.requests) != 0 {
		t.Fatal("校验不通过不得发起求解")
	}
	if len(records.saved) != 0 {
		t.Fatal("校验不通过不得入库")
	}
	if page.toastText == "" {
		t.Fatal("应有校验问题 toast")
	}

	page.loadFen(t, solverTestFen)
	if problems := page.validate(); len(problems) != 0 {
		t.Fatalf("FEN-A 应通过校验: %v", problems)
	}
	page.saveUnsolvedRecord()
	if len(records.saved) != 1 || records.saved[0].SolveStatus != state.SolveNone {
		t.Fatalf("未求解保存 = %+v", records.saved)
	}
	if records.saved[0].Mode != "endgame" || records.saved[0].Title == "" {
		t.Fatalf("未求解记录 = %+v", records.saved[0])
	}
}

// spec: 求解全链路——startSolve → Runner → SolveDone → 自动入库 →
// RecordSaveDone → 结果面板打开（recordSaved）；重复发起由 solving 守卫拦截。
func TestStudioSolveFlowAndRecord(t *testing.T) {
	page, q, records, _ := newTestStudioPage(t)
	fake := newFakeSolver()
	page.solver = NewSolverClient(page.env.GameEnv, fake)
	page.loadFen(t, solverTestFen)

	page.timeIdx = 1  // 30 秒
	page.depthIdx = 1 // 标准（5 着内）
	page.startSolve()
	if !page.solving {
		t.Fatal("求解中标志应置位")
	}
	page.startSolve() // 非阻塞悬浮条下重复发起 → 守卫拦截
	if n := len(fake.requests); n != 1 {
		t.Fatalf("重复发起应被拦截，请求数 = %d", n)
	}
	req := fake.lastRequest(t)
	var p solver.SolvePayload
	if err := remarshal(jsonAny(t, req.Payload), &p); err != nil {
		t.Fatalf("载荷反解: %v", err)
	}
	// 载荷 FEN = 当前局面（BuildFen 全量，含计数字段）；棋盘+轮走方与测试局面一致
	if f := strings.Fields(p.Fen); len(f) < 2 || f[0]+" "+f[1] != solverTestFen || p.TimeLimitMs != 30_000 || p.MaxPlies != 9 {
		t.Fatalf("求解载荷 = %+v", p)
	}

	fake.deliver(solver.Response{ID: req.ID, OK: true, Result: solver.WireSolveResult{
		Status:        "solved",
		Solutions:     []solver.WireSolution{{Moves: []solver.WireMove{{From: solver.WirePos{Col: 0, Row: 5}, To: solver.WirePos{Col: 3, Row: 5}}}}},
		Elapsed:       42,
		SearchedPlies: 3,
	}})
	page.OnAppEvent(q.waitEvent(t).(SolveDone))

	if page.solving {
		t.Fatal("求解结束应复位 solving")
	}
	if len(records.saved) != 1 {
		t.Fatalf("求解结论应自动入库: %d", len(records.saved))
	}
	rec := records.saved[0]
	if rec.SolveStatus != state.SolveSolved || len(rec.Solutions) != 1 || rec.Solutions[0][0] != "a4d4" {
		t.Fatalf("入库记录 = %+v", rec)
	}

	// 入库回执（生产路径异步；此处直接回投——stub 同步保存不回调）
	page.OnAppEvent(RecordSaveDone{})
	if !page.sheetOpen || page.sheet == nil {
		t.Fatal("入库回执后应打开结果面板")
	}
	if !page.sheet.recordSaved {
		t.Fatal("结果面板应标记已入库")
	}
}

// spec: 求解失败/取消路径——错误透传 toast、solving 复位、不残留半程结果
// （04 §4：取消 → 状态回 none）。
func TestStudioSolveErrorPath(t *testing.T) {
	page, q, records, _ := newTestStudioPage(t)
	fake := newFakeSolver()
	page.solver = NewSolverClient(page.env.GameEnv, fake)
	page.loadFen(t, solverTestFen)

	page.startSolve()
	req := fake.lastRequest(t)
	fake.deliver(solver.Response{ID: req.ID, Error: "canceled"})
	page.OnAppEvent(q.waitEvent(t).(SolveDone))

	if page.solving || page.sheetOpen || len(records.saved) != 0 {
		t.Fatalf("取消后不应残留结果: solving=%v sheet=%v saved=%d", page.solving, page.sheetOpen, len(records.saved))
	}
}

// spec: 空结果防御（Result=nil）→ toast 不崩溃。
func TestStudioSolveNilResult(t *testing.T) {
	page, q, _, _ := newTestStudioPage(t)
	page.loadFen(t, solverTestFen)
	page.startSolve()
	page.OnAppEvent(SolveDone{RequestID: page.solveRequest})
	if page.solving {
		t.Fatal("应复位 solving")
	}
	if page.toastText == "" {
		t.Fatal("应有无结果 toast")
	}
	q.pump(page)
}

// spec: Dispose——在途请求取消（Runner ctx + 总线双收口 #G5）、节拍停止。
func TestStudioDispose(t *testing.T) {
	page, _, _, _ := newTestStudioPage(t)
	fake := newFakeSolver()
	page.solver = NewSolverClient(page.env.GameEnv, fake)
	page.loadFen(t, solverTestFen)

	page.startSolve()
	page.Dispose()
	if n := len(fake.cancels); n != 1 {
		t.Fatalf("Dispose 应取消在途求解: %v", fake.cancels)
	}
	if page.solving {
		t.Fatal("Dispose 应复位 solving")
	}
}

// spec: 进入对战联动——起点=求解局面 FEN，人机 AI 缺省执求解方。
func TestStudioBattleLink(t *testing.T) {
	page, _, _, launched := newTestStudioPage(t)
	page.loadFen(t, solverTestFen)
	page.sheet = &solveSheet{fen: solverTestFen, redTurn: true}
	page.sheetOpen = true
	page.launcher.Open(nil)

	page.launchBattle(BattleHumanVsAi, "", nil)
	page.launchBattle(BattleHumanVsHuman, "", nil)
	if len(*launched) != 2 {
		t.Fatalf("联动回调次数 = %d", len(*launched))
	}
	if (*launched)[0] != "humanVsAi|"+solverTestFen+"|红" {
		t.Fatalf("人机 AI 联动 = %q（缺省执求解方红）", (*launched)[0])
	}
	if (*launched)[1] != "humanVsHuman|"+solverTestFen+"|红" {
		t.Fatalf("双人联动 = %q", (*launched)[1])
	}
}

// spec: 入库失败呈现——保存失败时结果面板标记"已保存到棋谱库失败"。
func TestStudioRecordSaveFailure(t *testing.T) {
	page, _, records, _ := newTestStudioPage(t)
	page.loadFen(t, solverTestFen)
	records.errs = []error{errors.New("db closed")}

	page.startSolve()
	page.pendingSheet = &solveSheet{fen: page.solveFen, redTurn: true}
	page.persistSolveRecord()
	page.onRecordSaved(errors.New("db closed"))
	if !page.sheetOpen || page.sheet.recordSaved {
		t.Fatal("保存失败时面板应标记失败")
	}
}

// spec: 识图流程——三槽位掩码配置加载 → DR-009 借用解析（黑→红，注记来源）
// → 读文件+识图（fake VisionReader 面不可注入，走 ReadDataAsync+真实 Reader?
// 改为直接驱动 onVisionDone/onFilePicked 状态机 + ResolveAssistantConfig 分支）。
func TestStudioVisionSlotsAndGuards(t *testing.T) {
	page, _, _, _ := newTestStudioPage(t)

	// 槽位未齐：识图入口拦截
	page.beginVisionRead("/tmp/nope.png")
	if page.reading || page.visionMessage == "" {
		t.Fatal("配置未齐应拦截并提示")
	}

	// 三槽位回执（助手槽空 → 借用黑方）
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_assistant"})
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_black", Config: &llm.LlmEndpointConfig{
		BaseURL: "https://api.example.com/v1", Model: "vl-model", Preset: "dashscope",
	}})
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_red"})
	if !page.visionSlotsLoaded() {
		t.Fatal("三槽位应已加载")
	}

	// 助手全空 + 黑方有配置 → 识图发起并注记借用来源（K33：reading 置位）
	page.beginVisionRead("/tmp/board.png")
	if !page.reading {
		t.Fatal("识图应置位 reading")
	}
	if !strings.Contains(page.visionMessage, "已临时借用黑方对战配置") ||
		!strings.Contains(page.visionMessage, "可能不支持识图") {
		t.Fatalf("借用注记 = %q", page.visionMessage)
	}

	// K33 防重入：reading 中重复发起被拦
	reqID := page.visionRequest
	page.startVisionPick()
	page.beginVisionRead("/tmp/again.png")
	if page.visionRequest != reqID {
		t.Fatal("识别中重复发起应被拦截")
	}

	// 回执：迟到 id 丢弃（识别态保持等待真实回执）；有效 FEN 载入棋盘
	page.OnAppEvent(VisionReadDone{RequestID: "stale"})
	if !page.reading {
		t.Fatal("迟到回执应被丢弃（识别态保持）")
	}
	grid, err := rules.ParseBoardFen("3k5/9/9/9/R8/8R/9/9/9/4K4 w - - 0 1")
	if err != nil {
		t.Fatalf("ParseBoardFen: %v", err)
	}
	_ = grid
	page.OnAppEvent(VisionReadDone{RequestID: reqID, Fen: "3k5/9/9/9/R8/8R/9/9/9/4K4 w - - 0 1"})
	if page.reading {
		t.Fatal("回执后应复位 reading")
	}
	if !page.visionLoaded || len(page.grid[4]) == 0 || page.grid[4][0] == nil {
		t.Fatal("识图结果应载入棋盘")
	}
	if !strings.Contains(page.visionMessage, "请人工核对后再求解") {
		t.Fatalf("校正流提示 = %q", page.visionMessage)
	}
}

// spec: 识图失败路径——错误进消息区，不落盘面。
func TestStudioVisionError(t *testing.T) {
	page, _, _, _ := newTestStudioPage(t)
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_assistant"})
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_black"})
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_red"})
	page.beginVisionRead("/tmp/board.png")
	reqID := page.visionRequest
	page.OnAppEvent(VisionReadDone{RequestID: reqID, Err: errors.New("已重试 2 次仍失败")})
	if page.reading || page.visionLoaded {
		t.Fatal("失败后应复位且不载入")
	}
	if !strings.Contains(page.visionMessage, "识图失败") {
		t.Fatalf("消息 = %q", page.visionMessage)
	}
}

// ---- T6'.4 求解辅助与助手配置弹窗 ----

// fakeStudioLlmStore LlmStore fake（助手弹窗保存/槽位重载路径）。
type fakeStudioLlmStore struct {
	saved    map[string]llm.LlmEndpointConfig
	loaded   []string
	resolveK string
}

func newFakeStudioLlmStore() *fakeStudioLlmStore {
	return &fakeStudioLlmStore{saved: map[string]llm.LlmEndpointConfig{}}
}

func (f *fakeStudioLlmStore) LoadSlotAsync(requestID, slot string) { f.loaded = append(f.loaded, slot) }
func (f *fakeStudioLlmStore) SaveSlotAsync(requestID, slot string, cfg llm.LlmEndpointConfig) {
	f.saved[slot] = cfg
}
func (f *fakeStudioLlmStore) SaveSettings(settings state.LlmGameSettings, fields ...state.LlmSettingsField) {
}
func (f *fakeStudioLlmStore) ResolveAPIKey(slot string) string { return f.resolveK }

// spec: 求解辅助全链路——勾选大模型辅助：提议（借用黑方槽 toast）→ 提议有效
// → 求解器裁判 → 注释写入棋谱 llmNote（05 §6 时序）。
func TestStudioSolveAssistFlow(t *testing.T) {
	page, q, records, _ := newTestStudioPage(t)
	fake := newFakeSolver()
	page.solver = NewSolverClient(page.env.GameEnv, fake)
	page.loadFen(t, solverTestFen)
	// 助手槽空 → 借用黑方
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_assistant"})
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_black", Config: &llm.LlmEndpointConfig{
		BaseURL: "https://api.example.com/v1", Model: "m", Preset: "",
	}})
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_red"})

	var assistCfg llm.LlmEndpointConfig
	var assistSlot string
	page.assistRunner = func(requestID string, board *rules.Board, cfg llm.LlmEndpointConfig, authSlot string) {
		assistCfg, assistSlot = cfg, authSlot
		page.env.Emit("", AssistProposalDone{
			RequestID: requestID,
			Proposal:  llm.SolveProposal{FirstMoveCode: "a4-d4", Idea: "平车闷杀"},
		}, nil)
	}
	page.useLlmOn = true
	page.startSolve()
	q.pump(page) // 提议回执（assistRunner 同步 emit）回投页面

	if !strings.Contains(page.toastText, "已临时借用黑方对战配置") {
		t.Fatalf("借用 toast = %q", page.toastText)
	}
	if assistCfg.Model != "m" || assistSlot != "llm_config_black" {
		t.Fatalf("借用配置/槽位 = %+v / %q（DR-009/DR-010）", assistCfg, assistSlot)
	}

	// 提议回执 → 裁判请求提交（载荷首着 a4-d4）
	if len(fake.requests) != 1 || fake.requests[0].Type != solver.ReqIsWinningFirstMove {
		t.Fatalf("裁判请求 = %+v", fake.requests)
	}
	var vp solver.IsWinningFirstMovePayload
	if err := remarshal(jsonAny(t, fake.requests[0].Payload), &vp); err != nil {
		t.Fatalf("裁判载荷反解: %v", err)
	}
	if vp.Fen == "" || vp.FirstMove == nil {
		t.Fatalf("裁判载荷 = %+v", vp)
	}

	fake.deliver(solver.Response{ID: fake.requests[0].ID, OK: true, Result: true})
	page.OnAppEvent(q.waitEvent(t).(SolveWinDone)) // 裁判回执回投
	// 正式求解在裁判通过后提交
	reqs := len(fake.requests)
	if reqs != 2 || fake.requests[1].Type != solver.ReqSolve {
		t.Fatalf("裁判通过后应提交求解: %+v", fake.requests)
	}
	solveID := fake.requests[1].ID
	fake.deliver(solver.Response{ID: solveID, OK: true, Result: solver.WireSolveResult{Status: "solved"}})
	page.OnAppEvent(q.waitEvent(t).(SolveDone))

	if len(records.saved) != 1 {
		t.Fatalf("应入库: %d", len(records.saved))
	}
	want := "大模型首选 a4-d4（已验证为必胜着法）；思路: 平车闷杀"
	if records.saved[0].LlmNote == nil || *records.saved[0].LlmNote != want {
		t.Fatalf("llmNote = %v", records.saved[0].LlmNote)
	}
}

// spec: 提议无效/裁判未通过——说明文字生成、求解照常进行（05 §6）。
func TestStudioSolveAssistFailureNotes(t *testing.T) {
	t.Run("无有效提议", func(t *testing.T) {
		page, q, _, _ := newTestStudioPage(t)
		fake := newFakeSolver()
		page.solver = NewSolverClient(page.env.GameEnv, fake)
		page.loadFen(t, solverTestFen)
		page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_assistant"})
		page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_black", Config: &llm.LlmEndpointConfig{
			BaseURL: "https://api.example.com/v1", Model: "m", Preset: "",
		}})
		page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_red"})
		page.assistRunner = func(requestID string, _ *rules.Board, _ llm.LlmEndpointConfig, _ string) {
			page.env.Emit("", AssistProposalDone{RequestID: requestID, Message: "回复不在清单中"}, nil)
		}
		page.useLlmOn = true
		page.startSolve()
		q.pump(page) // 提议回执回投
		if len(fake.requests) != 1 || fake.requests[0].Type != solver.ReqSolve {
			t.Fatal("无效提议应直接求解（无裁判）")
		}
		solveID := fake.requests[0].ID
		fake.deliver(solver.Response{ID: solveID, OK: true, Result: solver.WireSolveResult{Status: "timeout"}})
		page.OnAppEvent(SolveDone{RequestID: solveID, Result: &solver.WireSolveResult{Status: "timeout"}})
		// timeout 结果 sheet 流程：pendingSheet + 入库
		if page.pendingSheet == nil || page.pendingSheet.llmNote != "大模型辅助未给出有效提议（回复不在清单中）" {
			t.Fatalf("llmNote = %+v", page.pendingSheet)
		}
	})
	t.Run("裁判未通过", func(t *testing.T) {
		page, q, _, _ := newTestStudioPage(t)
		fake := newFakeSolver()
		page.solver = NewSolverClient(page.env.GameEnv, fake)
		page.loadFen(t, solverTestFen)
		page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_assistant"})
		page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_black", Config: &llm.LlmEndpointConfig{
			BaseURL: "https://api.example.com/v1", Model: "m", Preset: "",
		}})
		page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_red"})
		page.assistRunner = func(requestID string, _ *rules.Board, _ llm.LlmEndpointConfig, _ string) {
			page.env.Emit("", AssistProposalDone{RequestID: requestID,
				Proposal: llm.SolveProposal{FirstMoveCode: "h0-g2", Idea: "马跳"}}, nil)
		}
		page.useLlmOn = true
		page.startSolve()
		q.pump(page) // 提议回执回投
		verify := fake.requests[0]
		fake.deliver(solver.Response{ID: verify.ID, OK: true, Result: false})
		page.OnAppEvent(q.waitEvent(t).(SolveWinDone))
		if len(fake.requests) != 2 || fake.requests[1].Type != solver.ReqSolve {
			t.Fatal("裁判后应提交求解")
		}
		solveID := fake.requests[1].ID
		fake.deliver(solver.Response{ID: solveID, OK: true, Result: solver.WireSolveResult{Status: "noSolution"}})
		page.OnAppEvent(SolveDone{RequestID: solveID, Result: &solver.WireSolveResult{Status: "noSolution"}})
		want := "大模型首选 h0-g2 未通过求解器验证，已忽略；思路: 马跳"
		if page.pendingSheet == nil || page.pendingSheet.llmNote != want {
			t.Fatalf("llmNote = %+v", page.pendingSheet)
		}
	})
}

// spec: 归一化——分隔符/杂质剥离。
func TestNormalizeAssistCode(t *testing.T) {
	if got := normalizeAssistCode("a5—d5。"); got != "a5d5" {
		t.Fatalf("normalize = %q", got)
	}
}

// spec: 助手配置弹窗——打开（掩码回显）、保存（掩码合并落盘 + 槽位重载 +
// 关闭）、测试连接回执回填。
func TestStudioAssistantDialog(t *testing.T) {
	q := newStudioEventQueue()
	store := newFakeStudioLlmStore()
	env := LlmEnv{
		GameEnv: GameEnv{
			Emit:         q.emitFunc(),
			Cancel:       func(string) {},
			NewRequestID: func(prefix string) string { return prefix + "-1" },
		},
		Store: store,
	}
	page := NewStudioPage(env, StudioHooks{})
	t.Cleanup(page.Dispose)
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_assistant"})
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_black"})
	page.OnAppEvent(SecureSlotLoaded{Slot: "llm_config_red"})

	page.openAssistantDialog()
	if !page.modalOpen() || page.assistantCard == nil {
		t.Fatal("弹窗应打开")
	}
	// 保存 → 槽位写入 + 重载 + 关闭
	page.assistantConfig.BaseURL = "https://x.example.com/v1"
	page.saveAssistant()
	if _, ok := store.saved["llm_config_assistant"]; !ok {
		t.Fatal("助手槽应写入")
	}
	page.OnAppEvent(SecureSlotSaved{Slot: "llm_config_assistant", Stored: "plainFallback"})
	if page.assistantOpen {
		t.Fatal("保存回执后应关闭弹窗")
	}
	if !strings.Contains(page.toastText, "已明文保存到本地") {
		t.Fatalf("toast = %q", page.toastText)
	}
	if len(store.loaded) == 0 || store.loaded[len(store.loaded)-1] != "llm_config_assistant" {
		t.Fatalf("保存后应重载助手槽: %v", store.loaded)
	}

	// 测试连接回执回填
	page.openAssistantDialog()
	page.OnAppEvent(LlmTestDone{RequestID: page.assistTestID, OK: true, Message: "ok"})
	if page.assistTestID != "" {
		t.Fatal("测试回执后应清空在途标记")
	}
	page.closeAssistantDialog()
}
