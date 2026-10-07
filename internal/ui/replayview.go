package ui

// 语料重放器（T5'.3 + T6'.5，翻译源 = 上游 frontend/src/features/puzzle/
// PuzzleDetailView.tsx 完整演示播放器；状态机 = state.PuzzleDemoPlayer 翻译件）。
// M6' 升级：播放/暂停/停止状态机、速度档位 0.5x/1x/2x、自定义间隔 200–4000ms
// （滑块，覆盖倍率回 1x）、循环播放；步进与中文记谱芯片跳转保留（跳转即停止
// 播放回到手动浏览）。
//
// 自动播放计时（Gio 形态）：goroutine + stop 通道 + ReplayTick 事件回主循环
//（00 §4 `replay:tick`；铁律 #G3——goroutine 只触碰自有通道与 emit），
// 每拍前读播放器 CurrentInterval()（速度/间隔切换下一拍生效）；
// 手动步进/换局/暂停/离开均 close stop 并 gen++（迟到 tick 按代次丢弃，#G5）。

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// ReplayAutoPlayInterval 自动播放步进间隔（上游 puzzleDemo 默认 800ms/步）。
const ReplayAutoPlayInterval = 800 * time.Millisecond

// ReplayView 重放器 state struct（主 goroutine 独占，铁律 #G3）。
type ReplayView struct {
	// OnBattle 进入对战回调（模式 + 起点 FEN + 玩家执方；nil = 不启用入口）。
	OnBattle func(mode BattleMode, fen, side string)
	// emit 事件提交面（页面注入 env.Emit；ReplayTick 回主循环 OnTick）。
	emit func(requestID string, payload any, err error)

	player   *state.PuzzleDemoPlayer // M6'：演示状态机（速度/循环/间隔）
	puzzle   *state.ParsedPuzzleView
	moves    []rules.Move
	notation []string

	pos  int // 手动浏览位置（0..len(moves)；播放中被 player 接管）
	gen  int // 播放代次（stop/手动/换局递增）
	stop chan struct{}

	// 演示控制（速度档位/循环/自定义间隔滑块）
	speedClicks   [3]widget.Clickable
	loopCheck     widget.Bool
	intervalFloat widget.Float // 0..1，映射 200–4000ms（步进 100）

	// 控件
	playBtn    widget.Clickable
	stopBtn    widget.Clickable
	toStartBtn widget.Clickable
	prevBtn    widget.Clickable
	nextBtn    widget.Clickable
	endBtn     widget.Clickable
	battleBtn  widget.Clickable
	exportBtn  widget.Clickable
	launcher   BattleLauncher
	moveList   layout.List
	rowClicks  map[int]*widget.Clickable

	exportText   string // 非空 = 待写剪贴板文本
	exportViaGio bool   // PowerShell 通道失败 → 回退 gio WriteCmd（非 WSL 面）
	clipSeq      int
	message      string // 状态行（已复制/错误）
}

// NewReplayView 创建重放器（emit 可 nil = 测试同步场景）。
func NewReplayView(emit func(requestID string, payload any, err error)) *ReplayView {
	return &ReplayView{
		emit:      emit,
		player:    state.NewPuzzleDemoPlayer(),
		moveList:  layout.List{Axis: layout.Vertical},
		rowClicks: map[int]*widget.Clickable{},
	}
}

// demoActive 演示是否接管呈现（playing/paused/completed）。
func (r *ReplayView) demoActive() bool {
	st := r.player.Snapshot().Status
	return st != state.DemoIdle && st != state.DemoError
}

// speedIndexOf 当前速度倍率 → chips 下标（0.5/1/2）。
func (r *ReplayView) speedIndexOf() int {
	switch r.player.Snapshot().Params.SpeedMultiplier {
	case state.DemoSlow:
		return 0
	case state.DemoFast:
		return 2
	}
	return 1
}

// Puzzle 当前详情（nil = 未打开）。
func (r *ReplayView) Puzzle() *state.ParsedPuzzleView { return r.puzzle }

// SetPuzzle 打开/更换详情（换局：停播放、归零；速度/循环参数保留——spec ⑥）。
func (r *ReplayView) SetPuzzle(v *state.ParsedPuzzleView) {
	r.haltPlay()
	r.puzzle = v
	r.moves = nil
	r.notation = nil
	r.pos = 0
	r.message = ""
	r.launcher.Close()
	r.rowClicks = map[int]*widget.Clickable{}
	if v == nil {
		return
	}
	r.player.InitializePuzzle(v.InitialFen, v.SolutionMoves)
	for _, code := range v.SolutionMoves {
		if ft := parsers.ParseIccs(code); ft != nil {
			r.moves = append(r.moves, rules.Move{From: ft.From, To: ft.To})
		}
	}
	r.notation = state.ChineseNotations(v.InitialFen, r.moves)
}

// Pos 当前显示位置（demo 接管时 = 已走步数；手动浏览 = pos；测试断言面）。
func (r *ReplayView) Pos() int {
	if r.demoActive() {
		if n := r.player.Snapshot().CurrentMoveIndex + 1; n < len(r.moves) {
			return n
		}
		return len(r.moves)
	}
	return r.pos
}

// Playing 自动播放中。
func (r *ReplayView) Playing() bool { return r.player.Snapshot().Status == state.DemoPlaying }

// completed 播放到末尾（"演示完毕"）。
func (r *ReplayView) completed() bool { return r.player.Snapshot().Status == state.DemoCompleted }

// replayFen 重放第 n 着后的局面 FEN（board_view_replay.dart:106-115 同语义：
// 与局面不符的走法止损）。
func (r *ReplayView) replayFen(n int) (string, *rules.Move) {
	board, err := rules.FromFen(r.puzzle.InitialFen)
	if err != nil {
		return r.puzzle.InitialFen, nil
	}
	last := (*rules.Move)(nil)
	for i := 0; i < n && i < len(r.moves); i++ {
		if board.PieceAtP(r.moves[i].From) == nil {
			break
		}
		board.ApplyMove(rules.Move{From: r.moves[i].From, To: r.moves[i].To})
		m := r.moves[i]
		last = &m
	}
	return board.ToFen(), last
}

// Jump 手动跳转（跳转即停止播放回到手动浏览——上游 jump 语义）。
func (r *ReplayView) Jump(n int) {
	r.haltPlay()
	r.player.StopDemo()
	if r.puzzle == nil {
		return
	}
	if n < 0 {
		n = 0
	}
	if n > len(r.moves) {
		n = len(r.moves)
	}
	r.pos = n
}

// TogglePlay 播放/暂停（上游 togglePlay：playing→暂停；paused→继续；
// idle/completed → 从头播放）。
func (r *ReplayView) TogglePlay() {
	if r.puzzle == nil {
		return
	}
	switch r.player.Snapshot().Status {
	case state.DemoPlaying:
		r.haltPlay()
		return
	case state.DemoPaused:
		r.player.ResumeDemo()
		r.startTicker()
		return
	}
	if len(r.moves) == 0 {
		return
	}
	r.player.StopDemo()
	r.player.StartDemo()
	r.startTicker()
}

// Stop 停止（回开局；上游 stopDemo 语义）。
func (r *ReplayView) Stop() {
	r.haltPlay()
	r.player.StopDemo()
	r.pos = 0
}

// haltPlay 暂停自动播放（保留当前位置——暂停语义；状态机进 paused）。
func (r *ReplayView) haltPlay() {
	r.player.PauseDemo()
	r.gen++
	if r.stop != nil {
		close(r.stop)
		r.stop = nil
	}
}

// startTicker 启动自动播放计时（goroutine 只触碰自有通道与捕获值，铁律 #G3）；
// 间隔在启动时捕获（速度/自定义间隔变更由 demoControls 变更点重建 ticker——
// 下一拍按新间隔生效；跨 goroutine 直读 player 字段=数据竞争，禁止）；
// 迟到 tick 由 OnTick 按 Gen 代次丢弃（#G5）。
func (r *ReplayView) startTicker() {
	r.gen++
	gen := r.gen
	interval := r.player.CurrentInterval() // 捕获值：goroutine 不读 player
	stop := make(chan struct{})
	r.stop = stop
	if r.emit == nil {
		return // 测试注入：直接调 OnTick
	}
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Duration(interval) * time.Millisecond):
			}
			r.emit("", ReplayTick{Gen: gen}, nil)
		}
	}()
}

// restartTickerIfPlaying 速度/间隔变更点调用：播放中重建 ticker（新间隔
// 下一拍生效）；暂停/idle 态无 ticker，无需处理。
func (r *ReplayView) restartTickerIfPlaying() {
	if !r.Playing() {
		return
	}
	if r.stop != nil {
		close(r.stop)
		r.stop = nil
	}
	r.startTicker()
}

// OnTick 自动播放步进回执（主 goroutine；Gen 过期忽略，#G5）：
// 状态机推进一步；completed/error 即停拍（循环播放由状态机重置续播）。
func (r *ReplayView) OnTick(ev ReplayTick) {
	if ev.Gen != r.gen || !r.Playing() {
		return
	}
	r.player.Advance()
	if st := r.player.Snapshot().Status; st != state.DemoPlaying {
		r.haltPlay()
	}
}

// copyAsync 异步写 Windows 剪贴板（KG-004 反方向；回执经事件总线）。
func (r *ReplayView) copyAsync(text string) {
	if r.emit == nil {
		return // 测试场景
	}
	r.clipSeq++
	seq := r.clipSeq
	CopyToWindowsClipboardAsync(text, func(err error) {
		r.emit("", ClipWriteDone{Seq: seq, Err: err}, nil)
	})
}

// OnClipDone 剪贴板写回执（Seq 过期忽略；失败回退 gio WriteCmd）。
func (r *ReplayView) OnClipDone(ev ClipWriteDone) {
	if ev.Seq != r.clipSeq || r.exportText == "" {
		return
	}
	if ev.Err != nil {
		r.exportViaGio = true
		return
	}
	r.exportText = ""
	r.message = "PGN 已复制到剪贴板"
}

// Dispose 离开页面（停播放；铁律 #G5）。
func (r *ReplayView) Dispose() { r.haltPlay() }

// BattleFen 进入对战起点 FEN（recordBattle.ts battleStartFen 语料等价）。
func (r *ReplayView) BattleFen() string {
	if r.puzzle == nil {
		return ""
	}
	return BattleStartFen(r.puzzle, r.moves)
}

// ExportRecord 导出用棋谱数据（语料 → GameRecordData 投影；结果恒 *——
// 语料无 result/solveStatus 面）。
func (r *ReplayView) ExportRecord() state.GameRecordData {
	mode := string(state.ModeHumanVsHuman)
	if r.puzzle.Endgame {
		mode = "endgame"
	}
	return state.GameRecordData{
		Title:       derefStr(r.puzzle.Title, "未命名"),
		Mode:        mode,
		InitialFen:  r.puzzle.InitialFen,
		Moves:       r.moves,
		SolveStatus: state.SolveNone,
		Solutions:   [][]string{},
	}
}

// handleEvents 控制条输入消费（主 goroutine）。
func (r *ReplayView) handleEvents(gtx layout.Context) {
	if r.puzzle == nil {
		return
	}
	for i := range r.speedClicks {
		if r.speedClicks[i].Clicked(gtx) {
			// 速度档位 0.5x/1x/2x：播放中即时生效（重建 ticker，下一拍新间隔）
			switch i {
			case 0:
				r.player.SetSpeedMultiplier(state.DemoSlow)
			case 2:
				r.player.SetSpeedMultiplier(state.DemoFast)
			default:
				r.player.SetSpeedMultiplier(state.DemoNormal)
			}
			r.restartTickerIfPlaying()
		}
	}
	if r.intervalFloat.Update(gtx) {
		// 自定义间隔滑块：0..1 → 200–4000ms（步进 100），倍率回 1x（spec ⑤）
		ms := demoIntervalMin + int(float32(demoIntervalMax-demoIntervalMin)*r.intervalFloat.Value+0.5)
		ms = ((ms + 50) / 100) * 100
		if ms < demoIntervalMin {
			ms = demoIntervalMin
		}
		if ms > demoIntervalMax {
			ms = demoIntervalMax
		}
		r.player.SetCustomInterval(ms)
		r.intervalFloat.Value = float32(float64(ms-demoIntervalMin) / float64(demoIntervalMax-demoIntervalMin))
		r.restartTickerIfPlaying()
	}
	if r.loopCheck.Update(gtx) {
		r.player.SetDemoLoop(r.loopCheck.Value)
	}
	switch {
	case r.playBtn.Clicked(gtx):
		r.TogglePlay()
	case r.stopBtn.Clicked(gtx):
		r.Stop()
	case r.toStartBtn.Clicked(gtx):
		r.Jump(0)
	case r.prevBtn.Clicked(gtx):
		r.Jump(r.pos - 1)
	case r.nextBtn.Clicked(gtx):
		r.Jump(r.pos + 1)
	case r.endBtn.Clicked(gtx):
		r.Jump(len(r.moves))
	case r.exportBtn.Clicked(gtx):
		r.exportText = state.WritePgn(r.ExportRecord(), time.Now())
		r.message = ""
		r.copyAsync(r.exportText)
	case r.battleBtn.Clicked(gtx):
		if r.OnBattle != nil {
			r.launcher.Open(nil)
		}
	}
	// 走法列表行点击（前/后半着分别跳到该着之后）
	for i := 0; i*2 < len(r.moves); i++ {
		if c := r.rowClicks[i]; c != nil && c.Clicked(gtx) {
			r.Jump(i*2 + 1)
			break
		}
	}
	for i := 0; i*2+1 < len(r.moves); i++ {
		if c := r.rowClicks[blackRowKey(i)]; c != nil && c.Clicked(gtx) {
			r.Jump(i*2 + 2)
			break
		}
	}
}

// blackRowKey 黑方半着行的点击器键（负数域，避免与行号冲突）。
func blackRowKey(i int) int { return -(i + 1) }

// Layout 重放器视图（PuzzleDetailView 布局：标题行 + 棋盘 + 控制条 + 走法列表；
// 进入对战模式弹层由 battleDialog 顶层承载（共用 BattleLauncher），调用方 Stack 叠放）。
func (r *ReplayView) Layout(gtx layout.Context) layout.Dimensions {
	r.handleEvents(gtx)
	// 剪贴板：PowerShell 通道失败时回退 gio WriteCmd（非 WSL 面）
	if r.exportViaGio && r.exportText != "" {
		gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(r.exportText))})
		r.exportViaGio = false
		r.exportText = ""
		r.message = "PGN 已复制到剪贴板"
	}
	if r.puzzle == nil {
		return layout.Dimensions{}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// 标题行：标题 + 元信息
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body1(PageTheme, derefStr(r.puzzle.Title, "未命名"))
					l.Color = ThemeOnSurface
					return layout.Inset{Right: unit.Dp(10)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					kind := "全局对局"
					if r.puzzle.Endgame {
						kind = "残局题"
					}
					l := material.Body2(PageTheme, fmt.Sprintf("%s · %d 着 · 难度 %s · %s",
						r.puzzle.Source, len(r.moves), parsers.DifficultyText(r.puzzle.Difficulty), kind))
					l.TextSize = unit.Sp(12)
					l.Color = ThemeSeedDark
					return l.Layout(gtx)
				}),
			)
		}),
		// 棋盘（静态呈现 + lastMove 高亮）
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				size := gtx.Constraints.Max.X
				if h := int(float32(gtx.Constraints.Max.Y) * 0.55); h < size {
					size = h // 高度 55% 封顶：控制条与走法列表保持可见
				}
				gtx.Constraints = layout.Constraints{
					Min: image.Pt(size, size),
					Max: image.Pt(size, size),
				}
				fen, lastMove := r.displayFen()
				return drawStaticBoard(gtx, fen, lastMove)
			})
		}),
		// 控制条：播放/暂停 停止 ⇤ ◀ pos ▶ ⇥ | 进入对战 导出
		layout.Rigid(r.layoutControls),
		// 演示控制行：速度档位 + 自定义间隔滑块 + 循环（T6'.5）
		layout.Rigid(r.layoutDemoControls),
		// 状态行
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			msg := r.message
			if r.completed() {
				msg = "演示完毕"
			}
			if r.player.Snapshot().Status == state.DemoError {
				msg = r.player.Snapshot().Error
				if msg == "" {
					msg = "演示出错"
				}
			}
			if msg == "" {
				return layout.Dimensions{}
			}
			l := material.Body2(PageTheme, msg)
			l.TextSize = unit.Sp(12)
			l.Color = ThemeSeedDark
			return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, l.Layout)
		}),
		// 走法列表（中文记谱；≤pos 亮显，>pos 半透明；点击跳转）
		layout.Flexed(1, r.layoutMoveList),
	)
}

// layoutControls 播放控制条 + 进入对战/导出按钮行。
func (r *ReplayView) layoutControls(gtx layout.Context) layout.Dimensions {
	playLabel := "播放"
	if r.Playing() {
		playLabel = "暂停"
	} else if r.player.Snapshot().Status == state.DemoPaused {
		playLabel = "继续"
	}
	return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(replayButton(&r.playBtn, playLabel, true)),
			layout.Rigid(replayButton(&r.stopBtn, "停止", false)),
			layout.Rigid(replayButton(&r.toStartBtn, "⇤", false)),
			layout.Rigid(replayButton(&r.prevBtn, "◀", false)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, fmt.Sprintf("%d / %d 着", r.pos, len(r.moves)))
				l.Color = ThemeOnSurface
				return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(replayButton(&r.nextBtn, "▶", false)),
			layout.Rigid(replayButton(&r.endBtn, "⇥", false)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
			layout.Rigid(replayButton(&r.battleBtn, "进入对战", true)),
			layout.Rigid(replayButton(&r.exportBtn, "导出 PGN（复制）", false)),
		)
	})
}

// displayFen 呈现局面与 lastMove：demo 接管时取播放器快照（走子逐手应用），
// 手动浏览取 replayFen(pos)。
func (r *ReplayView) displayFen() (string, *rules.Move) {
	if r.demoActive() {
		snap := r.player.Snapshot()
		fen := snap.Fen
		if fen == "" {
			fen = r.puzzle.InitialFen
		}
		return fen, snap.LastMove
	}
	return r.replayFen(r.pos)
}

// 滑块映射范围（puzzle_detail_page.dart:352-355）。
const (
	demoIntervalMin = 200
	demoIntervalMax = 4000
)

var demoSpeedLabels = [3]string{"0.5x", "1x", "2x"}

// layoutDemoControls 演示控制行（PuzzleDetailView：速度档位 + 自定义间隔 +
// 循环；Gio 形态：滑块为自绘轨道 + widget.Float 手势面）。
func (r *ReplayView) layoutDemoControls(gtx layout.Context) layout.Dimensions {
	snap := r.player.Snapshot()
	opts := make([]chipOpt, 3)
	for i := range r.speedClicks {
		opts[i] = chipOpt{click: &r.speedClicks[i], label: demoSpeedLabels[i], selected: r.speedIndexOf() == i}
	}
	intervalMs := snap.Params.MoveInterval
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, "速度:")
				l.TextSize = unit.Sp(12)
				l.Color = ThemeSeedDark
				return layout.Inset{Right: unit.Dp(4)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutOptionChips(gtx, 40, opts...)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, r.layoutIntervalSlider)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, fmt.Sprintf("%dms/步", intervalMs))
				l.TextSize = unit.Sp(12)
				l.Color = ThemeSeedDark
				return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx,
					material.CheckBox(PageTheme, &r.loopCheck, "循环").Layout)
			}),
		)
	})
}

// layoutIntervalSlider 自绘滑块（200–4000ms；widget.Float 手势面 + 轨道/滑块头）。
func (r *ReplayView) layoutIntervalSlider(gtx layout.Context) layout.Dimensions {
	const trackH = 4
	w, h := gtx.Dp(unit.Dp(120)), gtx.Dp(unit.Dp(20))
	gtx.Constraints = layout.Constraints{Min: image.Pt(w, h), Max: image.Pt(w, h)}
	r.intervalFloat.Layout(gtx, layout.Horizontal, unit.Dp(2))
	// 轨道
	op.Offset(image.Pt(0, (h-gtx.Dp(unit.Dp(trackH)))/2)).Add(gtx.Ops)
	fillRect(gtx.Ops, image.Rectangle{Max: image.Pt(w, gtx.Dp(unit.Dp(trackH)))}, ThemeSurfaceDim)
	op.Offset(image.Pt(0, -(h-gtx.Dp(unit.Dp(trackH)))/2)).Add(gtx.Ops)
	// 已填充段 + 滑块头
	filled := int(float32(w) * r.intervalFloat.Value)
	op.Offset(image.Pt(0, (h-gtx.Dp(unit.Dp(trackH)))/2)).Add(gtx.Ops)
	fillRect(gtx.Ops, image.Rectangle{Max: image.Pt(filled, gtx.Dp(unit.Dp(trackH)))}, ThemeSeed)
	op.Offset(image.Pt(0, -(h-gtx.Dp(unit.Dp(trackH)))/2)).Add(gtx.Ops)
	fillCircle(gtx.Ops, float32(filled), float32(h)/2, float32(gtx.Dp(unit.Dp(8))), ThemeSeed)
	return layout.Dimensions{Size: image.Pt(w, h)}
}

// replayButton 控制条小按钮。
func replayButton(c *widget.Clickable, label string, primary bool) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(PageTheme, c, label)
		if !primary {
			btn.Background = ThemeSurfaceDim
			btn.Color = ThemeSeedDark
		}
		btn.TextSize = unit.Sp(13)
		return layout.Inset{Left: unit.Dp(3), Right: unit.Dp(3)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(44))
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(120))
			gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(32))
			return btn.Layout(gtx)
		})
	}
}

// drawStaticBoard 静态棋盘呈现（BoardViewStatic 等价：底色+线路+高亮+棋子，
// 无交互；重放器/预览共用）。fen 异常帧跳过绘制（boardview.draw 防御口径）。
func drawStaticBoard(gtx layout.Context, fen string, lastMove *rules.Move) layout.Dimensions {
	l := ComputeBoardLayout(float32(gtx.Constraints.Max.X), float32(gtx.Constraints.Max.Y))
	DrawBoardArt(gtx, l, true) // 纵线号口径（08 §3.2 #4，D-003）
	grid, err := rules.ParseBoardFen(fen)
	if err != nil {
		return layout.Dimensions{}
	}
	DrawHighlights(gtx, l, &BoardState{Grid: grid, LastMove: lastMove})
	DrawPieces(gtx, l, grid, nil, nil)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// layoutMoveList 中文记谱走法列表（每行红/黑半着，点击跳转到该着之后；
// 未走到的半着 55% 透明——上游 opacity 0.55）。
func (r *ReplayView) layoutMoveList(gtx layout.Context) layout.Dimensions {
	if len(r.moves) == 0 {
		l := material.Body2(PageTheme, "该局无可演示走法")
		l.Color = ThemeSeedDark
		return l.Layout(gtx)
	}
	rows := (len(r.moves) + 1) / 2
	return r.moveList.Layout(gtx, rows, func(gtx layout.Context, i int) layout.Dimensions {
		redIdx, blackIdx := i*2, i*2+1
		return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(PageTheme, fmt.Sprintf("%d.", i+1))
					l.Color = ThemeSeedDark
					return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, l.Layout)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return r.plyCell(gtx, i, redIdx, ThemePieceRed)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					if blackIdx >= len(r.moves) {
						return layout.Dimensions{}
					}
					return r.plyCell(gtx, blackRowKey(i), blackIdx, ThemePieceBlack)
				}),
			)
		})
	})
}

// plyCell 单半着记谱按钮（key 为点击器键；idx 为着法下标）。
func (r *ReplayView) plyCell(gtx layout.Context, key, idx int, base color.NRGBA) layout.Dimensions {
	clicker, ok := r.rowClicks[key]
	if !ok {
		clicker = &widget.Clickable{}
		r.rowClicks[key] = clicker
	}
	l := material.Body2(PageTheme, r.notation[idx])
	l.Color = base
	if idx >= r.pos {
		dim := base
		dim.A = 140
		l.Color = dim
	}
	return clicker.Layout(gtx, l.Layout)
}

// battleDialog 进入对战模式弹层（共用 BattleLauncher，KG-009 居中口径——
// 与记录库启动器同界面；调用方在 Stack 顶层叠放）。
func (r *ReplayView) battleDialog(gtx layout.Context) {
	r.launcher.Layout(gtx, func(mode BattleMode, side string, _ any) {
		if r.OnBattle != nil {
			r.OnBattle(mode, r.BattleFen(), side)
		}
	})
}
