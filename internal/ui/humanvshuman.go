package ui

// 双人对弈页（T2'.4；翻译源 = 上游 features/board/HumanVsHumanPage.tsx，
// 对应 human_vs_human_page.dart；design_docs/08 §3/§4/§5）。
//
// 每局一实例：由 app Router 工厂在每次导航进入时创建（铁律 #G4），
// 离开时 Dispose 并触发离开保存（07 §2）。
// M2' 无棋谱续战来源（initialFen 路径随 M5' 棋谱库落地），canSave 恒允许。
// "保存为棋谱"/"分享棋局"随 M5' 棋谱对接落地（协议面 writeShareText 快照测试
// 在 09 §1 pgnWriter 翻译清单内，见 PROGRESS 记录）。

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

// HumanVsHumanHooks 页面回调（app 装配接线）。
type HumanVsHumanHooks struct {
	OnBack func()
}

// HumanVsHumanPage 双人对弈页 state struct（铁律 #G3：主 goroutine 独占）。
type HumanVsHumanPage struct {
	hooks HumanVsHumanHooks
	env   GameEnv

	store    *state.GameStore
	board    *BoardView
	judge    *RepetitionJudge
	autoSave *state.GameAutoSave
	canSave  func() bool

	// 进页恢复（07 §2）：取档回执前锁输入（#5），回执后按决策解锁/开新局。
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
	recordsCheck  widget.Bool
	moveList      layout.List
	newGameDialog *ModalDialog
	drawDialog    *ModalDialog
}

// NewHumanVsHumanPage 创建双人页（工厂：每次导航进入调用一次）。
// 创建即发起恢复取档并挂接自动保存（07 §2 进页语义）。
func NewHumanVsHumanPage(env GameEnv, hooks HumanVsHumanHooks) *HumanVsHumanPage {
	store := state.NewGameStore(state.GameStoreConfig{Mode: state.ModeHumanVsHuman})
	p := &HumanVsHumanPage{
		hooks:     hooks,
		env:       env,
		store:     store,
		board:     NewBoardView(store),
		moveList:  layout.List{Axis: layout.Vertical},
		restoreID: env.NewRequestID("restore"),
	}
	if env.NewRequestID == nil { // 测试注入容错
		p.restoreID = "restore-test"
	}
	p.canSave = func() bool { return true } // M2' 无棋谱续战来源
	p.board.Blocked = func() bool { return p.modalOpen() }

	// 重复裁决接线（08 §3.5：双方均为玩家，三次重复判和弹确认框）
	p.judge = NewRepetitionJudge(store.VM, func(rules.Side) bool { return true }, p.showToast)
	store.VM.OnHistoryGrow = p.judge.OnHistoryGrow
	store.VM.OnHistoryRewound = p.judge.OnHistoryRewound

	// 自动保存挂接（07 §2：blur/minimize ← 生命周期总线；dispose ← 页面卸载）
	p.autoSave = state.NewGameAutoSave(state.AutoSaveConfig{
		Mode:     state.ModeHumanVsHuman,
		VM:       store.VM,
		Repo:     env.Repo,
		Settings: env.Settings,
		Bus:      env.Bus,
		CanSave:  p.canSave,
	})
	p.autoSave.Attach()

	// 进页恢复：先锁输入，取档回执后应用决策（restore.go；存储异常降级开新局）
	store.VM.LockInput()
	p.restorePending = true
	if env.DB != nil {
		env.DB.LoadLatestAsync(p.restoreID, state.ModeHumanVsHuman)
	} else {
		p.restorePending = false
		store.VM.UnlockInput() // 无 DB 面（测试/降级）：直接可玩
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
func (p *HumanVsHumanPage) showToast(message string) {
	p.toastSeq++
	p.toastText = message
	seq := p.toastSeq
	go func() {
		time.Sleep(2200 * time.Millisecond)
		p.env.Emit("", ToastHide{Seq: seq}, nil)
	}()
}

// startTimer 用时计时（1s tick；终局后页面忽略累计——human_vs_human_page.dart:86-99）。
// goroutine 仅读构造期写定的 p.env 与局部 stop 通道（无共享字段访问，-race 干净）。
func (p *HumanVsHumanPage) startTimer() {
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

func (p *HumanVsHumanPage) stopTimer() {
	if p.timerStop != nil {
		close(p.timerStop)
		p.timerStop = nil
	}
}

// OnAppEvent 事件总线负载消费（主 goroutine；铁律 #G3 正方向）。
func (p *HumanVsHumanPage) OnAppEvent(payload any) {
	switch ev := payload.(type) {
	case DbLoadDone:
		if ev.Mode != state.ModeHumanVsHuman || !p.restorePending {
			return // 他页回执或已取消（迟到期由总线丢弃，这里防御）
		}
		p.restorePending = false
		outcome := state.RestoreOrNewGame(p.store.VM, ev.Mode, ev.Saved, ev.Err, p.env.Repo)
		if outcome == state.RestoreRestored {
			p.store.VM.UnlockInput() // newGame 路径由 vm.NewGame 自解锁
		}
	case DbSaveDone:
		if ev.Mode != state.ModeHumanVsHuman {
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

// OnClose 窗口关闭请求（ClosingEvent，07 §2）：同步保存 best-effort
// （K16 有界等待的 Go 对应）后放行退出；受全局开关与 canSave 约束。
func (p *HumanVsHumanPage) OnClose() {
	if p.env.Settings != nil && !p.env.Settings.AutoSave {
		return
	}
	if !p.canSave() {
		return
	}
	data := p.store.VM.Serialize()
	if err := p.env.DB.SaveSync(state.ModeHumanVsHuman, data.Fen, data.Moves); err != nil {
		log.Println("ui: 退出前保存失败（本地存储不可用）:", err)
	}
}

// Dispose 页面卸载（07 §2）：动画作废 → 取消在途恢复 → 离开保存+注销 → 解锁 → 停计时。
func (p *HumanVsHumanPage) Dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	p.board.CancelAnim()
	if p.restorePending {
		p.env.Cancel(p.restoreID)
		p.restorePending = false
	}
	p.autoSave.Dispose()
	p.store.VM.UnlockInput() // #5：dispose 解锁
	p.stopTimer()
}

// ---- 交互 ----

func (p *HumanVsHumanPage) handleEvents(gtx layout.Context) {
	// 弹窗打开：遮罩拦截页面级交互（上游 .cc-dialog-mask 全屏阻断）——
	// 仅消费本帧点击边沿，避免遮罩下的按钮被穿透触发。
	if p.modalOpen() {
		p.backBtn.Clicked(gtx)
		p.newGameBtn.Clicked(gtx)
		p.undoBtn.Clicked(gtx)
		p.saveBtn.Clicked(gtx)
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
}

// modalOpen 任一模态弹窗打开中。
func (p *HumanVsHumanPage) modalOpen() bool {
	return p.confirmingNewGame || (p.judge != nil && p.judge.DrawOffer != nil)
}

// cancelPendingRestore 取消在途恢复并解锁（新局/悔棋在恢复回执前发起的路径）。
func (p *HumanVsHumanPage) cancelPendingRestore() {
	if !p.restorePending {
		return
	}
	p.env.Cancel(p.restoreID) // 迟到回执按 requestId 丢弃（#G5）
	p.restorePending = false
	p.store.VM.UnlockInput()
}

// doNewGame 新游戏确认后的重置（human_vs_human_page.dart newGame）。
func (p *HumanVsHumanPage) doNewGame() {
	p.cancelPendingRestore()
	p.board.CancelAnim() // 动画中开新局：作废不落子（#4）
	p.store.VM.NewGame() // 触发 OnHistoryRewound → 裁决重置 declined/未决框
	p.elapsedSeconds = 0
}

// undoMove 双人页悔单手（08 §3.2）。
func (p *HumanVsHumanPage) undoMove() {
	p.cancelPendingRestore()
	p.board.CancelAnim()
	p.store.VM.Undo()
}

// saveGame 手动保存（"保存棋局"按钮）：不受全局开关限制（game_auto_save.dart:55），
// 完成/失败经回执 toast。
func (p *HumanVsHumanPage) saveGame() {
	if !p.canSave() {
		p.showToast("棋谱续战来源不写入对局存档")
		return
	}
	data := p.store.VM.Serialize()
	p.env.DB.SaveAsync(p.env.NewRequestID("save"), state.ModeHumanVsHuman, data.Fen, data.Moves, true)
}

// ---- 布局 ----

// Layout 页面布局：头部按钮行 + 棋盘/侧板 + 弹窗与 toast 浮层。
func (p *HumanVsHumanPage) Layout(gtx layout.Context) layout.Dimensions {
	p.handleEvents(gtx)
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	fillRect(gtx.Ops, image.Rectangle{Max: gtx.Constraints.Max}, ThemeSurface)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(p.layoutContent),
		layout.Stacked(p.layoutOverlays),
	)
}

func (p *HumanVsHumanPage) layoutContent(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutHeader),
		layout.Flexed(1, p.layoutBody),
	)
}

// layoutHeader 头部：返回/标题/操作按钮（上游 cc-game-header）。
func (p *HumanVsHumanPage) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(p.simpleButton(&p.backBtn, "返回", false)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					t := material.H6(PageTheme, "双人对弈")
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

func (p *HumanVsHumanPage) simpleButton(c *widget.Clickable, label string, primary bool) func(gtx layout.Context) layout.Dimensions {
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

// layoutBody 棋盘区（自适应）+ 侧板信息卡（定宽）。
func (p *HumanVsHumanPage) layoutBody(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, p.board.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			// 280dp 为含 8dp 内边距的总宽（约束加在 Inset 外层——加在内层会
			// 溢出窗口右缘，侧板卡片被裁，M3' 验收实证后一并修复）
			w := gtx.Dp(unit.Dp(280))
			gtx.Constraints.Min.X = w
			gtx.Constraints.Max.X = w
			return layout.UniformInset(unit.Dp(8)).Layout(gtx, p.layoutInfoCard)
		}),
	)
}

// layoutInfoCard 信息卡：结算横幅/回合/步数/用时/走法记录开关与列表（上游 cc-game-info）。
func (p *HumanVsHumanPage) layoutInfoCard(gtx layout.Context) layout.Dimensions {
	snap := p.store.State()
	defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurfaceDim)

	return layout.UniformInset(unit.Dp(14)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{}
		// 终局结算横幅（k=3 判负/判和复用，#12）
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

		// 当前回合（含将军标记）
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
		// 步数
		moveCount := len(snap.MoveHistory)
		children = append(children, layout.Rigid(p.infoLine(fmt.Sprintf("步数: %d", moveCount))))
		// 用时
		children = append(children, layout.Rigid(p.infoLine(formatTime(p.elapsedSeconds))))

		// 显示走法记录开关
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

func (p *HumanVsHumanPage) infoLine(text string) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(PageTheme, text)
		l.Color = ThemeOnSurface
		return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
}

// layoutMoveRecords 走法记录双列列表（side_panel.dart:178-207；限高滚动）。
func (p *HumanVsHumanPage) layoutMoveRecords(gtx layout.Context) layout.Dimensions {
	moves := p.store.State().MoveHistory
	if len(moves) == 0 {
		l := material.Body2(PageTheme, "暂无走法")
		l.Color = ThemeSeedDark
		return l.Layout(gtx)
	}
	maxH := gtx.Dp(unit.Dp(260))
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
func (p *HumanVsHumanPage) layoutOverlays(gtx layout.Context) layout.Dimensions {
	// 新游戏确认框
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
	// 三次重复判和确认框（双人页双方均为玩家 → 弹框）
	if p.judge.DrawOffer != nil {
		confirmed, canceled := p.drawDialog.LayoutFull(gtx)
		switch {
		case confirmed:
			p.judge.AcceptDraw()
		case canceled:
			p.judge.DeclineDraw()
		}
	}
	// toast（非阻塞，最后绘制在最顶层）
	if p.toastText != "" {
		DrawToast(gtx, p.toastText)
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// formatTime 秒 → MM:SS（side_panel.tsx formatTime）。
func formatTime(seconds int) string {
	minutes := seconds / 60
	rest := seconds % 60
	return fmt.Sprintf("%02d:%02d", minutes, rest)
}
