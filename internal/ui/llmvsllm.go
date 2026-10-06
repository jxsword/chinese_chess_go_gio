package ui

// 大模型对战页（T4'.3；翻译源 = 上游 features/board/LlmVsLlmPage.tsx，
// design_docs/08 §5 + 05 §8）：
//   - 红黑双方各接一个可独立配置的 LLM（HybridLlmPlayer），页面驱动互弈；
//   - 控制：开始 / 暂停·继续（同一按钮）/ 停止 / 新游戏；棋盘全自动无点击；
//   - 循环 = 主 goroutine 状态机（pump 步进：事件回执/间隔计时到点续跑）——
//     上游 async while 循环的 #G3 等价形态（vm 落子/页面字段只进主循环）；
//   - 暂停在两手之间生效（在途回复 cancel + 作废，代次 loopGen 收口）；
//   - 走棋间隔 0/1/2/5s；红黑参谋强度各自独立；
//   - 空配置一侧运行时跟随对方（DR-012，llm.ResolveLlmSideConfig）；
//   - 一方 failed（resign 策略）→ 显式判负终止循环（防错 #7）；
//   - 恢复存档后不自动续跑，由用户点"开始"从当前局面继续。
// 每局一实例（app Router 工厂，铁律 #G4）。

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

// LlmVsLlmHooks 页面回调。
type LlmVsLlmHooks struct {
	OnBack func()
}

// LlmVsLlmPage 大模型对战页 state struct（铁律 #G3：主 goroutine 独占）。
type LlmVsLlmPage struct {
	hooks    LlmVsLlmHooks
	env      GameEnv
	llmStore LlmStore
	llm      LlmRunner

	store    *state.GameStore
	board    *BoardView
	judge    *RepetitionJudge
	ai       *EngineClient // 内置 AI 侧应手 + 双方参谋共用 Runner
	advisor  *EngineRunnerAdapter
	autoSave *state.GameAutoSave
	canSave  func() bool

	redConfig    llm.LlmEndpointConfig
	blackConfig  llm.LlmEndpointConfig
	redLoaded    bool
	blackLoaded  bool
	redCard      *LlmConfigCard
	blackCard    *LlmConfigCard
	gameSettings state.LlmGameSettings

	// 循环状态机（上游 running/paused/moveActive + gameSeqRef 的代次形态）
	running      bool
	paused       bool
	loopGen      int    // 循环代次（停止/暂停/新局递增——上游 gameSeqRef 语义）
	moveRequest  string // 在途走子请求（引擎/LLM 共用面；空 = 无在途）
	moveActive   bool
	moveIsRed    bool
	moveStarted  time.Time
	attemptN     int
	attemptTotal int
	statusText   string
	redNote      string
	blackNote    string
	lastMoveText string

	restoreID      string
	restorePending bool

	persistSeq   int
	saveInFlight int // 双槽位保存计数（两回执齐 → toast）

	msgArea  *streamMessageArea
	disposed bool

	// 控件
	choiceTimeout  *choiceRow
	choiceAttempts *choiceRow
	choiceFallback *choiceRow
	choiceInterval *choiceRow
	choiceAdvisor  *choiceRow
	choiceRedBlend *choiceRow
	choiceBlkBlend *choiceRow
	choiceAdvDiff  *choiceRow
	sideRedLlm     widget.Clickable
	sideRedBuiltin widget.Clickable
	sideBlkLlm     widget.Clickable
	sideBlkBuiltin widget.Clickable
	startStopBtn   widget.Clickable // 开始/暂停·继续（同一按钮）
	stopBtn        widget.Clickable
	saveNowBtn     widget.Clickable
	newGameBtn     widget.Clickable
	backBtn        widget.Clickable

	confirmingNewGame bool
	toastText         string
	toastSeq          int
	timerStop         chan struct{}
	newGameDialog     *ModalDialog
	drawDialog        *ModalDialog
}

// NewLlmVsLlmPage 创建（工厂）。
func NewLlmVsLlmPage(env LlmEnv, hooks LlmVsLlmHooks) *LlmVsLlmPage {
	var resolve func(string) string
	if env.Store != nil {
		resolve = env.Store.ResolveAPIKey
	}
	llmClient := NewLlmClient(env.GameEnv, resolve, func() int {
		if env.Settings == nil {
			return 0
		}
		return llm.ResolveTimeoutSeconds(env.Settings.Get(llm.SettingKeyTimeoutSeconds))
	})
	return newLlmVsLlmPage(env, hooks, llmClient, nil)
}

func newLlmVsLlmPage(env LlmEnv, hooks LlmVsLlmHooks, llmRunner LlmRunner, runner EngineSubmitter) *LlmVsLlmPage {
	gs := state.LoadLlmSettings(env.Settings)
	p := &LlmVsLlmPage{
		hooks:        hooks,
		env:          env.GameEnv,
		llmStore:     env.Store,
		llm:          llmRunner,
		store:        state.NewGameStore(state.GameStoreConfig{Mode: state.ModeLlmVsLlm, PlayerSide: rules.Red}),
		statusText:   "等待开始",
		gameSettings: gs,
		msgArea:      newStreamMessageArea(),
		restoreID:    env.NewRequestID("restore"),
	}
	p.board = NewBoardView(p.store)
	p.ai = NewEngineClient(env.GameEnv, runner)
	p.advisor = NewEngineRunnerAdapter(p.ai.runner)
	p.redCard = NewLlmConfigCard("红方模型", storage.SlotRed, llm.LlmPresets, p.redConfig, func(c llm.LlmEndpointConfig) {
		p.redConfig = c
		p.schedulePersist()
	})
	p.blackCard = NewLlmConfigCard("黑方模型", storage.SlotBlack, llm.LlmPresets, p.blackConfig, func(c llm.LlmEndpointConfig) {
		p.blackConfig = c
		p.schedulePersist()
	})
	p.redCard.OnTestConnection = p.onRedTest
	p.blackCard.OnTestConnection = p.onBlackTest
	p.redCard.OnPaste = p.onRedPaste
	p.blackCard.OnPaste = p.onBlackPaste

	p.canSave = func() bool { return true }
	p.board.Blocked = func() bool { return p.modalOpen() } // 自动对局期间 vm 亦锁输入

	p.judge = NewRepetitionJudge(p.store.VM, func(rules.Side) bool { return false }, p.showToast) // 双方均为引擎方：全部自动
	p.store.VM.OnHistoryGrow = p.judge.OnHistoryGrow
	p.store.VM.OnHistoryRewound = p.judge.OnHistoryRewound

	p.autoSave = state.NewGameAutoSave(state.AutoSaveConfig{
		Mode:     state.ModeLlmVsLlm,
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
		env.DB.LoadLatestAsync(p.restoreID, state.ModeLlmVsLlm)
	} else {
		p.restorePending = false
		p.store.VM.UnlockInput()
	}

	if env.Store != nil {
		env.Store.LoadSlotAsync(env.NewRequestID("cfg-red"), storage.SlotRed)
		env.Store.LoadSlotAsync(env.NewRequestID("cfg-black"), storage.SlotBlack)
	} else {
		p.redLoaded = true
		p.blackLoaded = true
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

func (p *LlmVsLlmPage) initChoices() {
	p.choiceTimeout = newChoiceRow([]int{30, 60, 120, 180, 300}, []string{"30秒", "60秒", "120秒", "180秒", "300秒"})
	p.choiceAttempts = newChoiceRow([]int{1, 3, 5}, []string{"1次", "3次", "5次"})
	p.choiceFallback = newChoiceRow([]int{0, 1}, []string{"内置AI代走", "该方判负"})
	p.choiceInterval = newChoiceRow([]int{0, 1, 2, 5}, []string{"不等待", "1秒", "2秒", "5秒"})
	p.choiceAdvisor = newChoiceRow([]int{0, 1, 2}, []string{"关闭", "候选", "护航"})
	p.choiceRedBlend = newChoiceRow([]int{0, 25, 50, 75, 100}, []string{"0", "25", "50", "75", "100"})
	p.choiceBlkBlend = newChoiceRow([]int{0, 25, 50, 75, 100}, []string{"0", "25", "50", "75", "100"})
	p.choiceAdvDiff = newChoiceRow([]int{1, 3, 5}, []string{"快(2层)", "中(4层)", "强(6层)"})
}

func (p *LlmVsLlmPage) showToast(message string) {
	p.toastSeq++
	p.toastText = message
	seq := p.toastSeq
	go func() {
		time.Sleep(2200 * time.Millisecond)
		p.env.Emit("", ToastHide{Seq: seq}, nil)
	}()
}

func (p *LlmVsLlmPage) startTimer() {
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

func (p *LlmVsLlmPage) stopTimer() {
	if p.timerStop != nil {
		close(p.timerStop)
		p.timerStop = nil
	}
}

// OnAppEvent 事件总线消费（主 goroutine）。
func (p *LlmVsLlmPage) OnAppEvent(payload any) {
	switch ev := payload.(type) {
	case DbLoadDone:
		if ev.Mode != state.ModeLlmVsLlm || !p.restorePending {
			return
		}
		p.restorePending = false
		outcome := state.RestoreOrNewGame(p.store.VM, ev.Mode, ev.Saved, ev.Err, p.env.Repo)
		if outcome == state.RestoreRestored {
			p.store.VM.UnlockInput()
		}
		// 恢复存档后不自动续跑（上游语义）
	case DbSaveDone:
		if ev.Mode != state.ModeLlmVsLlm {
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
		if ev.RequestID == p.moveRequest {
			p.attemptN = ev.N
			p.attemptTotal = ev.Total
		}
	case LlmStreamChunk:
		if ev.RequestID == p.moveRequest { // K21 防线
			p.msgArea.appendChunk(ev.Delta)
		}
	case LlmStreamDone:
		if ev.RequestID == p.moveRequest {
			p.msgArea.endStream()
		}
	case LlmStreamError:
		if ev.RequestID == p.moveRequest {
			p.msgArea.endStream()
			p.msgArea.appendLine("〔请求失败〕" + ev.Message)
		}
	case LlmTestDone:
		p.redCard.SetTestResult(ev.OK, ev.Message)
		p.blackCard.SetTestResult(ev.OK, ev.Message)
	case PasteTextDone:
		p.redCard.ApplyPaste(ev.Target, ev.Text, ev.Err)
		p.blackCard.ApplyPaste(ev.Target, ev.Text, ev.Err)
	case LlmLoopTick:
		if ev.Gen == p.loopGen && p.running && !p.paused && !p.disposed {
			p.pump() // 间隔计时到点续跑
		}
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

// onSlotLoaded 双槽位配置落位（掩码 Key）。
func (p *LlmVsLlmPage) onSlotLoaded(ev SecureSlotLoaded) {
	cfg := llm.LlmEndpointConfig{}
	if ev.Config != nil {
		cfg = *ev.Config
	}
	switch ev.Slot {
	case storage.SlotRed:
		p.redLoaded = true
		p.redConfig = cfg
		p.redCard.SetConfig(cfg)
	case storage.SlotBlack:
		p.blackLoaded = true
		p.blackConfig = cfg
		p.blackCard.SetConfig(cfg)
	}
}

// onSlotSaved 双槽位写入回执（两回执齐 → 汇总 toast；上游 Promise.all 同语义）。
func (p *LlmVsLlmPage) onSlotSaved(ev SecureSlotSaved) {
	if p.saveInFlight == 0 {
		return
	}
	if ev.Err != nil {
		p.saveInFlight = 0
		p.showToast("保存失败：本地存储不可用")
		return
	}
	p.saveInFlight--
	if p.saveInFlight == 0 {
		if ev.Stored == "plainFallback" {
			p.showToast("双方模型配置已保存（系统安全存储不可用，已明文保存到本地）")
		} else {
			p.showToast("双方模型配置已保存")
		}
	}
}

// sideLabel 当前在途方的显示名（状态条）。
func (p *LlmVsLlmPage) sideLabel(isRed bool) string {
	if isRed {
		return "红方"
	}
	return "黑方"
}

// ---- 循环状态机（上游 runLoop 的主 goroutine 步进形态）----

// start 开始对局（校验配置；DR-012/DR-014 口径：内置 AI 侧不要求 LLM 配置）。
func (p *LlmVsLlmPage) start() {
	if !p.redLoaded || !p.blackLoaded {
		p.showToast("配置加载中，请稍候再开始")
		return
	}
	st := p.gameSettings
	if st.RedSideType == llm.SideEngineLLM {
		redEff := llm.ResolveLlmSideConfig(p.redConfig, p.blackConfig, storage.SlotRed, storage.SlotBlack)
		if !llm.IsConfigured(redEff.Config) {
			p.showToast("大模型一侧需填写端点地址与模型 ID（或把该侧切换为内置 AI）")
			return
		}
	}
	if st.BlackSideType == llm.SideEngineLLM {
		blackEff := llm.ResolveLlmSideConfig(p.blackConfig, p.redConfig, storage.SlotBlack, storage.SlotRed)
		if !llm.IsConfigured(blackEff.Config) {
			p.showToast("大模型一侧需填写端点地址与模型 ID（或把该侧切换为内置 AI）")
			return
		}
	}
	p.running = true
	p.paused = false
	p.statusText = "继续对局…"
	p.store.VM.LockInput() // 自动对局期间锁定棋盘点击
	p.pump()
}

// togglePause 暂停/继续（同一按钮；暂停=作废在途回复，继续后重新思考）。
func (p *LlmVsLlmPage) togglePause() {
	if !p.running {
		return
	}
	if p.paused {
		p.loopGen++
		p.cancelMove()
		p.paused = false
		p.statusText = "继续对局…"
		p.pump()
		return
	}
	p.loopGen++
	p.cancelMove()
	p.paused = true
	p.statusText = "已暂停（模型回复已作废，继续后重新思考）"
}

// stop 停止循环（解锁；可再次开始）。
func (p *LlmVsLlmPage) stop() {
	p.loopGen++
	p.running = false
	p.paused = false
	p.cancelMove()
	p.store.VM.UnlockInput()
	p.statusText = "已停止"
}

// cancelMove 作废在途走子请求（代次已递增，迟到回执由总线/页面双重丢弃）。
func (p *LlmVsLlmPage) cancelMove() {
	if p.moveRequest == "" {
		return
	}
	if p.llm != nil {
		p.llm.Cancel(p.moveRequest)
	}
	p.ai.Cancel(p.moveRequest)
	p.msgArea.endStream()
	p.moveRequest = ""
	p.moveActive = false
}

// pump 循环步进：驱动当前轮走方应手（上游 runLoop 循环体一次迭代）。
func (p *LlmVsLlmPage) pump() {
	if p.disposed || !p.running || p.paused {
		return
	}
	if p.store.State().Result != nil {
		p.running = false
		p.store.VM.UnlockInput()
		p.statusText = "对局结束：" + resultText(*p.store.State().Result)
		return
	}
	snap := p.store.State()
	isRedTurn := snap.IsRedTurn
	p.moveIsRed = isRedTurn
	id := p.env.NewRequestID("llm-loop")
	p.moveRequest = id
	p.moveActive = true
	p.moveStarted = time.Now()
	p.attemptN = 0
	p.attemptTotal = p.gameSettings.MaxAttempts

	if (isRedTurn && p.gameSettings.RedSideType == llm.SideEngineBuiltin) ||
		(!isRedTurn && p.gameSettings.BlackSideType == llm.SideEngineBuiltin) {
		// DR-014：内置 AI 直接应手（L2 历史回避入参，DR-018）。
		p.ai.FindBestMoveAsync(id, snap.Fen, 3, snap.FenHistory)
		return
	}
	if p.llm == nil {
		p.moveRequest = ""
		p.moveActive = false
		p.onSideFailed(isRedTurn, "LLM 通道不可用")
		return
	}
	// DR-012：空配置一侧运行时跟随对方；authSlot 随生效配置来源（DR-010）。
	var resolved llm.ResolvedLlmSideConfig
	if isRedTurn {
		resolved = llm.ResolveLlmSideConfig(p.redConfig, p.blackConfig, storage.SlotRed, storage.SlotBlack)
	} else {
		resolved = llm.ResolveLlmSideConfig(p.blackConfig, p.redConfig, storage.SlotBlack, storage.SlotRed)
	}
	blend := p.gameSettings.RedStrengthBlend
	if !isRedTurn {
		blend = p.gameSettings.BlackStrengthBlend
	}
	p.statusText = fmt.Sprintf("%s（%s）思考中…", p.sideLabel(isRedTurn), strings.TrimSpace(resolved.Config.Model))
	p.msgArea.begin(p.sideLabel(isRedTurn))
	p.llm.MoveAsync(id, LlmMoveSpec{
		Endpoint:          resolved.Config,
		AuthSlot:          resolved.AuthSlot,
		Board:             p.store.VM.Board().Copy(),
		History:           append([]rules.Move(nil), snap.MoveHistory...),
		Advisor:           p.advisor,
		AdvisorMode:       p.gameSettings.AdvisorMode,
		StrengthBlend:     blend,
		AdvisorDifficulty: p.gameSettings.AdvisorDifficulty,
		MaxAttempts:       p.gameSettings.MaxAttempts,
		Fallback:          p.gameSettings.Fallback,
		OnAttempt:         p.onAttempt,
	})
}

// onAttempt 尝试进度回调（后台 goroutine → 事件总线）。
func (p *LlmVsLlmPage) onAttempt(n, total int) {
	p.env.Emit(p.moveRequest, LlmAttemptProgress{RequestID: p.moveRequest, N: n, Total: total}, nil)
}

// genMatch 当前代次校验（在途期间可能已停止/暂停/重开：作废结果）。
func (p *LlmVsLlmPage) settleMove(id string) bool {
	if id != p.moveRequest {
		return false // 迟到/已作废（K21 二次收口）
	}
	p.moveRequest = ""
	p.moveActive = false
	return true
}

// onLlmMoveDone LLM 应手回执。
func (p *LlmVsLlmPage) onLlmMoveDone(ev LlmMoveDone) {
	if !p.settleMove(ev.RequestID) {
		return
	}
	if ev.Err != nil {
		if errors.Is(ev.Err, llm.ErrCanceled) {
			return // 取消（暂停/停止/新局）：无需提示
		}
		p.onSideFailed(p.moveIsRed, "走子来源异常："+ev.Err.Error())
		return
	}
	p.applyMoveResult(ev.Result)
}

// onBuiltinMoveDone 内置 AI 侧应手回执。
func (p *LlmVsLlmPage) onBuiltinMoveDone(ev EngineMoveDone) {
	if !p.settleMove(ev.RequestID) {
		return
	}
	if ev.Err != nil {
		p.onSideFailed(p.moveIsRed, "走子来源异常："+ev.Err.Error())
		return
	}
	if ev.Move == nil {
		p.statusText = "该方已无合法着法"
		p.running = false
		p.store.VM.UnlockInput()
		return
	}
	p.applyMoveResult(engine.MoveSourceResult{Status: engine.StatusOK, Move: ev.Move})
}

// applyMoveResult 走子结果落盘 + 续跑调度（上游 runLoop 结果段翻译）。
func (p *LlmVsLlmPage) applyMoveResult(result engine.MoveSourceResult) {
	switch result.Status {
	case engine.StatusOK:
		if result.Move == nil {
			p.scheduleNext()
			return
		}
		if !p.store.VM.PlayMove(result.Move.From, result.Move.To) {
			// nextMove 内部已按合法清单校验，此处为极端兜底：终止本方。
			p.onSideFailed(p.moveIsRed, "着法未通过最终校验")
			return
		}
		p.lastMoveText = fmt.Sprintf("%s %s", p.sideLabel(p.moveIsRed), formatMoveText(*result.Move))
		if result.FromFallback {
			p.setNote(p.moveIsRed, noteOr(result.Note, "已由内置 AI 兜底走子"))
			p.msgArea.appendLine("〔兜底〕" + p.redOrBlack(p.moveIsRed) + noteOr(result.Note, "已由内置 AI 兜底走子"))
		} else if result.Note != "" {
			p.setNote(p.moveIsRed, result.Note)
			p.msgArea.appendLine("〔参谋〕" + p.redOrBlack(p.moveIsRed) + result.Note)
		}
		p.scheduleNext()
	case engine.StatusNoLegalMove:
		p.statusText = "该方已无合法着法"
		p.running = false
		p.store.VM.UnlockInput()
	case engine.StatusFailed:
		p.onSideFailed(p.moveIsRed, result.Note)
	}
}

// scheduleNext 走棋间隔后步进（间隔=0 立即续跑；tick 以当代循环 id 关联，
// 停止/暂停/新局后按 id 丢弃——#G5）。
func (p *LlmVsLlmPage) scheduleNext() {
	if !p.running || p.paused {
		return
	}
	interval := p.gameSettings.IntervalSeconds
	if interval <= 0 {
		p.pump()
		return
	}
	gen := p.loopGen
	time.AfterFunc(time.Duration(interval)*time.Second, func() {
		p.env.Emit("", LlmLoopTick{Gen: gen}, nil)
	})
}

// onSideFailed 一方走子失败：显式判负终止循环（上游 _onSideFailed，防错 #7）。
func (p *LlmVsLlmPage) onSideFailed(isRed bool, reason string) {
	p.loopGen++
	p.running = false
	p.paused = false
	p.cancelMove()
	p.store.VM.Resign(sideOf(isRed))
	p.store.VM.UnlockInput()
	p.statusText = fmt.Sprintf("%s走子失败，对局终止", p.sideLabel(isRed))
	p.setNote(isRed, reason)
	p.msgArea.appendLine("〔判负〕" + p.redOrBlack(isRed) + reason)
}

func (p *LlmVsLlmPage) setNote(isRed bool, note string) {
	if isRed {
		p.redNote = note
	} else {
		p.blackNote = note
	}
}

func (p *LlmVsLlmPage) redOrBlack(isRed bool) string {
	if isRed {
		return "红方"
	}
	return "黑方"
}

// doNewGame 新游戏确认后重置（循环终止、棋局清空）。
func (p *LlmVsLlmPage) doNewGame() {
	p.loopGen++
	p.running = false
	p.paused = false
	p.cancelMove()
	p.cancelPendingRestore()
	p.board.CancelAnim()
	p.store.VM.NewGame()
	p.statusText = "等待开始"
	p.redNote = ""
	p.blackNote = ""
	p.lastMoveText = ""
	p.msgArea.clear()
}

// cancelPendingRestore 取消在途恢复并解锁。
func (p *LlmVsLlmPage) cancelPendingRestore() {
	if !p.restorePending {
		return
	}
	p.env.Cancel(p.restoreID)
	p.restorePending = false
	p.store.VM.UnlockInput()
}

// ---- 测试连接 / 粘贴（双卡定向）----

func (p *LlmVsLlmPage) onRedTest(cfg llm.LlmEndpointConfig, slot string) {
	if p.llm == nil {
		p.redCard.SetTestResult(false, "连接失败：测试通道不可用")
		return
	}
	p.llm.TestConnectionAsync(p.env.NewRequestID("test-red"), cfg, slot)
}

func (p *LlmVsLlmPage) onBlackTest(cfg llm.LlmEndpointConfig, slot string) {
	if p.llm == nil {
		p.blackCard.SetTestResult(false, "连接失败：测试通道不可用")
		return
	}
	p.llm.TestConnectionAsync(p.env.NewRequestID("test-black"), cfg, slot)
}

func (p *LlmVsLlmPage) onRedPaste(target int) {
	pasteFromWindowsAsync(func(text string, err error) {
		p.env.Emit("", PasteTextDone{Target: target, Text: text, Err: err}, nil)
	})
}

func (p *LlmVsLlmPage) onBlackPaste(target int) {
	pasteFromWindowsAsync(func(text string, err error) {
		p.env.Emit("", PasteTextDone{Target: target, Text: text, Err: err}, nil)
	})
}

// ---- 保存 ----

func (p *LlmVsLlmPage) schedulePersist() {
	if p.llmStore == nil {
		return
	}
	p.persistSeq++
	seq := p.persistSeq
	time.AfterFunc(llmPersistDebounce, func() {
		p.env.Emit("", LlmPersistTick{Seq: seq}, nil)
	})
}

// persistNow 全部设置 + 双槽位落盘（本页显示并拥有全部字段——上游整对象回写）。
func (p *LlmVsLlmPage) persistNow() {
	if p.llmStore == nil || !p.redLoaded || !p.blackLoaded {
		return
	}
	p.llmStore.SaveSlotAsync(p.env.NewRequestID("save-red"), storage.SlotRed, p.redConfig)
	p.llmStore.SaveSlotAsync(p.env.NewRequestID("save-black"), storage.SlotBlack, p.blackConfig)
	p.llmStore.SaveSettings(p.gameSettings)
}

// saveNow 立即保存（粘性置底按钮）。
func (p *LlmVsLlmPage) saveNow() {
	if !p.redLoaded || !p.blackLoaded {
		p.showToast("配置加载中，请稍候")
		return
	}
	p.saveInFlight = 2
	p.llmStore.SaveSlotAsync(p.env.NewRequestID("save-red"), storage.SlotRed, p.redConfig)
	p.llmStore.SaveSlotAsync(p.env.NewRequestID("save-black"), storage.SlotBlack, p.blackConfig)
	p.llmStore.SaveSettings(p.gameSettings)
}

// Dispose 页面卸载：终止循环 + 取消在途 + 兜底回写 + 停计时 + 解锁（#5）。
func (p *LlmVsLlmPage) Dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	p.loopGen++
	p.running = false
	p.paused = false
	p.board.CancelAnim()
	p.cancelMove()
	p.cancelPendingRestore()
	p.persistNow()
	p.autoSave.Dispose()
	p.stopTimer()
	p.store.VM.UnlockInput()
}

// OnClose 窗口关闭（07 §2）：同步保存 best-effort。
func (p *LlmVsLlmPage) OnClose() {
	if p.env.Settings == nil || !p.env.Settings.AutoSave || !p.canSave() {
		return
	}
	data := p.store.VM.Serialize()
	if p.env.DB != nil {
		_ = p.env.DB.SaveSync(state.ModeLlmVsLlm, data.Fen, data.Moves)
	}
}

// ---- 交互 ----

func (p *LlmVsLlmPage) modalOpen() bool {
	return p.confirmingNewGame || (p.judge != nil && p.judge.DrawOffer != nil)
}

func (p *LlmVsLlmPage) handleEvents(gtx layout.Context) {
	if p.modalOpen() {
		// 弹窗遮罩：消费全部点击边沿但不生效（KG-008——含 chips/设置行）
		p.backBtn.Clicked(gtx)
		p.newGameBtn.Clicked(gtx)
		p.startStopBtn.Clicked(gtx)
		p.stopBtn.Clicked(gtx)
		p.consumeSideChips(gtx, true)
		p.consumeChoices(gtx, true)
		return
	}
	if p.backBtn.Clicked(gtx) && p.hooks.OnBack != nil {
		p.hooks.OnBack()
	}
	if p.newGameBtn.Clicked(gtx) {
		p.confirmingNewGame = true
	}
	if p.startStopBtn.Clicked(gtx) {
		if !p.running {
			p.start()
		} else {
			p.togglePause()
		}
	}
	if p.stopBtn.Clicked(gtx) && p.running {
		p.stop()
	}
	p.consumeSideChips(gtx, p.running)
	p.consumeChoices(gtx, false)
}

// consumeSideChips 红黑引擎类型 chips（busy=对局运行中/弹窗遮罩——收口为禁改）。
func (p *LlmVsLlmPage) consumeSideChips(gtx layout.Context, busy bool) {
	if p.sideRedLlm.Clicked(gtx) && !busy {
		p.gameSettings.RedSideType = llm.SideEngineLLM
		p.schedulePersist()
	}
	if p.sideRedBuiltin.Clicked(gtx) && !busy {
		p.gameSettings.RedSideType = llm.SideEngineBuiltin
		p.schedulePersist()
	}
	if p.sideBlkLlm.Clicked(gtx) && !busy {
		p.gameSettings.BlackSideType = llm.SideEngineLLM
		p.schedulePersist()
	}
	if p.sideBlkBuiltin.Clicked(gtx) && !busy {
		p.gameSettings.BlackSideType = llm.SideEngineBuiltin
		p.schedulePersist()
	}
}

// consumeChoices 对局设置 chips（变更即更新 + 防抖落盘；busy=弹窗遮罩全消费不生效）。
func (p *LlmVsLlmPage) consumeChoices(gtx layout.Context, busy bool) {
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
	p.gameSettings.IntervalSeconds = apply(p.gameSettings.IntervalSeconds, p.choiceInterval.consume(gtx, p.gameSettings.IntervalSeconds))
	p.gameSettings.AdvisorMode = advisorValues[apply(advisorIndexOf(p.gameSettings.AdvisorMode), p.choiceAdvisor.consume(gtx, advisorIndexOf(p.gameSettings.AdvisorMode)))]
	if p.gameSettings.AdvisorMode != llm.AdvisorOff {
		p.gameSettings.RedStrengthBlend = apply(p.gameSettings.RedStrengthBlend, p.choiceRedBlend.consume(gtx, p.gameSettings.RedStrengthBlend))
		p.gameSettings.BlackStrengthBlend = apply(p.gameSettings.BlackStrengthBlend, p.choiceBlkBlend.consume(gtx, p.gameSettings.BlackStrengthBlend))
		p.gameSettings.AdvisorDifficulty = apply(p.gameSettings.AdvisorDifficulty, p.choiceAdvDiff.consume(gtx, p.gameSettings.AdvisorDifficulty))
	}
}

// ---- 布局 ----

func (p *LlmVsLlmPage) Layout(gtx layout.Context) layout.Dimensions {
	p.handleEvents(gtx)
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	fillRect(gtx.Ops, image.Rectangle{Max: gtx.Constraints.Max}, ThemeSurface)
	return layout.Stack{}.Layout(gtx,
		layout.Expanded(p.layoutContent),
		layout.Stacked(p.layoutOverlays),
	)
}

func (p *LlmVsLlmPage) layoutContent(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutHeader),
		layout.Rigid(p.layoutStatusBar),
		layout.Flexed(1, p.layoutBody),
	)
}

func (p *LlmVsLlmPage) layoutHeader(gtx layout.Context) layout.Dimensions {
	startLabel := "开始对战"
	if p.running {
		if p.paused {
			startLabel = "继续"
		} else {
			startLabel = "暂停"
		}
	}
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(p.simpleButton(&p.backBtn, "返回", false)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					t := material.H6(PageTheme, "大模型对战")
					t.Color = ThemeOnSurface
					return t.Layout(gtx)
				})
			}),
			layout.Rigid(p.simpleButton(&p.startStopBtn, startLabel, true)),
			layout.Rigid(p.simpleButton(&p.stopBtn, "停止", false)),
			layout.Rigid(p.simpleButton(&p.newGameBtn, "新游戏", false)),
		)
	})
}

func (p *LlmVsLlmPage) simpleButton(c *widget.Clickable, label string, primary bool) func(gtx layout.Context) layout.Dimensions {
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

// layoutStatusBar 循环状态条（结果优先；moveActive 附加思考后缀）。
func (p *LlmVsLlmPage) layoutStatusBar(gtx layout.Context) layout.Dimensions {
	snap := p.store.State()
	text := p.statusText
	bg := ThemeSurfaceDim
	fg := ThemeOnSurface
	if snap.Result != nil {
		text = "对局结束：" + resultText(*snap.Result)
	} else if p.moveActive {
		text += thinkingSuffix(p.moveStarted, p.attemptN, p.attemptTotal)
		bg = ThemeSeed
		fg = ThemeSurface
	}
	return layout.Inset{Top: unit.Dp(2), Bottom: unit.Dp(2), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := image.Point{X: gtx.Constraints.Max.X, Y: gtx.Dp(unit.Dp(30))}
		defer clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(unit.Dp(6))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, bg)
		gtx.Constraints = layout.Exact(size)
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(PageTheme, text)
			l.Color = fg
			return l.Layout(gtx)
		})
	})
}

func (p *LlmVsLlmPage) layoutBody(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, p.board.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			w := gtx.Dp(unit.Dp(320))
			gtx.Constraints.Min.X = w
			gtx.Constraints.Max.X = w
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, p.layoutSidePanel)
		}),
	)
}

// 侧板滚动区行号（双信息 + 双配置卡 + 设置区）。
const (
	llvRowInfo = iota
	llvRowMsg
	llvRowRedCard
	llvRowBlackCard
	llvRowSideTypes
	llvRowSettingsBase // 自此起对局设置行（0=超时 1=重试 2=降级 3=间隔 4=参谋 5=红强 6=黑强 7=深度）
)

func (p *LlmVsLlmPage) settingRowCount() int {
	if p.gameSettings.AdvisorMode != llm.AdvisorOff {
		return 8
	}
	return 5
}

// layoutSidePanel 侧板：滚动区 + 立即保存粘性置底。
func (p *LlmVsLlmPage) layoutSidePanel(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			rows := llvRowSettingsBase + p.settingRowCount()
			list := layout.List{Axis: layout.Vertical}
			return list.Layout(gtx, rows, p.sideRow)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return material.Button(PageTheme, &p.saveNowBtn, "立即保存").Layout(gtx)
			})
		}),
	)
}

func (p *LlmVsLlmPage) sideRow(gtx layout.Context, i int) layout.Dimensions {
	switch {
	case i == llvRowInfo:
		return p.layoutGameInfo(gtx)
	case i == llvRowMsg:
		return messageAreaCard(gtx, p.msgArea, "消息区")
	case i == llvRowRedCard:
		return p.layoutRedCard(gtx)
	case i == llvRowBlackCard:
		return p.layoutBlackCard(gtx)
	case i == llvRowSideTypes:
		return p.layoutSideTypeRow(gtx)
	default:
		idx := i - llvRowSettingsBase
		if idx < 0 || idx >= p.settingRowCount() {
			return layout.Dimensions{}
		}
		return p.layoutSettingRow(gtx, idx)
	}
}

func (p *LlmVsLlmPage) layoutGameInfo(gtx layout.Context) layout.Dimensions {
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
			if p.redNote != "" {
				l := material.Body2(PageTheme, "红方："+p.redNote)
				l.Color = ThemeOnSurface
				l.TextSize = unit.Sp(12)
				return l.Layout(gtx)
			}
			return layout.Dimensions{}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if p.blackNote != "" {
				l := material.Body2(PageTheme, "黑方："+p.blackNote)
				l.Color = ThemeOnSurface
				l.TextSize = unit.Sp(12)
				return l.Layout(gtx)
			}
			return layout.Dimensions{}
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if p.lastMoveText != "" {
				l := material.Body2(PageTheme, p.lastMoveText)
				l.Color = ThemeSeedDark
				l.TextSize = unit.Sp(12)
				return l.Layout(gtx)
			}
			return layout.Dimensions{}
		}),
	)
}

// layoutRedCard 红方配置卡 + DR-012 镜像提示。
func (p *LlmVsLlmPage) layoutRedCard(gtx layout.Context) layout.Dimensions {
	if !p.redLoaded {
		return hintLine(gtx, "配置加载中…")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.redCard.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			blackConfigured := p.blackLoaded && llm.IsConfigured(p.blackConfig)
			switch {
			case llm.IsEmptyLlmConfig(p.redConfig) && blackConfigured:
				return hintLine(gtx, "未配置——对局时将使用黑方的模型配置")
			case llm.IsEmptyLlmConfig(p.redConfig):
				return hintLine(gtx, "未配置——保存后其他页面（如人机对战）黑方未配置时将使用本侧")
			}
			return layout.Dimensions{}
		}),
	)
}

// layoutBlackCard 黑方配置卡 + DR-012 镜像提示。
func (p *LlmVsLlmPage) layoutBlackCard(gtx layout.Context) layout.Dimensions {
	if !p.blackLoaded {
		return hintLine(gtx, "配置加载中…")
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.blackCard.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			redConfigured := p.redLoaded && llm.IsConfigured(p.redConfig)
			switch {
			case llm.IsEmptyLlmConfig(p.blackConfig) && redConfigured:
				return hintLine(gtx, "本页未配置——对局时将使用红方的模型配置")
			case llm.IsEmptyLlmConfig(p.blackConfig):
				return hintLine(gtx, "未配置——人机对战（大模型）的黑方也将使用红方配置（保存后生效）")
			}
			return layout.Dimensions{}
		}),
	)
}

// layoutSideTypeRow 红黑引擎类型 chips（上游"红方引擎/黑方引擎" select 翻译）。
func (p *LlmVsLlmPage) layoutSideTypeRow(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions { return sectionTitle(gtx, "引擎类型（DR-014）") }),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.sideTypeRow(gtx, "红方", &p.sideRedLlm, &p.sideRedBuiltin, p.gameSettings.RedSideType)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.sideTypeRow(gtx, "黑方", &p.sideBlkLlm, &p.sideBlkBuiltin, p.gameSettings.BlackSideType)
		}),
	)
}

func (p *LlmVsLlmPage) sideTypeRow(gtx layout.Context, label string, llmBtn, builtinBtn *widget.Clickable, current llm.SideEngineType) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(40))
			l := material.Body2(PageTheme, label)
			l.Color = ThemeOnSurface
			l.TextSize = unit.Sp(12)
			return l.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutOptionChips(gtx, 80, chipOpt{llmBtn, "大模型", current == llm.SideEngineLLM})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layoutOptionChips(gtx, 80, chipOpt{builtinBtn, "内置 AI", current == llm.SideEngineBuiltin})
		}),
	)
}

// layoutSettingRow 对局设置行（idx=0..7；与 consumeChoices 行序一致）。
func (p *LlmVsLlmPage) layoutSettingRow(gtx layout.Context, idx int) layout.Dimensions {
	switch idx {
	case 0:
		return p.choiceTimeout.draw(gtx, "空闲超时", p.gameSettings.TimeoutSeconds, 84)
	case 1:
		return p.choiceAttempts.draw(gtx, "无效回复重试", p.gameSettings.MaxAttempts, 84)
	case 2:
		return p.choiceFallback.draw(gtx, "持续失败时", fallbackIndexOf(p.gameSettings.Fallback), 84)
	case 3:
		return p.choiceInterval.draw(gtx, "走棋间隔", p.gameSettings.IntervalSeconds, 84)
	case 4:
		return p.choiceAdvisor.draw(gtx, "引擎参谋", advisorIndexOf(p.gameSettings.AdvisorMode), 84)
	case 5:
		return p.choiceRedBlend.draw(gtx, "红方强度", p.gameSettings.RedStrengthBlend, 84)
	case 6:
		return p.choiceBlkBlend.draw(gtx, "黑方强度", p.gameSettings.BlackStrengthBlend, 84)
	case 7:
		return p.choiceAdvDiff.draw(gtx, "参谋深度", p.gameSettings.AdvisorDifficulty, 84)
	}
	return layout.Dimensions{}
}

func (p *LlmVsLlmPage) layoutOverlays(gtx layout.Context) layout.Dimensions {
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

// sideOf 红黑布尔 → rules.Side。
func sideOf(isRed bool) rules.Side {
	if isRed {
		return rules.Red
	}
	return rules.Black
}
