package parsers

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// ICCS 坐标走法工具（对应 iccs.dart，06 文档 §2；逐行翻译 iccs.ts）。
//
// ICCS（International Chinese Chess Standard）记谱：列 a-i（红方视角从左到右），
// 行 0-9（0 为红方底线、9 为黑方底线），一着写作起止两格，如 `h3e3` / `H3-E3`。
//
// 本项目内部坐标为 Position（col 0-8、row 0-9，row 0 为黑方底线），
// 因此与 ICCS 的换算关系是 **row = 9 − rank**。
//
// 注意与走子源 encodeCell 的区别：后者行 0 为黑底线（LLM 协议），两者行号
// 镜像，禁止混用。解析器（XQF/PGN）与演示 VM 共用本文件，避免两处实现漂移。

// iccsPattern 解析宽松正则（iccs.dart:7-23）：兼容黑底线写 10 的遗留写法。
var iccsPattern = regexp.MustCompile(`^\s*([a-iA-I])(\d{1,2})\s*-?\s*([a-iA-I])(\d{1,2})\s*$`)

// FromTo 解析结果：起止坐标（iccs.ts 的 {from, to}）。
type FromTo struct {
	From rules.Position
	To   rules.Position
}

// ParseIccs 解析 ICCS 走法为起止坐标；格式非法或坐标越界返回 nil（iccs.dart:25-43）。
func ParseIccs(iccs string) *FromTo {
	m := iccsPattern.FindStringSubmatch(iccs)
	if m == nil {
		return nil
	}
	from := parseIccsSquare(m[1], m[2])
	to := parseIccsSquare(m[3], m[4])
	if from == nil || to == nil {
		return nil
	}
	return &FromTo{From: *from, To: *to}
}

// parseIccsSquare 单格解析：列 a-i + 行 0-9（0 为红方底线）。
func parseIccsSquare(file, rankStr string) *rules.Position {
	rank, err := strconv.Atoi(rankStr)
	if err != nil || rank > 10 {
		return nil
	}
	col := int(strings.ToLower(file)[0] - 'a')
	if col < 0 || col > 8 {
		return nil
	}
	// 行号兼容个别记谱把黑方底线写成 10 的情况（此时按 0 处理）。
	row := 9 - rank
	if rank >= 10 {
		row = 0
	}
	if row < 0 || row > 9 {
		return nil
	}
	p := rules.Pos(col, row)
	return &p
}

// FormatIccs 把起止坐标编码为小写紧凑 ICCS（`h3e3`），坐标越界返回空串（iccs.dart:46-58）。
func FormatIccs(from, to rules.Position) string {
	square := func(p rules.Position) string {
		if !rules.InBoard(p.Col, p.Row) {
			return ""
		}
		file := byte('a' + p.Col)
		rank := 9 - p.Row
		return string([]byte{file}) + strconv.Itoa(rank)
	}
	f := square(from)
	t := square(to)
	if f == "" || t == "" {
		return ""
	}
	return f + t
}
