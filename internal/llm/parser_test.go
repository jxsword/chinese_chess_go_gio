package llm

// LlmMoveParser 30 条解析用例（T4.2，09 §2.3 表 + 05 §4 第 3/4 层；
// test/llm/moveParser.spec.ts 移植）：归一化（围栏/全角/零宽/BOM/小写）
// 与坐标提取（标记优先/最后坐标/越界 nil）。

import "testing"

func codePtr(s string) *string { return &s }

func TestExtractMove30Cases(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want *string
	}{
		{"01 严格单行格式", "着法: b2-e2", codePtr("b2-e2")},
		{"02 全角冒号+全角字母数字+全角横线", "着法：ｂ２－ｅ２", codePtr("b2-e2")},
		{"03 markdown 围栏（无语言标记）", "```\n着法: b2-e2\n```", codePtr("b2-e2")},
		{"04 markdown 围栏（带语言标记）", "```text\n着法: b2-e2\n```", codePtr("b2-e2")},
		{"05 零宽字符混入坐标", "着法: b\u200b2-\u200be2", codePtr("b2-e2")},
		{"06 BOM 前缀", "\uFEFF着法: b2-e2", codePtr("b2-e2")},
		{"07 多坐标对无标记 → 取最后一个", "先看 b2-e2 再看 h2-e2", codePtr("h2-e2")},
		{"08 多坐标对 + 标记 → 标记后第一个优先", "b2-e2 是错的。着法: h2-e2 另外 h0-g2", codePtr("h2-e2")},
		{"09 无分隔符", "着法: b2e2", codePtr("b2-e2")},
		{"10a 中文分隔「到」", "着法: b2到e2", codePtr("b2-e2")},
		{"10b 中文分隔「至」", "着法: b2至e2", codePtr("b2-e2")},
		{"11a 长破折号 –", "着法: b2–e2", codePtr("b2-e2")},
		{"11b 长破折号 —", "着法: b2—e2", codePtr("b2-e2")},
		{"11c 波浪线 ~", "着法: b2~e2", codePtr("b2-e2")},
		{"12 空白分隔", "着法: b2 e2", codePtr("b2-e2")},
		{"13 大写字母 → 小写归一", "着法: B2-E2", codePtr("b2-e2")},
		{"14 v2 分析段 + 最后一行着法", "分析: 进攻中路，威胁黑炮。\n着法: h2-e2", codePtr("h2-e2")},
		{"15 思维链长文本中取标记后坐标", "我先考虑车九平八，然后马二进三，对方可能炮8平5，我决定着法: c3-c4", codePtr("c3-c4")},
		{"16 行越界数字 b10 → 无法成对", "着法: b10-e2", nil},
		{"17 边界行列 a0 / i9 合法", "着法: a0-i9", codePtr("a0-i9")},
		{"18a 字母越界 j", "着法: j2-e2", nil},
		{"18b 字母越界 z", "着法: z9-a0", nil},
		{"19a 空串", "", nil},
		{"19b 纯空白", "   ", nil},
		{"20 纯杂质无坐标", "抱歉，我无法回答。", nil},
		{"21 全角大写 Ｂ２ → 先小写再转半角", "着法：Ｂ２－Ｅ２", codePtr("b2-e2")},
		{"22 相同起点终点也原样提取（白名单层负责拒绝）", "着法: e3-e3", codePtr("e3-e3")},
		{"23 无空格冒号", "着法:b2-e2", codePtr("b2-e2")},
		{"24 冒号后带空白变体", "着法 :   b2-e2", codePtr("b2-e2")},
		{"24b 全角空格冒号（TS \\s 全集对齐回归）", "着法　: b2-e2", codePtr("b2-e2")},
		{"25 引号包裹", "\"着法: b2-e2\"", codePtr("b2-e2")},
		{"26 多个标记 → 最后一个标记后优先", "着法: a0-a1\n着法: i9-i8", codePtr("i9-i8")},
		{"27 标记前有候选坐标时仍以标记后为准", "候选 b2-e2 与 h2-e2 之间犹豫。最终回答：\n着法: h0-g2", codePtr("h0-g2")},
		{"28 归一化为 b2-e2 形式（多余空白压缩）", "着法:  b2 -  e2", codePtr("b2-e2")},
		{"29 多行回复含空行", "分析: 稳健出子。\n\n着法: c3-c4\n", codePtr("c3-c4")},
		{"30 代码块内零宽+全角混合", "```\n着法：ｂ\u200b２－ｅ２\n```", codePtr("b2-e2")},
	}
	for _, c := range cases {
		got := ExtractMove(c.in)
		if c.want == nil {
			if got != nil {
				t.Errorf("%s：ExtractMove(%q) = %q, want nil", c.name, c.in, *got)
			}
			continue
		}
		if got == nil || *got != *c.want {
			t.Errorf("%s：ExtractMove(%q) = %v, want %q", c.name, c.in, got, *c.want)
		}
	}
}

func TestNormalizeReply(t *testing.T) {
	// 剥离 ``` 围栏标记本身
	if got := NormalizeReply("```ts\nx\n```"); got != "\nx\n" {
		t.Errorf("围栏剥离 = %q", got)
	}
	// 删除零宽字符与 BOM，保留普通中文
	if got := NormalizeReply("\u200b着\u200c法\uFEFF"); got != "着法" {
		t.Errorf("零宽删除 = %q", got)
	}
	// 全角 ASCII 区平移回半角（！到 ～；先小写故大写亦转小写）
	if got := NormalizeReply("ＡＺａｚ０９！～：－"); got != "azaz09!~:-" {
		t.Errorf("全角平移 = %q", got)
	}
	// 小写化（保留中文与全角区外字符）
	if got := NormalizeReply("着法: B2-E2"); got != "着法: b2-e2" {
		t.Errorf("小写化 = %q", got)
	}
}
