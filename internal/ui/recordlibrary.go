package ui

// 棋谱记录库（T5'.3，D-004 勘误落位；翻译源 = 上游
// frontend/src/features/record/RecordLibraryPage.tsx + RecordDetailPage.tsx，
// 对应 record_library_page.dart / record_detail_page.dart，07 文档 §5 E，TC-LIB/TC-DET）。
// 列表（RecordSummaries）+ 求解状态筛选 + 详情（主变/解法线路重放）+
// 进入对战（RecordLauncherDialog 语义，人机 AI 附执方选择）+
// 导出 PGN/分享文本（复制，clipboard.WriteOp）+ 删除确认。
// "保存为棋谱"写入面在对局页（recordsavedialog.go）；工作室求解入库随 M6'。
// 工厂页：每次导航进入重载列表（上游 useEffect(reload, [])）。

import (
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"
	"time"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// RecordLibraryHooks 记录库页回调。
type RecordLibraryHooks struct {
	OnBack func()
	// OnBattle 进入对战（RecordLauncherDialog 语义）：mode + 起点 FEN + 玩家执方
	//（"red"/"black"，仅 humanVsAi 携带；空 = 默认红——上游 battleRouteFor）。
	OnBattle func(mode BattleMode, fen, playerSide string)
}

// recordLine 线路选择：-1 = 主变，>=0 = 解法序号（record_detail_page.dart line）。
const recordMainLine = -1

// 记录库行操作（上游 actions switch）。
const (
	actBattle = iota
	actExport
	actShare
	actFile
	actDelete
)

// statusFilters 求解状态筛选档位（STATUS_FILTERS；再点同档取消——上游 toggle）。
var statusFilters = [4]state.SolveStatus{state.SolveNone, state.SolveSolved, state.SolveNoSolution, state.SolveTimeout}

// actLabels 行操作菜单项文案（下标=操作常量；M6' 验收反馈收拢为"操作 ▾"
// 下拉菜单——上游 08 §5.1"每条菜单"同锚点，展开按钮带形态作废）。
var actLabels = []string{"进入对战", "导出 PGN", "分享文本", "导出文件", "删除"}

// RecordLibraryPage 记录库页 state struct（主 goroutine 独占；工厂页）。
type RecordLibraryPage struct {
	hooks RecordLibraryHooks
	env   GameEnv

	// 列表态
	records []storage.GameRecordSummary
	filter  *state.SolveStatus // nil = 全部
	loaded  bool

	// 详情态
	detailID      int64
	detailLoading bool
	detail        *state.GameRecordData
	line          int
	pos           int
	moves         []rules.Move
	notations     []string

	// 弹层/动作
	launcher      BattleLauncher
	launchTarget  *state.GameRecordData
	saveFileID    string
	listID        string // 在途列表请求 id（迟到回执按 id 丢弃，#G5）
	getID         string // 在途详情请求 id（连续点开两条记录时区分先后）
	deleteID      string // 在途删除请求 id
	deletingTitle string
	deletingID    int64
	pendingAction func(*state.GameRecordData)
	pendingExport string
	pendingShare  string
	exportViaGio  bool // PowerShell 通道失败 → 回退 gio WriteCmd（回执在主循环处置）
	shareViaGio   bool
	clipExportSeq int
	clipShareSeq  int
	toastText     string
	toastSeq      int

	// 控件
	backBtn        widget.Clickable
	filterClicks   [4]widget.Clickable
	list           layout.List
	moveList       layout.List
	titleClicks    map[int64]*widget.Clickable
	menuBtns       map[int64]*widget.Clickable // 每卡"操作 ▾"按钮
	menuOpenID     int64                       // 打开的操作菜单所属记录（0=无）
	menuOpenSum    storage.GameRecordSummary   // 菜单所属记录摘要（动作分发用）
	menuItemBtns   [5]widget.Clickable         // 菜单项点击器（浮层同时只开一个，全局一组）
	menuMaskClick  widget.Clickable            // 菜单遮罩（点外关闭）
	detailBackBtn  widget.Clickable
	battleBtn      widget.Clickable
	exportBtn      widget.Clickable
	shareBtn       widget.Clickable
	deleteBtn      widget.Clickable
	lineMainBtn    widget.Clickable
	lineSolBtns    []widget.Clickable
	toStartBtn     widget.Clickable
	prevBtn        widget.Clickable
	nextBtn        widget.Clickable
	endBtn         widget.Clickable
	plyClicks      map[int]*widget.Clickable
	deleteConfirmB widget.Clickable
	deleteCancelB  widget.Clickable
}

// NewRecordLibraryPage 创建记录库页（构造即拉列表）。
func NewRecordLibraryPage(env GameEnv, hooks RecordLibraryHooks) *RecordLibraryPage {
	p := &RecordLibraryPage{
		hooks:       hooks,
		env:         env,
		list:        layout.List{Axis: layout.Vertical},
		moveList:    layout.List{Axis: layout.Vertical},
		titleClicks: map[int64]*widget.Clickable{},
		menuBtns:    map[int64]*widget.Clickable{},
		plyClicks:   map[int]*widget.Clickable{},
	}
	p.reload()
	return p
}

func (p *RecordLibraryPage) newID(prefix string) string {
	if p.env.NewRequestID != nil {
		return p.env.NewRequestID(prefix)
	}
	return prefix + "-test"
}

func (p *RecordLibraryPage) reload() {
	if p.env.Records != nil {
		p.listID = p.newID("records-list")
		p.env.Records.RecordsListAsync(p.listID)
	}
}

// showToast 非阻塞提示（2.2s 自动消失；上游 2200ms 同款）。
func (p *RecordLibraryPage) showToast(message string) {
	p.toastSeq++
	p.toastText = message
	seq := p.toastSeq
	if p.env.Emit != nil {
		time.AfterFunc(2200*time.Millisecond, func() {
			p.env.Emit("", ToastHide{Seq: seq}, nil)
		})
	}
}

// Dispose 离页（无在途长任务面；在途记录 I/O 回执由 bus 分发到新页后被忽略）。
func (p *RecordLibraryPage) Dispose() {}

// OnAppEvent 事件总线分发（主 goroutine）。
func (p *RecordLibraryPage) OnAppEvent(payload any) {
	switch ev := payload.(type) {
	case RecordsListDone:
		if ev.RequestID != p.listID {
			return // 迟到列表回执（连续 reload 时区分先后，#G5）
		}
		p.records = ev.Records
		p.loaded = true
	case RecordGetDone:
		if ev.RequestID != p.getID {
			return // 迟到详情回执（连续点开两条记录时旧响应不得落新请求，#G5）
		}
		p.detailLoading = false
		if ev.Err != nil || ev.Record == nil {
			p.pendingAction = nil
			p.showToast("棋谱不存在或读取失败")
			return
		}
		data := state.RecordDataFromStorage(ev.Record)
		if fn := p.pendingAction; fn != nil {
			p.pendingAction = nil
			fn(&data)
			return
		}
		p.openDetail(&data)
	case RecordDeleteDone:
		if ev.RequestID != p.deleteID {
			return
		}
		if ev.Err != nil {
			p.showToast("删除失败：本地存储不可用")
		} else {
			p.showToast("棋谱已删除")
		}
		p.deletingTitle = ""
		p.deletingID = 0
		p.reload()
	case ClipWriteDone:
		switch {
		case ev.Seq == p.clipExportSeq && p.pendingExport != "":
			if ev.Err != nil {
				p.exportViaGio = true // 回退 gio WriteCmd（Layout 处置）
			} else {
				p.pendingExport = ""
				p.showToast("PGN 已复制")
			}
		case ev.Seq == p.clipShareSeq && p.pendingShare != "":
			if ev.Err != nil {
				p.shareViaGio = true
			} else {
				p.pendingShare = ""
				p.showToast("棋谱文本已复制")
			}
		}
	case FileSaveDone:
		if ev.RequestID != p.saveFileID {
			return
		}
		p.saveFileID = ""
		switch {
		case ev.Err != nil:
			p.showToast("导出失败：" + ev.Err.Error())
		case ev.Path == "":
			p.showToast("已取消导出")
		default:
			p.showToast("PGN 文件已导出")
		}
	case ToastHide:
		if ev.Seq == p.toastSeq {
			p.toastText = ""
		}
	}
}

// saveFileAsync 保存文件对话框（D-006；上游"file"操作：取消=已取消导出、
// 成功=PGN 文件已导出）。
func (p *RecordLibraryPage) saveFileAsync(defaultName, content string) {
	if p.env.Emit == nil {
		return
	}
	id := p.newID("dialog-savefile")
	p.saveFileID = id
	SaveFileAsync(defaultName, content, func(savedPath string, err error) {
		p.env.Emit(id, FileSaveDone{RequestID: id, Path: savedPath, Err: err}, nil) // id 值捕获（#G3）
	})
}

// copyAsync 异步写 Windows 剪贴板（KG-004 反方向；回执经事件总线回主循环
// 处置——后台回调禁止直写页面字段，铁律 #G3）。
func (p *RecordLibraryPage) copyAsync(text *string, seq *int) {
	if p.env.Emit == nil || *text == "" {
		return
	}
	*seq++
	n := *seq
	CopyToWindowsClipboardAsync(*text, func(err error) {
		p.env.Emit("", ClipWriteDone{Seq: n, Err: err}, nil)
	})
}

// openRecordByID 拉全量记录后分流（上游 fullRecord+actions 的异步形态）：
// action 非 nil 时回调记录，否则打开详情。
func (p *RecordLibraryPage) openRecordByID(id int64, action func(*state.GameRecordData)) {
	p.pendingAction = action
	p.detailID = id
	p.detailLoading = true
	if p.env.Records == nil {
		// 降级环境（无记录库代理）：立即失败呈现，不得停在"加载中…"
		p.detailLoading = false
		p.pendingAction = nil
		p.showToast("本地存储不可用")
		return
	}
	p.getID = p.newID("records-get")
	p.env.Records.RecordsGetAsync(p.getID, id)
}

// openDetail 打开详情（record_detail_page.dart 载入语义：残局类默认第一条解法、
// 对局类定位保存时局面）。
func (p *RecordLibraryPage) openDetail(data *state.GameRecordData) {
	p.detail = data
	p.detailLoading = false
	p.pos = 0
	p.line = recordMainLine
	if len(data.Moves) > 0 {
		p.pos = len(data.Moves) // 打开即所见保存的当前棋局；步进可往回复盘
	} else if len(data.Solutions) > 0 {
		p.line = 0 // 残局类：默认进第一条解法，便于直接演示
	}
	p.applyLine()
}

// applyLine 线路切换（主变直接用记录走法；解法补棋子——lineMoves 语义）并重算记谱。
func (p *RecordLibraryPage) applyLine() {
	if p.detail == nil {
		return
	}
	if p.line == recordMainLine {
		p.moves = append([]rules.Move(nil), p.detail.Moves...)
	} else if p.line < len(p.detail.Solutions) {
		raw := make([]rules.Move, 0, len(p.detail.Solutions[p.line]))
		for _, code := range p.detail.Solutions[p.line] {
			if ft := parsers.ParseIccs(code); ft != nil {
				raw = append(raw, rules.Move{From: ft.From, To: ft.To})
			}
		}
		p.moves = state.FillMovePieces(p.detail.InitialFen, raw)
	}
	p.notations = state.ChineseNotations(p.detail.InitialFen, p.moves)
	p.plyClicks = map[int]*widget.Clickable{}
}

// canLaunch 记录是否提供进入对战入口（recordBattle.ts canLaunchBattle：
// 残局类恒有；对局类未分胜负时有）。
func (p *RecordLibraryPage) canLaunch(d *state.GameRecordData) bool {
	if d.Mode == "endgame" {
		return true
	}
	return d.Result == nil
}

// battleFen 起点 FEN（battleStartFen：残局=initialFen；对局=终局局面；
// 已分胜负对局无入口——canLaunch 门控）。
func (p *RecordLibraryPage) battleFen(d *state.GameRecordData) string {
	if d.Mode == "endgame" {
		return d.InitialFen
	}
	return state.FinalFenOf(d.InitialFen, d.Moves)
}

// filtered 筛选后的列表（同一档再点取消）。
func (p *RecordLibraryPage) filtered() []storage.GameRecordSummary {
	if p.filter == nil {
		return p.records
	}
	out := make([]storage.GameRecordSummary, 0, len(p.records))
	for _, r := range p.records {
		st := r.SolveStatus
		if st == nil || *st == "" {
			v := string(state.SolveNone)
			st = &v
		}
		if *st == string(*p.filter) {
			out = append(out, r)
		}
	}
	return out
}

// handleEvents 输入消费（主 goroutine）。
func (p *RecordLibraryPage) handleEvents(gtx layout.Context) {
	// 操作菜单打开：遮罩拦截底层交互（筛选/列表仅消费本帧边沿防穿透误触发——
	// KG-008 收口口径；菜单自身输入在下方浮层分支处理）
	if p.menuOpenID != 0 {
		p.backBtn.Clicked(gtx)
		for i := range p.filterClicks {
			p.filterClicks[i].Clicked(gtx)
		}
		for _, c := range p.titleClicks {
			c.Clicked(gtx)
		}
		for _, c := range p.menuBtns {
			c.Clicked(gtx)
		}
		p.consumeDetailEdges(gtx)
	} else {
		if p.backBtn.Clicked(gtx) && p.hooks.OnBack != nil {
			p.hooks.OnBack()
		}
		for i := range p.filterClicks {
			if p.filterClicks[i].Clicked(gtx) {
				st := statusFilters[i]
				if p.filter != nil && *p.filter == st {
					p.filter = nil
				} else {
					p.filter = &st
				}
			}
		}
	}
	// 列表：标题点击 → 详情；"操作 ▾"按钮 → 打开菜单（菜单打开时上方已消费边沿，
	// 此分支不会执行——防误开/误进详情）
	items := p.filtered()
	for i := range items {
		id := items[i].ID
		if c := p.titleClicks[id]; c != nil && c.Clicked(gtx) {
			p.openRecordByID(id, nil)
		}
		if c := p.menuBtns[id]; c != nil && c.Clicked(gtx) {
			if p.menuOpenID == id {
				p.closeActionMenu() // 重复点同卡=收起
			} else {
				p.menuOpenID = id
				p.menuOpenSum = items[i]
			}
		}
	}
	// 操作菜单浮层（打开时：遮罩点外关闭；菜单项分发动作）
	if p.menuOpenID != 0 {
		if p.menuMaskClick.Clicked(gtx) {
			p.closeActionMenu()
		} else {
			for a := range p.menuItemBtns {
				if p.menuItemBtns[a].Clicked(gtx) {
					sum := p.menuOpenSum
					p.closeActionMenu()
					p.runAction(a, sum)
					break
				}
			}
		}
	}
	// 详情控件
	if p.detail != nil {
		switch {
		case p.detailBackBtn.Clicked(gtx):
			p.detail = nil
			p.detailID = 0
		case p.battleBtn.Clicked(gtx):
			p.launchTarget = p.detail
			p.launcher.Open(p.detail)
		case p.exportBtn.Clicked(gtx):
			p.pendingExport = state.WritePgn(*p.detail, time.Now())
			p.copyAsync(&p.pendingExport, &p.clipExportSeq)
		case p.shareBtn.Clicked(gtx):
			p.pendingShare = state.WriteShareText(*p.detail)
			p.copyAsync(&p.pendingShare, &p.clipShareSeq)
		case p.deleteBtn.Clicked(gtx):
			p.deletingTitle = p.detail.Title
			p.deletingID = p.detail.ID
		}
		// 线路切换
		if p.lineMainBtn.Clicked(gtx) {
			p.line = recordMainLine
			p.applyLine()
			p.pos = len(p.detail.Moves) // 主变定位到保存时的局面
		}
		for i := range p.lineSolBtns {
			if p.lineSolBtns[i].Clicked(gtx) {
				p.line = i
				p.applyLine()
				p.pos = 0 // 解法线路从开局演示
			}
		}
		// 步进
		switch {
		case p.toStartBtn.Clicked(gtx):
			p.pos = 0
		case p.prevBtn.Clicked(gtx):
			p.pos--
		case p.nextBtn.Clicked(gtx):
			p.pos++
		case p.endBtn.Clicked(gtx):
			p.pos = len(p.moves)
		}
		if p.pos < 0 {
			p.pos = 0
		}
		if p.pos > len(p.moves) {
			p.pos = len(p.moves)
		}
		// 记谱芯片跳转
		for i := 0; i < len(p.moves); i++ {
			if c := p.plyClicks[i]; c != nil && c.Clicked(gtx) {
				p.pos = i + 1
			}
		}
	}
	// 删除确认
	if p.deletingTitle != "" {
		switch {
		case p.deleteConfirmB.Clicked(gtx):
			if p.env.Records != nil {
				p.deleteID = p.newID("records-delete")
				p.env.Records.RecordsDeleteAsync(p.deleteID, p.deletingID)
			}
			p.deletingTitle = ""
			p.deletingID = 0
		case p.deleteCancelB.Clicked(gtx):
			p.deletingTitle = ""
			p.deletingID = 0
		}
	}
}

// launchBattle 进入对战回调（BattleLauncher onLaunch；target=启动时上下文）。
func (p *RecordLibraryPage) launchBattle(mode BattleMode, playerSide string, target any) {
	d, _ := target.(*state.GameRecordData)
	if p.hooks.OnBattle != nil && d != nil {
		p.hooks.OnBattle(mode, p.battleFen(d), playerSide)
	}
}

// consumeDetailEdges 详情视图控件边沿消费（菜单打开时的防穿透面）。
func (p *RecordLibraryPage) consumeDetailEdges(gtx layout.Context) {
	if p.detail == nil {
		return
	}
	p.detailBackBtn.Clicked(gtx)
	p.battleBtn.Clicked(gtx)
	p.exportBtn.Clicked(gtx)
	p.shareBtn.Clicked(gtx)
	p.deleteBtn.Clicked(gtx)
	p.lineMainBtn.Clicked(gtx)
	for i := range p.lineSolBtns {
		p.lineSolBtns[i].Clicked(gtx)
	}
	p.toStartBtn.Clicked(gtx)
	p.prevBtn.Clicked(gtx)
	p.nextBtn.Clicked(gtx)
	p.endBtn.Clicked(gtx)
	for _, c := range p.plyClicks {
		c.Clicked(gtx)
	}
}

// closeActionMenu 收起操作菜单。
func (p *RecordLibraryPage) closeActionMenu() {
	p.menuOpenID = 0
	p.menuOpenSum = storage.GameRecordSummary{}
}

// runAction 行操作（上游 actions switch：全量记录拉取后执行）。
func (p *RecordLibraryPage) runAction(a int, summary storage.GameRecordSummary) {
	switch a {
	case actDelete:
		p.deletingTitle = summary.Title
		p.deletingID = summary.ID
	case actBattle, actExport, actShare, actFile:
		p.openRecordByID(summary.ID, func(d *state.GameRecordData) {
			switch a {
			case actBattle:
				if !p.canLaunch(d) {
					p.showToast("已分胜负的对局无对战入口")
					return
				}
				p.launchTarget = d
				p.launcher.Open(d)
			case actExport:
				p.pendingExport = state.WritePgn(*d, time.Now())
				p.copyAsync(&p.pendingExport, &p.clipExportSeq)
			case actFile:
				p.saveFileAsync(fmt.Sprintf("chess-record-%d.pgn", d.ID), state.WritePgn(*d, time.Now()))
			case actShare:
				p.pendingShare = state.WriteShareText(*d)
				p.copyAsync(&p.pendingShare, &p.clipShareSeq)
			}
		})
	}
}

// Layout 页面骨架：header + 列表 / 详情 + 弹层/toast（Stack 顶层）。
func (p *RecordLibraryPage) Layout(gtx layout.Context) layout.Dimensions {
	p.handleEvents(gtx)
	// 剪贴板：PowerShell 通道失败时回退 gio WriteCmd（非 WSL 面）
	for _, fb := range []*struct {
		text   *string
		viaGio *bool
		label  string
	}{{&p.pendingExport, &p.exportViaGio, "PGN 已复制"}, {&p.pendingShare, &p.shareViaGio, "棋谱文本已复制"}} {
		if *fb.viaGio && *fb.text != "" {
			gtx.Execute(clipboard.WriteCmd{Type: "text/plain", Data: io.NopCloser(strings.NewReader(*fb.text))})
			*fb.viaGio = false
			*fb.text = ""
			p.showToast(fb.label)
		}
	}
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(p.layoutHeader),
				layout.Flexed(1, p.layoutBody),
			)
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { // 操作菜单浮层（右上锚定）
			if p.menuOpenID != 0 {
				p.layoutActionMenu(gtx)
			}
			return layout.Dimensions{}
		}),
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { // 进入对战启动器（共用组件）
			p.launcher.Layout(gtx, p.launchBattle)
			return layout.Dimensions{}
		}),
		layout.Stacked(p.layoutDeleteConfirm), // 删除确认
		layout.Stacked(func(gtx layout.Context) layout.Dimensions { // toast（非模态浮层）
			if p.toastText != "" {
				DrawToast(gtx, p.toastText)
			}
			return layout.Dimensions{}
		}),
	)
}

// layoutHeader 头部（列表：标题；详情：标题+操作按钮——上游 cc-game-header）。
func (p *RecordLibraryPage) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		row := layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}
		back := &p.backBtn // 列表：返回主页
		if p.detail != nil {
			back = &p.detailBackBtn // 详情：回列表（上游 navigate(-1) 语义）
		}
		children := []layout.FlexChild{
			layout.Rigid(p.simpleButton(back, "返回", false)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				title := "棋谱库"
				if p.detail != nil {
					title = p.detail.Title
				}
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					t := material.H6(PageTheme, title)
					t.Color = ThemeOnSurface
					return t.Layout(gtx)
				})
			}),
		}
		if p.detail != nil {
			children = append(children,
				layout.Rigid(p.simpleButton(&p.battleBtn, "进入对战", true)),
				layout.Rigid(p.simpleButton(&p.exportBtn, "导出 PGN", false)),
				layout.Rigid(p.simpleButton(&p.shareBtn, "分享文本", false)),
				layout.Rigid(p.simpleButton(&p.deleteBtn, "删除", false)),
			)
		}
		return row.Layout(gtx, children...)
	})
}

// layoutBody 列表 / 详情路由（详情载入中提示）。
func (p *RecordLibraryPage) layoutBody(gtx layout.Context) layout.Dimensions {
	if p.detailLoading && p.detail == nil {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			l := material.Body1(PageTheme, "加载中…")
			l.Color = ThemeSeedDark
			return l.Layout(gtx)
		})
	}
	if p.detail != nil {
		return p.layoutDetail(gtx)
	}
	return p.layoutList(gtx)
}

// layoutList 列表视图（筛选 chips + 记录卡行）。
func (p *RecordLibraryPage) layoutList(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16), Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				opts := make([]chipOpt, 0, 4)
				for i := range p.filterClicks {
					opts = append(opts, chipOpt{click: &p.filterClicks[i], label: state.SolveStatusLabel(statusFilters[i]),
						selected: p.filter != nil && *p.filter == statusFilters[i]})
				}
				return layoutOptionChips(gtx, 72, opts...)
			})
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			if !p.loaded || len(p.filtered()) != 0 {
				return layout.Dimensions{}
			}
			return layout.Inset{Left: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, "暂无棋谱。可在对局中保存，或在残局工作室求解后自动入库。")
				l.Color = ThemeSeedDark
				return l.Layout(gtx)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			items := p.filtered()
			return layout.Inset{Left: unit.Dp(16), Right: unit.Dp(16)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return p.list.Layout(gtx, len(items), func(gtx layout.Context, i int) layout.Dimensions {
					return p.layoutRow(gtx, items[i])
				})
			})
		}),
	)
}

// layoutRow 记录卡行（标题+meta 居左点进详情；右侧"操作 ▾"开菜单——上游 08 §5.1"每条菜单"锚点）。
func (p *RecordLibraryPage) layoutRow(gtx layout.Context, summary storage.GameRecordSummary) layout.Dimensions {
	titleC, ok := p.titleClicks[summary.ID]
	if !ok {
		titleC = &widget.Clickable{}
		p.titleClicks[summary.ID] = titleC
	}
	menuC, ok := p.menuBtns[summary.ID]
	if !ok {
		menuC = &widget.Clickable{}
		p.menuBtns[summary.ID] = menuC
	}
	isEndgame := summary.Mode == "endgame"
	status := ""
	if summary.SolveStatus != nil && *summary.SolveStatus != "" && *summary.SolveStatus != "none" {
		status = " · " + state.SolveStatusLabel(state.SolveStatus(*summary.SolveStatus))
	}
	meta := fmt.Sprintf("%s · %s · %s%s",
		state.ModeLabelOf(summary.Mode), dateText(summary.CreatedAt),
		map[bool]string{true: "残局", false: "对局"}[isEndgame], status)
	return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = gtx.Constraints.Max.X // 行卡铺满容器宽
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, ThemeSurfaceDim)
		return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			// 单行卡：标题+meta 居左（点进详情），"操作 ▾"贴右缘（M6' 验收反馈：
			// 操作收拢为右侧下拉菜单——上游 08 §5.1"每条菜单"同锚点）
			return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
				layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
					return titleC.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Body2(PageTheme, summary.Title)
								l.TextSize = unit.Sp(15)
								l.Color = ThemeOnSurface
								return l.Layout(gtx)
							}),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								l := material.Body2(PageTheme, meta)
								l.TextSize = unit.Sp(12)
								l.Color = ThemeSeedDark
								return l.Layout(gtx)
							}),
						)
					})
				}),
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(PageTheme, menuC, "操作 ▾")
						btn.Background = ThemeSurface
						btn.Color = ThemeSeedDark
						btn.TextSize = unit.Sp(12)
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(76))
						gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(34))
						return btn.Layout(gtx)
					})
				}),
			)
		})
	})
}

// layoutActionMenu 操作菜单浮层（M6' 验收反馈：右上锚定 + 遮罩点外关闭；
// 固定锚定规避虚拟化列表滚动导致的锚点错位）。Stack 顶层调用。
func (p *RecordLibraryPage) layoutActionMenu(gtx layout.Context) layout.Dimensions {
	// 遮罩（拦截并消费点击——点外关闭）
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgba(0x000000, 0.25))
	p.menuMaskClick.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Dimensions{Size: gtx.Constraints.Max}
	})

	// 面板：右上锚定（右/上边距 16dp）
	const menuW = 180
	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	w := gtx.Dp(unit.Dp(menuW))
	if W < w+gtx.Dp(unit.Dp(32)) {
		w = W - gtx.Dp(unit.Dp(32))
	}
	macro := op.Record(gtx.Ops)
	mctx := gtx
	mctx.Constraints = layout.Constraints{Max: image.Point{X: w, Y: H}}
	dims := layout.UniformInset(unit.Dp(6)).Layout(mctx, p.menuItems)
	call := macro.Stop()

	menuH := dims.Size.Y
	x, y := W-gtx.Dp(unit.Dp(16))-w, gtx.Dp(unit.Dp(60)) // 顶部与列表首卡对齐
	tr := op.Offset(image.Pt(x, y)).Push(gtx.Ops)
	defer tr.Pop()
	defer clip.UniformRRect(image.Rectangle{Max: image.Point{X: w, Y: menuH}}, gtx.Dp(unit.Dp(10))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// menuItems 菜单项列（删除项红字；每项整行可点）。
func (p *RecordLibraryPage) menuItems(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			l := material.Body2(PageTheme, "记录操作")
			l.TextSize = unit.Sp(11)
			l.Color = ThemeSeedDark
			return layout.Inset{Left: unit.Dp(10), Top: unit.Dp(6), Bottom: unit.Dp(2)}.Layout(gtx, l.Layout)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			children := make([]layout.FlexChild, 0, len(p.menuItemBtns))
			for a := range p.menuItemBtns {
				a := a
				children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return p.menuItemBtns[a].Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						return layout.UniformInset(unit.Dp(10)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							l := material.Body2(PageTheme, actLabels[a])
							l.TextSize = unit.Sp(14)
							l.Color = ThemeOnSurface
							if a == actDelete {
								l.Color = ThemeError
							}
							return l.Layout(gtx)
						})
					})
				}))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		}),
	)
}

// smallAction 行操作小按钮。
func (p *RecordLibraryPage) smallAction(c *widget.Clickable, label string) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(PageTheme, c, label)
		btn.Background = ThemeSurface
		btn.Color = ThemeSeedDark
		btn.TextSize = unit.Sp(12)
		return layout.Inset{Right: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(unit.Dp(84))
			gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(30))
			return btn.Layout(gtx)
		})
	}
}

// layoutDetail 详情视图（棋盘静态 + 步进条 + 记谱芯片 | 信息卡）。
func (p *RecordLibraryPage) layoutDetail(gtx layout.Context) layout.Dimensions {
	fen, lastMove := p.detailFen()
	return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(func(gtx layout.Context) layout.Dimensions {
					return layout.Inset{Top: unit.Dp(8), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
						size := gtx.Constraints.Max.X
						if h := int(float32(gtx.Constraints.Max.Y) * 0.72); h < size {
							size = h // 高度封顶：记谱芯片保持可见
						}
						gtx.Constraints = layout.Constraints{Min: image.Pt(size, size), Max: image.Pt(size, size)}
						return drawStaticBoard(gtx, fen, lastMove)
					})
				}),
				layout.Rigid(p.layoutStepper),
				layout.Flexed(1, p.layoutPlyChips),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8), Right: unit.Dp(16)}.Layout(gtx, p.layoutInfoCard)
		}),
	)
}

// detailFen 当前线路重放局面（board_view_replay.dart:106-115 同语义：
// 与局面不符的走法止损）。
func (p *RecordLibraryPage) detailFen() (string, *rules.Move) {
	board, err := rules.FromFen(p.detail.InitialFen)
	if err != nil {
		return p.detail.InitialFen, nil
	}
	last := (*rules.Move)(nil)
	for i := 0; i < p.pos && i < len(p.moves); i++ {
		if board.PieceAtP(p.moves[i].From) == nil {
			break
		}
		board.ApplyMove(rules.Move{From: p.moves[i].From, To: p.moves[i].To})
		m := p.moves[i]
		last = &m
	}
	return board.ToFen(), last
}

// layoutStepper 步进条（⇤ ◀ pos ▶ ⇥——记录详情为手动步进，上游同款无自动播放）。
func (p *RecordLibraryPage) layoutStepper(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(replayButton(&p.toStartBtn, "⇤", false)),
			layout.Rigid(replayButton(&p.prevBtn, "◀", false)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, fmt.Sprintf("%d / %d 着", p.pos, len(p.moves)))
				l.Color = ThemeOnSurface
				return layout.Inset{Left: unit.Dp(6), Right: unit.Dp(6)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(replayButton(&p.nextBtn, "▶", false)),
			layout.Rigid(replayButton(&p.endBtn, "⇥", false)),
		)
	})
}

// layoutPlyChips 中文记谱芯片（未走到 55% 透明；点击跳转该着之后）。
func (p *RecordLibraryPage) layoutPlyChips(gtx layout.Context) layout.Dimensions {
	if len(p.moves) == 0 {
		l := material.Body2(PageTheme, "该线路无可演示走法")
		l.Color = ThemeSeedDark
		return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, l.Layout)
	}
	return layout.Inset{Top: unit.Dp(8), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.Y = 0
		return p.moveList.Layout(gtx, len(p.moves), func(gtx layout.Context, i int) layout.Dimensions {
			c, ok := p.plyClicks[i]
			if !ok {
				c = &widget.Clickable{}
				p.plyClicks[i] = c
			}
			l := material.Body2(PageTheme, fmt.Sprintf("%d.%s %s", i/2+1, map[bool]string{true: "..", false: ""}[i%2 == 1], p.notations[i]))
			l.TextSize = unit.Sp(12)
			l.Color = ThemeOnSurface
			if i >= p.pos {
				dim := l.Color
				dim.A = 140
				l.Color = dim
			}
			return layout.Inset{Right: unit.Dp(6), Bottom: unit.Dp(2)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return c.Layout(gtx, l.Layout)
			})
		})
	})
}

// layoutInfoCard 棋谱信息卡（上游 cc-game-info：模式/状态/解法数/结果/起始 FEN/
// 备注/大模型注释/线路切换/求解结论块）。
func (p *RecordLibraryPage) layoutInfoCard(gtx layout.Context) layout.Dimensions {
	d := p.detail
	solutions := d.Solutions
	unique := ""
	if state.HasUniqueSolution(d.SolveStatus, solutions) {
		unique = "，唯一"
	}
	solCount := ""
	if len(solutions) > 0 {
		solCount = fmt.Sprintf("（%d 条解法%s）", len(solutions), unique)
	}
	for len(p.lineSolBtns) < len(solutions) {
		p.lineSolBtns = append(p.lineSolBtns, widget.Clickable{})
	}
	w := gtx.Dp(unit.Dp(360)) // 300dp 下解法 chips 换行裁切（M3'/M4' 同类教训）
	return layout.Inset{}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		gtx.Constraints.Min.X = w
		gtx.Constraints.Max.X = w
		defer clip.UniformRRect(image.Rectangle{Max: gtx.Constraints.Max}, gtx.Dp(unit.Dp(8))).Push(gtx.Ops).Pop()
		paint.Fill(gtx.Ops, ThemeSurfaceDim)
		return layout.UniformInset(unit.Dp(12)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			children := []layout.FlexChild{
				layout.Rigid(p.infoLine("棋谱信息", ThemeSeedDark)),
				layout.Rigid(p.infoLine(fmt.Sprintf("%s · %s%s",
					state.ModeLabelOf(d.Mode), state.SolveStatusLabel(d.SolveStatus), solCount), ThemeOnSurface)),
			}
			if d.Result != nil {
				children = append(children, layout.Rigid(p.infoLine("结果: "+state.ResultLabel(*d.Result), ThemeOnSurface)))
			}
			if !state.IsInitialBoardFen(d.InitialFen) {
				children = append(children, layout.Rigid(p.infoLine("起始 FEN: "+d.InitialFen, ThemeSeedDark)))
			}
			if d.Note != nil && *d.Note != "" {
				children = append(children, layout.Rigid(p.infoLine("备注: "+*d.Note, ThemeOnSurface)))
			}
			if d.LlmNote != nil && *d.LlmNote != "" {
				children = append(children, layout.Rigid(p.infoLine("大模型注释: "+*d.LlmNote, ThemeOnSurface)))
			}
			// 线路切换（select 的 Gio chips 形态；主变定位到保存局面，解法从开局）
			mainLabel := "主变（无着法）"
			if len(d.Moves) > 0 {
				mainLabel = fmt.Sprintf("主变（%d 着）", len(d.Moves))
			}
			children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					opts := []chipOpt{{click: &p.lineMainBtn, label: mainLabel, selected: p.line == recordMainLine}}
					for i := range solutions {
						opts = append(opts, chipOpt{click: &p.lineSolBtns[i], label: fmt.Sprintf("解法 %d（%d 着）", i+1, len(solutions[i])), selected: p.line == i})
					}
					return layoutOptionChips(gtx, 80, opts...)
				})
			}))
			if lines := state.SolveVerdictLines(*d); len(lines) > 0 {
				children = append(children, layout.Rigid(p.infoLine(strings.Join(lines, "\n"), ThemeOnSurface)))
			}
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
		})
	})
}

// infoLine 信息卡行（长 FEN 换行由 text 包承载）。
func (p *RecordLibraryPage) infoLine(text string, c color.NRGBA) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(PageTheme, text)
		l.Color = c
		l.TextSize = unit.Sp(13)
		return layout.Inset{Bottom: unit.Dp(4)}.Layout(gtx, l.Layout)
	}
}

// layoutDeleteConfirm 删除确认弹层（ConfirmDialog：确定删除「title」？不可恢复；
// KG-009 口径居中）。
func (p *RecordLibraryPage) layoutDeleteConfirm(gtx layout.Context) layout.Dimensions {
	if p.deletingTitle == "" {
		return layout.Dimensions{}
	}
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgba(0x000000, 0.4))
	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	panelW := gtx.Dp(unit.Dp(380))
	if W < panelW {
		panelW = W
	}
	macro := op.Record(gtx.Ops)
	mctx := gtx
	mctx.Constraints = layout.Constraints{Max: image.Point{X: panelW, Y: H}}
	dims := layout.UniformInset(unit.Dp(20)).Layout(mctx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				t := material.H6(PageTheme, "删除棋谱")
				t.Color = ThemeOnSurface
				return t.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				l := material.Body2(PageTheme, fmt.Sprintf("确定删除「%s」？不可恢复。", p.deletingTitle))
				l.Color = ThemeOnSurface
				return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(16)}.Layout(gtx, l.Layout)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.End}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions { return layout.Dimensions{} }),
					layout.Rigid(p.simpleButton(&p.deleteCancelB, "取消", false)),
					layout.Rigid(p.simpleButton(&p.deleteConfirmB, "删除", true)),
				)
			}),
		)
	})
	call := macro.Stop()
	panelH := dims.Size.Y
	if panelH > H {
		panelH = H
	}
	tr := op.Offset(image.Pt((W-panelW)/2, (H-panelH)/2)).Push(gtx.Ops)
	defer tr.Pop()
	defer clip.UniformRRect(image.Rectangle{Max: image.Point{X: panelW, Y: panelH}}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// dateText 记录日期（yyyy-MM-dd——上游 dateText）。
func dateText(createdAt int64) string {
	d := time.UnixMilli(createdAt)
	return fmt.Sprintf("%d-%02d-%02d", d.Year(), int(d.Month()), d.Day())
}

// simpleButton 通用按钮（页面共用口径，与对局页同款）。
func (p *RecordLibraryPage) simpleButton(c *widget.Clickable, label string, primary bool) func(gtx layout.Context) layout.Dimensions {
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
