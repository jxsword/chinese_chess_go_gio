package ui

// 对局页环境与生命周期挂接（T2'.2，design_docs/07 §2/§6.2）。
// app 装配层注入；页面不 import app（依赖方向：app → ui）。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// GameDB 对局页异步 DB 面（internal/app repo 异步代理实现；requestId 关联，
// 离页 Cancel 后迟到回执按 id 丢弃——铁律 #G5）。
type GameDB interface {
	// LoadLatestAsync 后台取模式存档，回执 ui.DbLoadDone（含错误）。
	LoadLatestAsync(requestID string, mode state.GameMode)
	// SaveAsync 后台写模式存档，回执 ui.DbSaveDone（Manual=true 成功/失败均回执）。
	SaveAsync(requestID string, mode state.GameMode, fen string, moves [][]int, manual bool)
	// SaveSync 同步写（仅窗口关闭路径：K16 有界等待 best-effort，07 §2）。
	SaveSync(mode state.GameMode, fen string, moves [][]int) error
}

// GameEnv 对局页环境（铁律 #G3：字段与回调仅在主 goroutine 触达；
// Repo/DB 内部 goroutine 只做 I/O，结果经 Emit 回主循环）。
type GameEnv struct {
	// Settings 全局设置单例（autoSave 开关）。
	Settings *state.GlobalSettings
	// Bus 生命周期总线（blur/minimize → 自动保存）。
	Bus *state.LifecycleBus
	// Repo 自动保存/恢复状态机仓储（GameAutoSave/RestoreOrNewGame 用）。
	Repo state.GameRepo
	// DB 异步 DB 面（取档/手动保存/关闭同步保存）。
	DB GameDB
	// Emit 提交事件回主循环（requestId 可空 = 直通）。
	Emit func(requestID string, payload any, err error)
	// Cancel 取消在途异步请求（迟到回执按 id 丢弃）。
	Cancel func(requestID string)
	// NewRequestID 生成页面内唯一请求 ID。
	NewRequestID func(prefix string) string
}

// CloseHandler 页面可选实现：窗口关闭请求（ClosingEvent，07 §2）——
// 页面在此同步保存（best-effort）后放行退出。
type CloseHandler interface {
	OnClose()
}
