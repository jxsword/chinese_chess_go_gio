package engine

// Zobrist 哈希（DR-006 前置，03 文档 §4；Electron 版 zobrist.ts 的 Go 化）。
//
// - Go 直接用 uint64 单键（TS 拆 hashLo/hashHi 双 32 位，Go 无需）；
// - 随机键表：15 种棋子编码（-7..7，0=空不占键）× 90 格的 uint64 表 + 一枚先手方键；
// - 固定种子 xorshift64（种子 0x9e3779b9，Marsaglia 移位 13/7/17）惰性初始化——
//   键值序列跨进程确定，测试以确定性快照锁定（Go 键值不要求与 TS 一致）；
// - 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。

import "sync"

// 棋子编码种类数（-7..7 共 15 种，0=空不占用键）。
const zobristCodeSpan = 15

// pieceKeys[code+7][sq] 棋子键；turnKey 先手方（红方走子）键：黑方走子时异或，使轮走方参与键。
var (
	zobristOnce sync.Once
	pieceKeys   [zobristCodeSpan][90]uint64
	turnKey     uint64
)

// initZobrist 固定种子 xorshift64 填表（首次使用时执行一次）。
// 换种子/移位会改变全部键，须同步 review 快照测试。
func initZobrist() {
	zobristOnce.Do(func() {
		s := uint64(0x9e3779b9) // 黄金比例常数做种子
		next := func() uint64 {
			s ^= s << 13
			s ^= s >> 7
			s ^= s << 17
			return s
		}
		for i := 0; i < zobristCodeSpan; i++ {
			for sq := 0; sq < 90; sq++ {
				pieceKeys[i][sq] = next()
			}
		}
		turnKey = next()
	})
}

// rebuildZobrist 全量重建键（fromFen 与测试的一致性基准）：
// 逐格异或 (code,sq) 键，黑方走子再异或先手方键。
func rebuildZobrist(data *[90]int8, isRedTurn bool) uint64 {
	initZobrist()
	var key uint64
	for sq := 0; sq < 90; sq++ {
		code := data[sq]
		if code == 0 {
			continue
		}
		key ^= pieceKeys[code+7][sq]
	}
	if !isRedTurn {
		key ^= turnKey
	}
	return key
}
