package llm

// 从模型回复中提取着法（纯函数，便于单测；Electron 版 parser.ts 1:1 移植，
// llm_move_source.dart:162-216 同源）。
//
// 模型即使被严格约束也可能输出杂质（markdown 代码块、全角字符、
// 零宽字符、多余空白），这里全部归一化后再解析。
// 纯 Go：禁止 import Wails / net/http / frontend（铁律 #1）。

import (
	"regexp"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// movePattern 坐标对：列字母 + 行数字 + 可选分隔符（含中文"到/至"与长破折号）。
// 空白类显式列举 TS \s 全集（RE2 的 \s 缺 \v/\u00a0/\u3000 等 Unicode 空白，
// 会导致「着法〈全角空格〉:」这类标记失配——第一轮复审 R1-P3d 对齐项）。
var movePattern = regexp.MustCompile(`([a-i])[ \t\n\v\f\r\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]*(\d)[ \t\n\v\f\r\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]*[-–—~到至]?[ \t\n\v\f\r\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]*([a-i])[ \t\n\v\f\r\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]*(\d)`)

// labeledPattern 「着法:」标记（允许冒号前空白；全角冒号已在归一化时转半角）。
var labeledPattern = regexp.MustCompile(`着法[ \t\n\v\f\r\x{00a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]*:`)

// ExtractMove 返回归一化的 "b2-e2" 形式；无法解析返回 nil。
//
// 若回复含多个坐标对，优先取「着法:」标记之后的，否则取最后一个
// （llm_move_source.dart:178-199）。
func ExtractMove(rawContent string) *string {
	content := NormalizeReply(rawContent)
	if strings.TrimSpace(content) == "" {
		return nil
	}
	matches := movePattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	pick := matches[len(matches)-1]
	if labeled := labeledPattern.FindAllStringSubmatchIndex(content, -1); len(labeled) > 0 {
		lastLabeled := labeled[len(labeled)-1]
		labeledPos := lastLabeled[1] // 标记结尾（index + 匹配长度）
		for _, m := range matches {
			if m[0] >= labeledPos {
				pick = m
				break
			}
		}
	}
	group := func(g int) string {
		return content[pick[2*g]:pick[2*g+1]]
	}
	from := DecodeCell(group(1) + group(2))
	to := DecodeCell(group(3) + group(4))
	if from == nil || to == nil {
		return nil
	}
	code := EncodeMove(rules.Move{From: *from, To: *to})
	return &code
}

// NormalizeReply 解析前的清洗（parser.ts:51-62 逐字移植）：
// 剥离代码块围栏、去零宽字符与 BOM、小写化、全角 ASCII 区
// （！到 ～，U+FF01–U+FF5E）逐码位平移 −0xFEE0 转半角。
func NormalizeReply(raw string) string {
	text := fencePattern.ReplaceAllString(raw, "")
	text = strings.ReplaceAll(text, "```", "")
	text = zeroWidthPattern.ReplaceAllString(text, "")
	text = strings.ToLower(text)
	var out strings.Builder
	out.Grow(len(text))
	for _, u := range text {
		// 全角 ASCII 区平移回半角（与 Dart/TS 逐码位处理等价）。
		if u >= 0xff01 && u <= 0xff5e {
			out.WriteRune(u - 0xfee0)
		} else {
			out.WriteRune(u)
		}
	}
	return out.String()
}

var (
	// fencePattern ``` 围栏（可带语言标记）——剥离标记本身（保留围栏内文本）。
	fencePattern = regexp.MustCompile("```[a-zA-Z]*")
	// zeroWidthPattern 零宽字符与 BOM（parser.ts:53）。
	zeroWidthPattern = regexp.MustCompile("[\u200b-\u200f\uFEFF\u2060]")
)
