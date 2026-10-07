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

// RecordRepo 记录库异步面（T5'.3，00 §4 `cc:db:records*` 行；requestId 关联，
// 离页/关闭弹层 Cancel 后迟到回执按 id 丢弃——铁律 #G5）。
type RecordRepo interface {
	// RecordsListAsync 后台读记录列表，回执 ui.RecordsListDone（失败降级空列表）。
	RecordsListAsync(requestID string)
	// RecordsGetAsync 后台读单条记录，回执 ui.RecordGetDone（Err 非 nil = 失败）。
	RecordsGetAsync(requestID string, id int64)
	// RecordsSaveAsync 后台写棋谱记录，回执 ui.RecordSaveDone（成功/失败均回执）。
	RecordsSaveAsync(requestID string, record state.GameRecordData)
	// RecordsDeleteAsync 后台删除记录，回执 ui.RecordDeleteDone。
	RecordsDeleteAsync(requestID string, id int64)
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
	// Records 记录库异步面（"保存为棋谱"；nil = 测试/降级场景无记录库）。
	Records RecordRepo
	// Emit 提交事件回主循环（requestId 可空 = 直通）。
	Emit func(requestID string, payload any, err error)
	// Cancel 取消在途异步请求（迟到回执按 id 丢弃）。
	Cancel func(requestID string)
	// NewRequestID 生成页面内唯一请求 ID。
	NewRequestID func(prefix string) string
	// BattleStart "进入对战"起点（T5'.3，recordBattle 语义；nil = 正常进页）。
	// 非 nil 时页面跳过存档恢复、以该 FEN 开局，canSave=false（续战来源
	// 不写存档桶——防错 #6，上游 battleRoute 页面门控同口径）。
	BattleStart *BattleStart
}

// CloseHandler 页面可选实现：窗口关闭请求（ClosingEvent，07 §2）——
// 页面在此同步保存（best-effort）后放行退出。
type CloseHandler interface {
	OnClose()
}
