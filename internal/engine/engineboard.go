package engine

// 引擎内部棋盘（03 文档 §3；Electron 版 engineBoard.ts 的逐行翻译）：
//
//   - 棋盘用 [90]int8 一维数组，index = row*9 + col（row 0 为黑方底线）；
//   - 棋子编码为整数：0 = 空；红方 1..7；黑方 -1..-7。
//     kind 编码 1=帅/将 2=仕/士 3=相/象 4=马 5=车 6=炮 7=兵/卒；
//   - applyMove/undoMove 栈式回退（captured 编码由调用方保存），零对象分配；
//   - 走法打包为整数输出到复用数组：位段 [排序等级 | captured | to | from]，
//     MVV-LVA 排序键内嵌高位，升序排后倒序遍历即按键降序；
//   - Zobrist 增量键内置（DR-006 前置，03 §4）：FromFen 全量重建，
//     apply/undo 严格互逆、对称异或（xor 自逆，无需撤销栈）。
//
// 与 internal/rules 的 Board 语义 1:1（pseudoMovesFor / isCheck / apply+undo
// 结果集等价，由 engineboard_test.go 对拍锁定）。
// 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。

import (
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// kind 编码（1..7，见文件头注释）。
const (
	kingCode   int8 = 1
	knightCode int8 = 4
	rookCode   int8 = 5
	cannonCode int8 = 6
	pawnCode   int8 = 7
)

// kindCode rules.Kind → 引擎编码（1..7）。
var kindCode = map[rules.Kind]int8{
	rules.King:     1,
	rules.Advisor:  2,
	rules.Minister: 3,
	rules.Knight:   4,
	rules.Rook:     5,
	rules.Cannon:   6,
	rules.Pawn:     7,
}

// codeKind 编码（1..7）→ kind，packed 位段反解用。
var codeKind = [7]rules.Kind{rules.King, rules.Advisor, rules.Minister, rules.Knight, rules.Rook, rules.Cannon, rules.Pawn}

// pieceValue 子力价值（厘兵，03 §4；下标 = kind 编码）。
var pieceValue = [8]int{0, 10000, 200, 200, 400, 900, 450, 100}

// rankTable MVV-LVA 排序等级：Dart 版排序键 = victim 价值×10 − attacker 价值（吃子）、
// 非吃子 = 0。把全部（victim, attacker）组合的键值降序去重后映射为紧凑等级
// （0 = 最优先），非吃子与键值为 0 的互吃（如兵吃兵）共享同一等级
// ——与 Dart 的排序键数值序完全一致。等级占 6 bit（最多 43 档）。
var rankTable [64]int8

// rankQuiet 非吃子排序等级。
var rankQuiet int8

func init() {
	keys := map[int]struct{}{0: {}}
	for v := 1; v <= 7; v++ {
		for a := 1; a <= 7; a++ {
			keys[pieceValue[v]*10-pieceValue[a]] = struct{}{}
		}
	}
	sorted := make([]int, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	// 降序去重排序（等级 0 = 键值最大）。
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] > sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	rankOf := make(map[int]int8, len(sorted))
	for i, k := range sorted {
		rankOf[k] = int8(i)
	}
	for v := 1; v <= 7; v++ {
		for a := 1; a <= 7; a++ {
			rankTable[v*8+a] = rankOf[pieceValue[v]*10-pieceValue[a]]
		}
	}
	rankQuiet = rankOf[0]
}

// psq 位置修正表（03 §4，Dart _pieceSquareBonus 的查表化）：
// psq[kind 编码][sq] 为红方视角修正值；黑子按行镜像 sq 查同一张表
// （黑兵过河 row≥5 ⇔ 镜像后 row≤4，与红兵条件重合；沉底车同理）。
var psq [8][90]int16

func init() {
	for kind := 1; kind <= 7; kind++ {
		for sq := 0; sq < 90; sq++ {
			col := sq % 9
			row := sq / 9
			colCenter := 4 - abs(col-4)
			var bonus int16
			switch {
			case kind == int(pawnCode):
				// 红兵过河：row ≤ 4。
				if row <= 4 {
					bonus = int16(40 + colCenter*8)
				}
			case kind == int(knightCode) || kind == int(cannonCode):
				bonus = int16(colCenter * 4)
			case kind == int(rookCode):
				// 沉底车：红 row 0。
				if row == 0 {
					bonus = 10
				}
			}
			psq[kind][sq] = bonus
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// mirrorSq 行镜像（黑子查表用）：row r → 9−r，col 不变。
func mirrorSq(sq int) int { return (9-sq/9)*9 + sq%9 }

// 位段宽度：from/to 各 7 bit（0..89），captured 4 bit（0..15），等级占高位。
const (
	packFromMask      = 0x7f
	packToShift       = 7
	packCapShift      = 14
	packRankShift     = 18
	packCapMask       = 0xf
	capturedBlackBias = 8 // 黑子 captured 编码 = 8 − code（9..15）
)

// PackedFrom/PackedTo/PackedCaptCode 解包工具（引擎输出层与测试共用）。
func PackedFrom(packed int32) int { return int(packed & packFromMask) }
func PackedTo(packed int32) int   { return int((packed >> packToShift) & packFromMask) }
func PackedCaptCode(packed int32) int {
	return int((packed >> packCapShift) & packCapMask)
}

// packMove 走法打包：等级占最高位。
func packMove(rank int8, capCode, to, from int) int32 {
	return int32(rank)<<packRankShift | int32(capCode)<<packCapShift | int32(to)<<packToShift | int32(from)
}

// PackedToMove packed 整数 → 规则层 Move（captured 从位段反解，无需查盘）。
// 与 rules.Board 伪合法走法一致：不含 piece 字段。
func PackedToMove(packed int32) rules.Move {
	cc := PackedCaptCode(packed)
	var captured *rules.Piece
	switch {
	case cc == 0:
	case cc <= 7:
		captured = &rules.Piece{Kind: codeKind[cc-1], Side: rules.Red}
	default:
		captured = &rules.Piece{Kind: codeKind[cc-9], Side: rules.Black}
	}
	return rules.Move{
		From:     IndexToPos(PackedFrom(packed)),
		To:       IndexToPos(PackedTo(packed)),
		Captured: captured,
	}
}

// PosToIndex / IndexToPos index = row*9+col ↔ Position。
func PosToIndex(p rules.Position) int { return p.Row*9 + p.Col }
func IndexToPos(sq int) rules.Position {
	return rules.Position{Col: sq % 9, Row: sq / 9}
}

// 直线四方向 [dCol, dRow]（车/炮/帅共用枚举顺序，对齐 rules/board.go）。
var dirsOrtho = [4][2]int{{0, 1}, {0, -1}, {1, 0}, {-1, 0}}

var dirsDiagonal = [4][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}}

var dirsElephant = [4][2]int{{2, 2}, {2, -2}, {-2, 2}, {-2, -2}}

// knightPatterns 马 8 组 [dCol, dRow, legCol, legRow]（顺序对齐 rules/board.go knightPatterns）。
var knightPatterns = [8][4]int{
	{1, 2, 0, 1},
	{-1, 2, 0, 1},
	{1, -2, 0, -1},
	{-1, -2, 0, -1},
	{2, 1, 1, 0},
	{2, -1, 1, 0},
	{-2, 1, -1, 0},
	{-2, -1, -1, 0},
}

// EngineBoard 引擎内部棋盘：语义与 rules.Board 1:1，表示为 [90]int8。
// 每次搜索新建实例（FromFen），超时/取消后随 Search 一起废弃（Dart 同语义）。
type EngineBoard struct {
	data [90]int8
	// kings [红帅 index, 黑将 index]，无王为 -1（isCheck 对齐 Dart：无王返回 false）。
	kings     [2]int8
	isRedTurn bool
	// key Zobrist 增量键（DR-006 前置，03 §4）：FromFen 全量重建，apply/undo 对称异或维护。
	key uint64
}

// FromFen 从 FEN 构造（解析复用 rules/fen，保持解析行为单一事实源）。
func FromFen(fen string) (*EngineBoard, error) {
	grid, err := rules.ParseBoardFen(fen)
	if err != nil {
		return nil, err
	}
	b := &EngineBoard{}
	for r := 0; r < 10; r++ {
		for c := 0; c < 9; c++ {
			p := grid[r][c]
			if p == nil {
				continue
			}
			code := kindCode[p.Kind]
			if p.Side != rules.Red {
				code = -code
			}
			sq := r*9 + c
			b.data[sq] = code
			if p.Kind == rules.King {
				if p.Side == rules.Red {
					b.kings[0] = int8(sq)
				} else {
					b.kings[1] = int8(sq)
				}
			}
		}
	}
	b.isRedTurn = rules.ParseTurnFen(fen)
	b.key = rebuildZobrist(&b.data, b.isRedTurn)
	return b, nil
}

// ZobristKey 当前局面 Zobrist 键（03 §4：暴露只读键）。
func (b *EngineBoard) ZobristKey() uint64 { return b.key }

// RedTurn 当前是否红方走子。
func (b *EngineBoard) RedTurn() bool { return b.isRedTurn }

// PieceAt 某格棋子编码（0 = 空）。
func (b *EngineBoard) PieceAt(sq int) int8 { return b.data[sq] }

// ApplyMove 执行走子（不做合法性校验），返回被吃子编码（0 = 空）。
// 与 rules.Board.ApplyMove 一致：翻转轮走方。
// Zobrist 增量：mover@from 出、captured@to 出（若有）、mover@to 入、翻轮走方键。
func (b *EngineBoard) ApplyMove(from, to int) int8 {
	d := &b.data
	mover := d[from]
	captured := d[to]
	d[to] = mover
	d[from] = 0
	if mover == kingCode {
		b.kings[0] = int8(to)
	} else if mover == -kingCode {
		b.kings[1] = int8(to)
	}
	initZobrist()
	key := b.key
	key ^= pieceKeys[mover+7][from]
	if captured != 0 {
		key ^= pieceKeys[captured+7][to]
	}
	key ^= pieceKeys[mover+7][to]
	key ^= turnKey
	b.key = key
	b.isRedTurn = !b.isRedTurn
	return captured
}

// UndoMove 撤销 ApplyMove（captured 为 ApplyMove 返回值）；翻转回轮走方。
// Zobrist 逆序同式异或（异或自逆）：mover@to 出、mover@from 入、
// captured@to 入（若有）、翻轮走方键——与 ApplyMove 的异或集合相同，净效果为零。
func (b *EngineBoard) UndoMove(from, to int, captured int8) {
	d := &b.data
	mover := d[to]
	d[from] = mover
	d[to] = captured
	if mover == kingCode {
		b.kings[0] = int8(from)
	} else if mover == -kingCode {
		b.kings[1] = int8(from)
	}
	initZobrist()
	key := b.key
	key ^= pieceKeys[mover+7][to]
	key ^= pieceKeys[mover+7][from]
	if captured != 0 {
		key ^= pieceKeys[captured+7][to]
	}
	key ^= turnKey
	b.key = key
	b.isRedTurn = !b.isRedTurn
}

// IsCheck isRed 方的将是否正被将军。判定集合与 rules.Board.IsCheck 等价：
// ① 将帅照面（同列无阻）；② 车/炮直线（车 = 第一个子，炮 = 隔一子）；
// ③ 马位反查（含蹩腿）；④ 兵（正面 + 过河横走）。
// 士象活动范围不超出己方半场，攻击不到敌方九宫内的将，无需检查。
func (b *EngineBoard) IsCheck(isRed bool) bool {
	d := &b.data
	k := b.kings[0]
	if !isRed {
		k = b.kings[1]
	}
	if k < 0 {
		return false
	}
	ks := int(k)
	kCol := ks % 9
	kRow := ks / 9
	// ① 照面。
	ek := b.kings[0]
	if isRed {
		ek = b.kings[1]
	}
	if ek >= 0 && int(ek)%9 == kCol {
		lo, hi := ks, int(ek)
		if lo > hi {
			lo, hi = hi, lo
		}
		blocked := false
		for sq := lo + 9; sq < hi; sq += 9 {
			if d[sq] != 0 {
				blocked = true
				break
			}
		}
		if !blocked {
			return true
		}
	}
	foe := 1 // 敌子编码符号（乘子）
	if isRed {
		foe = -1
	}
	// ② 直线：第一个子 = 敌车；隔一子后 = 敌炮。敌王仅能攻击相邻格，
	//   将帅分处两个九宫永不同行（不同列相邻不存在），同列情形已被照面覆盖。
	for _, dir := range dirsOrtho {
		dc, dr := dir[0], dir[1]
		col := kCol + dc
		row := kRow + dr
		screen := false
		for col >= 0 && col <= 8 && row >= 0 && row <= 9 {
			p := int(d[row*9+col]) * foe
			if p != 0 {
				if !screen {
					if p == int(rookCode) {
						return true
					}
					screen = true
				} else {
					if p == int(cannonCode) {
						return true
					}
					break
				}
			}
			col += dc
			row += dr
		}
	}
	// ③ 马位反查：马在 k−delta 且蹩腿位（马位 + leg）为空。
	//   腿位与马位/目标同处一个矩形，二者都在盘内则腿位必在盘内。
	for _, pat := range knightPatterns {
		sc := kCol - pat[0]
		sr := kRow - pat[1]
		if sc < 0 || sc > 8 || sr < 0 || sr > 9 {
			continue
		}
		if d[sr*9+sc] == knightCode*int8(foe) && d[(sr+pat[3])*9+sc+pat[2]] == 0 {
			return true
		}
	}
	// ④ 兵：正面（兵在将的行进反向一格）+ 横走（兵已过河才能横走；
	//   将在九宫 ⇒ 与将同行的敌兵必已过河，无需再判）。
	if isRed {
		if kRow >= 1 && d[ks-9] == -pawnCode {
			return true
		}
		if kCol >= 1 && d[ks-1] == -pawnCode {
			return true
		}
		if kCol <= 7 && d[ks+1] == -pawnCode {
			return true
		}
	} else {
		if kRow <= 8 && d[ks+9] == pawnCode {
			return true
		}
		if kCol >= 1 && d[ks-1] == pawnCode {
			return true
		}
		if kCol <= 7 && d[ks+1] == pawnCode {
			return true
		}
	}
	return false
}

// GenerateMovesFor 生成某格棋子的全部伪合法走法（不检查自将），打包写入 buf[offset..]，
// 返回数量。语义与 rules.Board.PseudoMovesFor 1:1。
func (b *EngineBoard) GenerateMovesFor(buf []int32, offset, from int, capturesOnly bool) int {
	d := &b.data
	piece := d[from]
	if piece == 0 {
		return 0
	}
	kind := piece
	if kind < 0 {
		kind = -kind
	}
	isRed := piece > 0
	col := from % 9
	row := from / 9
	n := offset

	// emit 记录一步到 buf（capturesOnly 时跳过非吃子）。
	// 返回是否"到此停止滑动"（遇到任何棋子即停，供车/炮滑行用）。
	emit := func(to int) bool {
		t := d[to]
		if t != 0 && (t > 0) == isRed {
			return true // 己方子占据：停，不记录
		}
		if !(capturesOnly && t == 0) {
			var capCode int
			var rank int8
			if t == 0 {
				rank = rankQuiet
			} else {
				if t > 0 {
					capCode = int(t)
				} else {
					capCode = capturedBlackBias - int(t)
				}
				victim := t
				if victim < 0 {
					victim = -victim
				}
				rank = rankTable[victim*8+kind]
			}
			buf[n] = packMove(rank, capCode, to, from)
			n++
		}
		return t != 0
	}

	switch kind {
	case kingCode:
		// 帅：九宫内 4 直向 ×1 格（九宫 col 3-5，row 红 7-9 / 黑 0-2）。
		lo, hi := 0, 2
		if isRed {
			lo, hi = 7, 9
		}
		if row > lo {
			emit(from - 9)
		}
		if row < hi {
			emit(from + 9)
		}
		if col > 3 {
			emit(from - 1)
		}
		if col < 5 {
			emit(from + 1)
		}
	case 2:
		// 士：九宫内 4 斜向 ×1 格。
		lo, hi := 0, 2
		if isRed {
			lo, hi = 7, 9
		}
		for _, dir := range dirsDiagonal {
			c := col + dir[0]
			r := row + dir[1]
			if c < 3 || c > 5 || r < lo || r > hi {
				continue
			}
			emit(r*9 + c)
		}
	case 3:
		// 象：田字 ×2 格，象眼 = 田字中心，不能过河。
		lo, hi := 0, 4
		if isRed {
			lo, hi = 5, 9
		}
		for _, dir := range dirsElephant {
			dc, dr := dir[0], dir[1]
			c := col + dc
			r := row + dr
			if c < 0 || c > 8 || r < lo || r > hi {
				continue
			}
			if d[(row+dr/2)*9+col+dc/2] != 0 {
				continue
			}
			emit(r*9 + c)
		}
	case knightCode:
		// 马：8 个 delta+leg 模式，马腿 = 起点往该方向先走一步的格子。
		for _, pat := range knightPatterns {
			c := col + pat[0]
			r := row + pat[1]
			if c < 0 || c > 8 || r < 0 || r > 9 {
				continue
			}
			if d[(row+pat[3])*9+col+pat[2]] != 0 {
				continue
			}
			emit(r*9 + c)
		}
	case rookCode:
		// 车：4 方向滑动，遇子停止（敌子已由 emit 记录）。
		for _, dir := range dirsOrtho {
			dc, dr := dir[0], dir[1]
			c := col + dc
			r := row + dr
			for c >= 0 && c <= 8 && r >= 0 && r <= 9 {
				if emit(r*9 + c) {
					break
				}
				c += dc
				r += dr
			}
		}
	case cannonCode:
		// 炮：直线滑空格；越过炮架（第一个非空格）后吃第一个子（敌子）。
		for _, dir := range dirsOrtho {
			dc, dr := dir[0], dir[1]
			c := col + dc
			r := row + dr
			for c >= 0 && c <= 8 && r >= 0 && r <= 9 && d[r*9+c] == 0 {
				emit(r*9 + c)
				c += dc
				r += dr
			}
			if c < 0 || c > 8 || r < 0 || r > 9 {
				continue
			}
			c += dc // 越过炮架
			r += dr
			for c >= 0 && c <= 8 && r >= 0 && r <= 9 {
				if d[r*9+c] != 0 {
					emit(r*9 + c)
					break
				}
				c += dc
				r += dr
			}
		}
	default:
		// 兵：前进 1 格；已过河可横走；底线后仍仅平移，无升变。
		fwd := 1
		if isRed {
			fwd = -1
		}
		crossed := row >= 5
		if isRed {
			crossed = row <= 4
		}
		r := row + fwd
		if r >= 0 && r <= 9 {
			emit(r*9 + col)
		}
		if crossed {
			if col >= 1 {
				emit(row*9 + col - 1)
			}
			if col <= 7 {
				emit(row*9 + col + 1)
			}
		}
	}
	return n - offset
}

// GenerateMoves 生成走子方全部伪合法走法（全盘扫描，格序与 rules 一致：row 0→9、col 0→8），
// 写入 buf[offset..]，返回数量。排序由调用方完成（等级在高位）。
func (b *EngineBoard) GenerateMoves(buf []int32, offset int, capturesOnly bool) int {
	n := offset
	for sq := 0; sq < 90; sq++ {
		p := b.data[sq]
		if p == 0 {
			continue
		}
		if (p > 0) != b.isRedTurn {
			continue
		}
		n += b.GenerateMovesFor(buf, n, sq, capturesOnly)
	}
	return n - offset
}

// HasPseudoMove from 格是否有伪合法走法到 to（几何合法性，evaluateMove 前置校验用）。
func (b *EngineBoard) HasPseudoMove(from, to int) bool {
	buf := make([]int32, 32)
	n := b.GenerateMovesFor(buf, 0, from, false)
	for i := 0; i < n; i++ {
		if PackedTo(buf[i]) == to {
			return true
		}
	}
	return false
}

// Evaluate 静态评估（厘兵，红方为正 → 按轮走方视角取正负，03 §4）：
// Σ(红子价值+修正) − Σ(黑子价值+修正)，黑子修正查行镜像表。
func (b *EngineBoard) Evaluate() int {
	d := &b.data
	score := 0
	for sq := 0; sq < 90; sq++ {
		p := d[sq]
		if p == 0 {
			continue
		}
		if p > 0 {
			score += pieceValue[p] + int(psq[p][sq])
		} else {
			kind := -p
			score -= pieceValue[kind] + int(psq[kind][mirrorSq(sq)])
		}
	}
	if b.isRedTurn {
		return score
	}
	return -score
}
