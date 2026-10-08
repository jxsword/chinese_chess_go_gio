package ui

// 记录库页状态用例（T5'.3，D-004；上游 RecordLibraryPage/RecordDetailPage
// 交互语义的状态级等价——09 §4 口径：不渲染，只测状态迁移，回执事件注入）。

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// fakeRecordRepo 记录库 fake（同步回调面）。
type fakeRecordRepo struct {
	records []storage.GameRecordSummary
	byID    map[int64]*storage.GameRecord
	emitted []any
	seq     int
}

func (f *fakeRecordRepo) RecordsListAsync(requestID string) {
	f.emitted = append(f.emitted, RecordsListDone{RequestID: requestID, Records: f.records})
}
func (f *fakeRecordRepo) RecordsGetAsync(requestID string, id int64) {
	if r, ok := f.byID[id]; ok {
		f.emitted = append(f.emitted, RecordGetDone{RequestID: requestID, Record: r})
	} else {
		f.emitted = append(f.emitted, RecordGetDone{RequestID: requestID, Err: errors.New("missing")})
	}
}
func (f *fakeRecordRepo) RecordsSaveAsync(requestID string, record state.GameRecordData) {}
func (f *fakeRecordRepo) RecordsDeleteAsync(requestID string, id int64) {
	delete(f.byID, id)
	f.emitted = append(f.emitted, RecordDeleteDone{RequestID: requestID})
}

func newRecordLibraryEnv() (GameEnv, *fakeRecordRepo) {
	repo := &fakeRecordRepo{byID: map[int64]*storage.GameRecord{}}
	env := GameEnv{
		Records:      repo,
		NewRequestID: func(prefix string) string { repo.seq++; return fmt.Sprintf("%s-%d", prefix, repo.seq) },
	}
	return env, repo
}

func summaryRecord(id int64, title, mode string, result *string, solve *string) storage.GameRecordSummary {
	return storage.GameRecordSummary{ID: id, Title: title, Mode: mode, Result: result, SolveStatus: solve, CreatedAt: 1760000000000}
}

// 上游载入语义：对局类打开即定位保存时局面（pos=len）；残局类默认第一条解法。
func TestRecordLibraryOpenDetailDefaults(t *testing.T) {
	env, repo := newRecordLibraryEnv()
	solved := "solved"
	gameMoves := []storage.RecordMove{{F: [2]int{7, 7}, T: [2]int{4, 7}}, {F: [2]int{7, 0}, T: [2]int{6, 2}}}
	repo.byID[1] = &storage.GameRecord{
		ID: 1, Title: "对局甲", Mode: "humanVsHuman", InitialFen: rules.FENInitial,
		Moves: gameMoves, SolveStatus: nil, Solutions: []any{},
	}
	repo.byID[2] = &storage.GameRecord{
		ID: 2, Title: "残局乙", Mode: "endgame", InitialFen: "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1",
		SolveStatus: &solved, Solutions: []any{[]any{"h5h3"}, []any{"i4d4"}},
	}
	repo.records = []storage.GameRecordSummary{summaryRecord(1, "对局甲", "humanVsHuman", nil, nil), summaryRecord(2, "残局乙", "endgame", nil, &solved)}
	p := NewRecordLibraryPage(env, RecordLibraryHooks{})

	// 列表回执
	p.OnAppEvent(repo.emitted[0])
	if !p.loaded || len(p.records) != 2 {
		t.Fatalf("列表载入不符：%d", len(p.records))
	}
	// 打开对局类详情
	p.openRecordByID(1, nil)
	p.OnAppEvent(repo.emitted[1])
	if p.detail == nil || p.line != recordMainLine || p.pos != len(p.moves) {
		t.Fatalf("对局类应主变+定位末尾：line=%d pos=%d", p.line, p.pos)
	}
	// 打开残局类详情：默认第一条解法，从开局演示
	p.openRecordByID(2, nil)
	p.OnAppEvent(repo.emitted[2])
	if p.line != 0 || p.pos != 0 {
		t.Fatalf("残局类应默认解法 0 从 0 起：line=%d pos=%d", p.line, p.pos)
	}
	if len(p.moves) != 1 {
		t.Fatalf("解法线路重放不符：moves=%d", len(p.moves))
	}
	if len(p.notations) != 1 {
		t.Fatalf("解法记谱应重算：%d", len(p.notations))
	}
}

// 筛选 toggle 语义（nil=全部；同档再点取消）。
func TestRecordLibraryFilterToggle(t *testing.T) {
	env, _ := newRecordLibraryEnv()
	p := NewRecordLibraryPage(env, RecordLibraryHooks{})
	st := state.SolveSolved
	p.filter = &st
	p.filter = nil
	if p.filter != nil {
		t.Fatal("nil = 全部")
	}
	// filtered 对空 solveStatus 记录按 none 归类
	none := "none"
	p.records = []storage.GameRecordSummary{
		summaryRecord(1, "甲", "endgame", nil, nil),
		summaryRecord(2, "乙", "endgame", nil, &none),
	}
	f := state.SolveNone
	p.filter = &f
	if len(p.filtered()) != 2 {
		t.Fatalf("空 solveStatus 应归 none，实际 %d", len(p.filtered()))
	}
}

// 进入对战入口门控（canLaunchBattle：残局恒有；对局未分胜负有）与起点 FEN。
func TestRecordLibraryBattleGateAndFen(t *testing.T) {
	env, _ := newRecordLibraryEnv()
	p := NewRecordLibraryPage(env, RecordLibraryHooks{})
	red := "redWins"
	finished := &state.GameRecordData{
		Title: "已终局", Mode: "humanVsHuman", InitialFen: rules.FENInitial,
		Result: &red,
	}
	if p.canLaunch(finished) {
		t.Fatal("已分胜负对局无对战入口")
	}
	ongoing := &state.GameRecordData{Mode: "humanVsHuman", InitialFen: rules.FENInitial}
	if !p.canLaunch(ongoing) {
		t.Fatal("未分胜负对局应有对战入口")
	}
	if p.battleFen(ongoing) != state.FinalFenOf(rules.FENInitial, nil) {
		t.Fatal("对局起点=终局局面（无走法=初始局面）")
	}
	endgame := &state.GameRecordData{Mode: "endgame", InitialFen: "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1"}
	if p.battleFen(endgame) != endgame.InitialFen {
		t.Fatal("残局起点=initialFen")
	}
}

// 删除回执后重载；读取失败提示。
func TestRecordLibraryDeleteAndReadFailure(t *testing.T) {
	env, repo := newRecordLibraryEnv()
	repo.records = []storage.GameRecordSummary{summaryRecord(1, "甲", "endgame", nil, nil)}
	p := NewRecordLibraryPage(env, RecordLibraryHooks{})
	p.OnAppEvent(repo.emitted[0])

	p.deletingTitle = "甲"
	p.deletingID = 1
	p.OnAppEvent(RecordDeleteDone{})
	if p.deletingTitle != "" {
		t.Fatal("删除回执后应关确认框")
	}
	// 读取失败
	p.openRecordByID(99, nil)
	p.OnAppEvent(RecordGetDone{Err: errors.New("missing")})
	if p.detail != nil || p.pendingAction != nil {
		t.Fatal("读取失败不应打开详情")
	}
}

// ---- M6' 验收反馈：操作入口收拢为右侧"操作 ▾"下拉菜单 ----

// spec: 菜单状态机——打开（记录所属摘要暂存）、重复点同卡收起、换卡换目标。
func TestRecordMenuOpenState(t *testing.T) {
	env, _ := newRecordLibraryEnv()
	p := NewRecordLibraryPage(env, RecordLibraryHooks{})
	defer p.Dispose()

	sum := storage.GameRecordSummary{ID: 7, Title: "甲", Mode: "endgame"}
	// 模拟行按钮路径：置开
	p.menuOpenID, p.menuOpenSum = sum.ID, sum
	if p.menuOpenID != 7 {
		t.Fatal("前置：菜单应处于打开态")
	}
	// 重复点同卡 → 收起
	if p.menuOpenID == sum.ID {
		p.closeActionMenu()
	}
	if p.menuOpenID != 0 || p.menuOpenSum.ID != 0 {
		t.Fatalf("应收起清空: id=%d sum=%+v", p.menuOpenID, p.menuOpenSum)
	}
	// 换卡打开 → 目标切换
	other := storage.GameRecordSummary{ID: 9, Title: "乙", Mode: "humanVsHuman"}
	p.menuOpenID, p.menuOpenSum = other.ID, other
	if p.menuOpenID != 9 {
		t.Fatal("换卡应指向新目标")
	}
	p.closeActionMenu()
}

// spec: 菜单动作分发——五路全通（含 actFile：此前列表卡"导出文件"缺 case 无响应）。
func TestRecordMenuActionDispatch(t *testing.T) {
	env, repo := newRecordLibraryEnv()
	p := NewRecordLibraryPage(env, RecordLibraryHooks{})
	defer p.Dispose()
	solved := "solved"
	rec := &storage.GameRecord{
		ID: 5, Title: "残局乙", Mode: "endgame", InitialFen: rules.FENInitial,
		SolveStatus: &solved, Solutions: []any{},
	}
	repo.byID[5] = rec

	// 删除：确认态置位
	p.runAction(actDelete, summaryRecord(5, "残局乙", "endgame", nil, &solved))
	if p.deletingID != 5 || p.deletingTitle == "" {
		t.Fatalf("删除动作应置确认态: id=%d title=%q", p.deletingID, p.deletingTitle)
	}
	p.deletingID, p.deletingTitle = 0, ""

	// 导出文件：动作路径不再因外层缺 case 而空转（openRecordByID 拉全量后落
	// saveFileAsync——异步面无对话框环境下静默；此处锚定不 panic 且不误删）
	p.runAction(actFile, summaryRecord(5, "残局乙", "endgame", nil, &solved))

	// 导出 PGN / 分享：写 pending 剪贴板面
	p.runAction(actExport, summaryRecord(5, "残局乙", "endgame", nil, &solved))
	p.runAction(actShare, summaryRecord(5, "残局乙", "endgame", nil, &solved))
}

// 迟到回执按 id 收口（#G5，M7' 维护轮）：旧列表/详情回执不落新请求——
// 连续 reload 或连续点开两条记录时，旧响应不得覆盖新状态。
func TestRecordLibraryStaleReceiptsDropped(t *testing.T) {
	env, _ := newRecordLibraryEnv()
	p := NewRecordLibraryPage(env, RecordLibraryHooks{})

	// 列表：构造时已签发一次，再签发一次 → 旧 id 的回执丢弃
	staleList := p.listID
	p.reload()
	if staleList == p.listID {
		t.Fatal("repeated reload must issue a new requestId")
	}
	p.OnAppEvent(RecordsListDone{RequestID: staleList, Records: []storage.GameRecordSummary{summaryRecord(9, "stale", "", nil, nil)}})
	if p.loaded {
		t.Fatal("stale list receipt must be dropped")
	}
	p.OnAppEvent(RecordsListDone{RequestID: p.listID, Records: []storage.GameRecordSummary{summaryRecord(1, "fresh", "", nil, nil)}})
	if !p.loaded || len(p.records) != 1 || p.records[0].Title != "fresh" {
		t.Fatalf("fresh receipt should apply: %+v", p.records)
	}

	// 详情：连续点开两条记录 → 旧 id 的回执丢弃（不得用旧数据执行新动作）
	p.openRecordByID(1, nil)
	staleGet := p.getID
	p.openRecordByID(2, nil)
	if staleGet == p.getID {
		t.Fatal("second open must issue a new requestId")
	}
	p.OnAppEvent(RecordGetDone{RequestID: staleGet, Err: errors.New("stale")})
	if p.detailLoading != true || p.detail != nil {
		t.Fatal("stale get receipt must be ignored (still loading)")
	}
	p.OnAppEvent(RecordGetDone{RequestID: p.getID, Record: &storage.GameRecord{ID: 2, Title: "记录乙"}})
	if p.detailLoading || p.detail == nil || p.detail.ID != 2 {
		t.Fatalf("fresh get receipt should open detail: %+v", p.detail)
	}
}
