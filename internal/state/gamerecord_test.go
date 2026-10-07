package state

// 记录数据面用例（翻译源 = 上游 frontend/test/storage/pgnWriter.spec.ts 的
// shareText 组合断言 + gameRecord.ts recordFromSession 语义）。

import (
	"strings"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 上游用例：中文记谱分享文本（writeShareText 与 PGN 组合断言）
func TestWriteShareTextSession(t *testing.T) {
	text := WriteShareText(sessionRecord())
	for _, want := range []string{
		"【中国象棋 Ultra 棋谱】对局测试",
		"模式: 双人对弈",
		"1. 炮二平五  马8进7",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("分享文本缺少 %q\n%s", want, text)
		}
	}
}

// 分享文本快照（协议面；sessionRecord 同一输入的上游组合断言全文）。
func TestSnapshotShareTextSession(t *testing.T) {
	want := `【中国象棋 Ultra 棋谱】对局测试
模式: 双人对弈  红方: 玩家甲  黑方: 玩家乙
着法（中文记谱）:
1. 炮二平五  马8进7`
	got := WriteShareText(sessionRecord())
	if got != want {
		t.Fatalf("分享文本快照不符\n--- want ---\n%q\n--- got ---\n%q", want, got)
	}
}

// 残局分享文本：SetFen 行 + 求解结论块。
func TestWriteShareTextEndgame(t *testing.T) {
	text := WriteShareText(endgameRecord(SolveSolved, [][]string{{"h5h3"}, {"i4d4"}}))
	for _, want := range []string{
		"模式: 残局破解",
		"起始 FEN: 3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1",
		"破解之法（2 条）:",
		"解法2: i4d4",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("残局分享文本缺少 %q\n%s", want, text)
		}
	}
}

// 上游 recordFromSession 语义：标题留空自动生成、initialFen 从终局反推、
// solveStatus=none、moves 拷贝。
func TestRecordFromSession(t *testing.T) {
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.Local)
	moves := sessionMoves()
	finalFen := FinalFenOf(rules.FENInitial, moves)
	rec := RecordFromSession(RecordFromSessionInput{
		Mode:     string(ModeHumanVsAi),
		FinalFen: finalFen,
		Moves:    moves,
	}, now)

	if rec.Title != "2026-10-07 人机对战" {
		t.Fatalf("标题自动生成不符：%q", rec.Title)
	}
	if rec.InitialFen != rules.FENInitial {
		t.Fatalf("initialFen 反推应= 标准开局，实际 %s", rec.InitialFen)
	}
	if rec.SolveStatus != SolveNone || len(rec.Moves) != 2 || rec.CreatedAt != now.UnixMilli() {
		t.Fatalf("会话投影不符：%+v", rec)
	}
	// 标题优先用输入
	rec2 := RecordFromSession(RecordFromSessionInput{Title: "我的谱", Mode: "humanVsAi", FinalFen: finalFen, Moves: moves}, now)
	if rec2.Title != "我的谱" {
		t.Fatalf("输入标题应优先：%q", rec2.Title)
	}
}

// 模式/结果标签（记录库列表行消费）。
func TestModeAndResultLabels(t *testing.T) {
	cases := map[string]string{
		"humanVsAi":    "人机对战",
		"humanVsHuman": "双人对弈",
		"aiVsAi":       "机机对战",
		"humanVsLlm":   "人机(大模型)",
		"llmVsLlm":     "大模型对战",
		"endgame":      "残局破解",
	}
	for mode, want := range cases {
		if got := ModeLabelOf(mode); got != want {
			t.Fatalf("modeLabelOf(%s)=%s，期望 %s", mode, got, want)
		}
	}
	if ResultLabel("redWins") != "红方胜" || ResultLabel("blackWins") != "黑方胜" || ResultLabel("draw") != "和棋" {
		t.Fatal("resultLabel 分支不符")
	}
}
