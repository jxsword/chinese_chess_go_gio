package ui

// 语料库/棋谱库页（T5'.1，翻译源 = 上游 frontend/src/features/puzzle/CorpusBrowserPage.tsx；
// 对应 corpus_browser_page.dart + corpus_pgn_browser_page.dart，06 文档 §6；Gio 规格 08 §8）。
//
// 一级入口为分类列表；XQF 分类：搜索/仅残局/难度筛选/三种排序 + 分批解析进度；
// PGN 大文件分类：按局索引虚拟化长列表（T5'.2）；单局/XQF 条目点击进入详情重放
// （T5'.3 重放器）。语料缺失时显示引导（期望路径 + 下载按钮约 45MB + 取消）。
// "选择其他棋谱目录"依赖 00 §4 开放决策项（T5'.4），定案前不实现（08 §8 注记③）。

import (
	"fmt"
	"image"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// CorpusDownloadURL 语料包下载地址（corpus_paths.dart:34-35，GitHub Release
// 附件，永远指向最新一版；上游 shared/constants.ts CORPUS_DOWNLOAD_URL）。
const CorpusDownloadURL = "https://github.com/jxsword/qp-corpus/releases/latest/download/qp-corpus.zip"

// difficultyLabels 难度筛选档位（DIFFICULTY_LABELS，0 = 全部）。
var difficultyLabels = []string{"全部难度", "入门", "初级", "中级", "高级", "职业"}

// sortLabels 排序档位。
var sortLabels = []struct {
	mode  state.CorpusSortMode
	label string
}{
	{state.SortName, "按名称"},
	{state.SortMoves, "按步数"},
	{state.SortDifficulty, "按难度"},
}

// CorpusHooks 语料页回调。
type CorpusHooks struct {
	OnBack func()
	// OnBattle 进入对战（T5'.3 落位）：mode + 起点 FEN（字段随 T5'.3 接线）。
}

// CorpusPage 语料库页 state struct（铁律 #G3：主 goroutine 独占；工厂页，
// 每次导航进入创建新实例并重扫——对齐上游 useEffect(load, []) 每挂载执行）。
type CorpusPage struct {
	hooks CorpusHooks
	env   CorpusEnv

	store      *state.CorpusBrowser
	downloader *CorpusDownloader

	// 下载引导 UI 态（上游 MissingGuide 组件态；防错 #10：可取消，半成品保留）
	downloading   bool
	downloadID    string
	cancelFlag    atomic.Int32 // IsCancelled 探针（复制物取消语义）
	receivedBytes int64
	totalBytes    int64
	message       string

	// 控件
	backBtn           widget.Clickable
	downloadBtn       widget.Clickable
	cancelDownloadBtn widget.Clickable
	closeDetailBtn    widget.Clickable
	searchEditor      widget.Editor
	pgnSearchEditor   widget.Editor
	pasteSearchBtn    widget.Clickable
	pastePgnBtn       widget.Clickable
	onlyEndgame       widget.Bool
	diffClicks        [6]widget.Clickable
	sortClicks        [3]widget.Clickable
	categoryClicks    []widget.Clickable

	// 列表（虚拟化：仅 layout 可见条目；KG-002 口径——手势滚动，不经 ScrollBy）
	catList   layout.List
	list      layout.List
	pgnList   layout.List
	rowClicks map[int]*widget.Clickable
	pgnClicks map[int]*widget.Clickable

	// PGN 面板缓存：140k 级索引的过滤结果不逐帧重算（ dirty 置位后重算一次）
	pgnFiltered []storage.PgnIndexEntry
	pgnDirty    bool

	// 性能实测（T5'.2 验证门，POC-4 口径；CC_GIO_SYNTH_SCROLL=1 启用，
	// 非交互路径）：自动滚动 + 帧开销/FPS 统计浮层。
	perf *framePerf

	// PGN 搜索（T5'.2）与详情视图（T5'.3）控件随后续任务落位
}

// NewCorpusPage 创建语料页（构造即发起扫描——上游挂载语义）。
func NewCorpusPage(env CorpusEnv, hooks CorpusHooks) *CorpusPage {
	p := &CorpusPage{
		hooks:     hooks,
		env:       env,
		catList:   layout.List{Axis: layout.Vertical},
		list:      layout.List{Axis: layout.Vertical},
		pgnList:   layout.List{Axis: layout.Vertical},
		rowClicks: map[int]*widget.Clickable{},
		pgnClicks: map[int]*widget.Clickable{},
	}
	p.searchEditor.SingleLine = true
	p.pgnSearchEditor.SingleLine = true
	if os.Getenv("CC_GIO_SYNTH_SCROLL") == "1" {
		p.perf = newFramePerf()
	}
	p.downloader = NewCorpusDownloader(env)
	io := env.IO
	if io == nil {
		io = NewCorpusIO(env.Root)
	}
	p.store = state.NewCorpusBrowser(io, goCorpusDriver{}, func(requestID string, ev state.CorpusEvent) {
		env.Emit(requestID, ev, nil)
	})
	p.store.Load(p.newRequestID("corpus-scan"))
	return p
}

func (p *CorpusPage) newRequestID(prefix string) string {
	if p.env.NewRequestID != nil {
		return p.env.NewRequestID(prefix)
	}
	return prefix + "-test"
}

// emitBus 页面事件提交（空 id 直通）。
func (p *CorpusPage) emitBus(requestID string, payload any) {
	if p.env.Emit != nil {
		p.env.Emit(requestID, payload, nil)
	}
}

// cancelBus 取消在途请求（迟到回执按 id 丢弃，铁律 #G5）。
func (p *CorpusPage) cancelBus(requestID string) {
	if requestID != "" && p.env.Cancel != nil {
		p.env.Cancel(requestID)
	}
}

// Dispose 页面卸载（07 §2 挂接点）：取消在途扫描/解析与下载（防错 #10——
// 半成品+ETag sidecar 由复制物保留，重试续传）。
func (p *CorpusPage) Dispose() {
	p.cancelBus(p.store.InFlightID())
	if p.downloading {
		p.cancelDownload()
	}
}

// cancelDownload 取消下载（页面取消标志 → 复制物 IsCancelled 探针；总线 Cancel
// 丢弃迟到进度/回执）。
func (p *CorpusPage) cancelDownload() {
	p.cancelFlag.Store(1)
	p.cancelBus(p.downloadID)
	p.downloading = false
}

// OnAppEvent 事件总线分发（主 goroutine；app.Run 每帧 drain）。
func (p *CorpusPage) OnAppEvent(payload any) {
	switch ev := payload.(type) {
	case state.CorpusEvent:
		p.store.Apply(ev)
		p.pgnDirty = true // 分类/索引/单局回执都可能改 PGN 视图
	case CorpusDownloadProgress:
		if ev.RequestID == p.downloadID {
			p.receivedBytes = ev.Received
			p.totalBytes = ev.Total
		}
	case CorpusDownloadDone:
		if ev.RequestID != p.downloadID {
			return
		}
		p.downloading = false
		p.cancelFlag.Store(0)
		if ev.Err != nil {
			p.message = fmt.Sprintf("下载失败：%s（可重试）", ev.Err.Error())
			return
		}
		p.message = "下载完成，正在重新扫描语料目录…"
		p.store.Load(p.newRequestID("corpus-scan"))
	case PasteTextDone:
		p.applyPaste(ev.Target, ev.Text, ev.Err)
	}
}

// 搜索框粘贴目标（PasteTextDone.Target 定向回填；KG-004 口径）。
const (
	pasteTargetCorpusSearch = iota
	pasteTargetPgnSearch
)

// requestPaste 发起异步粘贴（I/O 在后台 goroutine，回执经事件总线——铁律 #G3）。
func (p *CorpusPage) requestPaste(target int) {
	pasteFromWindowsAsync(func(text string, err error) {
		p.emitBus("", PasteTextDone{Target: target, Text: text, Err: err})
	})
}

// applyPaste 粘贴回填（主 goroutine）。
func (p *CorpusPage) applyPaste(target int, text string, err error) {
	if err != nil || strings.TrimSpace(text) == "" {
		return
	}
	switch target {
	case pasteTargetCorpusSearch:
		p.searchEditor.SetText(text)
	case pasteTargetPgnSearch:
		p.pgnSearchEditor.SetText(text)
	}
}

// visible 筛选排序后的可见条目（每帧重算：XQF 条目量级 ≤数千，排序开销可忽略；
// 上游 useMemo 语义的立即模式等价——输入不变则输出一致）。
func (p *CorpusPage) visible() []state.VisibleItem {
	return state.VisibleItems(p.store.Entries, p.store.Puzzles, p.store.Query,
		p.store.OnlyEndgame, p.store.DifficultyFilter, p.store.SortMode)
}

// Layout 页面骨架（CorpusBrowserPage.tsx）：header + 引导/主区。
func (p *CorpusPage) Layout(gtx layout.Context) layout.Dimensions {
	if p.perf != nil {
		defer p.perf.frame(gtx, time.Now())
	}
	p.handleEvents(gtx)
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)

	dims := layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutHeader),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			inset := layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(12)}
			if !p.store.CorpusExists {
				return inset.Layout(gtx, p.layoutMissingGuide)
			}
			return inset.Layout(gtx, p.layoutMain)
		}),
	)
	if p.perf != nil {
		p.perf.overlay(gtx, &p.pgnList, len(p.pgnVisible()))
	}
	return dims
}

// handleEvents 输入事件消费（每帧在 Layout 顶部统一处理）。
func (p *CorpusPage) handleEvents(gtx layout.Context) {
	if p.backBtn.Clicked(gtx) && p.hooks.OnBack != nil {
		p.hooks.OnBack()
	}
	// 搜索（Editor+IME，08 §10）：Editor 文本每帧回读，变更即投影
	//（ChangeEvent 在提交后可靠回读，KG/08 §10 已验证口径）。
	if q := strings.TrimSpace(p.searchEditor.Text()); q != p.store.Query {
		p.store.SetQuery(q)
		p.rowClicks = map[int]*widget.Clickable{}
	}
	// PGN 搜索：过滤 + 跳回列表顶部（08 §8 注记①：搜索跳转）
	if q := strings.TrimSpace(p.pgnSearchEditor.Text()); q != p.store.PgnQuery {
		p.store.SetPgnQuery(q)
		p.pgnDirty = true
		p.pgnList.Position.First = 0
	}
	// 粘贴按钮（KG-004：WSLg 中文搜索词经 Windows 剪贴板可靠输入）
	if p.pasteSearchBtn.Clicked(gtx) {
		p.requestPaste(pasteTargetCorpusSearch)
	}
	if p.pastePgnBtn.Clicked(gtx) {
		p.requestPaste(pasteTargetPgnSearch)
	}
	// PGN 行点击
	if p.store.PgnPath != "" && p.store.ViewingPuzzle == nil && !p.store.PgnLoading {
		filtered := p.pgnVisible()
		for i := range filtered {
			if c := p.pgnClicks[i]; c != nil && c.Clicked(gtx) {
				p.cancelBus(p.store.InFlightID())
				p.store.OpenPgnGame(p.newRequestID("corpus-pgngame"), filtered[i])
			}
		}
	}
	if p.onlyEndgame.Update(gtx) {
		p.store.SetOnlyEndgame(p.onlyEndgame.Value)
		p.rowClicks = map[int]*widget.Clickable{}
	}
	for i := range p.diffClicks {
		if p.diffClicks[i].Clicked(gtx) {
			p.store.SetDifficultyFilter(i)
			p.rowClicks = map[int]*widget.Clickable{}
		}
	}
	for i := range p.sortClicks {
		if p.sortClicks[i].Clicked(gtx) {
			p.store.SetSortMode(sortLabels[i].mode)
			p.rowClicks = map[int]*widget.Clickable{}
		}
	}
	for i := range p.categoryClicks {
		if p.categoryClicks[i].Clicked(gtx) {
			p.cancelBus(p.store.InFlightID())
			p.rowClicks = map[int]*widget.Clickable{}
			cat := p.store.Categories[i]
			if cat.Kind == "pgnFile" {
				p.store.OpenPgnCategory(p.newRequestID("corpus-pgnindex"), i)
			} else {
				p.store.SelectCategory(p.newRequestID("corpus-category"), i)
			}
		}
	}
	// 行点击（XQF 条目）/ 详情返回
	if p.store.ViewingPuzzle == nil {
		for i, it := range p.visible() {
			if c := p.rowClicks[i]; c != nil && c.Clicked(gtx) {
				p.store.OpenXqfPuzzle(it.Index)
			}
		}
	} else if p.closeDetailBtn.Clicked(gtx) {
		p.store.ClosePuzzle()
	}
	// 下载按钮
	if p.downloadBtn.Clicked(gtx) && !p.downloading {
		p.startDownload()
	}
	if p.cancelDownloadBtn.Clicked(gtx) && p.downloading {
		p.cancelDownload()
	}
}

func (p *CorpusPage) startDownload() {
	p.message = ""
	p.downloading = true
	p.cancelFlag.Store(0)
	p.receivedBytes = 0
	p.totalBytes = 0
	p.downloadID = p.newRequestID("corpus-download")
	// targetDir 空 = 复制物按 用户设置>legacy>默认 解析（上游 CorpusDownload 语义）
	p.downloader.StartAsync(p.downloadID, CorpusDownloadURL, "", func() bool {
		return p.cancelFlag.Load() == 1
	})
}

// layoutHeader 标题 + 返回。
func (p *CorpusPage) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(12), Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				title := material.H5(PageTheme, "棋谱库")
				title.Color = ThemeOnSurface
				return title.Layout(gtx)
			}),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return p.simpleButton(&p.backBtn, "返回主页", false)(gtx)
			}),
		)
	})
}

// layoutMissingGuide 下载引导（MissingGuide 组件）：缺失文案 + 期望路径 +
// 下载按钮（进度/取消）+ 提示。
func (p *CorpusPage) layoutMissingGuide(gtx layout.Context) layout.Dimensions {
	return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body1(PageTheme, "未找到本地棋谱语料")
				l.TextSize = unit.Sp(18)
				l.Color = ThemeOnSurface
				return l.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, "期望路径："+pathOr(p.store.CorpusPath, "（待解析）"))
				l.Color = ThemeSeedDark
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(12)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if p.downloading {
					return p.simpleButton(&p.cancelDownloadBtn, "取消下载", false)(gtx)
				}
				return p.simpleButton(&p.downloadBtn, "下载语料包（约 45MB）", true)(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !p.downloading {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					label := "下载中…"
					if p.totalBytes > 0 {
						label = fmt.Sprintf("下载中 %.1fMB / %.1fMB",
							float64(p.receivedBytes)/1048576, float64(p.totalBytes)/1048576)
					}
					l := material.Body2(PageTheme, label)
					l.Color = ThemeSeedDark
					return l.Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !p.downloading || p.totalBytes <= 0 {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, p.layoutProgressBar)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if p.message == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(PageTheme, p.message)
					l.Color = ThemeError
					return l.Layout(gtx)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, "提示：也可手动将语料目录放置到期望路径后重新进入本页（目录选择功能待文件对话框方案定案）")
				l.Color = ThemeSeedDark
				l.TextSize = unit.Sp(12)
				return layout.Inset{Top: unit.Dp(16)}.Layout(gtx, l.Layout)
			}),
		)
	})
}

// layoutProgressBar 下载进度条（320dp 宽，received/total 比例填充）。
func (p *CorpusPage) layoutProgressBar(gtx layout.Context) layout.Dimensions {
	w := gtx.Dp(unit.Dp(320))
	h := gtx.Dp(unit.Dp(8))
	gtx.Constraints = layout.Exact(image.Point{X: w, Y: h})
	defer clip.Rect{Max: image.Point{X: w, Y: h}}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurfaceDim)
	frac := 0.0
	if p.totalBytes > 0 {
		frac = float64(p.receivedBytes) / float64(p.totalBytes)
		if frac > 1 {
			frac = 1
		}
	}
	filled := int(float64(w) * frac)
	if filled > 0 {
		defer clip.Rect{Max: image.Point{X: filled, Y: h}}.Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, ThemeSeed)
	}
	return layout.Dimensions{Size: image.Point{X: w, Y: h}}
}

// layoutMain 主区：左分类列（200dp）+ 右面板。
func (p *CorpusPage) layoutMain(gtx layout.Context) layout.Dimensions {
	for len(p.categoryClicks) < len(p.store.Categories) {
		p.categoryClicks = append(p.categoryClicks, widget.Clickable{})
	}
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				gtx.Constraints.Min.X = gtx.Dp(unit.Dp(180))
				gtx.Constraints.Max.X = gtx.Dp(unit.Dp(180))
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						l := material.Body2(PageTheme, "分类")
						l.Color = ThemeSeedDark
						return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, l.Layout)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return p.catList.Layout(gtx, len(p.store.Categories), func(gtx layout.Context, i int) layout.Dimensions {
							selected := i == p.store.SelectedCategory
							return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
								return p.categoryClicks[i].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
										bg := ThemeSurfaceDim
										fg := ThemeSeedDark
										if selected {
											bg = ThemeSeed
											fg = ThemeSurface
										}
										defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
										paint.Fill(gtx.Ops, bg)
										l := material.Body2(PageTheme, p.store.Categories[i].Name)
										l.Color = fg
										return l.Layout(gtx)
									})
								})
							})
						})
					}),
				)
			})
		}),
		layout.Flexed(1, p.layoutRightPanel),
	)
}

// categoryList 分类列表（复用 list 字段——与 XQF 列表互斥呈现）。

// layoutRightPanel 右侧面板路由（CorpusBrowserPage 主体 switch）。
func (p *CorpusPage) layoutRightPanel(gtx layout.Context) layout.Dimensions {
	switch {
	case p.store.ViewingPuzzle != nil:
		return p.layoutDetailStub(gtx)
	case p.store.ViewingLoading:
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Body1(PageTheme, "单局解析中…")
			l.Color = ThemeSeedDark
			return l.Layout(gtx)
		})
	case p.store.ViewingError != "":
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, p.store.ViewingError)
				l.Color = ThemeError
				return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return p.simpleButton(&p.cancelDownloadBtn, "返回列表", false)(gtx)
			}),
		)
	case p.store.PgnPath != "":
		return p.layoutPgnPanel(gtx)
	default:
		return p.layoutXqfPanel(gtx)
	}
}

// layoutDetailStub 详情占位（T5'.3 重放器落地）。
func (p *CorpusPage) layoutDetailStub(gtx layout.Context) layout.Dimensions {
	v := p.store.ViewingPuzzle
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return p.simpleButton(&p.closeDetailBtn, "返回列表", false)(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body1(PageTheme, derefStr(v.Title, "未命名"))
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, l.Layout)
		}),
	)
}

// pgnVisible PGN 过滤索引（dirty 置位后重算一次；140k 级逐帧过滤不可接受）。
func (p *CorpusPage) pgnVisible() []storage.PgnIndexEntry {
	if p.pgnDirty {
		p.pgnFiltered = state.PgnFilter(p.store.PgnIndex, p.store.PgnQuery)
		p.pgnClicks = map[int]*widget.Clickable{}
		p.pgnDirty = false
	}
	return p.pgnFiltered
}

// layoutPgnPanel PGN 大文件分类面板（T5'.2，08 §8 落地注记①）：搜索 +
// 虚拟化连续长列表（KG-002 口径：自然手势滚动+搜索跳转；上游 DOM 分页的
// pgnPageSlice 语义保留于状态层，Gio UI 不消费分页控件）。
func (p *CorpusPage) layoutPgnPanel(gtx layout.Context) layout.Dimensions {
	if p.store.PgnLoading {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Body1(PageTheme, "索引扫描中…")
			l.Color = ThemeSeedDark
			return l.Layout(gtx)
		})
	}
	filtered := p.pgnVisible()
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layoutEditorBox(gtx, &p.pgnSearchEditor, "按赛事/棋手搜索")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, p.smallPasteButton(&p.pastePgnBtn))
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(PageTheme, fmt.Sprintf("共 %d 局", len(filtered)))
					l.Color = ThemeSeedDark
					return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, l.Layout)
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(filtered) != 0 {
				return layout.Dimensions{}
			}
			l := material.Body2(PageTheme, "无匹配对局")
			l.Color = ThemeSeedDark
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, l.Layout)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return p.pgnList.Layout(gtx, len(filtered), func(gtx layout.Context, i int) layout.Dimensions {
				entry := filtered[i]
				return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return p.pgnClicker(i).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
						paint.Fill(gtx.Ops, ThemeSurfaceDim)
						return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.Body2(PageTheme, parsers.PgnGameIndexTitle(entry))
									l.TextSize = unit.Sp(14)
									l.Color = ThemeOnSurface
									return l.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.Body2(PageTheme, fmt.Sprintf("%s vs %s",
										derefStr(entry.Red, "?"), derefStr(entry.Black, "?")))
									l.TextSize = unit.Sp(12)
									l.Color = ThemeSeedDark
									return l.Layout(gtx)
								}),
							)
						})
					})
				})
			})
		}),
	)
}

// pgnClicker PGN 行点击器（懒分配）。
func (p *CorpusPage) pgnClicker(i int) *widget.Clickable {
	c, ok := p.pgnClicks[i]
	if !ok {
		c = &widget.Clickable{}
		p.pgnClicks[i] = c
	}
	return c
}

// layoutXqfPanel XQF 分类面板：筛选/排序/进度/列表（XqfPanel）。
func (p *CorpusPage) layoutXqfPanel(gtx layout.Context) layout.Dimensions {
	items := p.visible()
	parsedCount := 0
	for _, v := range p.store.Puzzles {
		if v != nil {
			parsedCount++
		}
	}
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return layoutEditorBox(gtx, &p.searchEditor, "搜索棋谱名称")
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, p.smallPasteButton(&p.pasteSearchBtn))
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						cb := material.CheckBox(PageTheme, &p.onlyEndgame, "仅看残局")
						cb.TextSize = unit.Sp(14)
						return cb.Layout(gtx)
					})
				}),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(6), Bottom: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				opts := make([]chipOpt, 0, len(p.diffClicks)+len(p.sortClicks))
				for i := range p.diffClicks {
					opts = append(opts, chipOpt{click: &p.diffClicks[i], label: difficultyLabels[i], selected: p.store.DifficultyFilter == i})
				}
				for i := range p.sortClicks {
					opts = append(opts, chipOpt{click: &p.sortClicks[i], label: sortLabels[i].label, selected: p.store.SortMode == sortLabels[i].mode})
				}
				return layoutOptionChips(gtx, 64, opts...)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if p.store.Progress < 0 {
				return layout.Dimensions{}
			}
			return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, fmt.Sprintf("解析进度：%.0f%%  已解析 %d/%d",
					p.store.Progress*100, parsedCount, len(p.store.Entries)))
				l.Color = ThemeSeedDark
				return l.Layout(gtx)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if len(items) != 0 || p.store.Progress >= 0 {
				return layout.Dimensions{}
			}
			l := material.Body2(PageTheme, "该分类暂无已解析棋谱")
			l.Color = ThemeSeedDark
			return l.Layout(gtx)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return p.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
				it := items[i]
				return layout.Inset{Bottom: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return p.rowClicker(i).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
						paint.Fill(gtx.Ops, ThemeSurfaceDim)
						return layout.UniformInset(unit.Dp(8)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									l := material.Body2(PageTheme, derefStr(it.Puzzle.Title, it.Entry.DisplayName))
									l.TextSize = unit.Sp(15)
									l.Color = ThemeOnSurface
									return l.Layout(gtx)
								}),
								layout.Rigid(func(gtx layout.Context) layout.Dimensions {
									kind := "全局对局"
									if it.Puzzle.Endgame {
										kind = "残局题"
									}
									l := material.Body2(PageTheme, fmt.Sprintf("%s · %d 着 · 难度 %s · %s",
										it.Entry.Source, it.Puzzle.MoveCount,
										parsers.DifficultyText(it.Puzzle.Difficulty), kind))
									l.TextSize = unit.Sp(12)
									l.Color = ThemeSeedDark
									return l.Layout(gtx)
								}),
							)
						})
					})
				})
			})
		}),
	)
}

// rowClicker 行点击器（懒分配；列表内容变化时由 handleEvents 清空重建）。
func (p *CorpusPage) rowClicker(i int) *widget.Clickable {
	c, ok := p.rowClicks[i]
	if !ok {
		c = &widget.Clickable{}
		p.rowClicks[i] = c
	}
	return c
}

// smallPasteButton 搜索框旁的粘贴按钮（KG-004：中文搜索词经 Windows 剪贴板；
// M4' 配置卡粘贴按钮同款 52×28dp）。
func (p *CorpusPage) smallPasteButton(c *widget.Clickable) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(PageTheme, c, "粘贴")
		btn.Background = ThemeSurfaceDim
		btn.Color = ThemeSeedDark
		btn.TextSize = unit.Sp(13)
		return layout.Inset{Left: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(52))
			gtx.Constraints.Max.X = gtx.Dp(unit.Dp(52))
			gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(28))
			gtx.Constraints.Max.Y = gtx.Dp(unit.Dp(28))
			return btn.Layout(gtx)
		})
	}
}

// simpleButton 通用按钮（页面共用口径，与对局页同款）。
func (p *CorpusPage) simpleButton(c *widget.Clickable, label string, primary bool) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(PageTheme, c, label)
		if !primary {
			btn.Background = ThemeSurfaceDim
			btn.Color = ThemeSeedDark
		}
		return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(96))
			return btn.Layout(gtx)
		})
	}
}

func derefStr(s *string, dflt string) string {
	if s == nil {
		return dflt
	}
	return *s
}

func pathOr(s, dflt string) string {
	if strings.TrimSpace(s) == "" {
		return dflt
	}
	return s
}
