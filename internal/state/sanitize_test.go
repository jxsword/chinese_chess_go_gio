package state

// 表单文本清洗用例（M4' 验收反馈修复轮；实测存储形态锚定）。

import "testing"

func TestSanitizeTextField(t *testing.T) {
	cases := []struct{ in, want string }{
		{"qwen3.8-max\x00\x00\x00\x00", "qwen3.8-max"}, // 用户存档实测形态
		{"  https://a/v1\n", "https://a/v1"},
		{"sk-\x00key\x7f", "sk-key"},
		{"", ""},
		{"\x00", ""},
		{"保持中文", "保持中文"},
	}
	for _, c := range cases {
		if got := SanitizeTextField(c.in); got != c.want {
			t.Fatalf("SanitizeTextField(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
