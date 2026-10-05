package rules

// 棋子类型（对应 piece.dart / Electron 版 piece.ts）：中国象棋共 7 种棋子，
// 红黑两方共用同一个 Kind，颜色由 Side 区分。纯数据 + 纯函数。

// Side 棋子归属方（piece.dart:16-28）。
type Side string

const (
	Red   Side = "red"
	Black Side = "black"
)

// Kind 棋子类型（piece.dart:5-13）。
type Kind string

const (
	King     Kind = "king"     // 将/帅
	Advisor  Kind = "advisor"  // 士/仕
	Minister Kind = "minister" // 象/相
	Knight   Kind = "knight"   // 马
	Rook     Kind = "rook"     // 车
	Cannon   Kind = "cannon"   // 炮
	Pawn     Kind = "pawn"     // 兵/卒
)

// Piece 一个具体棋子（种类 + 归属方）。
type Piece struct {
	Kind Kind
	Side Side
}

// OpponentOf 对手方（piece.dart:21）。
func OpponentOf(side Side) Side {
	if side == Red {
		return Black
	}
	return Red
}

// IsRedSide 是否为红方（piece.dart:24）。
func IsRedSide(side Side) bool { return side == Red }

// ForwardOf 该方"前进方向"：红方从下往上走（行号减小），黑方反之（piece.dart:27）。
func ForwardOf(side Side) int {
	if side == Red {
		return -1
	}
	return 1
}

// FEN 字符映射（红大写、黑小写；象用 B 非 E，piece.dart:31-49）。
var kindToFenRed = map[Kind]byte{
	King: 'K', Advisor: 'A', Minister: 'B', Knight: 'N',
	Rook: 'R', Cannon: 'C', Pawn: 'P',
}

var kindToFenBlack = map[Kind]byte{
	King: 'k', Advisor: 'a', Minister: 'b', Knight: 'n',
	Rook: 'r', Cannon: 'c', Pawn: 'p',
}

// PieceFenChar 棋子的 FEN 字符（piece.dart:67）。
func PieceFenChar(p *Piece) string {
	if p.Side == Red {
		return string(kindToFenRed[p.Kind])
	}
	return string(kindToFenBlack[p.Kind])
}

// FEN 字符反查表（两套大小写均收录）。
var fenToPiece = map[byte]*Piece{
	'K': {Kind: King, Side: Red}, 'A': {Kind: Advisor, Side: Red},
	'B': {Kind: Minister, Side: Red}, 'N': {Kind: Knight, Side: Red},
	'R': {Kind: Rook, Side: Red}, 'C': {Kind: Cannon, Side: Red},
	'P': {Kind: Pawn, Side: Red},
	'k': {Kind: King, Side: Black}, 'a': {Kind: Advisor, Side: Black},
	'b': {Kind: Minister, Side: Black}, 'n': {Kind: Knight, Side: Black},
	'r': {Kind: Rook, Side: Black}, 'c': {Kind: Cannon, Side: Black},
	'p': {Kind: Pawn, Side: Black},
}

// PieceFromFenChar 从 FEN 字符解析单个棋子；非法字符返回 nil（piece.dart:104-108）。
func PieceFromFenChar(ch byte) *Piece { return fenToPiece[ch] }

// 中文 label：红 帅仕相马车炮兵；黑 将士象马车炮卒（piece.dart:70-90）。
var labelRed = map[Kind]string{
	King: "帅", Advisor: "仕", Minister: "相", Knight: "马",
	Rook: "车", Cannon: "炮", Pawn: "兵",
}

var labelBlack = map[Kind]string{
	King: "将", Advisor: "士", Minister: "象", Knight: "马",
	Rook: "车", Cannon: "炮", Pawn: "卒",
}

// PieceLabel 中文字符（用于走法记录展示，piece.dart:70）。
func PieceLabel(p *Piece) string {
	if p.Side == Red {
		return labelRed[p.Kind]
	}
	return labelBlack[p.Kind]
}

// SamePiece 值相等（piece.dart:93-94）。
func SamePiece(a, b *Piece) bool {
	return a == b || (a != nil && b != nil && a.Kind == b.Kind && a.Side == b.Side)
}
