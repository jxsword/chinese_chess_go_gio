package rules

// 中国象棋规则引擎（对应 board.dart / Electron 版 board.ts）。
//
// - 棋盘以 10 行 × 9 列矩阵存储，row 0 为黑方底线（FEN 第一行），row 9 为红方底线。
// - 两层走法：PseudoMovesFor（伪合法，供搜索与 IsCheck）→ LegalMovesFor
//   （自将过滤，供 UI/清单，T1.3 落地）。
// - 纯 Go，不依赖任何环境 API，可独立单测。

// 是否在九宫格内（board.dart:54-57）：col 3-5；红 row 7-9 / 黑 row 0-2。
func inPalace(col, row int, side Side) bool {
	if col < 3 || col > 5 {
		return false
	}
	if side == Red {
		return row >= 7 && row <= 9
	}
	return row >= 0 && row <= 2
}

// inOwnHalf 是否在自己半场（未过河，board.dart:60-62）：红 row≥5 / 黑 row≤4。
func inOwnHalf(row int, side Side) bool {
	if side == Red {
		return row >= 5
	}
	return row <= 4
}

// inOpponentHalf 是否已过河到对方半场（board.dart:65）。
func inOpponentHalf(row int, side Side) bool { return !inOwnHalf(row, side) }

// Board 棋盘（board.dart / Electron 版 board.ts Board）。
//
// DR-006：Zobrist 键不在此处（引擎内部单源，见 03 §4）——规则层保持无哈希，
// L3 用 FEN 字符串比较（02 §7），与 Electron 版口径一致。
//
// 非并发安全：ApplyMove/UndoMove/合法性模拟会原地改盘。跨 goroutine 传递
// 用 Copy() 快照（与原版 Worker 快照传参口径一致）。
type Board struct {
	grid    BoardGrid
	redTurn bool
}

// FromFen 从 FEN 字符串构造棋盘（board.dart:16-20）。无效 FEN 返回 error
// （引擎侧 L2 历史表对无效 FEN 静默跳过，02 文档 §1）。
func FromFen(fen string) (*Board, error) {
	grid, err := ParseBoardFen(fen)
	if err != nil {
		return nil, err
	}
	return &Board{grid: grid, redTurn: ParseTurnFen(fen)}, nil
}

// Initial 标准初始局面（board.dart:23）。FENInitial 为编译期常量、必然合法。
func Initial() *Board {
	b, err := FromFen(FENInitial)
	if err != nil {
		panic("rules: FENInitial 常量非法（不可能发生）: " + err.Error())
	}
	return b
}

// IsRedTurn 当前是否轮到红方走（board.dart:29）。
func (b *Board) IsRedTurn() bool { return b.redTurn }

// Turn 当前轮走方（board.dart:32）。
func (b *Board) Turn() Side {
	if b.redTurn {
		return Red
	}
	return Black
}

// PieceAt 取某格棋子（board.dart:35）。
func (b *Board) PieceAt(col, row int) *Piece { return b.grid[row][col] }

// PieceAtP 取某格棋子（Position 版，board.dart:38）。
func (b *Board) PieceAtP(p Position) *Piece { return b.grid[p.Row][p.Col] }

// ToFen 序列化为 FEN（board.dart:41）。
func (b *Board) ToFen() string { return BuildFen(b.grid, b.redTurn) }

// Copy 拷贝当前棋盘（深拷贝，Worker 快照传参用；board.dart:44-47）。
func (b *Board) Copy() *Board {
	grid := make(BoardGrid, 10)
	for r := 0; r < 10; r++ {
		grid[r] = make([]*Piece, 9)
		copy(grid[r], b.grid[r])
	}
	return &Board{grid: grid, redTurn: b.redTurn}
}

var dirsOrtho = []Position{{Col: 0, Row: 1}, {Col: 0, Row: -1}, {Col: 1, Row: 0}, {Col: -1, Row: 0}}

var dirsDiagonal = []Position{{Col: 1, Row: 1}, {Col: 1, Row: -1}, {Col: -1, Row: 1}, {Col: -1, Row: -1}}

var dirsElephant = []Position{{Col: 2, Row: 2}, {Col: 2, Row: -2}, {Col: -2, Row: 2}, {Col: -2, Row: -2}}

// 马的 8 个 (走子偏移, 马腿偏移) 模式（board.dart:300-309）。
var knightPatterns = []struct{ delta, leg Position }{
	{Pos(1, 2), Pos(0, 1)},    // 下右
	{Pos(-1, 2), Pos(0, 1)},   // 下左
	{Pos(1, -2), Pos(0, -1)},  // 上右
	{Pos(-1, -2), Pos(0, -1)}, // 上左
	{Pos(2, 1), Pos(1, 0)},    // 右下
	{Pos(2, -1), Pos(1, 0)},   // 右上
	{Pos(-2, 1), Pos(-1, 0)},  // 左下
	{Pos(-2, -1), Pos(-1, 0)}, // 左上
}

// ApplyMove 执行一步走子（不做合法性校验），返回含被吃子的快照（board.dart:124-131）。
func (b *Board) ApplyMove(move Move) Move {
	mover := b.PieceAtP(move.From)
	captured := b.PieceAtP(move.To)
	b.grid[move.To.Row][move.To.Col] = mover
	b.grid[move.From.Row][move.From.Col] = nil
	b.redTurn = !b.redTurn
	return Move{From: move.From, To: move.To, Captured: captured}
}

// UndoMove 撤销一步走子：用 captured 恢复 + 翻回轮走方（board.dart:134-139）。
func (b *Board) UndoMove(snapshot Move) {
	mover := b.PieceAtP(snapshot.To)
	b.grid[snapshot.From.Row][snapshot.From.Col] = mover
	b.grid[snapshot.To.Row][snapshot.To.Col] = snapshot.Captured
	b.redTurn = !b.redTurn
}

// -----------------------------------------------------------------------------
// 合法性过滤（自将检查）与胜负判定
// -----------------------------------------------------------------------------

// LegalMovesFor 合法走法：过滤掉走完会自将的着法；非轮走方棋子返回空（board.dart:96-102）。
func (b *Board) LegalMovesFor(p Position) []Move {
	piece := b.PieceAtP(p)
	if piece == nil || piece.Side != b.Turn() {
		return nil
	}
	pseudo := b.PseudoMovesFor(p)
	moves := make([]Move, 0, len(pseudo))
	for _, m := range pseudo {
		if !b.willBeInCheckAfter(m, piece.Side) {
			moves = append(moves, m)
		}
	}
	return moves
}

// HasAnyLegalMove 当前走子方是否还有任何合法走法（board.dart:105-119）。
func (b *Board) HasAnyLegalMove() bool {
	return b.HasAnyLegalMoveFor(b.Turn())
}

// HasAnyLegalMoveFor 指定一方是否还有任何合法走法（不影响轮走方，board.dart:203-217）。
func (b *Board) HasAnyLegalMoveFor(side Side) bool {
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			p := b.grid[r][c]
			if p != nil && p.Side == side {
				for _, m := range b.PseudoMovesFor(Pos(c, r)) {
					if !b.willBeInCheckAfter(m, side) {
						return true
					}
				}
			}
		}
	}
	return false
}

// AllLegalMoves 某一方（默认轮走方）的全部合法着法，供金标准对拍与搜索入口（09 §2.1）。
func (b *Board) AllLegalMoves(side Side) []Move {
	moves := []Move{}
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			p := b.grid[r][c]
			if p == nil || p.Side != side {
				continue
			}
			for _, m := range b.PseudoMovesFor(Pos(c, r)) {
				if !b.willBeInCheckAfter(m, side) {
					moves = append(moves, m)
				}
			}
		}
	}
	return moves
}

// KingPositionOf 查找某方将/帅位置；无将返回 nil（board.dart:146-156）。
func (b *Board) KingPositionOf(side Side) *Position {
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			p := b.grid[r][c]
			if p != nil && p.Kind == King && p.Side == side {
				pos := Pos(c, r)
				return &pos
			}
		}
	}
	return nil
}

// IsCheck side 方的将是否正被将军（board.dart:159-188）。
// ① 将帅照面：同列且中间无子 → 视为被将军（等效禁止照面）；
// ② 任意对方棋子（伪合法）可吃到本方将位。
func (b *Board) IsCheck(side Side) bool {
	kingPos := b.KingPositionOf(side)
	if kingPos == nil {
		return false
	}
	// ① 照面判定（board.dart:163-175）。
	enemyKing := b.KingPositionOf(OpponentOf(side))
	if enemyKing != nil && enemyKing.Col == kingPos.Col {
		lo := min(kingPos.Row, enemyKing.Row)
		hi := max(kingPos.Row, enemyKing.Row)
		blocked := false
		for r := lo + 1; r < hi; r++ {
			if b.grid[r][kingPos.Col] != nil {
				blocked = true
				break
			}
		}
		if !blocked {
			return true
		}
	}
	// ② 对方棋子可达本方将位（board.dart:177-186）。
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			p := b.grid[r][c]
			if p != nil && p.Side == OpponentOf(side) {
				for _, m := range b.PseudoMovesFor(Pos(c, r)) {
					if SamePos(m.To, *kingPos) {
						return true
					}
				}
			}
		}
	}
	return false
}

// IsCheckmate side 方是否被将死（被将军且无任何合法走法，board.dart:191-194）。
func (b *Board) IsCheckmate(side Side) bool {
	if !b.IsCheck(side) {
		return false
	}
	return !b.HasAnyLegalMoveFor(side)
}

// IsStalemate side 方是否被困毙（未被将军但无任何合法走法，判负；board.dart:197-200）。
func (b *Board) IsStalemate(side Side) bool {
	if b.IsCheck(side) {
		return false
	}
	return !b.HasAnyLegalMoveFor(side)
}

// willBeInCheckAfter 模拟执行走子后自己是否处于被将军状态（原地模拟+还原，不改轮走方，board.dart:220-230）。
func (b *Board) willBeInCheckAfter(move Move, side Side) bool {
	captured := b.PieceAtP(move.To)
	mover := b.PieceAtP(move.From)
	b.grid[move.To.Row][move.To.Col] = mover
	b.grid[move.From.Row][move.From.Col] = nil
	inCheck := b.IsCheck(side)
	// 还原。
	b.grid[move.From.Row][move.From.Col] = mover
	b.grid[move.To.Row][move.To.Col] = captured
	return inCheck
}

// -----------------------------------------------------------------------------
// 走法生成
// -----------------------------------------------------------------------------

// PseudoMovesFor 计算某格棋子的所有伪合法走法（不检查是否自将，board.dart:74-93）。
func (b *Board) PseudoMovesFor(p Position) []Move {
	piece := b.PieceAtP(p)
	if piece == nil {
		return nil
	}
	switch piece.Kind {
	case King:
		return b.kingMoves(p, piece)
	case Advisor:
		return b.advisorMoves(p, piece)
	case Minister:
		return b.ministerMoves(p, piece)
	case Knight:
		return b.knightMoves(p, piece)
	case Rook:
		return b.rookMoves(p, piece)
	case Cannon:
		return b.cannonMoves(p, piece)
	case Pawn:
		return b.pawnMoves(p, piece)
	}
	return nil
}

// kingMoves 将：九宫内 4 直向 ×1 格（board.dart:236-253）。
func (b *Board) kingMoves(p Position, piece *Piece) []Move {
	moves := []Move{}
	for _, d := range dirsOrtho {
		to := AddPos(p, d)
		if !InBoard(to.Col, to.Row) {
			continue
		}
		if !inPalace(to.Col, to.Row, piece.Side) {
			continue
		}
		target := b.PieceAtP(to)
		if target != nil && target.Side == piece.Side {
			continue
		}
		moves = append(moves, Move{From: p, To: to, Captured: target})
	}
	return moves
}

// advisorMoves 士：九宫内 4 斜向 ×1 格（board.dart:255-272）。
func (b *Board) advisorMoves(p Position, piece *Piece) []Move {
	moves := []Move{}
	for _, d := range dirsDiagonal {
		to := AddPos(p, d)
		if !InBoard(to.Col, to.Row) {
			continue
		}
		if !inPalace(to.Col, to.Row, piece.Side) {
			continue
		}
		target := b.PieceAtP(to)
		if target != nil && target.Side == piece.Side {
			continue
		}
		moves = append(moves, Move{From: p, To: to, Captured: target})
	}
	return moves
}

// ministerMoves 象：4 田字方向 ×2 格，检查象眼（田字中心）且不能过河（board.dart:274-295）。
func (b *Board) ministerMoves(p Position, piece *Piece) []Move {
	moves := []Move{}
	for _, d := range dirsElephant {
		to := AddPos(p, d)
		if !InBoard(to.Col, to.Row) {
			continue
		}
		// 象不能过河（board.dart:286）。
		if !inOwnHalf(to.Row, piece.Side) {
			continue
		}
		// 象眼 = 田字中心（d~/2，board.dart:288）。
		eye := Pos(p.Col+d.Col/2, p.Row+d.Row/2)
		if b.PieceAtP(eye) != nil {
			continue
		}
		target := b.PieceAtP(to)
		if target != nil && target.Side == piece.Side {
			continue
		}
		moves = append(moves, Move{From: p, To: to, Captured: target})
	}
	return moves
}

// knightMoves 马：8 个 delta+leg 模式，马腿 = 起点往该方向先走一步的位置（board.dart:297-320）。
func (b *Board) knightMoves(p Position, piece *Piece) []Move {
	moves := []Move{}
	for _, pat := range knightPatterns {
		to := AddPos(p, pat.delta)
		if !InBoard(to.Col, to.Row) {
			continue
		}
		// 马腿检查（board.dart:314）。
		if b.PieceAtP(AddPos(p, pat.leg)) != nil {
			continue
		}
		target := b.PieceAtP(to)
		if target != nil && target.Side == piece.Side {
			continue
		}
		moves = append(moves, Move{From: p, To: to, Captured: target})
	}
	return moves
}

// rookMoves 车：4 方向滑动，遇子停止，敌子可吃（board.dart:322-346）。
func (b *Board) rookMoves(p Position, piece *Piece) []Move {
	moves := []Move{}
	for _, d := range dirsOrtho {
		to := AddPos(p, d)
		for InBoard(to.Col, to.Row) {
			target := b.PieceAtP(to)
			if target == nil {
				moves = append(moves, Move{From: p, To: to})
			} else {
				if target.Side != piece.Side {
					moves = append(moves, Move{From: p, To: to, Captured: target})
				}
				break
			}
			to = AddPos(to, d)
		}
	}
	return moves
}

// cannonMoves 炮：直线滑空格；吃子需隔恰一个炮架后找第一个子，敌子可吃（board.dart:348-380）。
func (b *Board) cannonMoves(p Position, piece *Piece) []Move {
	moves := []Move{}
	for _, d := range dirsOrtho {
		// 第一阶段：直线无阻走空格（board.dart:358-362）。
		to := AddPos(p, d)
		for InBoard(to.Col, to.Row) && b.PieceAtP(to) == nil {
			moves = append(moves, Move{From: p, To: to})
			to = AddPos(to, d)
		}
		// 第二阶段：越过炮架（第一个非空格）后，再吃对方（board.dart:364-377）。
		if InBoard(to.Col, to.Row) {
			to = AddPos(to, d)
			for InBoard(to.Col, to.Row) {
				target := b.PieceAtP(to)
				if target != nil {
					if target.Side != piece.Side {
						moves = append(moves, Move{From: p, To: to, Captured: target})
					}
					break
				}
				to = AddPos(to, d)
			}
		}
	}
	return moves
}

// pawnMoves 兵：前进 1 格；已过河可左右横走；底线后仍仅平移，无升变（board.dart:382-400）。
func (b *Board) pawnMoves(p Position, piece *Piece) []Move {
	moves := []Move{}
	forward := ForwardOf(piece.Side)
	deltas := []Position{Pos(0, forward)}
	// 过河后可横走（board.dart:387-390）。
	if inOpponentHalf(p.Row, piece.Side) {
		deltas = append(deltas, Pos(1, 0), Pos(-1, 0))
	}
	for _, d := range deltas {
		to := AddPos(p, d)
		if !InBoard(to.Col, to.Row) {
			continue
		}
		target := b.PieceAtP(to)
		if target != nil && target.Side == piece.Side {
			continue
		}
		moves = append(moves, Move{From: p, To: to, Captured: target})
	}
	return moves
}

// InPalace 是否在九宫内（board.dart:52-58 同语义；Gio 版导出包装——工作室
// 摆盘校验消费，K9 处置：08 §7 既定导出差异，一行登记，实现不复制）。
func InPalace(col, row int, side Side) bool { return inPalace(col, row, side) }

// InOwnHalf 是否在自己半场（未过河：红 row≥5 / 黑 row≤4；Gio 版导出包装，
// 同上 K9 处置）。
func InOwnHalf(row int, side Side) bool { return inOwnHalf(row, side) }
