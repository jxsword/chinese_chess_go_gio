package state

// PGN 导出等价用例集（翻译源 = 上游 frontend/test/storage/pgnWriter.spec.ts，
// test/features/record/pgn_writer_test.dart 8 条，07 文档 §5 F2）+ 快照锁定
//（基准与上游 __snapshots__/pgnWriter.spec.ts.snap 同源，逐字符复制）。

import (
	"strings"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 固定时间戳：本地 2026-10-02 08:00（快照与用例共用，取本地时间避免时区漂移）。
var (
	testCreatedAt = time.Date(2026, 10, 2, 8, 0, 0, 0, time.Local)
	testNow       = testCreatedAt
)

// 标准开局两步（炮二平五 / 马8进7）。
func sessionMoves() []rules.Move {
	return FillMovePieces(rules.FENInitial, []rules.Move{
		{From: rules.Position{Col: 7, Row: 7}, To: rules.Position{Col: 4, Row: 7}},
		{From: rules.Position{Col: 7, Row: 0}, To: rules.Position{Col: 6, Row: 2}},
	})
}

// 两步对局记录（spec sessionRecord：recordFromSession 反推 initialFen=标准开局）。
func sessionRecord() GameRecordData {
	return GameRecordData{
		Title:       "对局测试",
		Mode:        string(ModeHumanVsHuman),
		InitialFen:  rules.FENInitial,
		Moves:       sessionMoves(),
		RedName:     strPtr("玩家甲"),
		BlackName:   strPtr("玩家乙"),
		SolveStatus: SolveNone,
		Solutions:   [][]string{},
		CreatedAt:   testCreatedAt.UnixMilli(),
	}
}

// 残局求解记录（无对局走法，只有解法）。
func endgameRecord(status SolveStatus, solutions [][]string) GameRecordData {
	return GameRecordData{
		Title:       "残局测试",
		Mode:        "endgame",
		InitialFen:  "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1",
		Moves:       []rules.Move{},
		Result:      nil,
		SolveStatus: status,
		Solutions:   solutions,
		CreatedAt:   testCreatedAt.UnixMilli(),
	}
}

// 上游用例：标准七标签 + ICCS 着法
func TestWritePgnStandardHeaders(t *testing.T) {
	pgn := WritePgn(sessionRecord(), testNow)
	for _, want := range []string{
		`[Event "中国象棋 Ultra"]`,
		`[Red "玩家甲"]`,
		`[Black "玩家乙"]`,
		`[Result "*"]`,
		"1. h2e2 h9g7",
	} {
		if !strings.Contains(pgn, want) {
			t.Fatalf("PGN 缺少 %s\n%s", want, pgn)
		}
	}
	// 标准开局不写 SetFen。
	if strings.Contains(pgn, "[SetFen") {
		t.Fatalf("标准开局不应写 SetFen\n%s", pgn)
	}
}

// 上游用例：残局记录带 SetFen/FEN 与结果标记
func TestWritePgnEndgameSetFen(t *testing.T) {
	pgn := WritePgn(endgameRecord(SolveSolved, [][]string{{"h5h3"}}), testNow)
	for _, want := range []string{
		`[SetFen "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1"]`,
		`[Result "1-0"]`,
		`[Annotator "已破解"]`,
	} {
		if !strings.Contains(pgn, want) {
			t.Fatalf("PGN 缺少 %s\n%s", want, pgn)
		}
	}
}

// 上游用例：无解残局结果标记为 0-1
func TestWritePgnNoSolutionTag(t *testing.T) {
	pgn := WritePgn(endgameRecord(SolveNoSolution, nil), testNow)
	if !strings.Contains(pgn, `[Result "0-1"]`) {
		t.Fatalf("无解应标 0-1\n%s", pgn)
	}
}

// 上游用例：结果标记分支：对局胜负与和棋
func TestPgnResultTagBranches(t *testing.T) {
	r := sessionRecord()
	red := "redWins"
	black := "blackWins"
	draw := "draw"
	if got := PgnResultTag(GameRecordData{Mode: r.Mode, InitialFen: r.InitialFen, Moves: r.Moves, SolveStatus: SolveNone, Result: &red}); got != "1-0" {
		t.Fatalf("redWins → 1-0，实际 %s", got)
	}
	if got := PgnResultTag(GameRecordData{Mode: r.Mode, InitialFen: r.InitialFen, Moves: r.Moves, SolveStatus: SolveNone, Result: &black}); got != "0-1" {
		t.Fatalf("blackWins → 0-1，实际 %s", got)
	}
	if got := PgnResultTag(GameRecordData{Mode: r.Mode, InitialFen: r.InitialFen, Moves: r.Moves, SolveStatus: SolveNone, Result: &draw}); got != "1/2-1/2" {
		t.Fatalf("draw → 1/2-1/2，实际 %s", got)
	}
	if got := PgnResultTag(endgameRecord(SolveTimeout, nil)); got != "*" {
		t.Fatalf("超时残局 → *，实际 %s", got)
	}
}

// 上游用例：ICCS 着法可被 Iccs 解析还原（导出可往返）
func TestWritePgnIccsRoundtrip(t *testing.T) {
	pgn := WritePgn(sessionRecord(), testNow)
	body := strings.SplitN(pgn, "\n\n", 2)[1]
	cleaned := strings.ReplaceAll(body, "*", "")
	fields := strings.Fields(cleaned)
	tokens := []string{}
	for _, f := range fields {
		if !strings.HasSuffix(f, ".") {
			tokens = append(tokens, f)
		}
	}
	if len(tokens) == 0 {
		t.Fatal("无着法 token")
	}
	for _, token := range tokens {
		if parsers.ParseIccs(token) == nil {
			t.Fatalf("token %s 无法被 Iccs 解析", token)
		}
	}
}

// 上游用例：多解残局列出全部解法并标注数量
func TestSolveVerdictLinesMultiple(t *testing.T) {
	lines := SolveVerdictLines(endgameRecord(SolveSolved, [][]string{{"h5h3"}, {"h5h4"}, {"h5g5"}}))
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "破解之法（3 条）:") || !strings.Contains(joined, "解法1: h5h3") || !strings.Contains(joined, "解法3: h5g5") {
		t.Fatalf("多解结论块不符\n%s", joined)
	}
}

// 上游用例：唯一解标注
func TestSolveVerdictLinesUnique(t *testing.T) {
	lines := SolveVerdictLines(endgameRecord(SolveSolved, [][]string{{"h5h3"}}))
	if !strings.Contains(strings.Join(lines, ""), "破解之法（1 条，唯一解）:") {
		t.Fatalf("唯一解标注不符\n%s", strings.Join(lines, "\n"))
	}
}

// 上游用例：无解与超时标记
func TestSolveVerdictLinesVerdicts(t *testing.T) {
	if no := strings.Join(SolveVerdictLines(endgameRecord(SolveNoSolution, nil)), ""); !strings.Contains(no, "无解") {
		t.Fatalf("无解文案不符：%s", no)
	}
	if to := strings.Join(SolveVerdictLines(endgameRecord(SolveTimeout, nil)), ""); !strings.Contains(to, "限时内未找到解法") {
		t.Fatalf("超时文案不符：%s", to)
	}
}

// --- PGN 导出快照（协议面；基准与上游 __snapshots__ 同源） ---

// 上游快照：标准对局导出全文
func TestSnapshotPgnStandardGame(t *testing.T) {
	want := `[Event "中国象棋 Ultra"]
[Site "ChineseChessUltra"]
[Date "2026.10.02"]
[Round "-"]
[Red "玩家甲"]
[Black "玩家乙"]
[Result "*"]

1. h2e2 h9g7 *
`
	got := WritePgn(sessionRecord(), testNow)
	if got != want {
		t.Fatalf("快照不符（标准对局）\n--- want ---\n%q\n--- got ---\n%q", want, got)
	}
}

// 上游快照：多解残局导出全文
func TestSnapshotPgnMultiSolutionEndgame(t *testing.T) {
	want := `[Event "中国象棋 Ultra"]
[Site "ChineseChessUltra"]
[Date "2026.10.02"]
[Round "-"]
[Red "红方"]
[Black "黑方"]
[Result "1-0"]
[Annotator "已破解"]
[SetFen "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1"]
[FEN "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1"]


`
	got := WritePgn(endgameRecord(SolveSolved, [][]string{{"h5h3"}, {"i4d4"}}), testNow)
	if got != want {
		t.Fatalf("快照不符（多解残局）\n--- want ---\n%q\n--- got ---\n%q", want, got)
	}
}

// 上游用例（shareText 组合断言的记谱面）：中文记谱序列（炮二平五 / 马8进7）
func TestChineseNotationsOpening(t *testing.T) {
	notations := ChineseNotations(rules.FENInitial, sessionMoves())
	if len(notations) != 2 {
		t.Fatalf("期望 2 条记谱，实际 %d", len(notations))
	}
	if notations[0] != "炮二平五" || notations[1] != "马8进7" {
		t.Fatalf("记谱不符：%v", notations)
	}
}
