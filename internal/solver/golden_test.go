package solver

// 残局求解器金标准测试（T6.1，04 文档 + 09 §2.2：6 个内置验证 FEN 对拍，
// 与 Electron 版 test/solver/solver.spec.ts 逐项一致——其源为 Flutter 版
// endgame_solver_test.dart / docs/phase4/03 内置数据）。
//
// FEN-A 多解 / FEN-B 无解 / FEN-C 超时 / FEN-D 已将死（0 步解）/
// FEN-E 非法 FEN / FEN-F 缺王（求解器不校验双王，由工作室五条校验拦截，
// 此处仅记录与原版一致的行为）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

/** 双车马闷杀残局（红先，两车各有一路 1 着杀；全部棋子位置合法）。 */
const fenA = "3k5/9/9/9/R8/8R/9/9/9/4K4 w"

/** 裸王局面（无解）。 */
const fenB = "3k5/9/9/9/9/9/9/9/9/4K4 w"

/** 初始局面（极短限时应超时）。 */
const fenC = "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w"

// 对方已被将死（0 步解）。求解器层夹具——工作室五条校验第 4 条（"轮走方行棋前
// 对方已被将军"）会在 UI 侧拦截此类局面，该分支经工作室不可达，仅记录求解器与
// 原版一致的行为（同 FEN-F 缺王口径）。原版夹具含 4 红车（子力数量非法），
// 求解器按设计不校验子力数量。
const fenD = "R2k4R/R8/9/9/3R5/9/9/9/9/4K4 w"

/** 非法 FEN（末行 10 列）。 */
const fenE = "k8/9/9/9/9/9/9/9/9/4K5 w"

/** 缺王（仅红帅）。 */
const fenF = "9/9/9/9/9/9/9/9/9/4K4 w"

func TestFenAMultiSolutions(t *testing.T) {
	result, err := Solve(fenA, Options{TimeLimitMs: 10_000, MaxPlies: 3})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	if result.Status != StatusSolved {
		t.Fatalf("status = %q, want solved", result.Status)
	}
	if len(result.Solutions) < 2 {
		t.Fatalf("解法数 = %d, want >= 2", len(result.Solutions))
	}
	toSquares := map[string]bool{}
	for _, solution := range result.Solutions {
		if len(solution.Moves) != 1 { // 一着制胜
			t.Fatalf("解法着数 = %d, want 1", len(solution.Moves))
		}
		to := solution.Moves[0].To
		toSquares[toString(to)] = true
	}
	if !toSquares["3,4"] { // 车一 (0,4) 平 3 列
		t.Errorf("解法缺少 (3,4)（车一平 3 列）: %v", toSquares)
	}
	if !toSquares["3,5"] { // 车二 (8,5) 平 3 列
		t.Errorf("解法缺少 (3,5)（车二平 3 列）: %v", toSquares)
	}
}

func TestFenAIsWinningFirstMove(t *testing.T) {
	win, err := IsWinningFirstMove(fenA, rules.Move{
		From: rules.Pos(0, 4),
		To:   rules.Pos(3, 4),
	}, Options{MaxPlies: 1})
	if err != nil || !win {
		t.Errorf("车一 (0,4)->(3,4) 应为必胜首着, got %v %v", win, err)
	}

	lose, err := IsWinningFirstMove(fenA, rules.Move{
		From: rules.Pos(2, 1),
		To:   rules.Pos(4, 2),
	}, Options{MaxPlies: 1})
	if err != nil || lose {
		t.Errorf("起点无子的着法 1 着内应非必胜, got %v %v", lose, err)
	}
}

func TestSolveIsUnique(t *testing.T) {
	// FEN-A 的单解变体：仅保留一路杀着（去掉第二台车）后深度内唯一。
	single, err := Solve("3k5/9/9/9/R8/9/9/1R7/9/4K4 w", Options{TimeLimitMs: 10_000, MaxPlies: 3})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	if single.Status == StatusSolved && len(single.Solutions) == 1 {
		if !SolveIsUnique(single) {
			t.Errorf("solved 且单条解法时 unique 应为真")
		}
	} else {
		// 结构验证：非 solved 或多解时 unique 必为假。
		if SolveIsUnique(single) {
			t.Errorf("非单解时 unique 应为假")
		}
	}
	if SolveIsUnique(Result{Status: StatusSolved, Solutions: nil, Elapsed: 0, SearchedPlies: 0}) {
		t.Errorf("solved 且无解法时 unique 应为假")
	}
}

func TestFenBNoSolution(t *testing.T) {
	result, err := Solve(fenB, Options{TimeLimitMs: 10_000, MaxPlies: 5})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	if result.Status != StatusNoSolution {
		t.Fatalf("status = %q, want noSolution", result.Status)
	}
	if len(result.Solutions) != 0 {
		t.Fatalf("solutions = %d, want 0", len(result.Solutions))
	}
	if result.SearchedPlies != 5 {
		t.Fatalf("searchedPlies = %d, want 5", result.SearchedPlies)
	}
}

func TestFenCTimeout(t *testing.T) {
	result, err := Solve(fenC, Options{TimeLimitMs: 1, MaxPlies: 9})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	if result.Status != StatusTimeout {
		t.Fatalf("status = %q, want timeout", result.Status)
	}
	if len(result.Solutions) != 0 {
		t.Fatalf("solutions = %d, want 0", len(result.Solutions))
	}
	if result.SearchedPlies < 1 {
		t.Fatalf("searchedPlies = %d, want >= 1", result.SearchedPlies)
	}
}

func TestFenDAlreadyCheckmated(t *testing.T) {
	result, err := Solve(fenD, Options{TimeLimitMs: 5_000, MaxPlies: 3})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	if result.Status != StatusSolved {
		t.Fatalf("status = %q, want solved", result.Status)
	}
	if len(result.Solutions) != 0 {
		t.Fatalf("solutions = %d, want 0（对方已被将死/困毙，无需再走）", len(result.Solutions))
	}
	if result.SearchedPlies != 0 {
		t.Fatalf("searchedPlies = %d, want 0", result.SearchedPlies)
	}
}

func TestFenEInvalidFen(t *testing.T) {
	_, err := Solve(fenE, Options{TimeLimitMs: 1_000, MaxPlies: 3})
	if err == nil {
		t.Fatal("非法 FEN 应上抛解析错误（工作室校验层拦截，不进入求解）")
	}
	var fenErr *rules.FenFormatError
	if !errors.As(err, &fenErr) {
		t.Fatalf("错误应为 FenFormatError, got %T: %v", err, err)
	}
}

func TestFenFMissingKing(t *testing.T) {
	// 缺王：与原版一致按"对方无子可动=困毙"判胜（上游工作室校验拦截缺王局面）。
	result, err := Solve(fenF, Options{TimeLimitMs: 5_000, MaxPlies: 3})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	if result.Status != StatusSolved {
		t.Fatalf("status = %q, want solved", result.Status)
	}
	if result.SearchedPlies != 1 {
		t.Fatalf("searchedPlies = %d, want 1", result.SearchedPlies)
	}
}

func TestIsWinningFirstMoveIllegal(t *testing.T) {
	// 首着不合法（起点无子/目标不符）→ false。
	win, err := IsWinningFirstMove(fenA, rules.Move{
		From: rules.Pos(0, 0),
		To:   rules.Pos(3, 4),
	}, Options{MaxPlies: 3})
	if err != nil || win {
		t.Errorf("不合法首着应返回 false, got %v %v", win, err)
	}
}

// Go 版差异（04 §5）：取消经 ctx 传递到递归深处——取消探针每 512 节点触发，
// 已取消的 ctx 使 Solve 以 timeout 结算（协议层随后以 canceled 收口，04 §4）。
func TestSolveContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	result, err := Solve(fenC, Options{TimeLimitMs: 60_000, MaxPlies: 13, Ctx: ctx})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	if result.Status != StatusTimeout {
		t.Fatalf("ctx 取消应以 timeout 结算, got %q", result.Status)
	}
}

// 置换表独立性与确定性：同一 FEN 连续两次求解结果逐位一致（表随状态重建）。
func TestSolveDeterministic(t *testing.T) {
	first, err := Solve(fenA, Options{TimeLimitMs: 10_000, MaxPlies: 5})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	second, err := Solve(fenA, Options{TimeLimitMs: 10_000, MaxPlies: 5})
	if err != nil {
		t.Fatalf("Solve 报错: %v", err)
	}
	if first.Status != second.Status || len(first.Solutions) != len(second.Solutions) {
		t.Fatalf("两次求解结果不一致: %+v vs %+v", first, second)
	}
	for i := range first.Solutions {
		if len(first.Solutions[i].Moves) != len(second.Solutions[i].Moves) {
			t.Fatalf("第 %d 条解法长度不一致", i)
		}
		for j, m := range first.Solutions[i].Moves {
			if m.From != second.Solutions[i].Moves[j].From || m.To != second.Solutions[i].Moves[j].To {
				t.Fatalf("第 %d 条解法第 %d 着不一致", i, j)
			}
		}
	}
}

func toString(p rules.Position) string {
	return string(rune('0'+p.Col)) + "," + string(rune('0'+p.Row))
}
