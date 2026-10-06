package state

// 对局 store 工厂（翻译源 = 上游 frontend/src/stores/createGameStore.ts，41 行；
// 07 文档 §6.1/§6.2，铁律 #G4）：进入页面时创建，离开时随页面 dispose。
// 替代原版 Riverpod 全局单例 boardViewModelProvider（切页互相污染的结构性缺陷）。
// 免锁：state 全部归 UI 主 goroutine 所有（07 §6.2）。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// GameStoreConfig 工厂配置（对应上游 GameStoreConfig）。
type GameStoreConfig struct {
	// Mode 归属模式（自动存档桶 / 棋谱入库模式）。
	Mode GameMode
	// InitialFen 棋谱续战/残局来源 FEN（canSave 通常应为 false，08 防错 #6）。
	InitialFen string
	// PlayerSide 执方（人机/LLM 页使用；默认红）。
	PlayerSide rules.Side
	// CanSave 页面级保存门控（07 §2：棋谱/残局续战来源不写模式存档桶）。
	CanSave func() bool
}

// GameStore 对局 store（对应上游 GameStoreState = 快照 + vm + config）：
// VM 快照每次变化整体替换到 snap（订阅桥接）；读侧经 State() 取防御性拷贝。
type GameStore struct {
	snap   GameSnapshot
	VM     *GameVm
	Config GameStoreConfig
}

// NewGameStore 创建每局一实例的 store 并桥接 VM 订阅。
func NewGameStore(cfg GameStoreConfig) *GameStore {
	vm := NewGameVm(cfg.InitialFen)
	s := &GameStore{snap: vm.Current(), VM: vm, Config: cfg}
	// VM 快照每次变化整体替换（上游 store.setState({ ...vm.current })）
	vm.Subscribe(func() { s.snap = vm.Current() })
	return s
}

// State 当前快照（对应 store.getState() 的快照部分；防御性拷贝）。
func (s *GameStore) State() GameSnapshot { return copySnapshot(s.snap) }
