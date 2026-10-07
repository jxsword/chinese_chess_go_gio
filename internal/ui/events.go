package ui

// 对局页事件载荷（design_docs/00 §4 内部事件行；T2'.2 登记后实现）。
// 均经 internal/app 事件总线回主循环消费（铁律 #G3：后台 goroutine 禁止直写 UI/state）。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
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

// SecureSlotLoaded 凭据槽位读取回执（00 §4 `secure:slot:get`，M4'）。Config 为 nil =
// 未配置；Config.APIKey 已掩码（****+末4 位，铁律 #G7——完整 Key 不进 UI/日志）。
type SecureSlotLoaded struct {
	Slot   string
	Config *llm.LlmEndpointConfig
}

// SecureSlotSaved 凭据槽位写入回执（00 §4 `secure:slot:set`，M4'）。Stored =
// "encrypted"（keyring）/ "plainFallback"（0600 明文回退，UI 如实提示）；空串 = 失败。
type SecureSlotSaved struct {
	Slot   string
	Stored string
	Err    error
}

// LlmStreamChunk LLM 流式增量（00 §4 llm:chunk，M4'）。Delta.Content 为 nil 的
// 纯思维链增量不出现在流式消息区（DR-005：思维链恒关，此字段仅为协议完备保留）。
type LlmStreamChunk struct {
	RequestID string
	Delta     llm.Delta
}

// LlmStreamDone LLM 流式正常结束（00 §4 llm:done）：Text = 全量回复文本。
type LlmStreamDone struct {
	RequestID string
	Text      string
}

// LlmStreamError LLM 流式失败结束（00 §4 llm:error）：Message = 错误文案。
type LlmStreamError struct {
	RequestID string
	Message   string
}

// LlmMoveDone LLM 走子来源结算回执（00 §4 `llm:move:done`，Gio 新增）：Result 为
// 复制物 MoveSourceResult（含 Note/Fallback 注解）；Err 非 nil = 取消/管线异常。
type LlmMoveDone struct {
	RequestID string
	Result    engine.MoveSourceResult
	Err       error
}

// LlmTestDone 配置卡"测试连接"回执（00 §4 `llm:test:done`）。
type LlmTestDone struct {
	RequestID string
	OK        bool
	Message   string
}

// LlmLoopTick LLM vs LLM 自动对局循环的步进回执（Gio 新增，M4'）：走棋间隔
// 计时到点后排帧续跑；Gen = 循环代次（停止/暂停/新局递增），过期 tick 忽略。
type LlmLoopTick struct {
	Gen int
}

// LlmAttemptProgress LLM 走子"第 N/M 次尝试"进度（OnAttempt 回调经总线回主循环，
// #G3 正方向——后台 goroutine 禁止直写页面字段）。
type LlmAttemptProgress struct {
	RequestID string
	N         int
	Total     int
}

// LlmPersistTick LLM 配置 800ms 防抖保存到点（Seq 代次过期则忽略）。
type LlmPersistTick struct {
	Seq int
}

// PasteTextDone "从 Windows 剪贴板粘贴"回执（KG-004 可靠路径异步化，M4'）：
// Target 为页面内目标字段编号；Text 为 UTF-8 剪贴板文本。
type PasteTextDone struct {
	Target int
	Text   string
	Err    error
}

// CorpusDownloadProgress 语料下载进度（00 §4 `corpus:progress`，M5'）。
// Received/Total 为字节；Total ≤ 0 表示总长未知。
type CorpusDownloadProgress struct {
	RequestID string
	Received  int64
	Total     int64
}

// CorpusDownloadDone 语料下载结算回执（00 §4 `corpus:download`，M5'）：
// Err = nil 成功（页面重扫语料目录）；取消时回执被总线按 id 丢弃（#G5）；
// 总线 Err 字段不进页面，错误内嵌本载荷。
type CorpusDownloadDone struct {
	RequestID string
	Err       error
}

// ReplayTick 重放器自动播放步进（00 §4 `replay:tick`，Gio 新增 M5'）：
// 定时器到点后排帧推进；Gen 代次过期忽略（M6' puzzleDemo 状态机接管）。
type ReplayTick struct {
	Gen int
}

// RecordsListDone 记录列表回执（00 §4 `cc:db:records:list`，D-004 记录库页）。
// Err 为 nil 但 Records 空 = 读取失败降级为空列表（上游 catch → []）。
type RecordsListDone struct {
	Records []storage.GameRecordSummary
}

// RecordGetDone 单条记录回执（`cc:db:records:get`）。
type RecordGetDone struct {
	Record *storage.GameRecord
	Err    error
}

// RecordSaveDone "保存为棋谱"回执（`cc:db:records:save`）。
type RecordSaveDone struct {
	Err error
}

// RecordDeleteDone 删除棋谱回执（`cc:db:records:delete`）。
type RecordDeleteDone struct {
	Err error
}
