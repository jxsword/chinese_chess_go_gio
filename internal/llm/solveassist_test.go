package llm

// 大模型求解辅助测试（T6'.4，05 文档 §6，llm_solve_assist 用例等价集；
// 逐项对齐 Electron 版 test/llm/solveAssist.spec.ts）：
// 提示词逐字（协议面，改动必须显式 review）、三行格式解析、
// 首着不在清单 → 追加失败原因重试（≤2 次）、未配置/无着短路。
//
// Go 版差异：config 无 disableThinking（DR-005），关闭参数由 BuildChatRequest
// 按预设恒发——成功用例附断言请求体恒发关闭参数。

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 双车马闷杀残局（红方 5 条合法着法左右，含两路杀着）。
const solveAssistFenA = "3k5/9/9/9/R8/8R/9/9/9/4K4 w - - 0 1"

// spec: system 提示词逐字（llm_solve_assist.dart:17-40，协议面快照）。
func TestSolveAssistSystemVerbatim(t *testing.T) {
	want := "你是中国象棋残局研究助手，协助分析一个残局是否有强制将死的杀法。\n" +
		"坐标约定：列用字母 a-i（从左到右），行用数字 0-9" +
		"（0 为黑方底线、棋盘顶部，9 为红方底线、棋盘底部）。\n" +
		"你只能从「合法着法清单」中选择首着，禁止编造清单之外的着法。\n" +
		"\n" +
		"【回复格式（唯一允许的格式，共三行）】\n" +
		"首选着法: 起点-终点\n" +
		"备选着法: 起点-终点（没有则写 无）\n" +
		"思路: 一句话说明攻击目标与关键点\n" +
		"禁止输出其他任何内容。"
	if SolveAssistSystem != want {
		t.Fatalf("SOLVE_ASSIST_SYSTEM 偏离逐字快照:\n got=%q\nwant=%q", SolveAssistSystem, want)
	}
}

// spec: user——FEN + ASCII 棋盘图 + 轮走方 + 任务 + 合法着法清单。
func TestSolveAssistUser(t *testing.T) {
	board, err := rules.FromFen(solveAssistFenA)
	if err != nil {
		t.Fatalf("FromFen: %v", err)
	}
	user := SolveAssistUser(board, []string{"a4-d4", "i5-d5"})
	for _, want := range []string{
		"【局面 FEN】" + solveAssistFenA,
		"【棋盘图（大写为红方、小写为黑方，第一行是黑方底线）】",
		"    a b c d e f g h i\n",
		"【轮走方】红方（求解方）",
		"【任务】判断该局面求解方是否有强制将死的杀法",
		"【合法着法清单（共 2 条）】\na4-d4, i5-d5",
	} {
		if !strings.Contains(user, want) {
			t.Fatalf("user 缺少 %q", want)
		}
	}
}

// spec: parseSolveProposal——标准三行（备选"无"→空）。
func TestParseSolveProposalStandard(t *testing.T) {
	p := ParseSolveProposal("首选着法: a4-d4\n备选着法: 无\n思路: 双车错，平车闷杀")
	if p.FirstMoveCode != "a4-d4" || p.AlternateCode != "" || p.Idea != "双车错，平车闷杀" {
		t.Fatalf("解析 = %+v", p)
	}
}

// spec: parseSolveProposal——备选存在 + 全角冒号 + 坐标杂质容错。
func TestParseSolveProposalTolerant(t *testing.T) {
	p := ParseSolveProposal("首选着法：分析后我选择 a5—d5。\n备选着法: i5-d5\n思路: 控制肋线")
	if p.FirstMoveCode != "a5-d5" || p.AlternateCode != "i5-d5" || p.Idea != "控制肋线" {
		t.Fatalf("解析 = %+v", p)
	}
}

// spec: parseSolveProposal——缺字段/无坐标 → 空（不抛错）。
func TestParseSolveProposalMissing(t *testing.T) {
	if got := ParseSolveProposal("我不知道").FirstMoveCode; got != "" {
		t.Fatalf("无格式应解析为空: %q", got)
	}
	if got := ParseSolveProposal("首选着法: 无\n思路: 无").FirstMoveCode; got != "" {
		t.Fatalf("「无」应解析为空: %q", got)
	}
}

func solveAssistConfig() LlmEndpointConfig {
	return LlmEndpointConfig{BaseURL: "https://api.example.com/v1", APIKey: "sk", Model: "glm-4.5v", Preset: ""}
}

func solveAssistBoard(t *testing.T) *rules.Board {
	t.Helper()
	board, err := rules.FromFen(solveAssistFenA)
	if err != nil {
		t.Fatalf("FromFen: %v", err)
	}
	return board
}

// spec: 成功——提议首着在合法清单 → 返回提议与思路（请求体恒发关闭参数，
// DR-005：求解辅助走对弈通道同款请求构造，无开关路径）。
func TestProposeSolveFirstMoveSuccess(t *testing.T) {
	ft := &fakeTransport{script: []scriptItem{scriptText("首选着法: a4-d4\n备选着法: 无\n思路: 平车闷杀")}}
	result, err := ProposeSolveFirstMove(context.Background(), solveAssistBoard(t), solveAssistConfig(), ft, SolveAssistOptions{})
	if err != nil {
		t.Fatalf("调用错误: %v", err)
	}
	if result.Proposal.FirstMoveCode != "a4-d4" || result.Message != "平车闷杀" {
		t.Fatalf("结果 = %+v", result)
	}
	// DR-005：请求体按端点预设恒发关闭参数（preset 空 → 兜底 enable_thinking:false）。
	ft.mu.Lock()
	body := ft.calls[0].Body
	ft.mu.Unlock()
	var parsed map[string]any
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("请求体非 JSON: %v", err)
	}
	if v, ok := parsed["enable_thinking"].(bool); !ok || v {
		t.Fatalf("应恒发 enable_thinking:false: %v", parsed["enable_thinking"])
	}
}

// spec: 首着不在清单 → user 末尾追加失败原因重试；第二次通过。
func TestProposeSolveFirstMoveRetry(t *testing.T) {
	ft := &fakeTransport{script: []scriptItem{
		scriptText("首选着法: h0-g2\n思路: 马跳"),
		scriptText("首选着法: a4-d4\n备选着法: 无\n思路: 闷杀"),
	}}
	result, err := ProposeSolveFirstMove(context.Background(), solveAssistBoard(t), solveAssistConfig(), ft, SolveAssistOptions{})
	if err != nil {
		t.Fatalf("调用错误: %v", err)
	}
	if result.Proposal.FirstMoveCode != "a4-d4" {
		t.Fatalf("重试后应通过: %+v", result)
	}
	if ft.callCount() != 2 {
		t.Fatalf("应调用 2 次: %d", ft.callCount())
	}
	_, user := chatContent(t, ft, 1)
	if !strings.Contains(user, "（上次回复无效：回复 h0-g2 不在合法清单中，请严格按三行格式重新回答）") {
		t.Fatalf("重试原因未追加: %q", user)
	}
}

// spec: 连续 2 次无效 → 提议空，说明为最后一次原因。
func TestProposeSolveFirstMoveExhausted(t *testing.T) {
	ft := &fakeTransport{script: []scriptItem{
		scriptText("好的，我来分析"),
		scriptText("首选着法: z9-z9"),
	}}
	result, err := ProposeSolveFirstMove(context.Background(), solveAssistBoard(t), solveAssistConfig(), ft, SolveAssistOptions{})
	if err != nil {
		t.Fatalf("调用错误: %v", err)
	}
	if result.Proposal.FirstMoveCode != "" {
		t.Fatalf("应为空提议: %+v", result)
	}
	if !strings.Contains(result.Message, "不在合法清单中") {
		t.Fatalf("说明 = %q", result.Message)
	}
}

// spec: 调用失败（onError）→ 重试后仍失败给出错误消息。
func TestProposeSolveFirstMoveErrorRetry(t *testing.T) {
	ft := &fakeTransport{script: []scriptItem{
		scriptError("HTTP 500"),
		scriptError("HTTP 502"),
	}}
	result, err := ProposeSolveFirstMove(context.Background(), solveAssistBoard(t), solveAssistConfig(), ft, SolveAssistOptions{})
	if err != nil {
		t.Fatalf("调用错误: %v", err)
	}
	if result.Proposal.FirstMoveCode != "" || result.Message != "HTTP 502" {
		t.Fatalf("结果 = %+v", result)
	}
}

// spec: 未配置短路 / 无合法着法短路。
func TestProposeSolveFirstMoveShortCircuit(t *testing.T) {
	ft := &fakeTransport{}
	unconfigured, err := ProposeSolveFirstMove(context.Background(), solveAssistBoard(t),
		LlmEndpointConfig{APIKey: "sk", Model: "m"}, ft, SolveAssistOptions{})
	if err != nil {
		t.Fatalf("调用错误: %v", err)
	}
	if unconfigured.Proposal.FirstMoveCode != "" || unconfigured.Message != "研究助手模型未配置" {
		t.Fatalf("未配置短路 = %+v", unconfigured)
	}

	noMoves, err := ProposeSolveFirstMove(context.Background(),
		mustBoard(t, "R2k1R3/R8/9/9/9/9/9/9/9/4K4 b - - 0 1"),
		solveAssistConfig(), ft, SolveAssistOptions{})
	if err != nil {
		t.Fatalf("调用错误: %v", err)
	}
	if noMoves.Proposal.FirstMoveCode != "" || noMoves.Message != "当前局面无合法着法" {
		t.Fatalf("无着短路 = %+v", noMoves)
	}
}
