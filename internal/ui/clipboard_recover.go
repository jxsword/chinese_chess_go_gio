// KG-004 workaround（M0' POC-3/验收实测）：WSLg 剪贴板桥接把 Windows 剪贴板
// 文本按 GB18030 双字节语义转发（含未分配码位→PUA U+E000-F8FF 的规范映射），
// 接收方按 UTF-8 解读产生 mojibake（如"中文粘贴测试"→"涓\ue15f枃绮樿创娴嬭瘯"）。
// 恢复=逐字符反向映射回原始字节序列再按 UTF-8 解读：
//   - PUA 字符 → cp936Pua 表中的双字节码；
//   - 其余字符 → GB18030/GBK 双字节编码（x/text，白名单依赖）。
//
// 仅当串含替换符/PUA（mojibake 标记）时尝试；恢复失败原样返回。
// 依据与限制见 docs/KNOWN_ISSUES.md KG-004；M4'（LLM 配置卡粘贴）沿用。
package ui

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// clipboardMojibakeRecover 尝试把 WSLg 剪贴板桥接产生的 GB18030 mojibake 恢复为原文。
// 返回 (恢复文本, 是否发生了恢复)。
func clipboardMojibakeRecover(s string) (string, bool) {
	if !strings.ContainsFunc(s, mojibakeMarker) {
		return s, false
	}
	var b []byte
	for _, r := range s {
		if code, ok := cp936Pua[r]; ok {
			b = append(b, byte(code>>8), byte(code))
			continue
		}
		enc, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(string(r)))
		if err != nil {
			return s, false
		}
		b = append(b, enc...)
	}
	if !utf8.Valid(b) {
		return s, false
	}
	recovered := string(b)
	// 恢复结果必须干净（无替换符/PUA）且确实含 CJK，否则视为误判放弃。
	if strings.ContainsFunc(recovered, mojibakeMarker) || !strings.ContainsFunc(recovered, isCJK) {
		return s, false
	}
	return recovered, true
}

// decodeGB18030Two 按 GB18030 双字节语义解码 (lead,trail)：
// 已分配→(字符,2)；未分配→(对应 PUA,2)；非法组合→(RuneError,1)。
// 测试用于构造 mojibake 样本（与 WSLg 桥同向）。
func decodeGB18030Two(lead, trail byte) (rune, int) {
	ch, err := simplifiedchinese.GB18030.NewDecoder().Bytes([]byte{lead, trail})
	if err != nil || len(ch) == 0 {
		return utf8.RuneError, 1
	}
	rs := []rune(string(ch))
	if len(rs) != 1 || !utf8.ValidRune(rs[0]) {
		return utf8.RuneError, 1
	}
	return rs[0], 2
}

// mojibakeMarker mojibake 特征：UTF-8 替换符或 GB18030 未分配区 PUA 映射字符。
func mojibakeMarker(r rune) bool {
	return r == utf8.RuneError || (r >= 0xE000 && r <= 0xF8FF)
}

// isCJK 基本汉字区（含扩展 A）。
func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF)
}
