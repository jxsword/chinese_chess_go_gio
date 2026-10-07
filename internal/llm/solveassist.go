package llm

// 大模型求解辅助（T6'.4，05 文档 §6；翻译锚点 = 上游
// frontend/src/packages/llm/solveAssist.ts，llm_solve_assist.dart 1:1，
// Electron 版 test/llm/solveAssist.spec.ts 为协议快照基准）。
//
// Hybrid 架构：模型只"提议"首着与思路，是否必胜由 EndgameSolver
//（isWinningFirstMove）验证后才写入棋谱注释——模型启发式、引擎裁判。
// 不走合法清单协议的五层过滤，仅复用坐标归一化提取（ExtractMove）；
// 提议不在合法清单 → user 末尾追加失败原因重试（≤2 次），无降级链。
// 提示词逐字（协议面，铁律 #G9，改动必须显式 review）。
//
// 本文件为上游 Go 版翻译补全件（上游 Wails 版该模块以前端 TS 直供，
// internal/llm 未含；Gio 版无前端，随 DR-G001 翻译纪律补齐，diff 范围=
// 本新增文件，既有复制物文件零改动）。
//
// 纯 Go：禁止 import gio / net/http / 前端任何符号（铁律 #G1）。

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// SolveAssistSystem system 提示词：三行固定格式（llm_solve_assist.dart:17-27，逐字）。
const SolveAssistSystem = "你是中国象棋残局研究助手，协助分析一个残局是否有强制将死的杀法。\n" +
	"坐标约定：列用字母 a-i（从左到右），行用数字 0-9" +
	"（0 为黑方底线、棋盘顶部，9 为红方底线、棋盘底部）。\n" +
	"你只能从「合法着法清单」中选择首着，禁止编造清单之外的着法。\n" +
	"\n" +
	"【回复格式（唯一允许的格式，共三行）】\n" +
	"首选着法: 起点-终点\n" +
	"备选着法: 起点-终点（没有则写 无）\n" +
	"思路: 一句话说明攻击目标与关键点\n" +
	"禁止输出其他任何内容。"

// SolveAssistUser user 提示词：FEN + ASCII 棋盘图 + 轮走方 + 任务说明 +
// 完整合法着法清单（llm_solve_assist.dart:29-40）。
func SolveAssistUser(board *rules.Board, legalCodes []string) string {
	var buf []string
	buf = append(buf, fmt.Sprintf("【局面 FEN】%s\n", board.ToFen()))
	buf = append(buf, "【棋盘图（大写为红方、小写为黑方，第一行是黑方底线）】\n")
	buf = append(buf, AsciiBoard(board))
	turn := "黑方"
	if board.IsRedTurn() {
		turn = "红方"
	}
	buf = append(buf, fmt.Sprintf("【轮走方】%s（求解方）\n", turn))
	buf = append(buf, "【任务】判断该局面求解方是否有强制将死的杀法；"+
		"若有，给出首选首着与备选首着（均取自合法着法清单）。\n")
	buf = append(buf, fmt.Sprintf("【合法着法清单（共 %d 条）】\n", len(legalCodes)))
	buf = append(buf, strings.Join(legalCodes, ", "))
	return strings.Join(buf, "")
}

// SolveProposal 模型提议的解析结果（llm_solve_assist.dart:44-53）。
type SolveProposal struct {
	// FirstMoveCode 归一化 ICCS 式 "h2-e2"（内部行号约定，同 LLM 对弈协议）。
	FirstMoveCode string
	AlternateCode string
	Idea          string
}

var solveAssistFieldPattern = func(label string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)` + label + `\s*[:：]\s*(.+)`)
}

// parseSolveField 三行格式字段提取（label 后的整行内容）。
func parseSolveField(content, label string) string {
	m := solveAssistFieldPattern(label).FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// ParseSolveProposal 解析三行格式回复；坐标归一化复用 ExtractMove（:101-120）。
func ParseSolveProposal(content string) SolveProposal {
	code := func(raw string) string {
		if raw == "" || strings.Contains(raw, "无") {
			return ""
		}
		if extracted := ExtractMove(raw); extracted != nil {
			return *extracted
		}
		return ""
	}
	idea := parseSolveField(content, "思路")
	return SolveProposal{
		FirstMoveCode: code(parseSolveField(content, "首选着法")),
		AlternateCode: code(parseSolveField(content, "备选着法")),
		Idea:          idea,
	}
}

// SolveAssistOptions 求解辅助选项。
type SolveAssistOptions struct {
	// MaxAttempts 最多尝试次数（含首次），默认 2（llm_solve_assist.dart:56）。
	MaxAttempts int
	// AuthSlot 掩码 Key 回读场景：传输层按槽位注入真实 Authorization（DR-010）。
	AuthSlot string
	// NewID requestId 工厂（测试注入）。
	NewID func() string
}

// SolveProposeResult 一次调用链结果：返回候选首着与思路注释；失败时提议代码
// 为空串，Message 给出原因。
type SolveProposeResult struct {
	Proposal SolveProposal
	Message  string
}

// ProposeSolveFirstMove 大模型求解辅助（llm_solve_assist.dart:63-98）。
func ProposeSolveFirstMove(ctx context.Context, board *rules.Board, config LlmEndpointConfig, transport Transport, options SolveAssistOptions) (SolveProposeResult, error) {
	if !IsConfigured(config) {
		return SolveProposeResult{Message: "研究助手模型未配置"}, nil
	}
	legal := board.AllLegalMoves(board.Turn())
	if len(legal) == 0 {
		return SolveProposeResult{Message: "当前局面无合法着法"}, nil
	}

	codesByMove := map[string]struct{}{}
	legalCodes := make([]string, 0, len(legal))
	for _, m := range legal {
		code := EncodeMove(m)
		if _, ok := codesByMove[code]; !ok {
			codesByMove[code] = struct{}{}
			legalCodes = append(legalCodes, code)
		}
	}

	client := NewLlmChatClient(config, transport, ChatClientOptions{AuthSlot: options.AuthSlot, NewID: options.NewID})
	system := SolveAssistSystem
	user := SolveAssistUser(board, legalCodes)

	lastError := ""
	maxAttempts := options.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 2
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		content, err := client.ChatOnce(ctx, system, user, false)
		if err != nil {
			lastError = err.Error()
			user = fmt.Sprintf("%s\n\n（上次回复无效：%s，请严格按三行格式重新回答）", user, lastError)
			continue
		}
		proposal := ParseSolveProposal(content)
		if proposal.FirstMoveCode != "" {
			if _, ok := codesByMove[proposal.FirstMoveCode]; ok {
				return SolveProposeResult{Proposal: proposal, Message: proposal.Idea}, nil
			}
		}
		shown := proposal.FirstMoveCode
		if shown == "" {
			shown = content
		}
		lastError = fmt.Sprintf("回复 %s 不在合法清单中", shown)
		user = fmt.Sprintf("%s\n\n（上次回复无效：%s，请严格按三行格式重新回答）", user, lastError)
	}
	if lastError == "" {
		lastError = "未知错误"
	}
	return SolveProposeResult{Message: lastError}, nil
}
