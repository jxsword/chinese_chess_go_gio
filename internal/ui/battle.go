package ui

// 棋谱/语料"进入对战"入口面（T5'.3，翻译源 = 上游
// frontend/src/packages/storage-schema/recordBattle.ts 的选项面）。
// 起点计算（battleStartFen 语义）：残局题 = initialFen；全局对局 = 终局局面；
// 上游"已分胜负不提供入口"分支不适用于语料（无 result 字段）。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
)

// BattleMode 进入对战的模式选项 ID（recordBattle.ts BattleModeOption）。
type BattleMode string

const (
	BattleHumanVsAi    BattleMode = "humanVsAi"
	BattleHumanVsHuman BattleMode = "humanVsHuman"
	BattleHumanVsLlm   BattleMode = "humanVsLlm"
	BattleLlmVsLlm     BattleMode = "llmVsLlm"
)

// BattleModeOption 模式选项（recordBattle.ts BATTLE_MODE_OPTIONS）。
type BattleModeOption struct {
	ID       BattleMode
	Label    string
	Subtitle string
}

// BattleModeOptions 模式选项清单（record_battle_launcher.dart:11-15）。
var BattleModeOptions = []BattleModeOption{
	{BattleHumanVsAi, "人机对战（内置 AI）", "可选难度与执方，AI 自动应手"},
	{BattleHumanVsHuman, "双人对弈", "同屏轮流走子"},
	{BattleHumanVsLlm, "人机对战（大模型）", "玩家执红，大模型执黑"},
	{BattleLlmVsLlm, "大模型对战", "红黑双模型自动对弈"},
}

// BattleStart 对局页"进入对战"起点（app 装配层经 GameEnv 注入；构造时消费）。
// Fen 非空时页面跳过存档恢复、以该 FEN 开局，且 canSave=false（续战来源
// 不写存档桶——防错 #6，上游 battleRoute 页面门控同口径）。
type BattleStart struct {
	Fen string
}

// BattleStartFen 重放视图的进入对战起点 FEN（recordBattle.ts battleStartFen
// 对语料的等价：残局题=initialFen；全局对局=终局局面）。
func BattleStartFen(puzzle *state.ParsedPuzzleView, moves []rules.Move) string {
	if puzzle.Endgame {
		return puzzle.InitialFen
	}
	return state.FinalFenOf(puzzle.InitialFen, moves)
}
