package rules

// 规则引擎测试（对齐原版 board_test.dart 全部 17 用例 + 02 §2.3/§3 合法性契约；
// Electron 版 test/rules/board.spec.ts 的 Go 对应）。
// 走法生成用例走 PseudoMovesFor（这些场面均无自将干扰，pseudo == legal）；
// 自将过滤 / 照面 / 将死 / 困毙走 LegalMovesFor 与 IsCheck 系列（T1.3 增补）。
import "testing"

// pieceSpec [col, row, FEN字符] 三元组，如 {4, 5, 'R'} = 红车在 (4,5)。
type pieceSpec struct {
	col, row int
	ch       byte
}

// boardWith 在空棋盘上指定棋子构造 Board（对齐 Dart _boardWith / TS boardWith）。
func boardWith(t *testing.T, pieces []pieceSpec, redTurn bool) *Board {
	t.Helper()
	grid := make(BoardGrid, 10)
	for r := range grid {
		grid[r] = make([]*Piece, 9)
	}
	for _, s := range pieces {
		p := PieceFromFenChar(s.ch)
		if p == nil {
			t.Fatalf("非法棋子字符: %c", s.ch)
		}
		grid[s.row][s.col] = p
	}
	return &Board{grid: grid, redTurn: redTurn}
}

// pseudoTargets 取 (col,row) 格棋子的伪合法走法目标集合。
func pseudoTargets(b *Board, col, row int) []Move {
	return b.PseudoMovesFor(Pos(col, row))
}

// hasTarget 走法集合中是否存在 to == (col,row) 的走法。
func hasTarget(moves []Move, col, row int) bool {
	for _, m := range moves {
		if m.To.Col == col && m.To.Row == row {
			return true
		}
	}
	return false
}

func TestBoardInitialPieceCount(t *testing.T) {
	board := Initial()
	red, black := 0, 0
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			p := board.PieceAt(c, r)
			if p == nil {
				continue
			}
			if p.Side == Red {
				red++
			} else {
				black++
			}
		}
	}
	if red != 16 || black != 16 {
		t.Errorf("初始局面红 %d 黑 %d, 期望各 16", red, black)
	}
}

func TestBoardInitialRedTurn(t *testing.T) {
	if !Initial().IsRedTurn() {
		t.Errorf("初始局面红方先行, IsRedTurn = false")
	}
}

func TestRookAllOpenLines(t *testing.T) {
	// 红车放在 (4,5)，红将放在九宫 (3,9)，无任何阻挡。
	board := boardWith(t, []pieceSpec{{4, 5, 'R'}, {3, 9, 'K'}}, true)
	moves := pseudoTargets(board, 4, 5)
	// 同列 9 格（不含自身），同行 8 格（不含自身），共 17。
	if len(moves) != 17 {
		t.Errorf("车空棋盘走法数 = %d, 期望 17", len(moves))
	}
}

func TestRookCannotJumpOver(t *testing.T) {
	board := boardWith(t, []pieceSpec{{4, 5, 'R'}, {4, 3, 'p'}, {4, 9, 'K'}}, true)
	moves := pseudoTargets(board, 4, 5)
	// 上方遇到黑兵在 (4,3)，可以吃，但不能跳到 (4,2)/(4,1)/(4,0)。
	if !hasTarget(moves, 4, 4) {
		t.Errorf("缺少走法 (4,4)")
	}
	if !hasTarget(moves, 4, 3) {
		t.Errorf("缺少吃兵走法 (4,3)")
	}
	if hasTarget(moves, 4, 2) || hasTarget(moves, 4, 1) || hasTarget(moves, 4, 0) {
		t.Errorf("车不应越过 (4,3) 的黑兵")
	}
}

func TestKnightEightDirections(t *testing.T) {
	board := boardWith(t, []pieceSpec{{4, 5, 'N'}, {4, 0, 'K'}}, true)
	moves := pseudoTargets(board, 4, 5)
	expected := []Position{
		Pos(5, 7), Pos(3, 7), Pos(6, 6), Pos(2, 6),
		Pos(6, 4), Pos(2, 4), Pos(5, 3), Pos(3, 3),
	}
	if len(moves) != len(expected) {
		t.Errorf("马走法数 = %d, 期望 %d", len(moves), len(expected))
	}
	for _, e := range expected {
		if !hasTarget(moves, e.Col, e.Row) {
			t.Errorf("缺少马走法 (%d,%d)", e.Col, e.Row)
		}
	}
}

func TestKnightLegBlocked(t *testing.T) {
	board := boardWith(t, []pieceSpec{{4, 5, 'N'}, {4, 4, 'p'}, {4, 9, 'K'}}, true)
	moves := pseudoTargets(board, 4, 5)
	// 马腿 (4,4) 被堵，向下不能走 (3,3) / (5,3)。
	if hasTarget(moves, 3, 3) || hasTarget(moves, 5, 3) {
		t.Errorf("马腿被堵时不应有 (3,3)/(5,3) 走法")
	}
	// 其余 6 个方向不受影响。
	if len(moves) != 6 {
		t.Errorf("马走法数 = %d, 期望 6", len(moves))
	}
}

func TestCannonScreenCapture(t *testing.T) {
	board := boardWith(t, []pieceSpec{{4, 5, 'C'}, {4, 3, 'p'}, {4, 1, 'a'}, {4, 9, 'K'}}, true)
	moves := pseudoTargets(board, 4, 5)
	// 上方可走到空格 (4,4)；(4,3) 是炮架不能吃；隔架可吃 (4,1)；(4,0) 越过目标不可达。
	if !hasTarget(moves, 4, 4) {
		t.Errorf("缺少空格走法 (4,4)")
	}
	if hasTarget(moves, 4, 3) {
		t.Errorf("(4,3) 是炮架，不可吃")
	}
	if !hasTarget(moves, 4, 1) {
		t.Errorf("缺少隔架吃子 (4,1)")
	}
	if hasTarget(moves, 4, 0) {
		t.Errorf("(4,0) 越过目标不可达")
	}
}

func TestMinisterElephantMove(t *testing.T) {
	board := boardWith(t, []pieceSpec{{4, 9, 'B'}, {4, 0, 'K'}}, true)
	moves := pseudoTargets(board, 4, 9)
	// 红相在 (4,9)，可走 (2,7)、(6,7)，不能过河（row < 5）。
	if !hasTarget(moves, 2, 7) || !hasTarget(moves, 6, 7) {
		t.Errorf("缺少象走田 (2,7)/(6,7)")
	}
	for _, m := range moves {
		if m.To.Row < 5 {
			t.Errorf("象过河走法 (%d,%d)", m.To.Col, m.To.Row)
		}
	}
}

func TestAdvisorPalaceOnly(t *testing.T) {
	board := boardWith(t, []pieceSpec{{3, 9, 'A'}, {4, 9, 'K'}}, true)
	moves := pseudoTargets(board, 3, 9)
	// 红士在 (3,9)，可斜走到 (4,8)。
	if !hasTarget(moves, 4, 8) {
		t.Errorf("缺少士斜走 (4,8)")
	}
	// 不能平走到 (3,8) 或 (2,9)（不属于斜走）。
	if hasTarget(moves, 3, 8) || hasTarget(moves, 2, 9) {
		t.Errorf("士不应有直走/横走")
	}
	if len(moves) != 1 {
		t.Errorf("士走法数 = %d, 期望 1", len(moves))
	}
}

func TestKingPalaceOnly(t *testing.T) {
	board := boardWith(t, []pieceSpec{{4, 9, 'K'}}, true)
	moves := pseudoTargets(board, 4, 9)
	if !hasTarget(moves, 4, 8) || !hasTarget(moves, 3, 9) || !hasTarget(moves, 5, 9) {
		t.Errorf("缺少九宫直走一格走法")
	}
	// 不能斜走。
	if hasTarget(moves, 3, 8) {
		t.Errorf("将不应斜走 (3,8)")
	}
	// 不能走出九宫。
	if hasTarget(moves, 4, 6) {
		t.Errorf("将不应走出九宫 (4,6)")
	}
	if len(moves) != 3 {
		t.Errorf("将走法数 = %d, 期望 3", len(moves))
	}
}

func TestPawnBeforeRiver(t *testing.T) {
	board := boardWith(t, []pieceSpec{{4, 6, 'P'}, {4, 9, 'K'}}, true)
	moves := pseudoTargets(board, 4, 6)
	if !hasTarget(moves, 4, 5) {
		t.Errorf("缺少兵前进 (4,5)")
	}
	if hasTarget(moves, 3, 6) || hasTarget(moves, 5, 6) {
		t.Errorf("未过河兵不应横走")
	}
	if len(moves) != 1 {
		t.Errorf("兵走法数 = %d, 期望 1", len(moves))
	}
}

func TestPawnAfterRiver(t *testing.T) {
	board := boardWith(t, []pieceSpec{{4, 4, 'P'}, {4, 9, 'K'}}, true)
	moves := pseudoTargets(board, 4, 4)
	if !hasTarget(moves, 4, 3) || !hasTarget(moves, 3, 4) || !hasTarget(moves, 5, 4) {
		t.Errorf("过河兵缺少前进/横走")
	}
	if hasTarget(moves, 4, 5) {
		t.Errorf("兵不能后退 (4,5)")
	}
	if len(moves) != 3 {
		t.Errorf("兵走法数 = %d, 期望 3", len(moves))
	}
}

func TestApplyMoveUndoMoveInverse(t *testing.T) {
	fen0 := Initial().ToFen()
	board, err := FromFen(fen0)
	if err != nil {
		t.Fatalf("FromFen 报错: %v", err)
	}
	move := Move{From: Pos(1, 7), To: Pos(2, 7)}
	snapshot := board.ApplyMove(move)
	if board.ToFen() == fen0 {
		t.Errorf("走子后 FEN 不应等于原 FEN")
	}
	if board.IsRedTurn() {
		t.Errorf("走子后应轮到黑方")
	}
	board.UndoMove(snapshot)
	if board.ToFen() != fen0 {
		t.Errorf("悔棋后 FEN = %q, 期望 %q", board.ToFen(), fen0)
	}
	if !board.IsRedTurn() {
		t.Errorf("悔棋后应轮回红方")
	}
}

// --- T1.3 合法性过滤与胜负负例（board.spec.ts 将军/将死/困毙/legalMovesFor 段） ---

func TestCheckFacingKings(t *testing.T) {
	// 将帅照面：双将同列且中间无子时双方都算被将军。
	board := boardWith(t, []pieceSpec{{4, 9, 'K'}, {4, 0, 'k'}}, true)
	if !board.IsCheck(Red) {
		t.Errorf("IsCheck(red) = false, 期望 true（照面）")
	}
	if !board.IsCheck(Black) {
		t.Errorf("IsCheck(black) = false, 期望 true（照面）")
	}
}

func TestCheckRookFacingKing(t *testing.T) {
	// isCheck：车直面对方将算将军。
	board := boardWith(t, []pieceSpec{{4, 5, 'R'}, {4, 0, 'k'}, {4, 9, 'K'}}, true)
	if !board.IsCheck(Black) {
		t.Errorf("IsCheck(black) = false, 期望 true")
	}
}

func TestCheckmateClassicSingleRook(t *testing.T) {
	// 黑将在九宫顶角 (3,0)，红车控制第三列与第 0 行；黑方无路可逃。
	board := boardWith(t, []pieceSpec{{3, 5, 'R'}, {0, 0, 'R'}, {3, 0, 'k'}, {4, 9, 'K'}}, false)
	if !board.IsCheck(Black) {
		t.Errorf("IsCheck(black) = false, 期望 true")
	}
	if !board.IsCheckmate(Black) {
		t.Errorf("IsCheckmate(black) = false, 期望 true")
	}
}

func TestStalemateInitialHasMoves(t *testing.T) {
	// isStalemate：初始局面未被将军且必然有合法着法。
	board := Initial()
	if board.IsCheck(Red) {
		t.Errorf("IsCheck(red) = true, 期望 false")
	}
	if board.IsStalemate(Red) {
		t.Errorf("IsStalemate(red) = true, 期望 false")
	}
}

func TestStalemateConstructedStalemate(t *testing.T) {
	// isStalemate：将+双仕+马被炮牵制构造的困毙局面（02 §3 困毙判负）。
	// 黑：将(4,0) 仕(3,0) 仕(5,0) 马(4,1)；红：炮(4,9) 兵(4,5)作炮架 帅(3,9)。
	// 黑方：将三格被己方子占；仕唯一落点 (4,1) 被马占；马任一落点都会撤掉炮架
	// 使红炮沿第 4 列直照黑将 → 全部非法，且黑方未被将军 → 困毙。
	board, err := FromFen("3aka3/4n4/9/9/9/4P4/9/9/9/3KC4 b - - 0 1")
	if err != nil {
		t.Fatalf("FromFen 报错: %v", err)
	}
	if board.IsCheck(Black) {
		t.Errorf("IsCheck(black) = true, 期望 false")
	}
	if !board.IsStalemate(Black) {
		t.Errorf("IsStalemate(black) = false, 期望 true")
	}
	if board.IsCheckmate(Black) {
		t.Errorf("IsCheckmate(black) = true, 期望 false")
	}
	if moves := board.AllLegalMoves(Black); len(moves) != 0 {
		t.Errorf("AllLegalMoves(black) = %d 条, 期望 0", len(moves))
	}
}

func TestLegalMovesForOpponentSideEmpty(t *testing.T) {
	// legalMovesFor：非轮走方棋子返回空列表。
	board := boardWith(t, []pieceSpec{{4, 9, 'K'}, {0, 0, 'k'}}, true)
	// 红方轮走：黑车（此处为黑将所在格之外的任意黑子）的合法走法为空。
	if moves := board.LegalMovesFor(Pos(0, 0)); len(moves) != 0 {
		t.Errorf("LegalMovesFor(黑将) = %d 条, 期望 0（非轮走方）", len(moves))
	}
}

func TestLegalMovesForPinnedRook(t *testing.T) {
	// 送将着法被过滤：炮架车不能横移离开被牵制的纵线。
	// 红车 (4,5) 在红帅 (4,9) 与黑车 (4,0) 之间，横移会暴露红帅。
	board := boardWith(t, []pieceSpec{{4, 0, 'r'}, {4, 5, 'R'}, {4, 9, 'K'}}, true)
	moves := board.LegalMovesFor(Pos(4, 5))
	// 只能沿第 4 列移动（含吃黑车 (4,0)），共 8 着。
	if len(moves) != 8 {
		t.Errorf("被牵制车合法走法 = %d, 期望 8", len(moves))
	}
	for _, m := range moves {
		if m.To.Col != 4 {
			t.Errorf("存在离开纵线的走法 to=(%d,%d)", m.To.Col, m.To.Row)
		}
	}
	if hasTarget(moves, 3, 5) {
		t.Errorf("横移 (3,5) 送将，应被过滤")
	}
	if !hasTarget(moves, 4, 0) {
		t.Errorf("缺少沿纵线吃黑车 (4,0)")
	}
}

func TestLegalMovesForFacingNoScreen(t *testing.T) {
	// 照面负例：双将同列无遮蔽时，同列移动被过滤、横移合法。
	board := boardWith(t, []pieceSpec{{4, 9, 'K'}, {4, 0, 'k'}}, true)
	moves := board.LegalMovesFor(Pos(4, 9))
	// (4,8) 仍与黑将同列 → 非法；(3,9)/(5,9) 合法。
	if hasTarget(moves, 4, 8) {
		t.Errorf("(4,8) 仍照面，应被过滤")
	}
	if !hasTarget(moves, 3, 9) || !hasTarget(moves, 5, 9) {
		t.Errorf("缺少横移 (3,9)/(5,9)")
	}
	if len(moves) != 2 {
		t.Errorf("帅合法走法 = %d, 期望 2", len(moves))
	}
}

func TestLegalMovesForFacingWithScreen(t *testing.T) {
	// 照面负例：有遮蔽时同列移动合法；遮蔽子离开该列的着法全部非法。
	board := boardWith(t, []pieceSpec{{4, 9, 'K'}, {4, 0, 'k'}, {4, 5, 'N'}}, true)
	// 红帅沿同列移动 OK（马仍是遮蔽）。
	kingMoves := board.LegalMovesFor(Pos(4, 9))
	if !hasTarget(kingMoves, 4, 8) {
		t.Errorf("有遮蔽时 (4,8) 应合法")
	}
	if len(kingMoves) != 3 {
		t.Errorf("帅合法走法 = %d, 期望 3", len(kingMoves))
	}
	// 马的任何落点都离开第 4 列 → 撤掉遮蔽 → 送将 → 全部被过滤。
	knightMoves := board.LegalMovesFor(Pos(4, 5))
	if len(knightMoves) != 0 {
		t.Errorf("遮蔽马合法走法 = %d 条, 期望 0", len(knightMoves))
	}
}
