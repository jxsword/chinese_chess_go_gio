package state

// 进页恢复（翻译源 = 上游 frontend/src/stores/gameRestore.ts，41 行；
// 对应 game_restore.dart，07 文档 §2）。
//
// - 有该模式存档且 FEN 有效：restore 重放到 VM，返回 Restored；
// - 重放后已分胜负：视为死局，删除该存档并开新局（game_restore.dart:32-37）；
// - 无存档 / FEN 无效 / 存储异常：开新局（human_vs_human_page.dart:59-65 降级语义）。
//
// 异步拆分（铁律 #G3）：生产侧由 internal/app 后台取档（LoadLatestAsync），
// 回执在主循环调 RestoreOrNewGame 完成决策应用；RestoreOrNewGameSync 为上游
// restoreOrNewGame 的同步直调形态（行为锚点用例直测）。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// RestoreOutcome 恢复结果（对应上游 'restored' | 'newGame'）。
type RestoreOutcome string

const (
	RestoreRestored RestoreOutcome = "restored"
	RestoreNewGame  RestoreOutcome = "newGame"
)

// RestoreOrNewGame 恢复决策（saved/loadErr 为异步取档回执；repo 用于死局删档）。
func RestoreOrNewGame(vm *GameVm, mode GameMode, saved *storage.SavedGame, loadErr error, repo GameRepo) RestoreOutcome {
	if loadErr == nil && saved != nil && rules.IsValidFen(saved.Fen) {
		vm.Restore(saved.Fen, saved.Moves)
		// restore 同步完成，可直接检查终局（原版经 microtask 后检查）
		if vm.IsFinished() {
			// 已分胜负 = 死局：删存档开新局，避免每次进入都恢复同一盘已结束的棋
			_ = repo.DeleteForMode(mode)
			vm.NewGame()
			return RestoreNewGame
		}
		return RestoreRestored
	}
	vm.NewGame()
	return RestoreNewGame
}

// RestoreOrNewGameSync 上游 restoreOrNewGame 的同步直调形态（测试锚点；
// 生产走异步取档 + RestoreOrNewGame）。
func RestoreOrNewGameSync(repo GameRepo, mode GameMode, vm *GameVm) RestoreOutcome {
	saved, err := repo.LoadLatest(mode)
	return RestoreOrNewGame(vm, mode, saved, err, repo)
}
