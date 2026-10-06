// POC-4 长列表虚拟化（T0'.5，design_docs/08 §8）：合成 14 万局数据集 +
// layout.List 仅渲染可见条目（Gio 内建虚拟化），实测滚动帧率与每帧布局开销。
// 行投影对齐 corpusTypes 语义（标题/日期/步数，不解析全谱）；本文件不继承。
package ui

import (
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget/material"
)

// pocGameCount 合成数据集规模（08 §8：14 万局）。
const pocGameCount = 140000

// pocGameRow 行投影（对应 PgnGameIndex 投影字段的最小集）。
type pocGameRow struct {
	title    string
	date     string
	moves    int
	category string
}

// pocSynthGames 合成 14 万局数据集（固定种子，确定性；启动一次 ~几十 ms）。
func pocSynthGames() []pocGameRow {
	rng := rand.New(rand.NewSource(20261006))
	players := []string{"许银川", "洪智", "赵鑫鑫", "王天一", "郑惟桐", "蒋川", "孟辰", "谢靖",
		"吕钦", "陶汉明", "徐天红", "于幼华", "孙勇征", "党斐", "张学潮", "李翰林"}
	events := []string{"全国象棋甲级联赛", "个人赛", "五羊杯", "碧桂园杯", "排位赛", "世锦赛"}
	cats := []string{"PGN", "XQF", "语料"}
	rows := make([]pocGameRow, pocGameCount)
	for i := range rows {
		a, b := players[rng.Intn(len(players))], players[rng.Intn(len(players))]
		if a == b {
			b = players[(rng.Intn(len(players)-1)+1+len(players))%len(players)]
		}
		rows[i] = pocGameRow{
			title: fmt.Sprintf("%s 第%d轮 %s 先和 %s", events[rng.Intn(len(events))],
				rng.Intn(28)+1, a, b),
			date:     fmt.Sprintf("20%02d-%02d-%02d", 10+rng.Intn(26), rng.Intn(12)+1, rng.Intn(28)+1),
			moves:    20 + rng.Intn(280),
			category: cats[rng.Intn(len(cats))],
		}
	}
	return rows
}

// PocList POC-4 页面状态（主 goroutine 独占，铁律 #G3）。
type PocList struct {
	games []pocGameRow
	list  layout.List

	autoscroll bool // POC_LIST_AUTOSCROLL=1：每帧程序化滚动（最坏情况压测）
	scrollDir  float32
	frameCosts [64]time.Duration // 最近 64 帧布局耗时环形缓冲
	costIdx    int
	frames     int
	fpsMark    time.Time
	fps        int
}

// NewPocList 构造 POC-4 页（合成数据 + 自动滚动开关）。
func NewPocList() *PocList {
	games := pocSynthGames()
	log.Printf("poc4: 合成数据集 %d 局就绪", len(games))
	return &PocList{
		games:      games,
		list:       layout.List{Axis: layout.Vertical},
		autoscroll: os.Getenv("POC_LIST_AUTOSCROLL") == "1",
		scrollDir:  1,
	}
}

// Layout 统计条 + 虚拟化列表。
func (p *PocList) Layout(gtx layout.Context) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)

	start := time.Now()
	defer func() {
		p.frameCosts[p.costIdx] = time.Since(start)
		p.costIdx = (p.costIdx + 1) % len(p.frameCosts)
		p.frames++
		if p.fpsMark.IsZero() {
			p.fpsMark = gtx.Now
		}
		if el := gtx.Now.Sub(p.fpsMark); el >= time.Second {
			p.fps = p.frames
			p.frames = 0
			p.fpsMark = gtx.Now
		}
	}()

	if p.autoscroll {
		// 程序化滚动：每帧 ±1 行（ScrollBy 单位=行），端点反弹。
		// 教训（KG-002 候选）：First 越界为负会触发 gio text 迭代器以天文坐标
		// 造字形路径、GPU 帧失败黑屏——故每帧硬钳制 First。
		p.list.Position.First += int(p.scrollDir) // 程序化推进（ScrollBy 有 KG-002 病理，见下注）
		p.list.Position.First = min(max(p.list.Position.First, 0), len(p.games)-1)
		if p.list.Position.First < 0 {
			p.list.Position.First = 0
		}
		if p.list.Position.First > len(p.games)-1 {
			p.list.Position.First = len(p.games) - 1
		}
		if p.scrollDir < 0 && p.list.Position.First == 0 {
			p.scrollDir = 1
		} else if p.scrollDir > 0 && p.list.Position.First >= len(p.games)-2 {
			p.scrollDir = -1
		}
		gtx.Execute(op.InvalidateCmd{})
	}
	// KG-002（M0' 实测）：layout.List.ScrollBy 在"每帧程序化滚动+每帧 Invalidate"
	// 场景下使 text 绘制进入病理性慢路径（字形 Bitmaps/Shape 巨型化，帧永不完
	// 成→窗口黑屏，goroutine 栈显示卡在 widget/label.go paintGlyph）。规避：直接
	// 推进 Position.First（本 POC 口径）；M5' 正式页用自然手势滚动，不经 ScrollBy。

	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutStats),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return p.list.Layout(gtx, len(p.games), func(gtx layout.Context, i int) layout.Dimensions {
				return p.layoutRow(gtx, i)
			})
		}),
	)
}

// layoutRow 单行投影（标题/日期/步数/分类徽标）——每帧只对可见行执行。
func (p *PocList) layoutRow(gtx layout.Context, i int) layout.Dimensions {
	g := p.games[i]
	return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						t := material.Body1(pocTheme, g.title)
						t.Color = ThemeOnSurface
						return t.Layout(gtx)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Dimensions{}
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						b := material.Body2(pocTheme, g.category)
						b.Color = ThemeBoardLine
						return b.Layout(gtx)
					}),
				)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := material.Body2(pocTheme, g.date+" · "+itoa(g.moves)+"手")
				t.Color = ThemeBoardLine
				return t.Layout(gtx)
			}),
		)
	})
}

// layoutStats 统计条：FPS + 最近帧开销均值/峰值。
func (p *PocList) layoutStats(gtx layout.Context) layout.Dimensions {
	var sum, max time.Duration
	n := 0
	for _, c := range p.frameCosts {
		if c > 0 {
			n++
			sum += c
			if c > max {
				max = c
			}
		}
	}
	avg := time.Duration(0)
	if n > 0 {
		avg = sum / time.Duration(n)
	}
	mode := "手动拖拽滚动"
	if p.autoscroll {
		mode = "程序化自动滚动（最坏情况）"
	}
	return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := material.H5(pocTheme, "POC-4 长列表虚拟化")
				t.Color = ThemeOnSurface
				return t.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(16))
				return layout.Dimensions{Size: gtx.Constraints.Min}
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := material.Body1(pocTheme, fmt.Sprintf(
					"%d 局 · FPS %d · 帧开销 avg %v / max %v · %s",
					len(p.games), p.fps, avg.Round(time.Microsecond), max.Round(time.Microsecond), mode))
				t.Color = ThemeOnSurface
				return t.Layout(gtx)
			}),
		)
	})
}
