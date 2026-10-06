package ui

// 语料重放器（T5'.3，翻译源 = 上游 frontend/src/features/puzzle/PuzzleDetailView.tsx
// 的基础播放面；Gio 规格 08 §8 落地注记④：M5' 为步进 + 800ms 自动播放 +
// 中文记谱走法列表 + 进入对战 + 导出 PGN（复制），速度档位/循环/自定义间隔
// 随 M6' puzzleDemo 状态机（06 附录 §7）。
//
// 自动播放计时（Gio 形态）：goroutine + stop 通道 + ReplayTick 事件回主循环
//（00 §4 `replay:tick`；铁律 #G3——goroutine 只触碰自有通道与 emit）。
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
	"gioui.org/op/clip"
	"gioui.org/op/paint"
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
	// OnBattle 进入对战回调（模式 + 起点 FEN；nil = 不启用入口）。
	OnBattle func(mode BattleMode, fen string)
	// emit 事件提交面（页面注入 env.Emit；ReplayTick 回主循环 OnTick）。
	emit func(requestID string, payload any, err error)

	puzzle   *state.ParsedPuzzleView
	moves    []rules.Move
	notation []string

	pos       int  // 已显示着数（0..len(moves)，replayFen 语义）
	playing   bool // 自动播放中
	completed bool // 播放到末尾（"演示完毕"）
	gen       int  // 播放代次（stop/手动/换局递增）
	stop      chan struct{}

	// 控件
	playBtn          widget.Clickable
	stopBtn          widget.Clickable
	toStartBtn       widget.Clickable
	prevBtn          widget.Clickable
	nextBtn          widget.Clickable
	endBtn           widget.Clickable
	battleBtn        widget.Clickable
	exportBtn        widget.Clickable
	battleModeClicks [4]widget.Clickable
	moveList         layout.List
	rowClicks        map[int]*widget.Clickable

	battleOpen bool
	exportText string // 非空 = 下一帧写剪贴板
	message    string // 状态行（已复制/错误）
}

// NewReplayView 创建重放器（emit 可 nil = 测试同步场景）。
func NewReplayView(emit func(requestID string, payload any, err error)) *ReplayView {
	return &ReplayView{
		emit:      emit,
		moveList:  layout.List{Axis: layout.Vertical},
		rowClicks: map[int]*widget.Clickable{},
	}
}

// Puzzle 当前详情（nil = 未打开）。
func (r *ReplayView) Puzzle() *state.ParsedPuzzleView { return r.puzzle }

// SetPuzzle 打开/更换详情（换局：停播放、归零）。
func (r *ReplayView) SetPuzzle(v *state.ParsedPuzzleView) {
	r.haltPlay()
	r.puzzle = v
	r.moves = nil
	r.notation = nil
	r.pos = 0
	r.completed = false
	r.message = ""
	r.battleOpen = false
	r.rowClicks = map[int]*widget.Clickable{}
	if v == nil {
		return
	}
	for _, code := range v.SolutionMoves {
		if ft := parsers.ParseIccs(code); ft != nil {
			r.moves = append(r.moves, rules.Move{From: ft.From, To: ft.To})
		}
	}
	r.notation = state.ChineseNotations(v.InitialFen, r.moves)
}

// Pos 当前显示位置（测试断言面）。
func (r *ReplayView) Pos() int { return r.pos }

// Playing 自动播放中。
func (r *ReplayView) Playing() bool { return r.playing }

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

// Jump 手动跳转（跳转即停止播放——上游 jump 语义）。
func (r *ReplayView) Jump(n int) {
	r.haltPlay()
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
	r.completed = false
}

// TogglePlay 播放/暂停（上游 togglePlay：playing→暂停；idle/completed → 从头播放）。
func (r *ReplayView) TogglePlay() {
	if r.puzzle == nil {
		return
	}
	if r.playing {
		r.haltPlay()
		return
	}
	if r.completed || r.pos >= len(r.moves) {
		r.Jump(0)
	}
	if len(r.moves) == 0 {
		return
	}
	r.playing = true
	r.startTicker()
}

// Stop 停止（回开局；M5' 基础语义，完整状态机随 M6' puzzleDemo）。
func (r *ReplayView) Stop() {
	r.haltPlay()
	r.pos = 0
	r.completed = false
}

// haltPlay 停止自动播放（保留当前位置——暂停语义）。
func (r *ReplayView) haltPlay() {
	r.playing = false
	r.gen++
	if r.stop != nil {
		close(r.stop)
		r.stop = nil
	}
}

// startTicker 启动自动播放计时（goroutine 只触碰自有通道与 emit，铁律 #G3）；
// 迟到 tick 由 OnTick 按 Gen 代次丢弃（#G5）。
func (r *ReplayView) startTicker() {
	r.gen++
	gen := r.gen
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
			case <-time.After(ReplayAutoPlayInterval):
			}
			r.emit("", ReplayTick{Gen: gen}, nil)
		}
	}()
}

// OnTick 自动播放步进回执（主 goroutine；Gen 过期忽略，#G5）。
func (r *ReplayView) OnTick(ev ReplayTick) {
	if !r.playing || ev.Gen != r.gen {
		return
	}
	if r.pos < len(r.moves) {
		r.pos++
	}
	if r.pos >= len(r.moves) {
		r.haltPlay()
		r.completed = true
	}
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
	case r.battleBtn.Clicked(gtx):
		if r.OnBattle != nil {
			r.battleOpen = true
		}
	}
	for i := range r.battleModeClicks {
		if r.battleOpen && r.battleModeClicks[i].Clicked(gtx) {
			r.battleOpen = false
			if r.OnBattle != nil {
				r.OnBattle(BattleModeOptions[i].ID, r.BattleFen())
			}
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
// 进入对战模式弹层由 layoutBattleDialog 顶层承载，调用方 Stack 叠放）。
func (r *ReplayView) Layout(gtx layout.Context) layout.Dimensions {
	r.handleEvents(gtx)
	if r.exportText != "" {
		gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(r.exportText))})
		r.exportText = ""
		r.message = "PGN 已复制到剪贴板"
	}
	if r.puzzle == nil {
		return layout.Dimensions{}
	}
	fen, lastMove := r.replayFen(r.pos)
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
				return drawStaticBoard(gtx, fen, lastMove)
			})
		}),
		// 控制条：播放/暂停 停止 ⇤ ◀ pos ▶ ⇥ | 进入对战 导出
		layout.Rigid(r.layoutControls),
		// 状态行
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			msg := r.message
			if r.completed {
				msg = "演示完毕"
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
	if r.playing {
		playLabel = "暂停"
	} else if r.pos > 0 && r.pos < len(r.moves) {
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

// layoutBattleDialog 进入对战模式选择弹层（RecordLauncherDialog 等价；
// 调用方在 Stack 顶层调用）。
func (r *ReplayView) layoutBattleDialog(gtx layout.Context) layout.Dimensions {
	if !r.battleOpen {
		return layout.Dimensions{}
	}
	// 半透明遮罩（拦截底层输入）
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgba(0x000000, 0.4))
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Max.X = gtx.Dp(unit.Dp(360))
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, ThemeSurface)
		return layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body1(PageTheme, "选择对战模式")
					l.Color = ThemeOnSurface
					return layout.Inset{Bottom: unit.Dp(10)}.Layout(gtx, l.Layout)
				}),
			}
			for i, opt := range BattleModeOptions {
				i, opt := i, opt
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return r.battleModeClicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
							paint.Fill(gtx.Ops, ThemeSurfaceDim)
							return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Body2(PageTheme, opt.Label)
										l.Color = ThemeOnSurface
										return l.Layout(gtx)
									}),
									layout.Rigid(func(gtx layout.Context) layout.Dimensions {
										l := material.Body2(PageTheme, opt.Subtitle)
										l.TextSize = unit.Sp(12)
										l.Color = ThemeSeedDark
										return l.Layout(gtx)
									}),
								)
							})
						})
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}
