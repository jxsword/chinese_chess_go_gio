package state

// T2'.2 验收：自动保存/恢复状态机全路径（07 §2 生命周期映射 + 死局清理
// game_restore.dart:32-37）。
//
// 翻译纪律（DR-G002）：上游 frontend/test/stores/autoSaveRestore.spec.ts 每个
// it 用例逐条翻译（用例名注释保留 spec 对应关系），禁止合并/删减断言。
// 上游 mock repo → Go 同步 fake；notifyLifecycle(phase) → LifecycleBus.Notify()
// （blur/minimize 相位；close 相位走页面 OnClose 同步保存，07 §2 落地口径）。

import (
	"errors"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

const (
	fenDeadBlack = "4k4/3P1P3/4P4/9/9/9/9/9/9/3K5 b - - 0 1" // 黑困毙 → 红胜（死局）
	fenInitialW  = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1"
)

// fakeRepo 同步 fake（对应上游 createMockApi().db）。
type fakeRepo struct {
	saved   map[GameMode]*storage.SavedGame
	loadErr error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{saved: map[GameMode]*storage.SavedGame{}} }

func (f *fakeRepo) SaveGame(mode GameMode, fen string, moves [][]int) error {
	f.saved[mode] = &storage.SavedGame{Mode: string(mode), Fen: fen, Moves: moves}
	return nil
}

func (f *fakeRepo) LoadLatest(mode GameMode) (*storage.SavedGame, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return f.saved[mode], nil
}

func (f *fakeRepo) DeleteForMode(mode GameMode) error {
	delete(f.saved, mode)
	return nil
}

func newTestSettings(autoSave bool) *GlobalSettings {
	return &GlobalSettings{AutoSave: autoSave, Loaded: true}
}

func makeAutoSave(repo GameRepo, store *GameStore, canSave func() bool) *GameAutoSave {
	return NewGameAutoSave(AutoSaveConfig{
		Mode:     ModeHumanVsHuman,
		VM:       store.VM,
		Repo:     repo,
		Settings: newTestSettings(true),
		CanSave:  canSave,
	})
}

// 上游 spec: "走子后 saveOnExit 写入模式桶（loadLatest 回读起始 FEN 与四元组）"
func TestSaveOnExitWritesModeBucket(t *testing.T) {
	repo := newFakeRepo()
	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	store.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7)) // 炮二平五
	autoSave := makeAutoSave(repo, store, nil)
	autoSave.SaveOnExit()

	saved := repo.saved[ModeHumanVsHuman]
	if saved == nil {
		t.Fatal("saveOnExit 未写入模式桶")
	}
	// DR-008：存档 fen 为本局起始 FEN（restore 据此重放重建整局）
	if saved.Fen != fenInitialW {
		t.Fatalf("存档 fen = %q, 期望起始 FEN", saved.Fen)
	}
	if len(saved.Moves) != 1 || saved.Moves[0][0] != 7 || saved.Moves[0][1] != 7 ||
		saved.Moves[0][2] != 4 || saved.Moves[0][3] != 7 {
		t.Fatalf("存档 moves = %v, 期望 [[7 7 4 7]]", saved.Moves)
	}
}

// 上游 spec: "全局开关关闭：saveOnExit 不写；saveManual 仍写（不受开关限制）"
func TestGlobalSwitchOffAutoNoWriteManualWrites(t *testing.T) {
	repo := newFakeRepo()
	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	store.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7))
	autoSave := NewGameAutoSave(AutoSaveConfig{
		Mode: ModeHumanVsHuman, VM: store.VM, Repo: repo, Settings: newTestSettings(false),
	})

	autoSave.SaveOnExit()
	if repo.saved[ModeHumanVsHuman] != nil {
		t.Fatal("开关关闭时 saveOnExit 不应写入")
	}

	if !autoSave.SaveManual() {
		t.Fatal("saveManual 应发起写入（不受开关限制）")
	}
	if repo.saved[ModeHumanVsHuman] == nil {
		t.Fatal("saveManual 未写入")
	}
}

// 上游 spec: "canSave=false（棋谱续战来源）：自动与手动保存均不写模式桶（防错 #6）"
func TestCanSaveFalseBlocksBothSaves(t *testing.T) {
	repo := newFakeRepo()
	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman, InitialFen: fenDeadBlack})
	autoSave := makeAutoSave(repo, store, func() bool {
		return store.Config.InitialFen == ""
	})
	store.VM.PlayMove(rules.Pos(4, 9), rules.Pos(5, 9))

	autoSave.SaveOnExit()
	if repo.saved[ModeHumanVsHuman] != nil {
		t.Fatal("canSave=false 时 saveOnExit 不应写入")
	}

	if autoSave.SaveManual() {
		t.Fatal("canSave=false 时 saveManual 应返回 false")
	}
	if repo.saved[ModeHumanVsHuman] != nil {
		t.Fatal("canSave=false 时 saveManual 不应写入")
	}
}

// 上游 spec: "生命周期事件 blur/minimize/before-quit 触发 saveOnExit；dispose 后注销"
func TestLifecycleTriggersAndDisposeDetaches(t *testing.T) {
	repo := newFakeRepo()
	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	store.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7))
	bus := &LifecycleBus{}
	autoSave := NewGameAutoSave(AutoSaveConfig{
		Mode: ModeHumanVsHuman, VM: store.VM, Repo: repo,
		Settings: newTestSettings(true), Bus: bus,
	})
	autoSave.Attach()

	bus.Notify() // blur/minimize/before-quit 相位广播
	if repo.saved[ModeHumanVsHuman] == nil {
		t.Fatal("挂载后生命周期事件应触发保存")
	}

	// dispose：先 saveOnExit 再注销；之后生命周期事件不再写入
	_ = repo.DeleteForMode(ModeHumanVsHuman)
	store.VM.PlayMove(rules.Pos(7, 0), rules.Pos(6, 2))
	autoSave.Dispose()
	if repo.saved[ModeHumanVsHuman] == nil {
		t.Fatal("dispose 自身的离开保存应写入")
	}

	_ = repo.DeleteForMode(ModeHumanVsHuman)
	bus.Notify() // 已注销，不再保存
	if repo.saved[ModeHumanVsHuman] != nil {
		t.Fatal("dispose 后生命周期事件不应再保存")
	}
}

// 上游 spec: "restoreOrNewGame：有存档且 FEN 有效 → restored（重放重建整局）"
func TestRestoreRestoredReplaysGame(t *testing.T) {
	repo := newFakeRepo()
	source := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	source.VM.PlayMove(rules.Pos(7, 7), rules.Pos(4, 7))
	source.VM.PlayMove(rules.Pos(7, 0), rules.Pos(6, 2))
	saver := makeAutoSave(repo, source, nil)
	saver.SaveManual()

	target := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	outcome := RestoreOrNewGameSync(repo, ModeHumanVsHuman, target.VM)
	if outcome != RestoreRestored {
		t.Fatalf("outcome = %q, 期望 restored", outcome)
	}
	// DR-008：以存档起始 FEN 重放重建整局——局面、历史均恢复到存档时点
	if target.State().Fen != source.State().Fen {
		t.Fatalf("恢复局面 %q ≠ 存档局面 %q", target.State().Fen, source.State().Fen)
	}
	if len(target.State().MoveHistory) != 2 {
		t.Fatalf("恢复手数 = %d, 期望 2", len(target.State().MoveHistory))
	}
	// 恢复后悔棋可用（跨恢复点回退到开局）
	target.VM.Undo()
	if len(target.State().MoveHistory) != 1 {
		t.Fatalf("悔棋后手数 = %d, 期望 1", len(target.State().MoveHistory))
	}
}

// 上游 spec: "恢复存档已分胜负（死局）→ 删存档并开新局（game_restore.dart:32-37）"
func TestRestoreDeadGameDeletesAndStartsNew(t *testing.T) {
	repo := newFakeRepo()
	// 直接落一个死局存档：黑困毙局面 + 空历史
	if err := repo.SaveGame(ModeHumanVsHuman, fenDeadBlack, nil); err != nil {
		t.Fatal(err)
	}

	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	outcome := RestoreOrNewGameSync(repo, ModeHumanVsHuman, store.VM)

	if outcome != RestoreNewGame {
		t.Fatalf("outcome = %q, 期望 newGame", outcome)
	}
	if store.State().Result != nil {
		t.Fatalf("result = %v, 期望已开新局（nil）", store.State().Result)
	}
	if len(store.State().Fen) < 9 || store.State().Fen[:9] != "rnbakabnr" {
		t.Fatalf("fen = %q, 期望初始局面", store.State().Fen)
	}
	if repo.saved[ModeHumanVsHuman] != nil {
		t.Fatal("死局存档应已删除")
	}
}

// 上游 spec: "存档 FEN 无效 → 开新局"
func TestRestoreInvalidFenStartsNew(t *testing.T) {
	repo := newFakeRepo()
	if err := repo.SaveGame(ModeHumanVsHuman, "不是 FEN", nil); err != nil {
		t.Fatal(err)
	}
	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	outcome := RestoreOrNewGameSync(repo, ModeHumanVsHuman, store.VM)
	if outcome != RestoreNewGame {
		t.Fatalf("outcome = %q, 期望 newGame", outcome)
	}
	if store.State().Fen[:9] != "rnbakabnr" {
		t.Fatalf("fen = %q, 期望初始局面", store.State().Fen)
	}
}

// 上游 spec: "存储异常 → 开新局不崩溃（页面降级路径）"
func TestRestoreRepoErrorStartsNew(t *testing.T) {
	repo := newFakeRepo()
	repo.loadErr = errors.New("sqlite not available")
	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	outcome := RestoreOrNewGameSync(repo, ModeHumanVsHuman, store.VM)
	if outcome != RestoreNewGame {
		t.Fatalf("outcome = %q, 期望 newGame", outcome)
	}
	if store.State().Fen[:9] != "rnbakabnr" {
		t.Fatalf("fen = %q, 期望初始局面", store.State().Fen)
	}
}

// 上游 spec: "无存档 → 开新局"
func TestRestoreNoSaveStartsNew(t *testing.T) {
	repo := newFakeRepo()
	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	outcome := RestoreOrNewGameSync(repo, ModeHumanVsHuman, store.VM)
	if outcome != RestoreNewGame {
		t.Fatalf("outcome = %q, 期望 newGame", outcome)
	}
	if len(store.State().MoveHistory) != 0 {
		t.Fatalf("手数 = %d, 期望 0", len(store.State().MoveHistory))
	}
}

// RestoreOrNewGame 拆分形态与同步形态等价（生产=异步取档回执 → 决策）。
func TestRestoreOrNewGameSplitForm(t *testing.T) {
	repo := newFakeRepo()
	if err := repo.SaveGame(ModeHumanVsHuman, fenInitialW, [][]int{{7, 7, 4, 7}}); err != nil {
		t.Fatal(err)
	}
	store := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	saved := repo.saved[ModeHumanVsHuman]
	outcome := RestoreOrNewGame(store.VM, ModeHumanVsHuman, saved, nil, repo)
	if outcome != RestoreRestored || len(store.State().MoveHistory) != 1 {
		t.Fatalf("拆分形态恢复不符: outcome=%q history=%d", outcome, len(store.State().MoveHistory))
	}

	// loadErr 透传 → newGame
	store2 := NewGameStore(GameStoreConfig{Mode: ModeHumanVsHuman})
	if o := RestoreOrNewGame(store2.VM, ModeHumanVsHuman, nil, errors.New("boom"), repo); o != RestoreNewGame {
		t.Fatalf("loadErr 路径 outcome = %q", o)
	}
}
