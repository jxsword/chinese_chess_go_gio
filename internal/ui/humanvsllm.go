package ui

// 人机对战（大模型）页（T4'.3；翻译源 = 上游 features/board/HumanVsLlmPage.tsx，
// design_docs/08 §5、00 §4、05 附录）：
//   - 玩家执红，黑方对手（大模型 HybridLlmPlayer / 内置 AI 二选一，DR-014）；
//   - LLM 配置卡（黑方槽位）+ 对局设置区（800ms 防抖保存，卸载兜底回写）；
//   - 流式消息区（chunk 追加渲染，K21 页面侧 requestId 二次收口）；
//   - 立即保存粘性置底（08 §5：侧板 = 滚动区 + 固定底部按钮行）；
//   - 空侧跟随（DR-014 跨页镜像：黑槽全空时视图用红方配置——运行时经红槽
//     authSlot 注入，不回写黑槽；偏离注记见 PROGRESS）；
//   - 调度对齐 _triggerLlmMove：requestId 作废在途应手 + Cancel 三收口；
//     失败（resign 策略）显式判负防软死锁（防错 #7）；
//   - 配置未加载完成不触发/不回写（防错 #4）。
// 每局一实例（app Router 工厂，铁律 #G4）；离开 Dispose 取消在途请求。

import (
	"errors"
	"fmt"
	"image"
	"strings"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/engine"
	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// HumanVsLlmHooks 页面回调。
type HumanVsLlmHooks struct {
	OnBack func()
}

// 凭据槽位（复制物 storage 常量原样沿用）。
const (
	humanVsLlmBlackSlot = storage.SlotBlack
	humanVsLlmRedSlot   = storage.SlotRed
)

// llmPersistDebounce 配置防抖保存间隔（上游 scheduleAutosave 800ms）。
const llmPersistDebounce = 800 * time.Millisecond

// HumanVsLlmPage 人机 LLM 页 state struct（铁律 #G3：主 goroutine 独占）。
type HumanVsLlmPage struct {
	hooks    HumanVsLlmHooks
	env      GameEnv
	llmStore LlmStore // nil = 存储降级（配置不可读写）
	llm      LlmRunner

	store    *state.GameStore
	board    *BoardView
	judge    *RepetitionJudge
	ai       *EngineClient // 内置 AI 应手 + 参谋共用 Runner
	advisor  *EngineRunnerAdapter
	autoSave *state.GameAutoSave
	canSave  func() bool

	// 配置（黑槽原始视图 + 生效视图；掩码 Key；DR-014 镜像时=红方配置+红槽注入）
	blackConfig  llm.LlmEndpointConfig
	config       llm.LlmEndpointConfig
	redConfig    llm.LlmEndpointConfig
	blackLoaded  bool
	redLoaded    bool
	mirrored     bool
	configCard   *LlmConfigCard
	gameSettings state.LlmGameSettings

	// LLM 走子在途状态（requestId 形态，同 humanvsai）
	llmRequestID string
	llmThinking  bool
	llmStartedAt time.Time
	attemptN     int
	attemptTotal int
	llmNote      string

	// 进页恢复
	restoreID      string
	restorePending bool

	// 防抖保存（seq 代次）
	persistSeq   int
	saveInFlight int

	opponentType llm.SideEngineType

	msgArea  *streamMessageArea
	disposed bool

	// 控件
	choiceTimeout  *choiceRow
	choiceAttempts *choiceRow
	choiceFallback *choiceRow
	choiceAdvisor  *choiceRow
	choiceBlend    *choiceRow
	choiceAdvDiff  *choiceRow
	sideLlmBtn     widget.Clickable
	sideBuiltinBtn widget.Clickable
	saveNowBtn     widget.Clickable
	newGameBtn     widget.Clickable
	undoBtn        widget.Clickable
	backBtn        widget.Clickable

	confirmingNewGame bool
	toastText         string
	toastSeq          int
	timerStop         chan struct{}
	newGameDialog     *ModalDialog
	drawDialog        *ModalDialog
}

// NewHumanVsLlmPage 创建（工厂：每次导航进入调用一次；llmClient 为页内 LLM 客户端）。
func NewHumanVsLlmPage(env LlmEnv, hooks HumanVsLlmHooks) *HumanVsLlmPage {
	var llmClient *LlmClient
	var resolve func(string) string
	if env.Store != nil {
		resolve = env.Store.ResolveAPIKey
	}
	llmClient = NewLlmClient(env.GameEnv, resolve, func() int {
		if env.Settings == nil {
			return 0 // transport 内部回落默认 60s 并 clamp
		}
		return llm.ResolveTimeoutSeconds(env.Settings.Get(llm.SettingKeyTimeoutSeconds))
	})
	return newHumanVsLlmPage(env, hooks, llmClient, nil)
}

// newHumanVsLlmPage 内部构造（llmRunner 非 nil 时注入 fake——测试用，00 §4）。
func newHumanVsLlmPage(env LlmEnv, hooks HumanVsLlmHooks, llmRunner LlmRunner, runner EngineSubmitter) *HumanVsLlmPage {
	gs := state.LoadLlmSettings(env.Settings)
	p := &HumanVsLlmPage{
		hooks:        hooks,
		env:          env.GameEnv,
		llmStore:     env.Store,
		llm:          llmRunner,
		store:        state.NewGameStore(state.GameStoreConfig{Mode: state.ModeHumanVsLlm, PlayerSide: rules.Red}),
		advisor:      nil,
		opponentType: gs.HumanVsLlmOpponentType,
		gameSettings: gs,
		msgArea:      newStreamMessageArea(),
		restoreID:    env.NewRequestID("restore"),
	}
	p.board = NewBoardView(p.store)
	p.ai = NewEngineClient(env.GameEnv, runner)
	p.advisor = NewEngineRunnerAdapter(p.ai.runner)
	p.configCard = NewLlmConfigCard("黑方模型（对手）", humanVsLlmBlackSlot, llm.LlmPresets, p.config, func(c llm.LlmEndpointConfig) {
		p.config = c
		p.schedulePersist()
	})
	p.configCard.OnTestConnection = p.onTestConnection
	p.configCard.OnPaste = p.onPasteRequest

	p.canSave = func() bool { return true }
	p.board.Blocked = func() bool { return p.modalOpen() }
	p.board.OnMoved = p.onPlayerMoved

	p.judge = NewRepetitionJudge(p.store.VM, func(side rules.Side) bool { return side == rules.Red }, p.showToast)
	p.store.VM.OnHistoryGrow = p.judge.OnHistoryGrow
	p.store.VM.OnHistoryRewound = p.judge.OnHistoryRewound

	p.autoSave = state.NewGameAutoSave(state.AutoSaveConfig{
		Mode:     state.ModeHumanVsLlm,
		VM:       p.store.VM,
		Repo:     env.Repo,
		Settings: env.GameEnv.Settings,
		Bus:      env.Bus,
		CanSave:  p.canSave,
	})
	p.autoSave.Attach()

	p.store.VM.LockInput()
	p.restorePending = true
	if env.DB != nil {
		env.DB.LoadLatestAsync(p.restoreID, state.ModeHumanVsLlm)
	} else {
		p.restorePending = false
		p.store.VM.UnlockInput()
	}

	// 配置槽位异步加载（未加载完成不触发/不回写，防错 #4）
	if env.Store != nil {
		env.Store.LoadSlotAsync(env.NewRequestID("cfg-black"), humanVsLlmBlackSlot)
		env.Store.LoadSlotAsync(env.NewRequestID("cfg-red"), humanVsLlmRedSlot)
	} else {
		p.blackLoaded = true
		p.redLoaded = true
	}
	p.initChoices()
	p.newGameDialog = NewModalDialog("开始新游戏", "当前棋局将被清空，确定要开始新游戏吗？")
	p.drawDialog = NewModalDialog("三次重复局面",
		"双方连续走出相同局面，按规则可判和。可接受和棋，或变着继续对局（再次重复将强制判和）。")
	p.drawDialog.ConfirmLabel = "接受和棋"
	p.drawDialog.CancelLabel = "变着继续"
	p.startTimer()
	return p
}

// initChoices 对局设置选择行（上游 <select> 的 chips 呈现）。
func (p *HumanVsLlmPage) initChoices() {
	p.choiceTimeout = newChoiceRow([]int{30, 60, 120, 180, 300}, []string{"30秒", "60秒", "120秒", "180秒", "300秒"})
	p.choiceAttempts = newChoiceRow([]int{1, 3, 5}, []string{"1次", "3次", "5次"})
	p.choiceFallback = newChoiceRow([]int{0, 1}, []string{"内置AI代走", "该方判负"})
	p.choiceAdvisor = newChoiceRow([]int{0, 1, 2}, []string{"关闭", "候选", "护航"})
	p.choiceBlend = newChoiceRow([]int{0, 25, 50, 75, 100}, []string{"0", "25", "50", "75", "100"})
	p.choiceAdvDiff = newChoiceRow([]int{1, 3, 5}, []string{"快(2层)", "中(4层)", "强(6层)"})
}

// showToast 非阻塞提示（2.2s；同各页）。
func (p *HumanVsLlmPage) showToast(message string) {
	p.toastSeq++
	p.toastText = message
	seq := p.toastSeq
	go func() {
		time.Sleep(2200 * time.Millisecond)
		p.env.Emit("", ToastHide{Seq: seq}, nil)
	}()
}

func (p *HumanVsLlmPage) startTimer() {
	stop := make(chan struct{})
	p.timerStop = stop
	go func() {
		tk := time.NewTicker(time.Second)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				p.env.Emit("", TimerTick{}, nil)
			}
		}
	}()
}

func (p *HumanVsLlmPage) stopTimer() {
	if p.timerStop != nil {
		close(p.timerStop)
		p.timerStop = nil
	}
}

// configReady 黑红槽位均加载完成。
func (p *HumanVsLlmPage) configReady() bool { return p.blackLoaded && p.redLoaded }

// OnAppEvent 事件总线消费（主 goroutine）。
func (p *HumanVsLlmPage) OnAppEvent(payload any) {
	switch ev := payload.(type) {
	case DbLoadDone:
		if ev.Mode != state.ModeHumanVsLlm || !p.restorePending {
			return
		}
		p.restorePending = false
		outcome := state.RestoreOrNewGame(p.store.VM, ev.Mode, ev.Saved, ev.Err, p.env.Repo)
		if outcome == state.RestoreRestored {
			p.store.VM.UnlockInput()
		}
		p.maybeTriggerLlm()
	case DbSaveDone:
		if ev.Mode != state.ModeHumanVsLlm {
			return
		}
		if ev.Err != nil {
			p.showToast("自动保存失败：本地存储不可用")
		}
	case SecureSlotLoaded:
		p.onSlotLoaded(ev)
	case SecureSlotSaved:
		p.onSlotSaved(ev)
	case EngineMoveDone:
		p.onBuiltinMoveDone(ev)
	case LlmMoveDone:
		p.onLlmMoveDone(ev)
	case LlmAttemptProgress:
		if ev.RequestID == p.llmRequestID {
			p.attemptN = ev.N
			p.attemptTotal = ev.Total
		}
	case LlmStreamChunk:
		if ev.RequestID == p.llmRequestID { // K21 防线：页面侧 requestId 二次收口
			p.msgArea.appendChunk(ev.Delta)
		}
	case LlmStreamDone:
		if ev.RequestID == p.llmRequestID {
			p.msgArea.endStream()
		}
	case LlmStreamError:
		if ev.RequestID == p.llmRequestID {
			p.msgArea.endStream()
			p.msgArea.appendLine("〔请求失败〕" + ev.Message)
		}
	case LlmTestDone:
		p.configCard.SetTestResult(ev.OK, ev.Message)
	case PasteTextDone:
		p.configCard.ApplyPaste(ev.Target, ev.Text, ev.Err)
	case LlmPersistTick:
		if ev.Seq == p.persistSeq && !p.disposed {
			p.persistNow()
		}
	case TimerTick:
		if p.disposed {
			return
		}
	case ToastHide:
		if ev.Seq == p.toastSeq {
			p.toastText = ""
		}
	}
}

// onSlotLoaded 凭据槽位加载回执（掩码 Key；DR-014 镜像判定）。
func (p *HumanVsLlmPage) onSlotLoaded(ev SecureSlotLoaded) {
	cfg := llm.LlmEndpointConfig{}
	if ev.Config != nil {
		cfg = *ev.Config
	}
	switch ev.Slot {
	case humanVsLlmBlackSlot:
		p.blackLoaded = true
		p.blackConfig = cfg
	case humanVsLlmRedSlot:
		p.redLoaded = true
		p.redConfig = cfg
	}
	if !p.configReady() {
		return
	}
	p.applySlotConfig()
	p.maybeTriggerLlm() // 配置就绪：黑先残局/恢复轮黑时模型先行
}

// applySlotConfig 黑槽配置落位（黑红两槽齐后调用）：黑槽全空且红槽有配置 →
// 镜像视图（DR-014；修改请在红方槽位/大模型对战页进行——本页不回写黑槽）。
func (p *HumanVsLlmPage) applySlotConfig() {
	if llm.IsEmptyLlmConfig(p.blackConfig) && !llm.IsEmptyLlmConfig(p.redConfig) {
		p.mirrored = true
		p.config = p.redConfig
	} else {
		p.mirrored = false
		p.config = p.blackConfig
	}
	p.configCard.SetConfig(p.config)
}

// onSlotSaved 槽位写入回执（立即保存 toast；上游 res.stored 分支同文案）。
func (p *HumanVsLlmPage) onSlotSaved(ev SecureSlotSaved) {
	if ev.Slot != humanVsLlmBlackSlot || p.saveInFlight == 0 {
		return
	}
	p.saveInFlight--
	if ev.Err != nil {
		p.showToast("保存失败：本地存储不可用")
		return
	}
	if ev.Stored == "plainFallback" {
		p.showToast("模型配置已保存（系统安全存储不可用，已明文保存到本地）")
		return
	}
	p.showToast("模型配置已保存")
}

// maybeTriggerLlm 恢复完成且配置就绪后触发黑方应手（两路异步汇合点，
// 上游 maybeTriggerRef）。
func (p *HumanVsLlmPage) maybeTriggerLlm() {
	if p.llmThinking || p.disposed {
		return
	}
	if p.store.State().Result != nil || p.store.VM.IsRedTurn() {
		return
	}
	if !p.configReady() {
		return
	}
	p.triggerLlmMove()
}

// triggerLlmMove 触发黑方应手（上游 _triggerLlmMove）：lockInput → goroutine 直调
// → 回执无条件解锁（P0-1）；迟到回执由 requestId 丢弃（#4/#G5）。
func (p *HumanVsLlmPage) triggerLlmMove() {
	if p.store.VM.IsFinished() {
		return
	}
	p.abandonLlm()
	id := p.env.NewRequestID("llm")
	p.llmRequestID = id
	p.llmThinking = true
	p.llmStartedAt = time.Now()
	p.attemptN = 0
	p.attemptTotal = p.gameSettings.MaxAttempts
	p.llmNote = ""
	p.store.VM.LockInput()
	snap := p.store.State()

	if p.opponentType == llm.SideEngineBuiltin {
		// DR-014：内置 AI 直接应手（非失败兜底），L2 历史回避入参（DR-018）。
		p.ai.FindBestMoveAsync(id, snap.Fen, 3, snap.FenHistory)
		return
	}
	if p.llm == nil {
		p.llmThinking = false
		p.llmRequestID = ""
		p.store.VM.UnlockInput()
		p.showToast("LLM 通道不可用")
		return
	}
	cfg, slot := p.config, humanVsLlmBlackSlot
	if p.mirrored {
		// DR-014 镜像：运行时用红方配置（authSlot 随来源槽——DR-010 闭环；
		// 偏离注记：上游此路径恒传黑槽，掩码 Key 下注入会落空，本仓按
		// testOverride 同口径取红槽）。
		cfg, slot = p.redConfig, humanVsLlmRedSlot
	}
	p.msgArea.begin("黑方")
	p.llm.MoveAsync(id, LlmMoveSpec{
		Endpoint:          cfg,
		AuthSlot:          slot,
		Board:             p.store.VM.Board().Copy(),
		History:           append([]rules.Move(nil), snap.MoveHistory...),
		Advisor:           p.advisor,
		AdvisorMode:       p.gameSettings.AdvisorMode,
		StrengthBlend:     p.gameSettings.StrengthBlend,
		AdvisorDifficulty: p.gameSettings.AdvisorDifficulty,
		MaxAttempts:       p.gameSettings.MaxAttempts,
		Fallback:          p.gameSettings.Fallback,
		OnAttempt:         p.onAttempt,
	})
}

// onAttempt 尝试进度回调（后台 goroutine → 事件回主循环，#G3 正方向）。
func (p *HumanVsLlmPage) onAttempt(n, total int) {
	p.env.Emit(p.llmRequestID, LlmAttemptProgress{RequestID: p.llmRequestID, N: n, Total: total}, nil)
}

// abandonLlm 仅作废在途应手（不解锁——undo 路径锁由重触发接管，上游 seq++ 语义）。
func (p *HumanVsLlmPage) abandonLlm() {
	if p.llmRequestID == "" {
		return
	}
	if p.llm != nil {
		p.llm.Cancel(p.llmRequestID)
	}
	p.ai.Cancel(p.llmRequestID) // 内置 AI 路径在途取消
	p.msgArea.endStream()
	p.llmRequestID = ""
	p.llmThinking = false
}

// cancelLlm 取消在途应手并解锁（新游戏/离页路径；#5）。
func (p *HumanVsLlmPage) cancelLlm() {
	p.abandonLlm()
	p.store.VM.UnlockInput()
}

// onLlmMoveDone LLM 应手回执（主 goroutine）：无论作废与否一律解锁（P0-1）；
// requestId 不匹配 = 迟到/非当前请求 → 丢弃（K21 二次收口，总线已先行）。
func (p *HumanVsLlmPage) onLlmMoveDone(ev LlmMoveDone) {
	if ev.RequestID != p.llmRequestID {
		return
	}
	p.llmRequestID = ""
	p.llmThinking = false
	p.store.VM.UnlockInput()
	if ev.Err != nil {
		if errors.Is(ev.Err, llm.ErrCanceled) {
			return // 取消（新局/悔棋/离页）：无需提示
		}
		p.llmNote = "黑方走子来源异常：" + ev.Err.Error()
		p.msgArea.appendLine("〔异常〕" + p.llmNote)
		return
	}
	p.handleMoveResult(ev.Result)
}

// onBuiltinMoveDone 内置 AI 应手回执（DR-014 独立选项路径；同 humanvsai 收口，
// 含 DR-G004 最小思考呈现 300ms——回执早到时延迟余量后按原 requestId 二次投递）。
func (p *HumanVsLlmPage) onBuiltinMoveDone(ev EngineMoveDone) {
	if ev.RequestID != p.llmRequestID {
		return
	}
	if remaining := minThinkDuration - time.Since(p.llmStartedAt); remaining > 0 {
		ev := ev
		time.AfterFunc(remaining, func() { p.env.Emit(ev.RequestID, ev, ev.Err) })
		return // 思考态与输入锁保持，待二次投递
	}
	p.llmRequestID = ""
	p.llmThinking = false
	p.store.VM.UnlockInput()
	if ev.Err != nil {
		p.showToast("引擎计算失败：" + ev.Err.Error())
		p.store.VM.Resign(rules.Black)
		return
	}
	if ev.Move == nil {
		return // noLegalMove：胜负由棋盘状态呈现
	}
	if !p.store.VM.PlayMove(ev.Move.From, ev.Move.To) {
		p.llmNote = "黑方着法未通过校验，被拒绝"
		return
	}
	p.board.AnimateMoveVisual(*ev.Move)
}

// handleMoveResult 走子结果收口（上游 result.status switch 翻译）。
func (p *HumanVsLlmPage) handleMoveResult(result engine.MoveSourceResult) {
	switch result.Status {
	case engine.StatusOK:
		if result.Move == nil {
			return
		}
		applied := p.store.VM.PlayMove(result.Move.From, result.Move.To)
		if result.FromFallback {
			p.llmNote = noteOr(result.Note, "已由内置 AI 兜底走子")
			p.msgArea.appendLine("〔兜底〕" + p.llmNote)
		} else if !applied {
			p.llmNote = "黑方着法未通过校验，被拒绝"
			p.msgArea.appendLine("〔拒绝〕" + p.llmNote)
		} else if result.Note != "" {
			p.llmNote = result.Note
			p.msgArea.appendLine("〔参谋〕" + result.Note)
		} else {
			p.llmNote = "黑方走子完成"
		}
		if applied {
			p.board.AnimateMoveVisual(*result.Move)
		}
	case engine.StatusNoLegalMove:
		p.llmNote = "" // 已分出胜负，结果由棋盘状态呈现
	case engine.StatusFailed:
		// resign 策略：显式写入胜负防软死锁（防错 #7）。
		p.store.VM.Resign(rules.Black)
		p.llmNote = fmt.Sprintf("黑方走子失败：%s，判红方胜", noteOr(result.Note, "未知原因"))
		p.msgArea.appendLine("〔判负〕" + p.llmNote)
	}
}

func noteOr(note, dflt string) string {
	if note == "" {
		return dflt
	}
	return note
}

// onPlayerMoved 玩家走子完成（仅历史增长触发，防错 #3）。
func (p *HumanVsLlmPage) onPlayerMoved() {
	if p.store.State().Result != nil {
		return
	}
	if p.store.VM.IsRedTurn() {
		return // 仍轮玩家（防御）
	}
	p.triggerLlmMove()
}

// ---- 交互 ----

func (p *HumanVsLlmPage) modalOpen() bool {
	return p.confirmingNewGame || (p.judge != nil && p.judge.DrawOffer != nil)
}

func (p *HumanVsLlmPage) handleEvents(gtx layout.Context) {
	if p.modalOpen() {
		// 弹窗遮罩：消费全部点击边沿但不生效（KG-008——含 chips/设置行）
		p.backBtn.Clicked(gtx)
		p.newGameBtn.Clicked(gtx)
		p.undoBtn.Clicked(gtx)
		p.saveNowBtn.Clicked(gtx)
		p.sideLlmBtn.Clicked(gtx)
		p.sideBuiltinBtn.Clicked(gtx)
		p.consumeChoices(gtx, true)
		return
	}
	if p.backBtn.Clicked(gtx) && p.hooks.OnBack != nil {
		p.hooks.OnBack()
	}
	if p.newGameBtn.Clicked(gtx) {
		p.confirmingNewGame = true
	}
	if p.undoBtn.Clicked(gtx) {
		p.undoMove()
	}
	if p.saveNowBtn.Clicked(gtx) {
		p.saveNow()
	}
	busy := p.llmThinking
	if p.sideLlmBtn.Clicked(gtx) && !busy {
		p.opponentType = llm.SideEngineLLM
	}
	if p.sideBuiltinBtn.Clicked(gtx) && !busy {
		p.opponentType = llm.SideEngineBuiltin
	}
	p.consumeChoices(gtx, busy)
}

// consumeChoices 对局设置 chips（变更即更新 + 防抖落盘；busy=思考中/弹窗遮罩下
// 全消费不生效——KG-008 收口口径）。
func (p *HumanVsLlmPage) consumeChoices(gtx layout.Context, busy bool) {
	apply := func(cur, next int) int {
		if busy || next == cur {
			return cur
		}
		p.schedulePersist()
		return next
	}
	p.gameSettings.TimeoutSeconds = apply(p.gameSettings.TimeoutSeconds, p.choiceTimeout.consume(gtx, p.gameSettings.TimeoutSeconds))
	p.gameSettings.MaxAttempts = apply(p.gameSettings.MaxAttempts, p.choiceAttempts.consume(gtx, p.gameSettings.MaxAttempts))
	p.gameSettings.Fallback = fallbackValues[apply(fallbackIndexOf(p.gameSettings.Fallback), p.choiceFallback.consume(gtx, fallbackIndexOf(p.gameSettings.Fallback)))]
	p.gameSettings.AdvisorMode = advisorValues[apply(advisorIndexOf(p.gameSettings.AdvisorMode), p.choiceAdvisor.consume(gtx, advisorIndexOf(p.gameSettings.AdvisorMode)))]
	if p.gameSettings.AdvisorMode != llm.AdvisorOff {
		p.gameSettings.StrengthBlend = apply(p.gameSettings.StrengthBlend, p.choiceBlend.consume(gtx, p.gameSettings.StrengthBlend))
		p.gameSettings.AdvisorDifficulty = apply(p.gameSettings.AdvisorDifficulty, p.choiceAdvDiff.consume(gtx, p.gameSettings.AdvisorDifficulty))
	}
}

// undoMove 悔一整轮（思考中=作废在途应手"中止重想"，同 humanvsai 语义）。
func (p *HumanVsLlmPage) undoMove() {
	p.abandonLlm()
	p.cancelPendingRestore()
	p.board.CancelAnim()
	p.store.VM.UndoRound(rules.Red)
	if p.store.State().Result == nil && !p.store.VM.IsRedTurn() && p.configReady() {
		p.triggerLlmMove()
		return
	}
	if p.llmRequestID == "" {
		p.store.VM.UnlockInput() // 防残锁（#5）
	}
}

// doNewGame 新游戏确认后重置。
func (p *HumanVsLlmPage) doNewGame() {
	p.cancelLlm()
	p.cancelPendingRestore()
	p.board.CancelAnim()
	p.store.VM.NewGame()
	p.llmNote = ""
	p.msgArea.clear()
	p.maybeTriggerLlm()
}

// cancelPendingRestore 取消在途恢复并解锁。
func (p *HumanVsLlmPage) cancelPendingRestore() {
	if !p.restorePending {
		return
	}
	p.env.Cancel(p.restoreID)
	p.restorePending = false
	p.store.VM.UnlockInput()
}

// onTestConnection 测试连接请求（DR-014 镜像：测的是真正会用于对局的配置）。
func (p *HumanVsLlmPage) onTestConnection(cfg llm.LlmEndpointConfig, slot string) {
	if p.llm == nil {
		p.configCard.SetTestResult(false, "连接失败：测试通道不可用")
		return
	}
	authSlot := slot
	if p.mirrored {
		cfg, authSlot = p.redConfig, humanVsLlmRedSlot
	}
	p.llm.TestConnectionAsync(p.env.NewRequestID("test"), cfg, authSlot)
}

// onPasteRequest KG-004 可靠粘贴（异步化；目标字段定向回填）。
func (p *HumanVsLlmPage) onPasteRequest(target int) {
	pasteFromWindowsAsync(func(text string, err error) {
		p.env.Emit("", PasteTextDone{Target: target, Text: text, Err: err}, nil)
	})
}

// schedulePersist 800ms 防抖保存（上游 scheduleAutosave；主 goroutine 计时）。
func (p *HumanVsLlmPage) schedulePersist() {
	if p.llmStore == nil {
		return
	}
	p.persistSeq++
	seq := p.persistSeq
	time.AfterFunc(llmPersistDebounce, func() {
		p.env.Emit("", LlmPersistTick{Seq: seq}, nil)
	})
}

// pageSettings 本页设置字段子集（防清空共享字段，上游 persist copyWith——
// 不含对手引擎类型：intervalSeconds/红黑强度归 llmVsLlm 页，防错 #4）。
func (p *HumanVsLlmPage) pageSettings() state.LlmGameSettings {
	return state.LlmGameSettings{
		TimeoutSeconds:    p.gameSettings.TimeoutSeconds,
		MaxAttempts:       p.gameSettings.MaxAttempts,
		Fallback:          p.gameSettings.Fallback,
		AdvisorMode:       p.gameSettings.AdvisorMode,
		StrengthBlend:     p.gameSettings.StrengthBlend,
		AdvisorDifficulty: p.gameSettings.AdvisorDifficulty,
	}
}

var pageSettingFields = []state.LlmSettingsField{
	state.LlmFieldTimeoutSeconds, state.LlmFieldMaxAttempts, state.LlmFieldFallback,
	state.LlmFieldAdvisorMode, state.LlmFieldStrengthBlend, state.LlmFieldAdvisorDifficulty,
}

// fullSettings 立即保存用整对象（含对手引擎类型；上游 {...lastLoaded, ...st}）。
func (p *HumanVsLlmPage) fullSettings() state.LlmGameSettings {
	st := p.gameSettings
	return state.LlmGameSettings{
		TimeoutSeconds:         st.TimeoutSeconds,
		MaxAttempts:            st.MaxAttempts,
		Fallback:               st.Fallback,
		AdvisorMode:            st.AdvisorMode,
		StrengthBlend:          st.StrengthBlend,
		AdvisorDifficulty:      st.AdvisorDifficulty,
		HumanVsLlmOpponentType: p.opponentType,
	}
}

var fullSettingFields = append(append([]state.LlmSettingsField(nil), pageSettingFields...), state.LlmFieldHumanVsLlmOpponentType)

// persistNow 配置 + 本页设置字段落盘（防抖与卸载兜底共用；镜像状态不回写黑槽——DR-014）。
func (p *HumanVsLlmPage) persistNow() {
	if p.llmStore == nil || !p.configReady() {
		return
	}
	if !p.mirrored {
		p.llmStore.SaveSlotAsync(p.env.NewRequestID("save-cfg"), humanVsLlmBlackSlot, p.config)
	}
	p.llmStore.SaveSettings(p.pageSettings(), pageSettingFields...)
}

// saveNow 立即保存（粘性置底按钮；上游 llm-save-now 翻译）。
func (p *HumanVsLlmPage) saveNow() {
	if !p.configReady() {
		p.showToast("配置加载中，请稍候")
		return
	}
	if p.mirrored {
		p.showToast("当前为红方配置的镜像视图，请在红方一侧修改配置")
		return
	}
	p.saveInFlight++
	p.llmStore.SaveSlotAsync(p.env.NewRequestID("save-cfg"), humanVsLlmBlackSlot, p.config)
	p.llmStore.SaveSettings(p.fullSettings(), fullSettingFields...)
}

// Dispose 页面卸载：取消在途 → 卸载兜底回写（配置未加载不回写，防错 #4）→
// 停计时 → 解锁（#5）。
func (p *HumanVsLlmPage) Dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	p.board.CancelAnim()
	p.cancelLlm()
	p.cancelPendingRestore()
	p.persistNow()
	p.autoSave.Dispose()
	p.stopTimer()
}

// OnClose 窗口关闭（07 §2）：同步保存 best-effort。
func (p *HumanVsLlmPage) OnClose() {
	if p.env.Settings == nil || !p.env.Settings.AutoSave || !p.canSave() {
		return
	}
	data := p.store.VM.Serialize()
	if p.env.DB != nil {
		_ = p.env.DB.SaveSync(state.ModeHumanVsLlm, data.Fen, data.Moves)
	}
}

// ---- 布局 ----

func (p *HumanVsLlmPage) Layout(gtx layout.Context) layout.Dimensions {
	p.handleEvents(gtx)
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	fillRect(gtx.Ops, image.Rectangle{Max: gtx.Constraints.Max}, ThemeSurface)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(p.layoutContent),
		layout.Stacked(p.layoutOverlays),
	)
}

func (p *HumanVsLlmPage) layoutContent(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutHeader),
		layout.Rigid(p.layoutStatusBar),
		layout.Flexed(1, p.layoutBody),
	)
}

func (p *HumanVsLlmPage) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(p.simpleButton(&p.backBtn, "返回", false)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					t := material.H6(PageTheme, "人机对战（大模型）")
					t.Color = ThemeOnSurface
					return t.Layout(gtx)
				})
			}),
			layout.Rigid(p.simpleButton(&p.newGameBtn, "新游戏", true)),
			layout.Rigid(p.simpleButton(&p.undoBtn, "悔棋", false)),
		)
	})
}

func (p *HumanVsLlmPage) simpleButton(c *widget.Clickable, label string, primary bool) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(PageTheme, c, label)
		if !primary {
			btn.Background = ThemeSurfaceDim
			btn.Color = ThemeSeedDark
		}
		return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(96))
			return btn.Layout(gtx)
		})
	}
}

// layoutStatusBar 状态条（上游 _statusOf 六态翻译）。
func (p *HumanVsLlmPage) layoutStatusBar(gtx layout.Context) layout.Dimensions {
	snap := p.store.State()
	modelLabel := "大模型"
	if p.opponentType == llm.SideEngineBuiltin {
		modelLabel = "内置 AI"
	} else if m := strings.TrimSpace(p.config.Model); m != "" {
		modelLabel = m
	}
	var statusText string
	bg := ThemeSurfaceDim
	fg := ThemeOnSurface
	switch {
	case snap.Result != nil:
		statusText = "对局结束：" + resultText(*snap.Result)
	case p.llmThinking:
		statusText = fmt.Sprintf("黑方 %s 正在思考…%s", modelLabel, thinkingSuffix(p.llmStartedAt, p.attemptN, p.attemptTotal))
		bg = ThemeSeed
		fg = ThemeSurface
	case p.llmNote != "":
		statusText = p.llmNote
	case snap.IsCheck && snap.IsRedTurn:
		statusText = "轮到你走棋（红方被将军！）"
		bg = ThemeCheckWarn
		fg = ThemeSurface
	case snap.IsCheck:
		statusText = "黑方被将军！" // 轮黑未进入思考态的瞬态
		bg = ThemeCheckWarn
		fg = ThemeSurface
	default:
		statusText = "轮到你走棋（红方）"
	}
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(unit.Dp(30))}
		defer clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(unit.Dp(6))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, bg)
		gtx.Constraints = layout.Exact(size)
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(PageTheme, statusText)
			l.Color = fg
			return l.Layout(gtx)
		})
	})
}

// layoutBody 棋盘区 + 侧板（滚动区 + 立即保存粘性置底，08 §5）。
func (p *HumanVsLlmPage) layoutBody(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, p.board.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			w := gtx.Dp(unit.Dp(380)) // 含内边距总宽（M3' 溢出修复口径；M4' 验收反馈 320→380 字号加大）
			gtx.Constraints.Min.X = w
			gtx.Constraints.Max.X = w
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, p.layoutSidePanel)
		}),
	)
}

// 侧板滚动区行号（行数随参谋模式增减）。
const (
	sideRowInfo = iota
	sideRowMsg
	sideRowOpponent
	sideRowConfig
	sideRowSettingsBase // 自此起为对局设置行（0=超时 1=重试 2=降级 3=参谋 4=强度 5=深度）
)

// layoutSidePanel 侧板：Flex 竖排{滚动区(layout.List), 固定底部按钮行}——
// 立即保存粘性置底（上游 §3.6 优化 3：滚动时按钮恒可见）。
func (p *HumanVsLlmPage) layoutSidePanel(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			rows := sideRowSettingsBase + 4
			if p.gameSettings.AdvisorMode != llm.AdvisorOff {
				rows += 2
			}
			list := layout.List{Axis: layout.Vertical}
			return list.Layout(gtx, rows, p.sideRow)
		}),
		// 粘性置底按钮行（恒可见）
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return material.Button(PageTheme, &p.saveNowBtn, "立即保存").Layout(gtx)
			})
		}),
	)
}

// settingRowCount 对局设置行数（参谋关闭=4 行；其余=6 行）。
func (p *HumanVsLlmPage) settingRowCount() int {
	if p.gameSettings.AdvisorMode != llm.AdvisorOff {
		return 6
	}
	return 4
}

// sideRow 侧板滚动区行投影。
func (p *HumanVsLlmPage) sideRow(gtx layout.Context, i int) layout.Dimensions {
	switch {
	case i == sideRowInfo:
		return p.layoutGameInfo(gtx)
	case i == sideRowMsg:
		return messageAreaCard(gtx, p.msgArea, "消息区")
	case i == sideRowOpponent:
		return p.layoutOpponentRow(gtx)
	case i == sideRowConfig:
		return p.layoutConfigSection(gtx)
	default:
		idx := i - sideRowSettingsBase
		if idx < 0 || idx >= p.settingRowCount() {
			return layout.Dimensions{}
		}
		return p.layoutSettingRow(gtx, idx)
	}
}

func (p *HumanVsLlmPage) layoutGameInfo(gtx layout.Context) layout.Dimensions {
	snap := p.store.State()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if snap.Result != nil {
				r := *snap.Result
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return DrawResultBanner(gtx, &r)
				})
			}
			return layout.Dimensions{}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(PageTheme, fmt.Sprintf("当前回合: %s", sideNameOf(snap.IsRedTurn)))
			l.Color = ThemeOnSurface
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !snap.IsCheck || snap.Result != nil {
				return layout.Dimensions{}
			}
			l := material.Body2(PageTheme, "将军！")
			l.Color = ThemeError
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(PageTheme, fmt.Sprintf("步数: %d", len(snap.MoveHistory)))
			l.Color = ThemeOnSurface
			return l.Layout(gtx)
		}),
	)
}

// layoutOpponentRow 对手设置（引擎类型 chips；上游"对手引擎" select 翻译）。
func (p *HumanVsLlmPage) layoutOpponentRow(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionTitle(gtx, "对手") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutOptionChips(gtx, 90, chipOpt{&p.sideLlmBtn, "大模型", p.opponentType == llm.SideEngineLLM})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layoutOptionChips(gtx, 90, chipOpt{&p.sideBuiltinBtn, "内置 AI", p.opponentType == llm.SideEngineBuiltin})
				}),
			)
		}),
	)
}

// layoutConfigSection 配置卡 + 镜像提示（DR-014）。
func (p *HumanVsLlmPage) layoutConfigSection(gtx layout.Context) layout.Dimensions {
	if !p.configReady() {
		return hintLine(gtx, "配置加载中…")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.configCard.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !p.mirrored {
				return layout.Dimensions{}
			}
			return hintLine(gtx, "黑方未配置——已使用红方的模型配置（修改请在红方槽位或大模型对战页进行）")
		}),
	)
}

// layoutSettingRow 对局设置行（idx=0..5；与 consumeChoices 行序一致）。
func (p *HumanVsLlmPage) layoutSettingRow(gtx layout.Context, idx int) layout.Dimensions {
	switch idx {
	case 0:
		return p.choiceTimeout.draw(gtx, "空闲超时", p.gameSettings.TimeoutSeconds, 84)
	case 1:
		return p.choiceAttempts.draw(gtx, "无效回复重试", p.gameSettings.MaxAttempts, 84)
	case 2:
		return p.choiceFallback.draw(gtx, "持续失败时", fallbackIndexOf(p.gameSettings.Fallback), 84)
	case 3:
		return p.choiceAdvisor.draw(gtx, "引擎参谋", advisorIndexOf(p.gameSettings.AdvisorMode), 84)
	case 4:
		return p.choiceBlend.draw(gtx, "参谋强度", p.gameSettings.StrengthBlend, 84)
	case 5:
		return p.choiceAdvDiff.draw(gtx, "参谋深度", p.gameSettings.AdvisorDifficulty, 84)
	}
	return layout.Dimensions{}
}

func (p *HumanVsLlmPage) layoutOverlays(gtx layout.Context) layout.Dimensions {
	if p.confirmingNewGame {
		confirmed, canceled := p.newGameDialog.LayoutFull(gtx)
		switch {
		case confirmed:
			p.confirmingNewGame = false
			p.doNewGame()
		case canceled:
			p.confirmingNewGame = false
		}
	}
	if p.judge != nil && p.judge.DrawOffer != nil {
		confirmed, canceled := p.drawDialog.LayoutFull(gtx)
		switch {
		case confirmed:
			p.judge.AcceptDraw()
		case canceled:
			p.judge.DeclineDraw()
		}
	}
	if p.toastText != "" {
		DrawToast(gtx, p.toastText)
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// ---- 枚举映射（index ↔ 复制物枚举；与 state.LlmSettingsFromRaw 同值表）----

var fallbackValues = []llm.LlmFallback{llm.FallbackBuiltinAI, llm.FallbackResign}
var advisorValues = []llm.AdvisorMode{llm.AdvisorOff, llm.AdvisorCandidate, llm.AdvisorGate}

func fallbackIndexOf(v llm.LlmFallback) int {
	for i, e := range fallbackValues {
		if e == v {
			return i
		}
	}
	return 0
}

func advisorIndexOf(v llm.AdvisorMode) int {
	for i, e := range advisorValues {
		if e == v {
			return i
		}
	}
	return 1
}

func sideNameOf(isRed bool) string {
	if isRed {
		return "红方"
	}
	return "黑方"
}
