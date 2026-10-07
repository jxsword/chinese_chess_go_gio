package ui

// 工作室页状态机单测（T6'.2，09 §4：纯 Go 表驱动，不渲染）。
// 锚点 = 08 §7（摆盘校验/求解/入库/进入对战联动）+ 08 §5（非阻塞悬浮条
// 防重复发起、结果面板置顶关闭）+ 上游 EndgameStudioPage 行为。

import (
	"errors"
	"strings"
	"testing"
	"time"

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
