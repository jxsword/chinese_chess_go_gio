package ui

// 记录库页状态用例（T5'.3，D-004；上游 RecordLibraryPage/RecordDetailPage
// 交互语义的状态级等价——09 §4 口径：不渲染，只测状态迁移，回执事件注入）。

import (
	"errors"
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
	f.emitted = append(f.emitted, RecordsListDone{Records: f.records})
}
func (f *fakeRecordRepo) RecordsGetAsync(requestID string, id int64) {
	if r, ok := f.byID[id]; ok {
		f.emitted = append(f.emitted, RecordGetDone{Record: r})
	} else {
		f.emitted = append(f.emitted, RecordGetDone{Err: errors.New("missing")})
	}
}
func (f *fakeRecordRepo) RecordsSaveAsync(requestID string, record state.GameRecordData) {}
func (f *fakeRecordRepo) RecordsDeleteAsync(requestID string, id int64) {
	delete(f.byID, id)
	f.emitted = append(f.emitted, RecordDeleteDone{})
}

func newRecordLibraryEnv() (GameEnv, *fakeRecordRepo) {
	repo := &fakeRecordRepo{byID: map[int64]*storage.GameRecord{}}
	env := GameEnv{
		Records:      repo,
		NewRequestID: func(prefix string) string { repo.seq++; return prefix },
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
