package llm

// 提示词逐字快照测试（T4.2，09 §2 llm_prompt_v2；test/llm/promptV2.spec.ts 移植）：
// 任何对提示词文本的改动必须在此显式 review diff——禁止意译改写、"优化"措辞。
// 金标准 = Flutter 版 llm_move_source.dart / hybrid_llm_move_source.dart 原文
//（基准=Electron 版既有快照，09 §2.3）。

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// 黑王 e0、红帅 d9、红马 e9 的小局面（双方不照面）。
const fenKnight = "4k4/9/9/9/9/9/9/9/9/3KN4 w - - 0 1"

// cannonB2E2 红炮 b2-e2（炮八平五）。
func cannonB2E2() rules.Move {
	return rules.Move{
		Piece: &rules.Piece{Kind: rules.Cannon, Side: rules.Red},
		From:  rules.Pos(1, 7),
		To:    rules.Pos(4, 7),
	}
}

// knightH0G2 黑马 h0-g2（马8进7）。
func knightH0G2() rules.Move {
	return rules.Move{
		Piece: &rules.Piece{Kind: rules.Knight, Side: rules.Black},
		From:  rules.Pos(7, 0),
		To:    rules.Pos(6, 2),
	}
}

func TestSystemV1Snapshot(t *testing.T) {
	// system（红方）逐字
	want := "你是中国象棋对弈引擎的着法接口，本局执红方。\n" +
		"坐标约定：列用字母 a-i（从左到右），行用数字 0-9" +
		"（0 为黑方底线、棋盘顶部，9 为红方底线、棋盘底部）。\n" +
		"你只能从用户提供的「合法着法清单」中选择一步，禁止编造清单之外的着法。\n" +
		"\n" +
		"【回复格式（唯一允许的格式，违反即视为无效）】\n" +
		"整个回复只包含一行，形式为：\n" +
		"着法: 起点-终点\n" +
		"示例：着法: b2-e2\n" +
		"\n" +
		"禁止输出：任何解释、推理过程、心理活动、道歉、开场白、" +
		"markdown、代码块、引号、多行文本。你的回复将被程序逐字解析，" +
		"任何多余字符都会导致这步棋作废。"
	if got := SystemV1(rules.Red); got != want {
		t.Fatalf("systemV1 快照不符：\n%q", got)
	}
	// system（黑方）仅执方变化
	black := SystemV1(rules.Black)
	if !strings.Contains(black, "本局执黑方。") || strings.Contains(black, "红方。") {
		t.Errorf("systemV1 黑方执方不符")
	}
}

func TestUserV1Snapshot(t *testing.T) {
	board := mustBoard(t, "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1")
	// user（开局无历史）：【最近着法】占位与两步清单
	want := "【当前局面 FEN】3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1\n" +
		"【轮走方】红方（该方是你）\n" +
		"【最近着法】（开局，暂无历史）\n" +
		"【合法着法清单（共 2 条，必须从中选择一条）】\n" +
		"e9-e8, e9-f9\n" +
		"【输出】仅一行，格式：着法: 起点-终点（起点与终点均取自上方清单）"
	if got := UserV1(board, nil, []string{"e9-e8", "e9-f9"}); got != want {
		t.Fatalf("userV1 快照不符：\n%q", got)
	}

	// user（有历史）：中文记法最新在最后，仅取最近 12 手
	got := UserV1(board, []rules.Move{cannonB2E2(), knightH0G2()}, []string{"e9-e8"})
	if !strings.Contains(got, "【最近着法（中文记法，最新在最后）】炮八平五  马8进7\n") {
		t.Fatalf("userV1 历史行不符：\n%s", got)
	}
	thirteen := make([]rules.Move, 13)
	for i := range thirteen {
		side := rules.Red
		if i%2 == 1 {
			side = rules.Black
		}
		row := 9
		if i%2 == 1 {
			row = 0
		}
		thirteen[i] = rules.Move{
			Piece: &rules.Piece{Kind: rules.Rook, Side: side},
			From:  rules.Pos(0, row),
			To:    rules.Pos(1, row),
		}
	}
	text := UserV1(board, thirteen, []string{"e9-e8"})
	var line string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "【最近着法") {
			line = l
		}
	}
	var parts []string
	for _, m := range thirteen[len(thirteen)-12:] {
		if m.Piece.Side == rules.Red {
			parts = append(parts, "车九平八")
		} else {
			parts = append(parts, "车1平2")
		}
	}
	expectedLast12 := "【最近着法（中文记法，最新在最后）】" + strings.Join(parts, "  ")
	if line != expectedLast12 {
		t.Fatalf("12 手截断不符：\n%q\nwant\n%q", line, expectedLast12)
	}
}

func TestRetryFeedbackSnapshot(t *testing.T) {
	want := "\n\n你上一次的回复无效（着法 b2-e2 不在合法清单中）。" +
		"请重新回答：整个回复只含一行「着法: 起点-终点」，" +
		"着法必须取自合法着法清单，不要输出任何其他文字。"
	if got := RetryFeedback("着法 b2-e2 不在合法清单中"); got != want {
		t.Fatalf("retryFeedback 快照不符：%q", got)
	}
}

func TestSystemV2Snapshot(t *testing.T) {
	head := "你是中国象棋对弈引擎的着法接口，本局执%s。\n" +
		"坐标约定：列用字母 a-i（从左到右），行用数字 0-9" +
		"（0 为黑方底线、棋盘顶部，9 为红方底线、棋盘底部）。\n" +
		"你只能从用户提供的「合法着法清单」中选择一步，禁止编造清单之外的着法。\n" +
		"\n" +
		"【回复格式（唯一允许的格式，共两段）】\n" +
		"第一段以「分析:」开头，用一两句话（不超过 100 字）说明你的计划" +
		"（进攻目标、需要提防的威胁）。\n" +
		"最后一段为一行，形式为：\n" +
		"着法: 起点-终点\n" +
		"示例：着法: b2-e2\n" +
		"\n"
	// systemV2（off/护航：无分档引导）
	want := fmt.Sprintf(head, "红方") + "合法着法清单中每条着法附有括号注解（中文记法/吃子/将军）。"
	if got := SystemV2(rules.Red, false); got != want {
		t.Fatalf("systemV2 快照不符：\n%q", got)
	}
	// systemV2（候选：withBucketGuide 分档引导）
	wantBlack := fmt.Sprintf(head, "黑方") +
		"合法着法清单中每条着法附有括号注解（中文记法/吃子/将军）" +
		"与「—」后的引擎评估分档，请优先考虑评估为「最佳/均势」的着法，" +
		"避免选择「大亏/致命」档的着法。"
	if got := SystemV2(rules.Black, true); got != wantBlack {
		t.Fatalf("systemV2 分档快照不符：\n%q", got)
	}
}

func TestUserV2SnapshotOpening(t *testing.T) {
	// userV2（开局）：棋盘图 + 注解清单全字快照
	board := mustBoard(t, fenKnight)
	legal := []string{"d9-d8", "e9-f7", "e9-d7", "e9-g8"}
	moves := make([]rules.Move, 0, len(legal))
	for _, code := range legal {
		m := mvCode(code)
		moves = append(moves, m)
	}
	want := "【当前局面 FEN】4k4/9/9/9/9/9/9/9/9/3KN4 w - - 0 1\n" +
		"【棋盘图】\n" +
		"    a b c d e f g h i\n" +
		"0  . . . . k . . . .\n" +
		"1  . . . . . . . . .\n" +
		"2  . . . . . . . . .\n" +
		"3  . . . . . . . . .\n" +
		"4  . . . . . . . . .\n" +
		"5  . . . . . . . . .\n" +
		"6  . . . . . . . . .\n" +
		"7  . . . . . . . . .\n" +
		"8  . . . . . . . . .\n" +
		"9  . . . K N . . . .\n" +
		"【轮走方】红方（该方是你）\n" +
		"【对局着法（中文记法，最新在最后）】（开局，暂无历史）\n" +
		"【合法着法清单（共 4 条，必须从中选择一条；括号内为中文记法/吃子/将军注解）】\n" +
		"d9-d8(帅六进一)\n" +
		"e9-f7(马五进四)\n" +
		"e9-d7(马五进六)\n" +
		"e9-g8(马五进三)\n" +
		"【输出】先输出「分析:」段，最后一行输出「着法: 起点-终点」（起点与终点均取自上方清单）"
	if got := UserV2(board, nil, moves); got != want {
		t.Fatalf("userV2 开局快照不符：\n%q", got)
	}
}

func TestUserV2RepetitionWarning(t *testing.T) {
	board := mustBoard(t, fenKnight)
	kingRed := rules.Move{Piece: &rules.Piece{Kind: rules.King, Side: rules.Red}, From: rules.Pos(3, 9), To: rules.Pos(3, 8)}
	kingBlack := rules.Move{Piece: &rules.Piece{Kind: rules.King, Side: rules.Black}, From: rules.Pos(4, 0), To: rules.Pos(4, 1)}
	back1 := rules.Move{Piece: &rules.Piece{Kind: rules.King, Side: rules.Red}, From: rules.Pos(3, 8), To: rules.Pos(3, 9)}
	back2 := rules.Move{Piece: &rules.Piece{Kind: rules.King, Side: rules.Black}, From: rules.Pos(4, 1), To: rules.Pos(4, 0)}
	history := []rules.Move{cannonB2E2(), knightH0G2(), kingRed, kingBlack, back1, back2}
	text := UserV2(board, history, nil)
	if !strings.Contains(text, "【对局着法（中文记法，最新在最后）】炮八平五  马8进7  帅六进一  将5进1  帅六退一  将5退1\n") {
		t.Fatalf("历史行不符：\n%s", text)
	}
	if !strings.Contains(text, "【警示】最近着法出现来回重复。长将/长捉判负，"+
		"重复局面会被视为无效——请选择打破循环的着法。\n") {
		t.Fatalf("警示行不符：\n%s", text)
	}
}

func TestUserV2NoWarningAndTruncation(t *testing.T) {
	board := mustBoard(t, fenKnight)
	// 无循环时不加警示
	if got := UserV2(board, []rules.Move{cannonB2E2(), knightH0G2()}, nil); strings.Contains(got, "【警示】") {
		t.Error("无循环不应加警示")
	}
	// >60 着从最早截断
	long := make([]rules.Move, 61)
	for i := range long {
		side := rules.Red
		if i%2 == 1 {
			side = rules.Black
		}
		row := 9
		if i%2 == 1 {
			row = 0
		}
		long[i] = rules.Move{
			Piece: &rules.Piece{Kind: rules.Rook, Side: side},
			From:  rules.Pos(0, row),
			To:    rules.Pos(1, row),
		}
	}
	var parts []string
	for _, m := range long[len(long)-60:] {
		if m.Piece.Side == rules.Red {
			parts = append(parts, "车九平八")
		} else {
			parts = append(parts, "车1平2")
		}
	}
	wantText := strings.Join(parts, "  ")
	if got := HistoryTextV2(long); got != wantText {
		t.Fatalf("historyTextV2 截断不符：\n%q", got)
	}
	text := UserV2(board, long, nil)
	var line string
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "【对局着法") {
			line = l
		}
	}
	if line != "【对局着法（中文记法，最新在最后）】"+wantText {
		t.Fatalf("userV2 历史行不符：\n%q", line)
	}
}

func TestRetryFeedbackV2Snapshot(t *testing.T) {
	// 含/不含上次着法
	want1 := "\n\n你上一次的回复无效（无法从回复中解析出着法）。" +
		"着法必须取自合法着法清单。" +
		"请重新回答：先「分析:」一两句，最后一行「着法: 起点-终点」。"
	if got := RetryFeedbackV2("无法从回复中解析出着法", ""); got != want1 {
		t.Fatalf("retryFeedbackV2 快照不符：%q", got)
	}
	want2 := "\n\n你上一次的回复的着法 x9-x9无效（着法 x9-x9 不在合法清单中）。" +
		"着法必须取自合法着法清单。" +
		"请重新回答：先「分析:」一两句，最后一行「着法: 起点-终点」。"
	if got := RetryFeedbackV2("着法 x9-x9 不在合法清单中", "x9-x9"); got != want2 {
		t.Fatalf("retryFeedbackV2 带着法快照不符：%q", got)
	}
}

func TestVetoFeedbackSnapshot(t *testing.T) {
	// vetoFeedback（hybrid_llm_move_source.dart:291-295 逐字）
	user := "【当前局面 FEN】x\n"
	got := VetoFeedback(user, "a9-a8", "大亏（丢一马/一炮级）", 300)
	want := user + "\n\n" +
		"【参谋否决】你上一次选择的 a9-a8 " +
		"会被引擎惩罚（大亏（丢一马/一炮级），" +
		"相对最佳损失 300 厘兵）。请重新从候选清单中选择，" +
		"优先考虑「最佳/均势」档；先「分析:」一句，最后一行「着法: 起点-终点」。"
	if got != want {
		t.Fatalf("vetoFeedback 快照不符：\n%q", got)
	}
}

func TestLooksLikeRepetition(t *testing.T) {
	mk := func(fc, fr, tc, tr int) rules.Move {
		return rules.Move{From: rules.Pos(fc, fr), To: rules.Pos(tc, tr)}
	}
	// 红 A→B、黑 C→D、红 B→A、黑 D→C → 循环
	loop := []rules.Move{mk(0, 9, 1, 9), mk(3, 0, 4, 0), mk(1, 9, 0, 9), mk(4, 0, 3, 0)}
	if !LooksLikeRepetition(loop) {
		t.Error("四手互逆应判循环")
	}
	// 只有最后一手回退（非两两互逆）→ 非循环
	if LooksLikeRepetition(loop[:3]) {
		t.Error("三手不应判循环")
	}
	if LooksLikeRepetition([]rules.Move{loop[0], loop[1], loop[2], mk(8, 0, 7, 0)}) {
		t.Error("末手非互逆不应判循环")
	}
	if LooksLikeRepetition(nil) {
		t.Error("空历史不应判循环")
	}
}
