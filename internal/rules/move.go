package rules

// Move 单步走法（对应 move.dart Move / Electron 版 move.ts）。
//
// 含起点/终点/走子棋子/被吃棋子，便于悔棋与走法记录展示（02 文档 §1.3）。
type Move struct {
	From     Position
	To       Position
	Piece    *Piece // 走子棋子（走子方可选填）
	Captured *Piece // 被吃棋子；走空格时 nil
}
