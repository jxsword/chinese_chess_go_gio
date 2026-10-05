package engine

// ChessAi 三接口行为等价单测（09 §2.2；Electron 版 chessAi.spec.ts
// "行为等价（Dart ai_engine_test.dart / ai_engine_ex_test.dart 用例集）"的 Go 对应）。
import (
	"os"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// FindBestMove：初始局面返回合法走法。
func TestFindBestMoveInitialLegal(t *testing.T) {
	board := rules.Initial()
	fenBefore := board.ToFen()
	move, err := FindBestMove(board.ToFen(), FindBestMoveOptions{Difficulty: 1})
	if err != nil {
		t.Fatalf("FindBestMove 报错: %v", err)
	}
	if move == nil {
		t.Fatal("应返回走法")
	}
	legal := false
	for _, m := range board.LegalMovesFor(move.From) {
		if rules.SamePos(m.To, move.To) {
			legal = true
			break
		}
	}
	if !legal {
		t.Errorf("走法 (%d,%d)->(%d,%d) 不合法", move.From.Col, move.From.Row, move.To.Col, move.To.Row)
	}
	if board.ToFen() != fenBefore {
		t.Error("调用方棋盘不应被修改")
	}
}

// FindBestMove：优先白吃高价值棋子（difficulty 2）。
func TestFindBestMoveEatsRook(t *testing.T) {
	fen := "4k4/9/9/9/4r4/4P4/9/4C4/9/4K4 w - - 0 1"
	move, err := FindBestMove(fen, FindBestMoveOptions{Difficulty: 2})
	if err != nil || move == nil {
		t.Fatalf("应返回走法: %v", err)
	}
	if !rules.SamePos(move.To, rules.Pos(4, 4)) {
		t.Errorf("应吃车到 (4,4)，got (%d,%d)", move.To.Col, move.To.Row)
	}
	if move.Captured == nil || move.Captured.Kind != rules.Rook {
		t.Errorf("captured 应为车")
	}
}

// FindBestMove：被将死局面返回 null。
func TestFindBestMoveDeadPosition(t *testing.T) {
	fen := "R3k4/4R4/4P4/9/9/9/9/9/9/4K4 b - - 0 1"
	board := mustBoard(t, fen)
	if !board.IsCheckmate(rules.Black) {
		t.Fatal("黑方应被将死")
	}
	move, err := FindBestMove(fen, FindBestMoveOptions{Difficulty: 1})
	if err != nil {
		t.Fatalf("FindBestMove 报错: %v", err)
	}
	if move != nil {
		t.Errorf("被将死应返回 null，got (%d,%d)->(%d,%d)", move.From.Col, move.From.Row, move.To.Col, move.To.Row)
	}
}

// FindBestMove：被将军时优先解将而不是进攻（difficulty 1）。
func TestFindBestMoveEscapesCheck(t *testing.T) {
	fen := "4k4/9/9/9/4R4/9/9/9/9/4K4 w - - 0 1"
	move, err := FindBestMove(fen, FindBestMoveOptions{Difficulty: 1})
	if err != nil || move == nil {
		t.Fatalf("应返回走法: %v", err)
	}
	board := mustBoard(t, fen)
	board.ApplyMove(*move)
	if board.IsCheck(rules.Red) {
		t.Error("走后红方不应仍被将军")
	}
}

// FindBestMove：难度 1-5 均给出合法走法。
func TestFindBestMoveAllDifficultiesLegal(t *testing.T) {
	board := rules.Initial()
	for level := 1; level <= 5; level++ {
		move, err := FindBestMove(board.ToFen(), FindBestMoveOptions{Difficulty: level})
		if err != nil {
			t.Fatalf("难度 %d 报错: %v", level, err)
		}
		if move == nil {
			t.Fatalf("难度 %d 应返回走法", level)
		}
		legal := false
		for _, m := range board.LegalMovesFor(move.From) {
			if rules.SamePos(m.To, move.To) {
				legal = true
				break
			}
		}
		if !legal {
			t.Fatalf("难度 %d 的走法 (%d,%d)->(%d,%d) 应合法", level, move.From.Col, move.From.Row, move.To.Col, move.To.Row)
		}
	}
}

// FindBestMoveEx：Top-K 降序排列，最佳与首位一致。
func TestFindBestMoveExTopKOrdering(t *testing.T) {
	fen := "3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1"
	report, err := FindBestMoveEx(fen, FindBestMoveExOptions{Depth: 4, TopK: 3})
	if err != nil || report == nil {
		t.Fatalf("应返回报告: %v", err)
	}
	if len(report.TopK) != 3 {
		t.Fatalf("topK 长度 = %d, want 3", len(report.TopK))
	}
	for i := 1; i < len(report.TopK); i++ {
		if report.TopK[i].Cp > report.TopK[i-1].Cp {
			t.Errorf("topK[%d].cp=%d > topK[%d].cp=%d，应降序", i, report.TopK[i].Cp, i-1, report.TopK[i-1].Cp)
		}
	}
	if moveKey(report.Best) != moveKey(report.TopK[0].Move) || report.BestCp != report.TopK[0].Cp {
		t.Error("best 应与 topK 首项一致")
	}
}

// FindBestMoveEx：吃车着法进入 Top-K 且大幅占优（> 800 厘兵）。
func TestFindBestMoveExEatRookInTopK(t *testing.T) {
	fen := "3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1"
	report, err := FindBestMoveEx(fen, FindBestMoveExOptions{Depth: 4, TopK: 8})
	if err != nil || report == nil {
		t.Fatalf("应返回报告: %v", err)
	}
	found := false
	for _, e := range report.TopK {
		if e.Move.From.Col == 0 && e.Move.From.Row == 6 && e.Move.To.Col == 0 && e.Move.To.Row == 4 {
			found = true
			if e.Cp <= 800 {
				t.Errorf("吃车着法应 > 800 厘兵，got %d", e.Cp)
			}
		}
	}
	if !found {
		t.Error("吃车着法未入 Top-K")
	}
}

// FindBestMoveEx：一步杀局面 bestCp 达到将杀分量级（> 25000）。
func TestFindBestMoveExMateBestCp(t *testing.T) {
	fen := "3k5/9/9/9/R8/8R/9/9/9/4K4 w - - 0 1"
	report, err := FindBestMoveEx(fen, FindBestMoveExOptions{Depth: 4, TopK: 3})
	if err != nil || report == nil {
		t.Fatalf("应返回报告: %v", err)
	}
	if report.BestCp <= 25000 {
		t.Errorf("bestCp = %d, want > 25000", report.BestCp)
	}
}

// FindBestMoveEx：黑方被将死（轮黑无合法着法）返回 null。
func TestFindBestMoveExDeadNull(t *testing.T) {
	report, err := FindBestMoveEx("R3k4/9/9/9/9/4R4/9/9/9/4K4 b - - 0 1", FindBestMoveExOptions{Depth: 2})
	if err != nil {
		t.Fatalf("FindBestMoveEx 报错: %v", err)
	}
	if report != nil {
		t.Error("被将死应返回 null")
	}
}

// EvaluateMove：好着（吃车）大幅占优，消极着法分差明显。
func TestEvaluateMoveGoodVsPassive(t *testing.T) {
	fen := "3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1"
	good, err := EvaluateMove(fen, rules.Move{From: rules.Pos(0, 6), To: rules.Pos(0, 4)}, EvaluateMoveOptions{Depth: 3})
	if err != nil {
		t.Fatalf("EvaluateMove 报错: %v", err)
	}
	passive, err := EvaluateMove(fen, rules.Move{From: rules.Pos(0, 6), To: rules.Pos(5, 6)}, EvaluateMoveOptions{Depth: 3})
	if err != nil {
		t.Fatalf("EvaluateMove 报错: %v", err)
	}
	if good == nil || passive == nil {
		t.Fatalf("两着法均应可评估: good=%v passive=%v", good, passive)
	}
	if *passive >= *good {
		t.Errorf("消极着法 %d 应低于吃车 %d", *passive, *good)
	}
}

// EvaluateMove：非法着法返回 null（起点无己方子 / 走完自将 / 非轮走方棋子）。
func TestEvaluateMoveIllegalNull(t *testing.T) {
	fen := "3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1"
	// 起点无子：(0,0) 为空格。
	cp, err := EvaluateMove(fen, rules.Move{From: rules.Pos(0, 0), To: rules.Pos(0, 1)}, EvaluateMoveOptions{})
	if err != nil {
		t.Fatalf("EvaluateMove 报错: %v", err)
	}
	if cp != nil {
		t.Errorf("空格起点应返回 null，got %d", *cp)
	}
	// 黑车（非轮走方棋子）：(0,4)→(0,6)。
	cp, err = EvaluateMove(fen, rules.Move{From: rules.Pos(0, 4), To: rules.Pos(0, 6)}, EvaluateMoveOptions{})
	if err != nil {
		t.Fatalf("EvaluateMove 报错: %v", err)
	}
	if cp != nil {
		t.Errorf("非轮走方棋子应返回 null，got %d", *cp)
	}
}

// EvaluateMove：走完即杀返回 MATE_SCORE（03 §6.3）。
func TestEvaluateMoveMateScore(t *testing.T) {
	fen := "3k5/9/9/9/R8/8R/9/9/9/4K4 w - - 0 1"
	// 车 (0,5)→(3,5)?? 一步杀着法：R(8,5)→(3,5)（金标准 mate 局 best）。
	cp, err := EvaluateMove(fen, rules.Move{From: rules.Pos(8, 5), To: rules.Pos(3, 5)}, EvaluateMoveOptions{Depth: 3})
	if err != nil {
		t.Fatalf("EvaluateMove 报错: %v", err)
	}
	if cp == nil || *cp != MATE_SCORE {
		t.Errorf("一步杀应返回 MATE_SCORE=%d，got %v", MATE_SCORE, cp)
	}
}

// EvaluateMove：几何非法（蹩腿马）返回 null——ApplyMove 不校验，须前置 hasPseudoMove。
func TestEvaluateMovePseudoIllegalNull(t *testing.T) {
	// 红马 (1,9)：蹩腿位 (1,8)?? 初始局面马 (1,9) 被 (1,8)?? 初始 (1,8) 为空，
	// 改用相：红相 (2,9)→(4,7) 田字但象眼 (3,8) 为空为合法；取 (2,9)→(0,7) 象眼 (1,8) 空。
	// 构造蹩腿：初始局面红马 (7,9) 前往 (6,7)，马腿 (7,8) 为空 → 合法；
	// 马腿 (7,8) 放红相后蹩腿。直接以初始局面马 (7,9)→(8,7)：马腿 (7,8) 空，合法。
	// 用马 (7,9)→(5,8)?? 非"日"字，必然无此伪合法走法。
	fen := rules.FENInitial
	cp, err := EvaluateMove(fen, rules.Move{From: rules.Pos(7, 9), To: rules.Pos(5, 8)}, EvaluateMoveOptions{Depth: 2})
	if err != nil {
		t.Fatalf("EvaluateMove 报错: %v", err)
	}
	if cp != nil {
		t.Errorf("非日字马步应返回 null，got %d", *cp)
	}
}

// ShouldAbort 探针（返回 false）经 Options 注入不影响正常评估（探针节奏对齐 03 §6）。
func TestEvaluateMoveShouldAbortNotTriggered(t *testing.T) {
	fen := "3k5/9/9/9/r8/9/R8/9/9/4K4 w - - 0 1"
	cp, err := EvaluateMove(fen, rules.Move{From: rules.Pos(0, 6), To: rules.Pos(0, 4)}, EvaluateMoveOptions{
		Depth:       3,
		ShouldAbort: func() bool { return false },
	})
	if err != nil || cp == nil {
		t.Fatalf("应正常评估: %v %v", cp, err)
	}
}

// 性能门（RUN_SLOW=1 启用，09 §2.2：难度 5 应答 ≤ 7.5s；对齐 Electron 版 @slow 用例）。
func TestPerformanceGateDifficulty5(t *testing.T) {
	if os.Getenv("RUN_SLOW") != "1" {
		t.Skip("性能门：需 RUN_SLOW=1（09 §2.2）")
	}
	board := rules.Initial()
	start := time.Now()
	move, err := FindBestMove(board.ToFen(), FindBestMoveOptions{Difficulty: 5})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("FindBestMove 报错: %v", err)
	}
	if move == nil {
		t.Fatal("应返回走法")
	}
	if elapsed > 7500*time.Millisecond {
		t.Errorf("难度 5 应答 %v > 7.5s 性能门", elapsed)
	}
	t.Logf("性能门：难度 5 初始局面应答 %v", elapsed)
}
