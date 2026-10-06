// KG-004 恢复助手单元测试：真实探针样本（clip.exe→WSLg 桥→gio 实测乱码）回归。
package ui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 实测样本：Windows 剪贴板"中文粘贴测试ABC123"经 WSLg 桥接后 gio 收到的串。
const mojibakeSample = "涓\ue15f枃绮樿创娴嬭瘯ABC123"

func TestClipboardMojibakeRecover(t *testing.T) {
	got, changed := clipboardMojibakeRecover(mojibakeSample)
	if !changed || got != "中文粘贴测试ABC123" {
		t.Fatalf("recover=%q changed=%v want 中文粘贴测试ABC123", got, changed)
	}
}

// 混排样本（含 CJK/ASCII 边界）：桥接在该边界处 (续字节,ASCII) 对非法、字节已
// 被 WSLg 桥接丢弃（替换为 RuneError），信息不可逆——恢复必须**拒绝**而非输出
// 残缺文本（KG-004 的关键契约：恢复仅用于边界损失前的纯中文段，见恢复函数守卫）。
func TestClipboardMojibakeRecoverMixedLossyRefused(t *testing.T) {
	orig := "在中问一次：GPT-5 与 GLM 的棋力评估，谁更强？结果 2:1。"
	// 构造 mojibake：orig 的 UTF-8 字节按 gb18030 双字节语义解码（与 WSLg 桥同向）
	var m strings.Builder
	b := []byte(orig)
	for i := 0; i < len(b); {
		if b[i] < 0x80 {
			m.WriteByte(b[i])
			i++
			continue
		}
		if i+1 < len(b) {
			ch, size := decodeGB18030Two(b[i], b[i+1])
			if size == 2 {
				m.WriteRune(ch)
				i += 2
				continue
			}
		}
		m.WriteRune(utf8.RuneError)
		i++
	}
	got, changed := clipboardMojibakeRecover(m.String())
	if changed || got != m.String() {
		t.Fatalf("有损混排必须拒绝恢复: recover=%q changed=%v", got, changed)
	}
}

func TestClipboardMojibakeRecoverNoop(t *testing.T) {
	// 正常文本（无标记）不触发恢复
	for _, s := range []string{"hello", "中文正常文本", "Gio 中文混排 ABC123", ""} {
		if got, changed := clipboardMojibakeRecover(s); changed || got != s {
			t.Fatalf("正常文本被误改: %q -> %q(%v)", s, got, changed)
		}
	}
}

func TestClipboardMojibakeRecoverGarbage(t *testing.T) {
	// 含标记但 GB18030 回写后不是有效 UTF-8 → 原样返回
	garbage := "�\ue000"
	got, changed := clipboardMojibakeRecover(garbage)
	if changed || got != garbage {
		t.Fatalf("垃圾串应原样返回: %q changed=%v", got, changed)
	}
}
