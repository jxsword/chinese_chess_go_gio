package ui

// 双人页 headless 单测（09 §4：状态级等价用例；交互/视觉走手测清单）。
// 锚点 = 07 §2 进页恢复流（锁输入→取档回执→决策应用→解锁）+ 手动保存回执 +
// 新游戏重置 + OnClose 同步保存（受全局开关约束）。

import (
	"sync"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

const pageFenDeadBlack = "4k4/3P1P3/4P4/9/9/9/9/9/9/3K5 b - - 0 1"

// ---- fake 环境 ----

type pageRepo struct {
	mu      sync.Mutex
	saved   map[state.GameMode]*storage.SavedGame
	loadErr error
}

func newPageRepo() *pageRepo { return &pageRepo{saved: map[state.GameMode]*storage.SavedGame{}} }

func (f *pageRepo) SaveGame(mode state.GameMode, fen string, moves [][]int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.saved[mode] = &storage.SavedGame{Mode: string(mode), Fen: fen, Moves: moves}
	return nil
}

func (f *pageRepo) LoadLatest(mode state.GameMode) (*storage.SavedGame, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.saved[mode], f.loadErr
}

func (f *pageRepo) DeleteForMode(mode state.GameMode) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.saved, mode)
	return nil
}

type pageSaveCall struct {
	Mode   state.GameMode
	Fen    string
	Moves  [][]int
	Manual bool
	Sync   bool
}

type pageDB struct {
	mu    sync.Mutex
	loads []state.GameMode
	saves []pageSaveCall
}

func (d *pageDB) LoadLatestAsync(_ string, mode state.GameMode) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.loads = append(d.loads, mode)
}

func (d *pageDB) SaveAsync(_ string, mode state.GameMode, fen string, moves [][]int, manual bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.saves = append(d.saves, pageSaveCall{Mode: mode, Fen: fen, Moves: moves, Manual: manual})
}

func (d *pageDB) SaveSync(mode state.GameMode, fen string, moves [][]int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.saves = append(d.saves, pageSaveCall{Mode: mode, Fen: fen, Moves: moves, Sync: true})
	return nil
}

type pageEnvFixture struct {
	env      GameEnv
	db       *pageDB
	repo     *pageRepo
	mu       sync.Mutex
	emitted  []any
	canceled []string
}

func newPageEnvFixture(repo *pageRepo, autoSave bool) *pageEnvFixture {
	f := &pageEnvFixture{db: &pageDB{}, repo: repo}
	f.env = GameEnv{
		Settings: &state.GlobalSettings{AutoSave: autoSave, Loaded: true},
		Bus:      &state.LifecycleBus{},
		Repo:     repo,
		DB:       f.db,
		Emit: func(_ string, payload any, _ error) {
			f.mu.Lock()
			f.emitted = append(f.emitted, payload)
			f.mu.Unlock()
		},
		Cancel:       func(id string) { f.mu.Lock(); f.canceled = append(f.canceled, id); f.mu.Unlock() },
		NewRequestID: func(prefix string) string { return prefix + "-1" },
	}
	return f
}

// canTapSelect 输入锁探针：未锁定时点击轮走方棋子应产生选中。
func canTapSelect(p *HumanVsHumanPage) bool {
	if p.store.VM.IsRedTurn() {
		p.store.VM.OnTap(7, 7) // 红炮
	} else {
		p.store.VM.OnTap(1, 2) // 黑炮
	}
	return p.store.State().Selected != nil
}

// spec: 进页恢复——无存档：取档请求发出；回执（无存档）后开新局并解锁（#5）。
func TestPageRestoreNoSaveUnlocks(t *testing.T) {
	f := newPageEnvFixture(newPageRepo(), true)
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	defer p.Dispose()

	if len(f.db.loads) != 1 || f.db.loads[0] != state.ModeHumanVsHuman {
		t.Fatalf("创建页应发起一次取档: %v", f.db.loads)
	}
	if canTapSelect(p) {
		t.Fatal("取档回执前输入应锁定")
	}
	p.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsHuman, Saved: nil, Err: nil})
	if !canTapSelect(p) {
		t.Fatal("回执后应解锁（无存档→开新局）")
	}
	if len(p.store.State().MoveHistory) != 0 {
		t.Fatal("应保持新局")
	}
}

// spec: 进页恢复——有存档且有效 → restored（重放重建整局）并解锁。
func TestPageRestoreRestored(t *testing.T) {
	repo := newPageRepo()
	_ = repo.SaveGame(state.ModeHumanVsHuman,
		"rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1",
		[][]int{{7, 7, 4, 7}})
	f := newPageEnvFixture(repo, true)
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	defer p.Dispose()

	saved, _ := repo.LoadLatest(state.ModeHumanVsHuman)
	p.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsHuman, Saved: saved, Err: nil})
	if len(p.store.State().MoveHistory) != 1 {
		t.Fatalf("恢复手数 = %d, 期望 1", len(p.store.State().MoveHistory))
	}
	if !canTapSelect(p) {
		t.Fatal("恢复后应解锁")
	}
}

// spec: 进页恢复——死局存档 → 删档开新局且解锁（game_restore.dart:32-37）。
func TestPageRestoreDeadGame(t *testing.T) {
	repo := newPageRepo()
	_ = repo.SaveGame(state.ModeHumanVsHuman, pageFenDeadBlack, nil)
	f := newPageEnvFixture(repo, true)
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	defer p.Dispose()

	saved, _ := repo.LoadLatest(state.ModeHumanVsHuman)
	p.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsHuman, Saved: saved, Err: nil})
	if p.store.State().Result != nil || len(p.store.State().MoveHistory) != 0 {
		t.Fatalf("死局应开新局: result=%v history=%d", p.store.State().Result, len(p.store.State().MoveHistory))
	}
	if !canTapSelect(p) {
		t.Fatal("死局清理后应解锁")
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.saved[state.ModeHumanVsHuman] != nil {
		t.Fatal("死局存档应已删除")
	}
}

// spec: 进页恢复——存储异常 → 开新局不崩溃（页面降级路径，07 §2）。
func TestPageRestoreRepoError(t *testing.T) {
	repo := newPageRepo()
	repo.mu.Lock()
	repo.loadErr = errFakeStorage
	repo.mu.Unlock()
	f := newPageEnvFixture(repo, true)
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	defer p.Dispose()

	p.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsHuman, Saved: nil, Err: errFakeStorage})
	if !canTapSelect(p) || len(p.store.State().MoveHistory) != 0 {
		t.Fatal("存储异常应降级开新局并解锁")
	}
}

// spec: 手动保存（保存棋局按钮）：SaveAsync(manual=true)，回执成功 toast；
// 全局开关关闭不影响手动保存。
func TestPageManualSave(t *testing.T) {
	f := newPageEnvFixture(newPageRepo(), false) // 开关关闭
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	defer p.Dispose()
	p.store.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7))

	p.saveGame()
	f.db.mu.Lock()
	if len(f.db.saves) != 1 || !f.db.saves[0].Manual || f.db.saves[0].Mode != state.ModeHumanVsHuman {
		t.Fatalf("手动保存应发起一次 SaveAsync(manual): %+v", f.db.saves)
	}
	if len(f.db.saves[0].Moves) != 1 {
		t.Fatal("应保存完整着法栈（DR-008）")
	}
	f.db.mu.Unlock()

	p.OnAppEvent(DbSaveDone{Mode: state.ModeHumanVsHuman, Manual: true, Err: nil})
	if p.toastText != "棋局已保存" {
		t.Fatalf("成功回执应 toast: %q", p.toastText)
	}

	// 失败回执
	p.saveGame()
	p.OnAppEvent(DbSaveDone{Mode: state.ModeHumanVsHuman, Manual: true, Err: errFakeStorage})
	if p.toastText != "保存失败：本地存储不可用" {
		t.Fatalf("失败回执应 toast: %q", p.toastText)
	}
}

// spec: 自动保存错误提示（不吞错，07 §2）——Manual=false 且 Err != nil。
func TestPageAutoSaveErrorToast(t *testing.T) {
	f := newPageEnvFixture(newPageRepo(), true)
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	defer p.Dispose()

	p.OnAppEvent(DbSaveDone{Mode: state.ModeHumanVsHuman, Manual: false, Err: errFakeStorage})
	if p.toastText != "自动保存失败：本地存储不可用" {
		t.Fatalf("自动保存失败应提示: %q", p.toastText)
	}
}

// spec: 新游戏确认后重置（棋局清空 + 计时归零 + 裁决状态重置）。
func TestPageNewGameResets(t *testing.T) {
	f := newPageEnvFixture(newPageRepo(), true)
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	defer p.Dispose()
	p.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsHuman, Saved: nil, Err: nil})
	p.store.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7))
	p.elapsedSeconds = 42

	p.confirmingNewGame = true
	p.doNewGame()
	if len(p.store.State().MoveHistory) != 0 || p.elapsedSeconds != 0 {
		t.Fatalf("新游戏应重置: history=%d elapsed=%d", len(p.store.State().MoveHistory), p.elapsedSeconds)
	}
}

// spec: OnClose 同步保存受全局开关约束（K16 best-effort；开关关闭不写）。
func TestPageOnClose(t *testing.T) {
	f := newPageEnvFixture(newPageRepo(), false)
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	p.store.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7))
	p.OnClose()
	f.db.mu.Lock()
	if len(f.db.saves) != 0 {
		t.Fatal("全局开关关闭时 OnClose 不应保存")
	}
	f.db.mu.Unlock()
	p.Dispose()

	f2 := newPageEnvFixture(newPageRepo(), true)
	p2 := NewHumanVsHumanPage(f2.env, HumanVsHumanHooks{})
	defer p2.Dispose()
	p2.store.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7))
	p2.OnClose()
	f2.db.mu.Lock()
	defer f2.db.mu.Unlock()
	if len(f2.db.saves) != 1 || !f2.db.saves[0].Sync {
		t.Fatalf("开关开启时 OnClose 应同步保存一次: %+v", f2.db.saves)
	}
}

// spec: dispose 取消在途恢复（requestId 取消，#G5/#5）。
func TestPageDisposeCancelsPendingRestore(t *testing.T) {
	f := newPageEnvFixture(newPageRepo(), true)
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	p.Dispose()
	if len(f.canceled) != 1 || f.canceled[0] != p.restoreID {
		t.Fatalf("dispose 应取消在途取档: %v", f.canceled)
	}
	p.Dispose() // 幂等
	if len(f.canceled) != 1 {
		t.Fatal("dispose 应幂等")
	}
	// 取消后回执迟到：restorePending=false → 忽略
	p.OnAppEvent(DbLoadDone{Mode: state.ModeHumanVsHuman, Saved: nil, Err: nil})
}

var errFakeStorage = &fakeStorageError{}

type fakeStorageError struct{}

func (*fakeStorageError) Error() string { return "sqlite not available" }

// T5'.3 进入对战（recordBattle 路由语义）：BattleStart 非 nil → 跳过存档恢复、
// 以起点 FEN 开局、canSave=false（续战来源不写存档桶——防错 #6）。
func TestPageBattleStartSkipsRestoreAndSetsFen(t *testing.T) {
	f := newPageEnvFixture(newPageRepo(), true)
	f.env.BattleStart = &BattleStart{Fen: "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1"}
	p := NewHumanVsHumanPage(f.env, HumanVsHumanHooks{})
	defer p.Dispose()

	if len(f.db.loads) != 0 {
		t.Fatalf("进入对战不应发起取档，实际 %d 次", len(f.db.loads))
	}
	if p.store.State().Fen == "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1" {
		t.Fatal("应以起点 FEN 开局（≠默认初始局面）")
	}
	if p.store.VM.IsFinished() {
		t.Fatal("起点 FEN 合法，不应终局")
	}
	p.autoSave.SaveManual()
	if len(f.db.saves) != 0 {
		t.Fatal("续战来源不写存档桶（canSave=false，防错 #6）")
	}
}
