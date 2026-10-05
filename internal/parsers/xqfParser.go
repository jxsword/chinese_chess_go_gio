package parsers

import (
	"fmt"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// XQF（象棋演播室）二进制棋谱解析器（对应 xqf_parser.dart，06 文档 §3；逐行翻译 xqfParser.ts）。
//
// 格式参考 XQF 规范（www.xqbase.com/protocol/cchess_xqf.htm）与
// walker8088/cchess 的 `io_xqf.py` 实现（已对原项目语料实测）。
//
// 要点：
// - 魔数为前两字节 `XQ`（0x58 0x51），第 3 字节是格式版本号，语料中分布
//   在 0x0A–0x12；版本 <= 0x0A 的旧格式无加密，之后的版本对棋子布局与
//   走子数据做字节变换加密。
// - 32 个棋子的存放顺序固定（车马相仕帅仕相马车 + 炮炮 + 兵×5，红大写黑小写），
//   每子 1 字节位置，编码为 `x*10 + y`（x=列 0-8，y=行，0 为红方底线），
//   0xFF 表示让子（无此子）。
// - 字符串字段为 GB18030（GBK 超集），按长度前缀存放。
// - 走子数据是一棵记录树（每条记录 4 字节 + 可选注解），本解析器只取
//   主线（变着分支仅存在于主线结束之后，不影响缓冲区对齐）。

// XQFHeaderSize XQF 格式头部常量（xqf_parser.dart:38-49）。
const XQFHeaderSize = 0x400

const (
	xqfMoveFromOffset = 0x18
	xqfMoveToOffset   = 0x20
	xqfStepFlagMask   = 0xe0
	xqfStepHasAnno    = 0x20
	xqfStepHasNext    = 0x80

	// xqfLegacyVersionMax 旧格式（版本 <= 0x0A）分界。
	xqfLegacyVersionMax = 0x0a
)

// xqfPieceChars 32 个棋子位次的 FEN 字符（红方大写、黑方小写，xqf_parser.dart:51-60）。
// XQF 存放顺序为对称回文序：车马相仕帅仕相马车（9）+ 炮炮（2）+ 兵×5（16 子），
// 黑方同序小写（已用语料实证校准，非部分文档所写的帅仕相马车序）。
var xqfPieceChars = [32]byte{
	'R', 'N', 'B', 'A', 'K', 'A', 'B', 'N', 'R', 'C', 'C',
	'P', 'P', 'P', 'P', 'P',
	'r', 'n', 'b', 'a', 'k', 'a', 'b', 'n', 'r', 'c', 'c',
	'p', 'p', 'p', 'p', 'p',
}

// xqfKeySeed 解密变换的种子串（XQF 作者版权串，用于生成 32 字节密钥表，xqf_parser.dart:63）。
const xqfKeySeed = "[(C) Copyright Mr. Dong Shiwei.]"

// xqfKeys XQF 解密密钥集（xqf_parser.dart:367-381）。
type xqfKeys struct {
	keyXY      int
	keyXYf     int
	keyXYt     int
	keyRmkSize int
	f32        [32]int
}

// decodeGb18030 从 GB18030 字节解码字符串；解码失败返回空串（xqf_parser.dart:169-178）。
func decodeGb18030(b []byte) string {
	out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(b)
	if err != nil {
		return ""
	}
	return string(out)
}

// readXqfString 读长度前缀字符串（1 字节长度 + 内容，xqf_parser.dart:169-178）。
func readXqfString(b []byte, lenOffset, maxLen int) string {
	if lenOffset >= len(b) {
		return ""
	}
	n := int(b[lenOffset])
	if n <= 0 || n > maxLen {
		return ""
	}
	start, end := lenOffset+1, lenOffset+1+n
	if end > len(b) {
		return ""
	}
	return decodeGb18030(b[start:end])
}

func defaultXqfTitle(redName, blackName string) string {
	if redName != "" || blackName != "" {
		return xqfPlayerOr(redName) + " 对 " + xqfPlayerOr(blackName)
	}
	return "未命名对局"
}

func xqfPlayerOr(name string) string {
	if name == "" {
		return "?"
	}
	return name
}

// deriveKeys 密钥派生（xqf_parser.dart:187-225）：
// 单字节变换基数 formula(x) = ((((x²*3+9)*3+8)*2+1)*3+8)（无尾因子）；
// KeyXY 多乘一次自身头字节；KeyXYf/KeyXYt 依次链乘前一把钥匙；
// FKeyBytes 由头部原始字节（而非派生密钥）与掩码/或值组合而成。
func deriveKeys(keyMask, keyOrA, keyOrB, keyOrC, keyOrD, keysSum, headKeyXY, headKeyXYf, headKeyXYt int) xqfKeys {
	formula := func(x int) int { return ((((x*x)*3+9)*3+8)*2+1)*3 + 8 }

	keyXY := (formula(headKeyXY) * headKeyXY) & 0xff
	keyXYf := (formula(headKeyXYf) * keyXY) & 0xff
	keyXYt := (formula(headKeyXYt) * keyXYf) & 0xff
	keyRmkSize := (((keysSum*256 + headKeyXY) % 32000) + 767) & 0xffff

	keyBytes := [4]int{
		(keysSum & keyMask) | keyOrA,
		(headKeyXY & keyMask) | keyOrB,
		(headKeyXYf & keyMask) | keyOrC,
		(headKeyXYt & keyMask) | keyOrD,
	}
	var f32 [32]int
	for i := 0; i < 32; i++ {
		f32[i] = int(xqfKeySeed[i]) & keyBytes[i%4]
	}
	return xqfKeys{keyXY: keyXY, keyXYf: keyXYf, keyXYt: keyXYt, keyRmkSize: keyRmkSize, f32: f32}
}

// decodeBoard 棋子布局 → 棋盘矩阵（xqf_parser.dart:229-265）。
// 仅版本 >= 12 的布局做了位置置换；解密后每字节减 keyXY。
func decodeBoard(boardBytes []byte, version int, keys *xqfKeys) rules.BoardGrid {
	positions := make([]int, 32)
	for i := range positions {
		positions[i] = 0xff
	}
	if keys == nil {
		for i := 0; i < 32; i++ {
			positions[i] = int(boardBytes[i])
		}
	} else {
		for i := 0; i < 32; i++ {
			if version >= 12 {
				// 版本 >= 12：布局位置置换（4 段各 8 列重排的等价环形写法）。
				positions[(keys.keyXY+i+1)&0x1f] = int(boardBytes[i])
			} else {
				positions[i] = int(boardBytes[i])
			}
		}
		for i := 0; i < 32; i++ {
			positions[i] = (positions[i] - keys.keyXY) & 0xff
		}
	}

	board := make(rules.BoardGrid, 10)
	for r := range board {
		board[r] = make([]*rules.Piece, 9)
	}
	for i := 0; i < 32; i++ {
		v := positions[i]
		if v > 89 {
			continue // 0xFF 或越界 = 无子
		}
		col, row := v/10, v%10
		if col > 8 || row > 9 {
			continue
		}
		board[9-row][col] = rules.PieceFromFenChar(xqfPieceChars[i])
	}
	return board
}

// decodeMainLine 走子主线（xqf_parser.dart:269-346）。
// 数据块每字节 (raw − f32[(headerSize+i) % 32]) & 0xFF；from/to 减偏移
// 0x18/0x20 再减 keyXYf/keyXYt；注解长度 int32 且减 keyRmkSize。
func decodeMainLine(data []byte, version int, keys *xqfKeys) []string {
	var buff []byte
	if keys == nil {
		buff = data[XQFHeaderSize:]
	} else {
		raw := data[XQFHeaderSize:]
		out := make([]byte, len(raw))
		for i := 0; i < len(raw); i++ {
			out[i] = byte((int(raw[i]) - keys.f32[(XQFHeaderSize+i)%32]) & 0xff)
		}
		buff = out
	}

	index := 0
	moves := make([]string, 0)
	readInt32 := func(off int) int {
		if off+4 > len(buff) {
			return 0
		}
		return int(int32(buff[off]) | int32(buff[off+1])<<8 | int32(buff[off+2])<<16 | int32(buff[off+3])<<24)
	}
	// 根记录：代表初始局面的伪走子，只关心其注解位。
	if len(buff)-index < 4 {
		return moves
	}
	rootFlag := buff[index+2]
	index += 4
	annoteLen := 0
	if keys == nil {
		annoteLen = readInt32(index)
		index += 4
	} else {
		if rootFlag&xqfStepFlagMask&xqfStepHasAnno != 0 {
			annoteLen = readInt32(index) - keys.keyRmkSize
			index += 4
		}
	}
	if annoteLen > 0 {
		index += annoteLen // 注解内容暂不展示，仅跳过
	}

	for len(buff)-index >= 4 {
		fromRaw := int(buff[index])
		toRaw := int(buff[index+1])
		flagRaw := int(buff[index+2])
		index += 4

		var hasNext bool
		var fromPos, toPos int
		if keys == nil {
			// 旧版本：注解长度总是存在；高 4 位有值 = 有后续走子。
			annoteLen = readInt32(index)
			index += 4
			if annoteLen > 0 {
				index += annoteLen
			}
			hasNext = flagRaw&0xf0 != 0
			fromPos = (fromRaw - xqfMoveFromOffset) & 0xff
			toPos = (toRaw - xqfMoveToOffset) & 0xff
		} else {
			flag := flagRaw & xqfStepFlagMask
			if flag&xqfStepHasAnno != 0 {
				annoteLen = readInt32(index) - keys.keyRmkSize
				index += 4
				if annoteLen > 0 {
					index += annoteLen
				}
			}
			hasNext = flag&xqfStepHasNext != 0
			fromPos = (fromRaw - xqfMoveFromOffset - keys.keyXYf) & 0xff
			toPos = (toRaw - xqfMoveToOffset - keys.keyXYt) & 0xff
		}

		from := decodeXqfPosition(fromPos)
		to := decodeXqfPosition(toPos)
		if from == nil || to == nil {
			// XQF 走子位置越界，主线终止（xqf_parser.dart:336-338）。
			break
		}
		iccs := FormatIccs(*from, *to)
		if iccs == "" {
			break
		}
		moves = append(moves, iccs)

		if !hasNext {
			break
		}
	}
	return moves
}

// decodeXqfPosition 解码位置字节 `x*10 + y` 为内部坐标（row 与 y 上下颠倒，xqf_parser.dart:349-355）。
func decodeXqfPosition(posByte int) *rules.Position {
	if posByte > 89 {
		return nil
	}
	col := posByte / 10
	row := 9 - posByte%10
	if col < 0 || col > 8 || row < 0 || row > 9 {
		return nil
	}
	p := rules.Pos(col, row)
	return &p
}

// ParseXqf 解析 XQF 字节流为 ParsedPuzzle（xqf_parser.dart:71-165）。
// source 为来源标注（如语料分类路径），缺省 `xqf`。
// 魔数错误 / 文件过短 / 无将帅时返回 *rules.FenFormatError（等价 Dart FormatException）。
func ParseXqf(data []byte, source string) (*ParsedPuzzle, error) {
	if len(data) < XQFHeaderSize+8 {
		return nil, &rules.FenFormatError{Message: fmt.Sprintf("XQF 文件过短: %d 字节", len(data))}
	}
	if data[0] != 0x58 || data[1] != 0x51 {
		return nil, &rules.FenFormatError{
			Message: fmt.Sprintf("XQF 魔数错误: 0x%x0x%x", data[0], data[1]),
		}
	}
	version := int(data[2])

	// 头部字段（与 cchess io_xqf.py 的 struct 布局一致）。
	var keys *xqfKeys
	if version > xqfLegacyVersionMax {
		k := deriveKeys(int(data[3]), int(data[8]), int(data[9]), int(data[10]), int(data[11]),
			int(data[12]), int(data[13]), int(data[14]), int(data[15]))
		keys = &k
	}

	boardBytes := data[16:48]
	title := readXqfString(data, 80, 63)
	event := readXqfString(data, 208, 63)
	date := readXqfString(data, 272, 15)
	redName := readXqfString(data, 352, 15)
	blackName := readXqfString(data, 368, 15)

	// 棋子布局 → 棋盘矩阵 + 缺将帅校验。
	board := decodeBoard(boardBytes, version, keys)
	hasRedKing, hasBlackKing := false, false
	for _, row := range board {
		for _, p := range row {
			if p != nil && p.Kind == rules.King {
				if p.Side == rules.Red {
					hasRedKing = true
				} else {
					hasBlackKing = true
				}
			}
		}
	}
	if !hasRedKing || !hasBlackKing {
		return nil, &rules.FenFormatError{Message: "XQF 局面缺少将/帅"}
	}

	// 走子主线。
	moves := decodeMainLine(data, version, keys)

	// 走子方：有走法则看第一着起点棋子颜色，否则默认红先。
	isRedTurn := true
	if len(moves) > 0 {
		if first := ParseIccs(moves[0]); first != nil {
			if mover := board[first.From.Row][first.From.Col]; mover != nil {
				isRedTurn = mover.Side == rules.Red
			}
		}
	}
	fen := rules.BuildFen(board, isRedTurn)

	// 元数据组装。
	titleText := strings.TrimSpace(title)
	if titleText == "" {
		titleText = defaultXqfTitle(redName, blackName)
	}
	descParts := make([]string, 0, 3)
	if e := strings.TrimSpace(event); e != "" {
		descParts = append(descParts, e)
	}
	if d := strings.TrimSpace(date); d != "" {
		descParts = append(descParts, d)
	}
	descParts = append(descParts, xqfPlayerOr(redName)+" vs "+xqfPlayerOr(blackName))

	return &ParsedPuzzle{
		ID:            fmt.Sprintf("xqf/%s/%s", source, titleText),
		InitialFen:    fen,
		SolutionMoves: moves,
		Title:         &titleText,
		Description:   optionalText(strings.Join(descParts, " · ")),
		Source:        source,
		Format:        "xqf",
		Difficulty:    DifficultyFromMoveCount(len(moves)),
	}, nil
}
