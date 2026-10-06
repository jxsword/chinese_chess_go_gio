// POC-3 中文 IME + 字体回退链 demo（T0'.4，design_docs/08 §1/§10）：
// widget.Editor 中文输入（WSLg fcitx5 / Windows IME 经 RDP）+ typesetting
// 显式注册与系统回退链对比。结论回填 08 §12/§1/§10；本文件不继承。
package ui

import (
	"image/color"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	"gioui.org/font"
	"gioui.org/io/clipboard"
	"gioui.org/io/key"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// pocClipSample 首帧自动写入系统剪贴板的样本（供 Ctrl+V 粘贴路径实测）。
const pocClipSample = "Gio中文粘贴测试ABC123"

// PocIme POC-3 页面状态（主 goroutine 独占，铁律 #G3）。
type PocIme struct {
	ed        widget.Editor
	committed string // 最近提交的完整文本（ChangeEvent 时回读）
	clipWrote bool

	winPasteBtn   widget.Clickable // 从 Windows 剪贴板粘贴（KG-004 可靠路径）
	winPasteArmed bool             // POC_WINPASTE=1 自动触发取证
	winPasteDone  bool
	winPasteAt    time.Time
}

// NewPocIme 构造 POC-3 页。POC_WINPASTE=1 → 启动 1.5s 后自动触发一次
// Windows 剪贴板粘贴（取证通道）。
func NewPocIme() *PocIme {
	p := &PocIme{}
	if os.Getenv("POC_WINPASTE") == "1" {
		p.winPasteArmed = true
	}
	return p
}

// inWSL 当前是否运行于 WSL（/proc/version 含 microsoft）。
func inWSL() bool {
	b, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// pocFontRow 字体回退链样例行：族名 + 说明 + Font。
type pocFontRow struct {
	name string
	desc string
	fnt  font.Font
}

func pocFontRows() []pocFontRow {
	return []pocFontRow{
		{"默认 gofont", "西文内建；中文应经 gio 内建系统回退", font.Font{}},
		{"Noto Sans CJK SC", "显式注册（poc_fonts.go），黑体风", font.Font{Typeface: "Noto Sans CJK SC"}},
		{"Noto Serif CJK SC", "显式注册，宋体风（棋子字近似楷体）", font.Font{Typeface: pocFontFaceSerifSC}},
		{"未知族 KaiTi", "本机不存在 → 观察回退结果", font.Font{Typeface: "KaiTi"}},
	}
}

// Layout 输入区 + 提交回显 + 字体链样例。
func (p *PocIme) Layout(gtx layout.Context) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, PocBackground)

	if !p.clipWrote {
		p.clipWrote = true
		gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(pocClipSample))})
		gtx.Execute(key.FocusCmd{Tag: &p.ed})
	}
	if p.winPasteArmed {
		if p.winPasteAt.IsZero() {
			p.winPasteAt = gtx.Now.Add(1500 * time.Millisecond)
		} else if !gtx.Now.Before(p.winPasteAt) && !p.winPasteDone {
			p.winPasteDone = true
			p.pasteFromWindows(gtx)
		} else if !p.winPasteDone {
			// 注：InvalidateCmd{At: 未来时刻} 在 WSLg 实测不排帧（KG-004 记录），
			// 用立即排帧轮询（1.5s 窗口，POC 可接受）。
			gtx.Execute(op.InvalidateCmd{})
		}
	}
	if p.winPasteBtn.Clicked(gtx) {
		p.pasteFromWindows(gtx)
	}
	// Editor 事件：Change=文本变化（含 IME commit 后）。
	for {
		ev, ok := p.ed.Update(gtx)
		if !ok {
			break
		}
		if _, changed := ev.(widget.ChangeEvent); changed {
			p.committed = p.ed.Text()
			log.Printf("poc3: 提交文本 %q（runes=%d bytes=%d）",
				p.committed, utf8.RuneCountInString(p.committed), len(p.committed))
		}
	}

	sample := "Gio 中文混排 Chinese ABC 123 ——回退链"
	return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		children := []layout.FlexChild{
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := material.H5(pocTheme, "POC-3 中文 IME + 字体回退链")
				t.Color = PocText
				return t.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				b := material.Body2(pocTheme, "输入框已自动聚焦、样本已写入剪贴板。实测：①fcitx5(XIM) 键入 ②Windows IME 经 RDP ③Ctrl+V 粘贴")
				b.Color = PocText
				return b.Layout(gtx)
			}),
			layout.Rigid(p.layoutEditor),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := material.Body2(pocTheme, "已提交文本（ChangeEvent 回读）→ "+p.committed)
				t.Color = PocPieceRed
				return t.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !inWSL() {
					return layout.Dimensions{}
				}
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						b := material.Button(pocTheme, &p.winPasteBtn, "从 Windows 剪贴板粘贴（KG-004 可靠路径）")
						return b.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(12))
						return layout.Dimensions{Size: gtx.Constraints.Min}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						t := material.Body2(pocTheme, "Ctrl+V 走 WSLg 桥接（CJK 乱码）；本按钮走 powershell 管道（UTF-8 正确）")
						t.Color = PocText
						return t.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(8))
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := material.Body2(pocTheme, "字体回退链（同一混排串 × 四种 Font 配置）：")
				t.Color = PocText
				return t.Layout(gtx)
			}),
		}
		for _, row := range pocFontRows() {
			row := row
			children = append(children,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					head := material.Body2(pocTheme, row.name+"（"+row.desc+"）")
					head.Color = PocBoardLine
					return head.Layout(gtx)
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.H5(pocTheme, sample)
					l.Color = PocText
					l.Font = row.fnt
					return l.Layout(gtx)
				}),
			)
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
	})
}

// pasteFromWindows KG-004 可靠粘贴路径：绕过 WSLg 剪贴板桥接（对 CJK 乱码且有损），
// 经 powershell.exe Get-Clipboard 控制台管道（UTF-8）读取 Windows 剪贴板。
// POC 期同步执行（阻塞事件循环数百 ms 可接受）；M4' 正式实现须走 app.Emit 异步（铁律 #G3）。
func (p *PocIme) pasteFromWindows(gtx layout.Context) {
	candidates := []string{
		"/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe", // WSL interop PATH 未注入时的兜底
		"powershell.exe",
	}
	var out []byte
	var err error
	for _, exe := range candidates {
		out, err = exec.Command(exe, "-NoProfile", "-Command", "Get-Clipboard").Output()
		if err == nil {
			break
		}
	}
	if err != nil {
		log.Printf("poc3: windows 剪贴板读取失败: %v", err)
		return
	}
	text := strings.TrimSpace(string(out))
	p.ed.SetText(p.ed.Text() + text)
	log.Printf("poc3: windows 剪贴板粘贴 %q（runes=%d）", text, utf8.RuneCountInString(text))
}

// layoutEditor 输入框（白底描边框内嵌 Editor，固定尺寸防撑满窗口）。
func (p *PocIme) layoutEditor(gtx layout.Context) layout.Dimensions {
	gtx.Constraints.Max.X = gtx.Dp(unit.Dp(480))
	gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(56))
	return layout.UniformInset(unit.Dp(4)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Dp(unit.Dp(480))
		gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(56))
		return layout.Stack{Alignment: layout.NW}.Layout(gtx,
			layout.Expanded(func(gtx layout.Context) layout.Dimensions {
				defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
				paint.FillShape(gtx.Ops, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, clip.Rect{Max: gtx.Constraints.Max}.Op())
				pocStrokeRect(gtx.Ops, 0, 0, float32(gtx.Constraints.Max.X), float32(gtx.Constraints.Max.Y),
					float32(gtx.Dp(1)), PocBoardLine)
				return layout.Dimensions{Size: gtx.Constraints.Max}
			}),
			layout.Stacked(func(gtx layout.Context) layout.Dimensions {
				ed := material.Editor(pocTheme, &p.ed, "在此输入中文（IME）…")
				ed.Color = PocText
				ed.TextSize = unit.Sp(16)
				return ed.Layout(gtx)
			}),
		)
	})
}
