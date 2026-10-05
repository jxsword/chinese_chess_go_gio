package engine

// 棋手接口（03 文档 §8；Electron 版 moveSource.ts 的 Go 对应，
// move_source.dart 的 1:1 移植同源）：
// 内置引擎与大模型是两个可互换的实现（M4 LlmPlayer/HybridLlmPlayer 复用）。
// 纯 Go：仅类型与纯函数，禁止 import Wails / net/http / frontend（铁律 #1）。

import (
	"context"
	"errors"
	"fmt"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// MoveSourceStatus 棋手一步棋的结果状态。
type MoveSourceStatus string

const (
	StatusOK          MoveSourceStatus = "ok"
	StatusNoLegalMove MoveSourceStatus = "noLegalMove"
	StatusFailed      MoveSourceStatus = "failed"
)

// MoveSourceResult 一步棋结果。
type MoveSourceResult struct {
	Status MoveSourceStatus
	Move   *rules.Move
	// Note 思路/解说/失败原因，供状态栏展示。
	Note string
	// FromFallback true 表示着法来自兜底而非模型本身（M4 兜底链使用）。
	FromFallback bool
}

// MoveSource "棋手"抽象。
//
// 接口契约：实现必须保证返回的 move 是合法着法；
// 页面侧的 playMove 仍做最终校验（双保险，铁律 #3）。
type MoveSource interface {
	// DisplayName 展示名，用于状态栏，如 "内置 AI（高级）" / "glm-4-flash"。
	DisplayName() string
	// NextMove 为 board 的当前走子方寻求一步棋。history 为对局走法记录
	// （各着含 ApplyMove 快照的 Captured），可组装上下文。
	NextMove(ctx context.Context, b *rules.Board, history []rules.Move) (MoveSourceResult, error)
}

// difficultyNames 难度显示名（move_source.dart:96-99）。
var difficultyNames = map[int]string{
	1: "初级",
	2: "中级",
	3: "高级",
	4: "专家",
	5: "大师",
}

// DifficultyName 难度显示名。
func DifficultyName(difficulty int) string {
	if name, ok := difficultyNames[difficulty]; ok {
		return name
	}
	return fmt.Sprintf("%d", difficulty)
}

// ChessAiPlayer 内置 AI 棋手（对应 move_source.dart ChessAiMoveSource）。
// L2 历史入参引擎侧单源（03 §8）：从 board+history 自行推导 fenHistory，
// 调用方无需感知哈希。
type ChessAiPlayer struct {
	difficulty int
}

// NewChessAiPlayer 构造内置 AI 棋手。
func NewChessAiPlayer(difficulty int) *ChessAiPlayer {
	return &ChessAiPlayer{difficulty: difficulty}
}

// DisplayName 展示名（"内置 AI（高级）"）。
func (p *ChessAiPlayer) DisplayName() string {
	return fmt.Sprintf("内置 AI（%s）", DifficultyName(p.difficulty))
}

// NextMove 包装 FindBestMove（goroutine+ctx 取消由调用方与协议层收口，03 §7）：
// 取消以 error 上抛（调用方丢弃语义），引擎错误转为 failed 结果（对齐 TS
// ChessAiPlayer：canceled rethrow、其余降级 failed）。
func (p *ChessAiPlayer) NextMove(ctx context.Context, b *rules.Board, history []rules.Move) (MoveSourceResult, error) {
	fens := HistoryFensFromBoard(b, history)
	move, err := FindBestMove(b.ToFen(), FindBestMoveOptions{
		Difficulty:  p.difficulty,
		HistoryFens: fens,
		Ctx:         ctx,
	})
	if err != nil {
		if errors.Is(err, ErrCanceled) || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return MoveSourceResult{}, ErrCanceled
		}
		return MoveSourceResult{Status: StatusFailed, Note: "引擎计算失败：" + err.Error()}, nil
	}
	if ctx.Err() != nil {
		// 取消发生在搜索返回之后：迟到结果按丢弃语义收口（03 §7）。
		return MoveSourceResult{}, ErrCanceled
	}
	if move == nil {
		return MoveSourceResult{Status: StatusNoLegalMove}, nil
	}
	return MoveSourceResult{Status: StatusOK, Move: move}, nil
}

// HistoryFensFromBoard 从当前盘面 + 走法记录推导 fenHistory（起始→当前，含轮走方）：
// 深拷贝当前盘逐手 UndoMove 回放，收集每个中间局面的 FEN 后反转。
// history 各着必须携带走子时的被吃子快照（rules.ApplyMove 返回值口径）。
func HistoryFensFromBoard(b *rules.Board, history []rules.Move) []string {
	fens := make([]string, 0, len(history)+1)
	if len(history) == 0 {
		return append(fens, b.ToFen())
	}
	probe := b.Copy()
	fens = append(fens, probe.ToFen()) // 当前局面（posN）
	for i := len(history) - 1; i >= 0; i-- {
		probe.UndoMove(history[i])
		fens = append(fens, probe.ToFen()) // pos(N-1) … pos0
	}
	// 反转 → [起始, ..., 当前]。
	for l, r := 0, len(fens)-1; l < r; l, r = l+1, r-1 {
		fens[l], fens[r] = fens[r], fens[l]
	}
	return fens
}
