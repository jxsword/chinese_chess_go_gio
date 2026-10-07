package state

// 残局演示播放器状态机（T6'.5，06 文档 §7；翻译锚点 = 上游
// frontend/src/stores/puzzleDemo.ts，puzzle_vm.dart 1:1；测试基准 =
// frontend/test/stores/puzzleDemo.spec.ts 11 用例）。
//
// 演示时内部持有一块独立于对局的 Board，按节拍逐条把破解走法（ICCS 字符串，
// 行 0 为红方底线约定）应用到棋盘上；状态机 idle → playing → paused/completed/
// error；循环播放重置棋盘重播；走子数据与局面不一致（坐标越界、起点无子）时
// 跳过该步防崩溃（防错 #8）。
//
// Gio 形态差异（注册备查）：上游持真实 setInterval 并由内部心跳驱动；本仓为
// 纯状态机——节拍由 UI 层 ticker（replay:tick 事件，#G3）调用 Advance() 驱动，
// 每次节拍前读 CurrentInterval() 取生效间隔（速度/自定义间隔切换下一拍生效）。
// 纯 Go：无框架依赖（铁律 #G1）。

import (
	"fmt"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// PuzzleDemoStatus 演示状态（puzzle_data.dart:207-220）。
type PuzzleDemoStatus string

const (
	DemoIdle      PuzzleDemoStatus = "idle"
	DemoPlaying   PuzzleDemoStatus = "playing"
	DemoPaused    PuzzleDemoStatus = "paused"
	DemoCompleted PuzzleDemoStatus = "completed"
	DemoError     PuzzleDemoStatus = "error"
)

// 演示参数：速度倍率 × 基准间隔（puzzle_data.dart:225-278）。
const (
	DemoSlow   = 0.5
	DemoNormal = 1.0
	DemoFast   = 2.0

	// DemoIntervalMinMs / DemoIntervalMaxMs 自定义间隔滑块范围
	//（puzzle_detail_page.dart:352-355）。
	DemoIntervalMinMs = 200
	DemoIntervalMaxMs = 4000
)

// DemoIntervalOf 实际生效间隔 = 基准间隔 / 倍率（慢速 1600 / 正常 800 / 快速 400）。
func DemoIntervalOf(speedMultiplier float64, moveInterval int) int {
	if speedMultiplier <= 0 {
		speedMultiplier = DemoNormal
	}
	iv := int(float64(moveInterval)/speedMultiplier + 0.5) // round
	if iv < 1 {
		iv = 1
	}
	return iv
}

// PuzzleDemoParams 演示参数。
type PuzzleDemoParams struct {
	// SpeedMultiplier 速度倍率：0.5 慢速 / 1 正常 / 2 快速。
	SpeedMultiplier float64
	// MoveInterval 基准走子间隔毫秒数（默认 800；滑块自定义 200–4000）。
	MoveInterval int
	// Loop 循环播放。
	Loop bool
}

// PuzzleDemoSnapshot 播放器对 UI 暴露的快照（PuzzleState 对应形状）。
type PuzzleDemoSnapshot struct {
	Status PuzzleDemoStatus
	// CurrentMoveIndex 最近一次已应用到棋盘的走法下标；-1 = 尚未走子。
	CurrentMoveIndex int
	CurrentSide      rules.Side
	Error            string
	// Fen 当前局面 FEN（初始化失败时为空串，对应上游 null）。
	Fen string
	// LastMove 最近一步演示走法（棋盘高亮用）；nil = 尚未走子。
	LastMove *rules.Move
	Params   PuzzleDemoParams
}

// PuzzleDemoPlayer 演示播放器（主 goroutine 独占；节拍由 UI 驱动）。
type PuzzleDemoPlayer struct {
	initialFen string
	moves      []string
	board      *rules.Board
	status     PuzzleDemoStatus
	index      int
	side       rules.Side
	errorMsg   string
	lastMove   *rules.Move
	params     PuzzleDemoParams
}

// NewPuzzleDemoPlayer 构造（默认参数：正常速度 / 800ms / 不循环）。
func NewPuzzleDemoPlayer() *PuzzleDemoPlayer {
	return &PuzzleDemoPlayer{
		status: DemoIdle,
		index:  -1,
		side:   rules.Red,
		params: PuzzleDemoParams{SpeedMultiplier: DemoNormal, MoveInterval: 800, Loop: false},
	}
}

// Snapshot 当前快照。
func (p *PuzzleDemoPlayer) Snapshot() PuzzleDemoSnapshot {
	return PuzzleDemoSnapshot{
		Status:           p.status,
		CurrentMoveIndex: p.index,
		CurrentSide:      p.side,
		Error:            p.errorMsg,
		Fen:              p.fen(),
		LastMove:         p.lastMove,
		Params:           p.params,
	}
}

func (p *PuzzleDemoPlayer) fen() string {
	if p.board == nil {
		return ""
	}
	return p.board.ToFen()
}

// CurrentInterval 当前生效间隔（毫秒；UI ticker 每拍前读取）。
func (p *PuzzleDemoPlayer) CurrentInterval() int {
	return DemoIntervalOf(p.params.SpeedMultiplier, p.params.MoveInterval)
}

// InitializePuzzle 初始化残局：重置演示进度与棋盘，但**保留**用户已选的速度
// 与循环（避免每次点"播放"都把速度悄悄重置回默认，puzzle_vm.dart:46-64）。
func (p *PuzzleDemoPlayer) InitializePuzzle(initialFen string, moves []string) {
	p.initialFen = initialFen
	p.moves = append([]string(nil), moves...)
	p.status = DemoIdle
	p.index = -1
	p.side = rules.Red
	p.errorMsg = ""
	p.lastMove = nil
	board, err := rules.FromFen(initialFen)
	if err != nil {
		p.board = nil
		p.status = DemoError
		p.errorMsg = fmt.Sprintf("初始化残局失败: %s", err.Error())
		return
	}
	p.board = board
}

// StartDemo 开始演示。
func (p *PuzzleDemoPlayer) StartDemo() {
	if p.initialFen == "" || p.status == DemoPlaying {
		return
	}
	if p.board == nil {
		return // 初始化失败（error 态）不可播放
	}
	p.status = DemoPlaying
}

// PauseDemo 暂停演示。
func (p *PuzzleDemoPlayer) PauseDemo() {
	if p.status != DemoPlaying {
		return
	}
	p.status = DemoPaused
}

// ResumeDemo 继续演示。
func (p *PuzzleDemoPlayer) ResumeDemo() {
	if p.status != DemoPaused || p.initialFen == "" {
		return
	}
	p.status = DemoPlaying
}

// StopDemo 停止演示：重置进度并回到初始局面。
func (p *PuzzleDemoPlayer) StopDemo() {
	p.status = DemoIdle
	p.index = -1
	p.side = rules.Red
	p.lastMove = nil
	if p.initialFen != "" {
		if board, err := rules.FromFen(p.initialFen); err == nil {
			p.board = board
		} else {
			p.board = nil
		}
	}
}

// SetSpeedMultiplier 设置速度倍率（0.5/1/2），播放中即时生效。
func (p *PuzzleDemoPlayer) SetSpeedMultiplier(multiplier float64) {
	p.params.SpeedMultiplier = multiplier
}

// SetCustomInterval 设置自定义走子间隔（毫秒/步，速度滑块用；puzzle_vm.dart
// :122-128 的语义：覆盖倍率回 1x，播放中即时生效）。
func (p *PuzzleDemoPlayer) SetCustomInterval(intervalMs int) {
	clamped := intervalMs
	if clamped > DemoIntervalMaxMs {
		clamped = DemoIntervalMaxMs
	}
	if clamped < DemoIntervalMinMs {
		clamped = DemoIntervalMinMs
	}
	p.params = PuzzleDemoParams{SpeedMultiplier: DemoNormal, MoveInterval: clamped, Loop: p.params.Loop}
}

// SetDemoLoop 设置循环播放。
func (p *PuzzleDemoPlayer) SetDemoLoop(loop bool) { p.params.Loop = loop }

// Advance 定时节拍（puzzle_vm.dart:148-160 onTick）：应用下一步走法，或结束/
// 循环演示。返回播放是否仍在进行（UI 据此停/续 ticker）。
func (p *PuzzleDemoPlayer) Advance() bool {
	if p.initialFen == "" || p.status != DemoPlaying {
		return false
	}
	if p.index+1 < len(p.moves) {
		p.index++
		if p.side == rules.Red {
			p.side = rules.Black
		} else {
			p.side = rules.Red
		}
		p.applyCurrentMove()
		return true
	}
	p.completeDemo()
	return p.status == DemoPlaying
}

// applyCurrentMove 把当前步的 ICCS 走法应用到演示棋盘；数据与局面不一致
// （解析失败、起点无棋子）时跳过该步，避免崩溃（防错 #8）。
func (p *PuzzleDemoPlayer) applyCurrentMove() {
	if p.board == nil || p.index >= len(p.moves) {
		return
	}
	parsed := parsers.ParseIccs(p.moves[p.index])
	if parsed == nil {
		return
	}
	mover := p.board.PieceAtP(parsed.From)
	if mover == nil {
		return
	}
	applied := p.board.ApplyMove(rules.Move{From: parsed.From, To: parsed.To})
	p.lastMove = &rules.Move{
		From:     applied.From,
		To:       applied.To,
		Piece:    mover,
		Captured: applied.Captured,
	}
}

// completeDemo 完成演示：循环则重置棋盘续播，否则停在 completed
// （puzzle_vm.dart:195-217；06 §7 "循环播放重置棋盘重播"）。
func (p *PuzzleDemoPlayer) completeDemo() {
	p.status = DemoCompleted
	if p.params.Loop {
		p.index = -1
		p.side = rules.Red
		p.lastMove = nil
		p.status = DemoPlaying
		if board, err := rules.FromFen(p.initialFen); err == nil {
			p.board = board
		} else {
			p.board = nil
		}
	}
}
