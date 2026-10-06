package ui

// 人机对战页（T3'.2；翻译源 = 上游 features/board/HumanVsAiPage.tsx，
// 对应 human_vs_ai_page.dart；design_docs/08 §5、00 §4、03 消费方式注记）。
//
// 与上游差异（呈现层，登记于 PROGRESS 偏离注记）：
//   - 难度/执方选择=侧板 chips（上游为头部 <select>；Gio 头部按钮行宽度限制）；
//   - AI 应手经 vm.PlayMove 即时落盘 + BoardView 视觉飞行层（上游 CSS transition
//     等价物，状态时序不变）；
//   - 取消入口（对齐上游）：无独立取消按钮——悔棋/新游戏/返回在思考中即取消
//     在途请求并丢弃迟到回执（防错 #4）。
//
// 每局一实例：由 app Router 工厂在每次导航进入时创建（铁律 #G4）；离开时
// Dispose 并取消在途 AI 请求 + 离开保存（07 §2）。
// M3' 无残局来源（initialFen 随 M6' 残局选关落地）；"保存为棋谱"/"分享棋局"
// 随 M5' 棋谱对接落地（同双人页注记）。

import (
	"fmt"
	"image"
	"log"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// HumanVsAiHooks 页面回调（app 装配接线）。
type HumanVsAiHooks struct {
	OnBack func()
}

// HumanVsAiPage 人机对战页 state struct（铁律 #G3：主 goroutine 独占）。
type HumanVsAiPage struct {
	hooks HumanVsAiHooks
	env   GameEnv

	store    *state.GameStore
	board    *BoardView
	judge    *RepetitionJudge
	ai       *EngineClient
	autoSave *state.GameAutoSave
	canSave  func() bool

	// 对局配置（执方/难度：变更即时生效——难度下一次思考用新档位；
	// 执方切换重开新局）。
	playerSide rules.Side
	difficulty int

	// AI 应手在途状态（上游 aiThinking + gameSeqRef 的 requestId 形态）：
	// aiRequestID 非空 = 在途；取消/换新请求时置空，迟到回执按 id 丢弃。
	aiRequestID string
	aiThinking  bool

	// 进页恢复（07 §2）：取档回执前锁输入（#5），回执后按决策解锁并按需触发 AI。
	restoreID      string
	restorePending bool

	// 页面状态
	confirmingNewGame bool
	elapsedSeconds    int
	toastText         string
	toastSeq          int
	disposed          bool

	timerStop chan struct{}

	// 控件
	backBtn       widget.Clickable
	newGameBtn    widget.Clickable
	undoBtn       widget.Clickable
	saveBtn       widget.Clickable
	sideRedBtn    widget.Clickable
	sideBlackBtn  widget.Clickable
	diffBtns      [5]widget.Clickable
	recordsCheck  widget.Bool
	moveList      layout.List
	newGameDialog *ModalDialog
	drawDialog    *ModalDialog
}

// chipOpt 选项 chip（选择类控件；selected=当前项高亮）。
type chipOpt struct {
	click    *widget.Clickable
	label    string
	selected bool
}

// layoutChipsRow 一行选项 chips（点击态由页面 handleEvents 消费）。
func (p *HumanVsAiPage) layoutChipsRow(gtx layout.Context, opts ...chipOpt) layout.Dimensions {
	children := make([]layout.FlexChild, 0, len(opts))
	for _, o := range opts {
		o := o
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			bg := ThemeSurface
			fg := ThemeSeedDark
			if o.selected {
				bg = ThemeSeed
				fg = ThemeSurface
			}
			return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				size := image.Point{X: gtx.Dp(unit.Dp(44)), Y: gtx.Dp(unit.Dp(26))}
				defer clip.UniformRRect(image.Rectangle{Max: size}, gtx.Dp(unit.Dp(13))).Push(gtx.Ops).Pop()
				paint.Fill(gtx.Ops, bg)
				for o.click.Clicked(gtx) {
				}
				gtx.Constraints = layout.Exact(size)
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(PageTheme, o.label)
					l.Color = fg
					l.TextSize = unit.Sp(12)
					return l.Layout(gtx)
				})
			})
		}))
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx, children...)
}

// difficultyName 难度显示名（move_source.dart:96-99）。
func difficultyName(difficulty int) string {
	switch difficulty {
	case 1:
		return "初级"
	case 2:
		return "中级"
	case 3:
		return "高级"
	case 4:
		return "专家"
	case 5:
		return "大师"
	}
	return fmt.Sprintf("%d", difficulty)
}

// aiSide AI 执方（= 对方）。
func (p *HumanVsAiPage) aiSide() rules.Side { return rules.OpponentOf(p.playerSide) }

// NewHumanVsAiPage 创建人机页（工厂：每次导航进入调用一次；玩家默认执红）。
func NewHumanVsAiPage(env GameEnv, hooks HumanVsAiHooks) *HumanVsAiPage {
	return newHumanVsAiPage(env, hooks, nil)
}

// newHumanVsAiPage 内部构造（runner 非 nil 时注入 fake——测试用，00 §4）。
func newHumanVsAiPage(env GameEnv, hooks HumanVsAiHooks, runner EngineSubmitter) *HumanVsAiPage {
	store := state.NewGameStore(state.GameStoreConfig{
		Mode:       state.ModeHumanVsAi,
		PlayerSide: rules.Red,
	})
	p := &HumanVsAiPage{
		hooks:      hooks,
		env:        env,
		store:      store,
		board:      NewBoardView(store),
		judge:      nil,
		ai:         NewEngineClient(env, runner),
		playerSide: rules.Red,
		difficulty: 3, // 上游 useState(3)
		moveList:   layout.List{Axis: layout.Vertical},
		restoreID:  env.NewRequestID("restore"),
	}
	if env.NewRequestID == nil { // 测试注入容错
		p.restoreID = "restore-test"
	}
	p.canSave = func() bool { return true } // M3' 无残局来源（防错 #6 面 M6' 落地）
	p.board.Blocked = func() bool { return p.modalOpen() }
	p.board.OnMoved = p.onPlayerMoved // 仅历史增长触发（防错 #3，BoardView 守卫）

	// 重复裁决接线（08 §3.5：玩家侧弹和棋确认框，AI 侧自动接受）
	p.judge = NewRepetitionJudge(store.VM, func(side rules.Side) bool { return side == p.playerSide }, p.showToast)
	store.VM.OnHistoryGrow = p.judge.OnHistoryGrow
	store.VM.OnHistoryRewound = p.judge.OnHistoryRewound

	// 自动保存挂接 + 进页恢复（同双人页流；恢复定局后按需 AI 先行）
	p.autoSave = state.NewGameAutoSave(state.AutoSaveConfig{
		Mode:     state.ModeHumanVsAi,
		VM:       store.VM,
		Repo:     env.Repo,
		Settings: env.Settings,
		Bus:      env.Bus,
		CanSave:  p.canSave,
	})
	p.autoSave.Attach()

	store.VM.LockInput()
	p.restorePending = true
	if env.DB != nil {
		env.DB.LoadLatestAsync(p.restoreID, state.ModeHumanVsAi)
	} else {
		p.restorePending = false
		store.VM.UnlockInput() // 无 DB 面（测试/降级）
	}

	p.newGameDialog = NewModalDialog("开始新游戏", "当前棋局将被清空，确定要开始新游戏吗？")
	p.drawDialog = NewModalDialog("三次重复局面",
		"双方连续走出相同局面，按规则可判和。可接受和棋，或变着继续对局（再次重复将强制判和）。")
	p.drawDialog.ConfirmLabel = "接受和棋"
	p.drawDialog.CancelLabel = "变着继续"

	p.startTimer()
	return p
}

// showToast 非阻塞提示（2.2s 自动消失；上游 2200ms 同款）。
func (p *HumanVsAiPage) showToast(message string) {
	p.toastSeq++
	p.toastText = message
	seq := p.toastSeq
	go func() {
		time.Sleep(2200 * time.Millisecond)
		p.env.Emit("", ToastHide{Seq: seq}, nil)
	}()
}

// startTimer/stopTimer 用时计时（同双人页：goroutine 仅读构造期写定的 env）。
func (p *HumanVsAiPage) startTimer() {
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

func (p *HumanVsAiPage) stopTimer() {
	if p.timerStop != nil {
		close(p.timerStop)
		p.timerStop = nil
	}
}

// OnAppEvent 事件总线负载消费（主 goroutine；铁律 #G3 正方向）。
func (p *HumanVsAiPage) OnAppEvent(payload any) {
	switch ev := payload.(type) {
	case DbLoadDone:
		if ev.Mode != state.ModeHumanVsAi || !p.restorePending {
			return
		}
		p.restorePending = false
		outcome := state.RestoreOrNewGame(p.store.VM, ev.Mode, ev.Saved, ev.Err, p.env.Repo)
		if outcome == state.RestoreRestored {
			p.store.VM.UnlockInput() // newGame 路径由 vm.NewGame 自解锁
		}
		// 恢复定局后若轮 AI（执黑/存档保存于 AI 思考前）→ AI 先行
		//（human_vs_ai_page.tsx 挂载 effect：必须等 restore 完成后再触发）。
		if p.store.State().Result == nil && p.store.VM.IsRedTurn() != (p.playerSide == rules.Red) {
			p.triggerAiMove()
		}
	case DbSaveDone:
		if ev.Mode != state.ModeHumanVsAi {
			return
		}
		switch {
		case ev.Err != nil:
			if ev.Manual {
				p.showToast("保存失败：本地存储不可用")
			} else {
				p.showToast("自动保存失败：本地存储不可用") // 不吞错（07 §2）
			}
		case ev.Manual:
			p.showToast("棋局已保存")
		}
	case EngineMoveDone:
		p.onAiMoveDone(ev)
	case TimerTick:
		if p.disposed {
			return
		}
		if p.store.State().Result == nil {
			p.elapsedSeconds++
		}
	case ToastHide:
		if ev.Seq == p.toastSeq {
			p.toastText = ""
		}
	}
}

// OnClose 窗口关闭请求（ClosingEvent，07 §2）：同步保存 best-effort 后放行。
func (p *HumanVsAiPage) OnClose() {
	if p.env.Settings != nil && !p.env.Settings.AutoSave {
		return
	}
	if !p.canSave() {
		return
	}
	data := p.store.VM.Serialize()
	if err := p.env.DB.SaveSync(state.ModeHumanVsAi, data.Fen, data.Moves); err != nil {
		log.Println("ui: 退出前保存失败（本地存储不可用）:", err)
	}
}

// Dispose 页面卸载（07 §2）：取消在途 AI 请求与恢复取档 → 离开保存+注销 →
// 解锁（#5）→ 停计时。
func (p *HumanVsAiPage) Dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	p.board.CancelAnim()
	p.cancelAi() // #4/#5：离页取消且丢弃迟到（上游 dispose: gameSeq++ + client.dispose）
	if p.restorePending {
		p.env.Cancel(p.restoreID)
		p.restorePending = false
	}
	p.autoSave.Dispose()
	p.store.VM.UnlockInput()
	p.stopTimer()
}

// ---- AI 应手调度（human_vs_ai_page.tsx triggerAiMove 的 requestId 形态）----

// triggerAiMove 发起 AI 应手：lockInput → Runner 异步搜索 → 回执无条件解锁
// （P0-1）；迟到回执由 requestId 丢弃（#4/#G5）。
func (p *HumanVsAiPage) triggerAiMove() {
	if p.store.VM.IsFinished() {
		return
	}
	p.cancelAi() // 防御：至多一个在途搜索（Runner 串行契约）
	id := p.env.NewRequestID("ai")
	p.aiRequestID = id
	p.aiThinking = true
	// DR-018：对局方 fenHistory 拷贝传入（重复治理 L1/L2 随复制物内置）
	snap := p.store.State()
	p.store.VM.LockInput()
	p.ai.FindBestMoveAsync(id, snap.Fen, p.difficulty, snap.FenHistory)
}

// cancelAi 取消在途 AI 请求并解锁（新游戏/换执方/离页路径；幂等）：Runner ctx
// 取消 + 总线取消（迟到回执丢弃）+ 解锁（#5）。
func (p *HumanVsAiPage) cancelAi() {
	p.abandonAi()
	p.store.VM.UnlockInput()
}

// abandonAi 仅作废在途 AI 请求（不解锁——思考中悔棋路径的锁由重触发接管，
// 上游 gameSeq++ 语义）。
func (p *HumanVsAiPage) abandonAi() {
	if p.aiRequestID == "" {
		return
	}
	p.ai.Cancel(p.aiRequestID)
	p.aiRequestID = ""
	p.aiThinking = false
}

// onAiMoveDone AI 应手回执（主 goroutine）：无论作废与否一律解锁（P0-1）；
// requestId 不匹配 = 迟到/非当前请求 → 丢弃（总线已丢取消 id，此处防御）。
func (p *HumanVsAiPage) onAiMoveDone(ev EngineMoveDone) {
	if ev.RequestID != p.aiRequestID {
		return
	}
	p.aiRequestID = ""
	p.aiThinking = false
	p.store.VM.UnlockInput()
	if ev.Err != nil {
		// 引擎失败防软死锁：显式判 AI 负（08 防错 #7；上游 status==='failed'）
		p.showToast("引擎计算失败：" + ev.Err.Error())
		p.store.VM.Resign(p.aiSide())
		return
	}
	if ev.Move == nil {
		return // noLegalMove：将死/困毙由棋盘状态呈现胜负
	}
	// playMove 最终校验（铁律 #G10）；拒绝仅防御性提示，不 crash
	if !p.store.VM.PlayMove(ev.Move.From, ev.Move.To) {
		p.showToast("AI 应手被拒绝")
		return
	}
	p.board.AnimateMoveVisual(*ev.Move)
}

// onPlayerMoved 玩家走子完成（BoardView OnMoved：仅历史增长才触发，防错 #3）。
func (p *HumanVsAiPage) onPlayerMoved() {
	if p.store.State().Result != nil {
		return
	}
	if p.store.VM.IsRedTurn() == (p.playerSide == rules.Red) {
		return // 仍轮玩家（防御）
	}
	p.triggerAiMove()
}

// ---- 交互 ----

func (p *HumanVsAiPage) handleEvents(gtx layout.Context) {
	// 弹窗打开：遮罩拦截页面级交互（仅消费本帧点击边沿，防穿透——含侧板
	// chips：执方切换/难度变更不得在弹窗下生效，KG-008 同类面收口）
	if p.modalOpen() {
		p.backBtn.Clicked(gtx)
		p.newGameBtn.Clicked(gtx)
		p.undoBtn.Clicked(gtx)
		p.saveBtn.Clicked(gtx)
		p.sideRedBtn.Clicked(gtx)
		p.sideBlackBtn.Clicked(gtx)
		for i := range p.diffBtns {
			p.diffBtns[i].Clicked(gtx)
		}
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
	if p.saveBtn.Clicked(gtx) {
		p.saveGame()
	}
	for i := range p.diffBtns {
		if p.diffBtns[i].Clicked(gtx) {
			p.difficulty = i + 1 // 难度变更即时生效（下一次思考用新档位）
		}
	}
	if p.sideRedBtn.Clicked(gtx) {
		p.switchSide(rules.Red)
	}
	if p.sideBlackBtn.Clicked(gtx) {
		p.switchSide(rules.Black)
	}
}

// modalOpen 任一模态弹窗打开中。
func (p *HumanVsAiPage) modalOpen() bool {
	return p.confirmingNewGame || (p.judge != nil && p.judge.DrawOffer != nil)
}

// cancelPendingRestore 取消在途恢复并解锁。
func (p *HumanVsAiPage) cancelPendingRestore() {
	if !p.restorePending {
		return
	}
	p.env.Cancel(p.restoreID) // 迟到回执按 requestId 丢弃（#G5）
	p.restorePending = false
	p.store.VM.UnlockInput()
}

// doNewGame 新游戏确认后的重置（human_vs_ai_page.tsx newGame；M3' 无残局来源）。
func (p *HumanVsAiPage) doNewGame() {
	p.cancelAi()
	p.cancelPendingRestore()
	p.board.CancelAnim() // 动画中开新局：作废不落子（#4）
	p.store.VM.NewGame() // 触发 OnHistoryRewound → 裁决重置
	p.elapsedSeconds = 0
	// 执黑：开局即 AI 先行
	if p.store.State().Result == nil && p.store.VM.IsRedTurn() != (p.playerSide == rules.Red) {
		p.triggerAiMove()
	}
}

// switchSide 执方切换：取消在途 + 重开新局 + AI 先行（上游 switchSide）。
func (p *HumanVsAiPage) switchSide(side rules.Side) {
	if side == p.playerSide {
		return
	}
	p.cancelAi()
	p.cancelPendingRestore()
	p.board.CancelAnim()
	p.playerSide = side
	p.store.VM.NewGame()
	p.elapsedSeconds = 0
	if p.store.State().Result == nil && p.store.VM.IsRedTurn() != (side == rules.Red) {
		p.triggerAiMove() // AI 先行
	}
}

// undoMove 悔一整轮（人机口径：同时撤 AI 应手与玩家最近一手）。
// 思考中点悔棋（上游语义）：作废在途应手后 undoRound 因输入锁 no-op，
// 轮 AI → 重新触发（等价"中止重想"，悔棋不生效）；非思考路径正常悔棋。
func (p *HumanVsAiPage) undoMove() {
	p.abandonAi()
	p.cancelPendingRestore()
	p.board.CancelAnim()
	p.store.VM.UndoRound(p.playerSide)
	if p.store.State().Result == nil && p.store.VM.IsRedTurn() != (p.playerSide == rules.Red) {
		p.triggerAiMove()
		return
	}
	if p.aiRequestID == "" {
		p.store.VM.UnlockInput() // 防御：未触发 AI 且无在途请求，不得残留输入锁（#5）
	}
}

// saveGame 手动保存（不受全局开关限制；完成/失败经回执 toast）。
func (p *HumanVsAiPage) saveGame() {
	if !p.canSave() {
		p.showToast("残局来源不写入对局存档")
		return
	}
	data := p.store.VM.Serialize()
	p.env.DB.SaveAsync(p.env.NewRequestID("save"), state.ModeHumanVsAi, data.Fen, data.Moves, true)
}

// ---- 布局 ----

// Layout 页面布局：头部按钮行 + 状态条 + 棋盘/侧板 + 弹窗与 toast 浮层。
func (p *HumanVsAiPage) Layout(gtx layout.Context) layout.Dimensions {
	p.handleEvents(gtx)
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	fillRect(gtx.Ops, image.Rectangle{Max: gtx.Constraints.Max}, ThemeSurface)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(p.layoutContent),
		layout.Stacked(p.layoutOverlays),
	)
}

func (p *HumanVsAiPage) layoutContent(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutHeader),
		layout.Rigid(p.layoutStatusBar),
		layout.Flexed(1, p.layoutBody),
	)
}

// layoutHeader 头部：返回/标题/操作按钮（上游 cc-game-header）。
func (p *HumanVsAiPage) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(p.simpleButton(&p.backBtn, "返回", false)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					t := material.H6(PageTheme, "人机对战")
					t.Color = ThemeOnSurface
					return t.Layout(gtx)
				})
			}),
			layout.Rigid(p.simpleButton(&p.newGameBtn, "新游戏", true)),
			layout.Rigid(p.simpleButton(&p.undoBtn, "悔棋", false)),
			layout.Rigid(p.simpleButton(&p.saveBtn, "保存棋局", false)),
		)
	})
}

func (p *HumanVsAiPage) simpleButton(c *widget.Clickable, label string, primary bool) func(gtx layout.Context) layout.Dimensions {
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

// layoutStatusBar 状态条四态（human_vs_ai_page.dart:_buildAiStatus）：对局结束 /
// AI 思考中 / 被将军 / 等待玩家。取消入口（对齐上游）：悔棋/新游戏/返回在思考中
// 即取消在途请求，不设独立取消按钮。
func (p *HumanVsAiPage) layoutStatusBar(gtx layout.Context) layout.Dimensions {
	snap := p.store.State()
	aiTurnNow := snap.Result == nil && snap.IsRedTurn != (p.playerSide == rules.Red)

	statusText := ""
	bg := ThemeSurfaceDim
	fg := ThemeOnSurface
	switch {
	case snap.Result != nil:
		statusText = "对局结束：" + resultText(*snap.Result)
		bg = ThemeSurfaceDim
	case p.aiThinking || aiTurnNow:
		statusText = "AI 正在思考..."
		bg = ThemeSeed
		fg = ThemeSurface
	case snap.IsCheck:
		// 被将军方=轮走方（上游 checkedSide = isRedTurn ? '红' : '黑'）
		checkedSide := "红"
		if !snap.IsRedTurn {
			checkedSide = "黑"
		}
		statusText = fmt.Sprintf("等待玩家（%s方）走棋（%s方被将军！）", sideName(p.playerSide), checkedSide)
		bg = ThemeCheckWarn
		fg = ThemeSurface
	default:
		statusText = fmt.Sprintf("等待玩家（%s方）走棋", sideName(p.playerSide))
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

// layoutBody 棋盘区（自适应）+ 侧板信息卡（定宽）。
func (p *HumanVsAiPage) layoutBody(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, p.board.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			w := gtx.Dp(unit.Dp(280))
			gtx.Constraints.Min.X = w
			gtx.Constraints.Max.X = w
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, p.layoutInfoCard)
		}),
	)
}

// layoutInfoCard 信息卡：结算横幅/回合/将军/步数/用时 + 走法记录 + 对手设置
// （执方/难度 chips；引擎类型 M3' 恒为内置 AI）。
func (p *HumanVsAiPage) layoutInfoCard(gtx layout.Context) layout.Dimensions {
	snap := p.store.State()
	defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurfaceDim)

	return layout.UniformInset(unit.Dp(14)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{}
		if snap.Result != nil {
			r := *snap.Result
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return DrawResultBanner(gtx, &r)
				})
			}))
		}
		title := material.Body1(PageTheme, "游戏信息")
		title.Color = ThemeSeedDark
		title.TextSize = unit.Sp(13)
		children = append(children, layout.Rigid(title.Layout))

		turnLabel := "红方"
		turnColor := ThemePieceRed
		if !snap.IsRedTurn {
			turnLabel = "黑方"
			turnColor = ThemePieceBlack
		}
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Body1(PageTheme, "当前回合: ")
						l.Color = ThemeOnSurface
						return l.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Body1(PageTheme, turnLabel)
						l.Color = turnColor
						return l.Layout(gtx)
					}),
				)
			})
		}))
		if snap.IsCheck && snap.Result == nil {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, "将军！")
				l.Color = ThemeError
				return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, l.Layout)
			}))
		}
		children = append(children, layout.Rigid(p.infoLine(fmt.Sprintf("步数: %d", len(snap.MoveHistory)))))
		children = append(children, layout.Rigid(p.infoLine(formatTime(p.elapsedSeconds))))

		// 对手设置（引擎类型 M3' 恒为内置 AI；执方/难度选择）
		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Body2(PageTheme, "对手")
						l.Color = ThemeSeedDark
						l.TextSize = unit.Sp(13)
						return l.Layout(gtx)
					}),
					layout.Rigid(p.infoLine(fmt.Sprintf("内置 AI（%s）", difficultyName(p.difficulty)))),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return p.layoutChipsRow(gtx,
								chipOpt{&p.sideRedBtn, "执红", p.playerSide == rules.Red},
								chipOpt{&p.sideBlackBtn, "执黑", p.playerSide == rules.Black},
							)
						})
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							opts := [5]chipOpt{}
							for i := range p.diffBtns {
								opts[i] = chipOpt{&p.diffBtns[i], difficultyName(i + 1), p.difficulty == i+1}
							}
							return p.layoutChipsRow(gtx, opts[0], opts[1], opts[2], opts[3], opts[4])
						})
					}),
				)
			})
		}))

		children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cb := material.CheckBox(PageTheme, &p.recordsCheck, "显示走法记录")
				cb.Color = ThemeSeed
				return cb.Layout(gtx)
			})
		}))
		if p.recordsCheck.Value {
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, p.layoutMoveRecords)
			}))
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

func (p *HumanVsAiPage) infoLine(text string) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(PageTheme, text)
		l.Color = ThemeOnSurface
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
}

// layoutMoveRecords 走法记录双列列表（同双人页）。
func (p *HumanVsAiPage) layoutMoveRecords(gtx layout.Context) layout.Dimensions {
	moves := p.store.State().MoveHistory
	if len(moves) == 0 {
		l := material.Body2(PageTheme, "暂无走法")
		l.Color = ThemeSeedDark
		return l.Layout(gtx)
	}
	maxH := gtx.Dp(unit.Dp(200))
	if gtx.Constraints.Max.Y > maxH {
		gtx.Constraints.Max.Y = maxH
	}
	gtx.Constraints.Min.Y = 0
	rows := (len(moves) + 1) / 2
	return p.moveList.Layout(gtx, rows, func(gtx layout.Context, i int) layout.Dimensions {
		red := moves[i*2]
		return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(PageTheme, fmt.Sprintf("%d.", i+1))
					l.Color = ThemeSeedDark
					return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(PageTheme, formatMoveText(red))
					l.Color = ThemePieceRed
					return layout.Inset{Right: unit.Dp(10)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if i*2+1 >= len(moves) {
						return layout.Dimensions{}
					}
					l := material.Body2(PageTheme, formatMoveText(moves[i*2+1]))
					l.Color = ThemePieceBlack
					return l.Layout(gtx)
				}),
			)
		})
	})
}

// layoutOverlays 弹窗层 + toast（Stack 顶层；弹窗打开时棋盘经 Blocked 禁手）。
func (p *HumanVsAiPage) layoutOverlays(gtx layout.Context) layout.Dimensions {
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
	// 三次重复判和确认框（面对方为玩家时弹出，#12；AI 侧自动接受）
	if p.judge.DrawOffer != nil {
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
