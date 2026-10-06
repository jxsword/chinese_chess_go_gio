package state

// 自动保存挂接（翻译源 = 上游 frontend/src/stores/gameAutoSave.ts，64 行；
// 对应 game_auto_save.dart，07 文档 §2）。
//
// 触发时机（Go 形态，07 §2 落地口径）：离开页面（Dispose）/失焦/最小化
// （LifecycleBus 相位，internal/app 派发）→ SaveOnExit；关闭（ClosingEvent）
// 走页面 OnClose 同步保存。全局开关关闭时不做任何自动保存，持久化只来自
// "保存棋局"按钮（SaveManual）。canSave（棋谱续战来源 = false）对自动与手动
// 保存同样生效（08 防错 #6）。
//
// DB I/O 并发口径：repo 实现决定——生产为 internal/app 异步代理（后台
// goroutine，铁律 #G3），测试注入同步 fake。

import "github.com/jxsword/chinese_chess_go_gio/internal/storage"

// GameRepo 对局存档仓储（对应上游 repo: Pick<WindowApi['db'], 'saveGame'|'loadLatest'|'deleteForMode'>；
// 方法均为同步签名，异步语义由实现承载：生产=internal/app 异步代理，测试=同步 fake）。
type GameRepo interface {
	// SaveGame 写入模式存档桶（DR-008：fen=本局起始 FEN + 完整着法栈四元组）。
	SaveGame(mode GameMode, fen string, moves [][]int) error
	// LoadLatest 读取模式最近存档；无存档返回 (nil, nil)。
	LoadLatest(mode GameMode) (*storage.SavedGame, error)
	// DeleteForMode 删除模式存档桶（死局清理）。
	DeleteForMode(mode GameMode) error
}

// AutoSaveConfig 自动保存构造配置（对应上游 GameAutoSaveOptions）。
type AutoSaveConfig struct {
	// Mode 归属模式（自动存档桶）。
	Mode GameMode
	// VM 序列化来源（当局）。
	VM *GameVm
	// Repo 存取实现。
	Repo GameRepo
	// Settings 全局设置（autoSave 开关；nil 视为开启——存储降级路径不阻断保存语义）。
	Settings *GlobalSettings
	// Bus 生命周期总线（blur/minimize 相位来源；nil = 不订阅，仅手动/离开触发）。
	Bus *LifecycleBus
	// CanSave 页面级附加条件（棋谱/残局续战来源不写模式存档桶，防错 #6）；nil = 允许。
	CanSave func() bool
}

// GameAutoSave 对局页自动保存（对应上游 GameAutoSave 类）。
type GameAutoSave struct {
	cfg         AutoSaveConfig
	unsubscribe func() // 非 nil = 已挂载（对应上游 unsubscribe 字段）
}

// NewGameAutoSave 创建自动保存挂接。
func NewGameAutoSave(cfg AutoSaveConfig) *GameAutoSave {
	return &GameAutoSave{cfg: cfg}
}

// Attach 页面挂载后调用：注册生命周期监听（blur/minimize → saveOnExit）；
// 幂等（game_auto_save.dart attach: 已挂载直接返回）。
func (a *GameAutoSave) Attach() {
	if a.unsubscribe != nil {
		return
	}
	if a.cfg.Bus != nil {
		a.unsubscribe = a.cfg.Bus.Subscribe(a.SaveOnExit)
	}
}

// SaveManual 手动保存（"保存棋局"按钮）：不受全局开关限制（game_auto_save.dart:55）。
// 返回是否发起写入（canSave=false 时为 false）。
func (a *GameAutoSave) SaveManual() bool { return a.write() }

// SaveOnExit 自动保存（离开/失焦/最小化触发）：受全局开关与页面条件限制
// （game_auto_save.dart:58-61）。
func (a *GameAutoSave) SaveOnExit() {
	if a.cfg.Settings != nil && !a.cfg.Settings.AutoSave {
		return
	}
	a.write()
}

// write 序列化并写入模式桶；返回是否发起写入（写入错误经 repo 实现/事件面提示，不吞错）。
func (a *GameAutoSave) write() bool {
	if a.cfg.CanSave != nil && !a.cfg.CanSave() {
		return false
	}
	data := a.cfg.VM.Serialize()
	return a.cfg.Repo.SaveGame(a.cfg.Mode, data.Fen, data.Moves) == nil
}

// Dispose 页面卸载：先触发离开保存，再注销生命周期监听（game_auto_save.dart:73-79 同序）。
func (a *GameAutoSave) Dispose() {
	a.SaveOnExit()
	if a.unsubscribe != nil {
		a.unsubscribe()
		a.unsubscribe = nil
	}
}
