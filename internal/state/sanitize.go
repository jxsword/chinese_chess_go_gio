package state

// 表单文本清洗（M4' 验收反馈修复轮）：剥离 C0 控制字符与 DEL + 首尾空白。
// 实证：百炼端点 404 回显模型名尾部 4 个 NUL——控制字符经键入/剪贴板路径
// 进入表单后原样落盘/进请求体。纯 Go（#G1），ui 与 app 双端共用：
//   - ui.LlmConfigCard 表单边界（编辑器文本 → 配置）；
//   - app 凭据代理读写边界（存储加载回读 + 落盘前）——修复"脏配置已存盘，
//     表单显示干净但走子管线仍用原始值"的半覆盖问题。

import "strings"

// SanitizeTextField 剥离全部 C0 控制字符（NUL/制表/回车等）与 DEL，并去首尾空白。
func SanitizeTextField(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, strings.TrimSpace(s))
}
