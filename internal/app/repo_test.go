package app

// repo 异步代理单测（T2'.2）：同步面直断；异步面以有界轮询等待回执
//（fire-and-forget 语义，07 §2 K3 对应）。

import (
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

func TestGameRepo_SyncPaths(t *testing.T) {
	s := OpenDataStore(t.TempDir())
	defer s.Close()
	repo := newGameRepo(s, func(string, any, error) {})

	// SaveSync → LoadLatest 往返（DR-008 形状）
	if err := repo.SaveSync(state.ModeHumanVsHuman, "fen-x", [][]int{{7, 7, 4, 7}}); err != nil {
		t.Fatalf("SaveSync: %v", err)
	}
	got, err := repo.LoadLatest(state.ModeHumanVsHuman)
	if err != nil || got == nil || got.Fen != "fen-x" {
		t.Fatalf("LoadLatest = (%v, %v)", got, err)
	}
	if len(got.Moves) != 1 || got.Moves[0][0] != 7 {
		t.Fatalf("moves = %v", got.Moves)
	}
}

func TestGameRepo_AutoSaveFireAndForget(t *testing.T) {
	s := OpenDataStore(t.TempDir())
	defer s.Close()
	done := make(chan ui.DbSaveDone, 1)
	emit := func(_ string, payload any, _ error) {
		if d, ok := payload.(ui.DbSaveDone); ok {
			done <- d
		}
	}
	repo := newGameRepo(s, emit)

	// 自动保存路径：SaveGame 立即返回，后台写入失败时才有回执（此处成功→无回执）
	if err := repo.SaveGame(state.ModeHumanVsHuman, "fen-y", nil); err != nil {
		t.Fatalf("SaveGame 应 fire-and-forget 返回 nil: %v", err)
	}
	// 有界轮询确认落盘（K3：不阻塞，但测试需等待 I/O 完成）
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, _ := repo.LoadLatest(state.ModeHumanVsHuman); got != nil {
			if got.Fen != "fen-y" {
				t.Fatalf("落盘 fen = %q", got.Fen)
			}
			select {
			case d := <-done:
				t.Fatalf("成功写入不应回执: %+v", d)
			default:
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("2s 内未观察到落盘")
}
