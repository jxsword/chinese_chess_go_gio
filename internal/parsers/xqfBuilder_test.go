package parsers

import (
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"golang.org/x/text/encoding/simplifiedchinese"
)

// 测试辅助：XQF 字节流构造器（xqfParser 的逆变换，Electron 版 test/helpers/xqfBuilder.ts 等价）。
//
// 用途：语料不可用时，以"构造 → 解析"往返验证各版本（旧格式/加密/位置置换）
// 的编解码一致性；真实格式锚点由 testdata/xqf/sample_xqf.xqf（v0x0D 实文件）承担。

// 与解析器一致的常量。
const builderHeaderSize = XQFHeaderSize

var builderPieceChars = xqfPieceChars

const builderKeySeed = xqfKeySeed

type xqfHead struct {
	keyMask, keyOrA, keyOrB, keyOrC, keyOrD, keysSum, headKeyXY, headKeyXYf, headKeyXYt int
}

type xqfBuildOptions struct {
	version int
	fen     string
	// moves ICCS 走法序列。
	moves []string
	title string
	event string
	date  string
	red   string
	black string
	head  *xqfHead
}

// builderFormula 单字节变换基数（与解析器同式）。
func builderFormula(x int) int { return ((((x*x)*3+9)*3+8)*2+1)*3 + 8 }

// writeGbString GB18030 长度前缀字符串写入（超长截断）。
func writeGbString(buf []byte, offset, maxLen int, text string) {
	if text == "" {
		return
	}
	raw, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(text))
	if err != nil {
		return
	}
	n := len(raw)
	if n > maxLen {
		n = maxLen
	}
	buf[offset] = byte(n)
	copy(buf[offset+1:], raw[:n])
}

// builderPositionsFromFen 把 FEN 盘面映射到 32 子固定序的位置字节（x*10+y，0xFF=让子）。
func builderPositionsFromFen(t *testing.T, fen string) []int {
	t.Helper()
	grid, err := rules.ParseBoardFen(fen)
	if err != nil {
		t.Fatalf("builder FEN 非法: %v", err)
	}
	used := make([][]bool, 10)
	for r := range used {
		used[r] = make([]bool, 9)
	}
	positions := make([]int, 0, 32)
	for _, ch := range builderPieceChars {
		found := 0xff
		for row := 0; row < 10 && found == 0xff; row++ {
			for col := 0; col < 9; col++ {
				if used[row][col] {
					continue
				}
				if piece := grid[row][col]; piece != nil && rules.PieceFenChar(piece) == string(ch) {
					found = col*10 + (9 - row)
					used[row][col] = true
					break
				}
			}
		}
		positions = append(positions, found)
	}
	return positions
}

// buildXqf 构造 XQF 文件字节流。
func buildXqf(t *testing.T, o xqfBuildOptions) []byte {
	t.Helper()
	head := o.head
	if head == nil {
		head = &xqfHead{keyMask: 0x3c, keyOrA: 0x2a, keyOrB: 0x51, keyOrC: 0x7e, keyOrD: 0x19,
			keysSum: 0x12, headKeyXY: 0x34, headKeyXYf: 0x56, headKeyXYt: 0x78}
	}
	encrypted := o.version > 0x0a

	keyXY, keyXYf, keyXYt := 0, 0, 0
	var f32 [32]int
	if encrypted {
		keyXY = (builderFormula(head.headKeyXY) * head.headKeyXY) & 0xff
		keyXYf = (builderFormula(head.headKeyXYf) * keyXY) & 0xff
		keyXYt = (builderFormula(head.headKeyXYt) * keyXYf) & 0xff
		keyBytes := [4]int{
			(head.keysSum & head.keyMask) | head.keyOrA,
			(head.headKeyXY & head.keyMask) | head.keyOrB,
			(head.headKeyXYf & head.keyMask) | head.keyOrC,
			(head.headKeyXYt & head.keyMask) | head.keyOrD,
		}
		for i := 0; i < 32; i++ {
			f32[i] = int(builderKeySeed[i]) & keyBytes[i%4]
		}
	}

	// 32 子布局（仅版本 >= 12 做位置置换；加密时加 keyXY）。
	positions := builderPositionsFromFen(t, o.fen)
	boardBytes := make([]byte, 32)
	for i := 0; i < 32; i++ {
		switch {
		case !encrypted:
			boardBytes[i] = byte(positions[i])
		case o.version >= 12:
			// 解析侧：pos[(keyXY+i+1)&0x1F] = boardBytes[i]，逆写为取该槽位的解密值。
			slot := (keyXY + i + 1) & 0x1f
			boardBytes[i] = byte((positions[slot] + keyXY) & 0xff)
		default:
			boardBytes[i] = byte((positions[i] + keyXY) & 0xff)
		}
	}

	// 走子树明文缓冲。
	plain := make([]byte, 0)
	// 根记录：flag 无注解位。
	plain = append(plain, 0, 0, 0, 0)
	if !encrypted {
		plain = append(plain, 0, 0, 0, 0) // 旧格式根记录也带注解长度
	}
	parseSquare := func(s string) int {
		// XQF 位置字节 = x*10 + y，y=0 为红方底线（= ICCS rank）。
		col := int(s[0] - 'a')
		rank := int(s[1] - '0')
		return col*10 + rank
	}
	for idx, iccs := range o.moves {
		from := parseSquare(iccs)
		to := parseSquare(iccs[2:])
		hasNext := idx < len(o.moves)-1
		var fromRaw, toRaw, flag int
		if !encrypted {
			fromRaw = (from + xqfMoveFromOffset) & 0xff
			toRaw = (to + xqfMoveToOffset) & 0xff
			if hasNext {
				flag |= 0x80 // 高 4 位有值 = 有后续
			}
		} else {
			fromRaw = (from + xqfMoveFromOffset + keyXYf) & 0xff
			toRaw = (to + xqfMoveToOffset + keyXYt) & 0xff
			if hasNext {
				flag |= 0x80
			}
		}
		plain = append(plain, byte(fromRaw), byte(toRaw), byte(flag), 0)
		if !encrypted {
			plain = append(plain, 0, 0, 0, 0) // 旧格式注解长度恒存在
		}
	}

	// 组装文件。
	buf := make([]byte, builderHeaderSize+len(plain)+8)
	buf[0] = 0x58
	buf[1] = 0x51
	buf[2] = byte(o.version)
	if encrypted {
		buf[3] = byte(head.keyMask)
		buf[8] = byte(head.keyOrA)
		buf[9] = byte(head.keyOrB)
		buf[10] = byte(head.keyOrC)
		buf[11] = byte(head.keyOrD)
		buf[12] = byte(head.keysSum)
		buf[13] = byte(head.headKeyXY)
		buf[14] = byte(head.headKeyXYf)
		buf[15] = byte(head.headKeyXYt)
	}
	copy(buf[16:], boardBytes)
	writeGbString(buf, 80, 63, o.title)
	writeGbString(buf, 208, 63, o.event)
	writeGbString(buf, 272, 15, o.date)
	writeGbString(buf, 352, 15, o.red)
	writeGbString(buf, 368, 15, o.black)

	if !encrypted {
		copy(buf[builderHeaderSize:], plain)
	} else {
		// 明文块逐字节加 f32（解析侧减回）。
		for i := 0; i < len(plain); i++ {
			buf[builderHeaderSize+i] = byte((int(plain[i]) + f32[(builderHeaderSize+i)%32]) & 0xff)
		}
	}
	return buf
}
