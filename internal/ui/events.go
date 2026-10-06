package ui

// 对局页事件载荷（design_docs/00 §4 内部事件行；T2'.2 登记后实现）。
// 均经 internal/app 事件总线回主循环消费（铁律 #G3：后台 goroutine 禁止直写 UI/state）。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// DbSaveDone 存档写入回执（00 §4 `db:save`）。Manual=false 为自动保存：
// 仅错误时提示（不吞错、不阻塞）；Manual=true 为手动保存：成功/失败均回执。
type DbSaveDone struct {
	Mode   state.GameMode
	Manual bool
	Err    error
}

// DbLoadDone 进页取档回执（00 §4 `db:load`）；Saved 为 nil 表示无存档。
type DbLoadDone struct {
	Mode  state.GameMode
	Saved *storage.SavedGame
	Err   error
}

// TimerTick 对局用时 1s 计时（终局后页面忽略累计）。
type TimerTick struct{}

// ToastHide toast 自动消失（Seq 对应 showToast 时的代次，过期则忽略）。
type ToastHide struct {
	Seq int
}

// EngineMoveDone 引擎 AI 应手回执（T3'.1，00 §4 engine Worker 行——结果经
// app 事件总线回主循环，requestId 关联迟到丢弃）。Move=nil 且 Err=nil =
// 无合法走法（将死/困毙，页面按终局呈现）。
type EngineMoveDone struct {
	RequestID string
	Move      *rules.Move
	Err       error
}

// EngineReportDone 参谋报告回执（FindBestMoveEx，Top-K 真实分差）。
type EngineReportDone struct {
	RequestID string
	Report    *engine.EngineReport
	Err       error
}

// EngineEvalDone 单着法评估回执（EvaluateMove，护航否决用；Cp=nil = 无评估值）。
type EngineEvalDone struct {
	RequestID string
	Cp        *int
	Err       error
}
