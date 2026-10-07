package ui

// 残局工作室页（T6'.2，design_docs/08 §7；翻译锚点 = 上游
// frontend/src/features/studio/EndgameStudioPage.tsx）。
//
// 三 Tab（08 §7 Gio 规格）：摆盘校验 / 求解 / 识图（T6'.3）。上游的 FEN 导入
// 独立 Tab 并入摆盘校验 Tab（Gio 版三 Tab 收口，偏离注记见 PROGRESS）。
//
// 交互规格：
//   - 摆盘：点击格位放置所选棋子（放置即时校验 state.PlacementIssue/数量上限，
//     toast 报因）；未选棋子点击已有棋子=取走；橡皮模式清除。
//   - 求解：Solver Runner 直调（T6'.1 客户端）；进度=非阻塞悬浮条（08 §5：
//     不挡棋盘不挡输入、无关闭入口——TC-SOL-007 语义，重复发起由 startSolve
//     守卫拦截，结束自动消失）；结果面板置顶关闭（08 §5：标题行常驻头部 ×，
//     底部关闭按钮保留兜底）；三种结论全部自动入库为棋谱（solve_status）；
//     进入对战联动（当前局面 FEN 起点，玩家执求解方）。
//   - 整体校验五条（state.ValidateStudioPosition）不通过时 toast + 面板列问题。

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"strings"
	"time"

	"gioui.org/gesture"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/solver"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// solveTickInterval 求解进度节拍（上游 setInterval 200ms 同口径，00 §4 solve:tick）。
const solveTickInterval = 200 * time.Millisecond

// solveTimeOptions 限时选项（EndgameStudioPage SOLVE_TIME_OPTIONS）。
var solveTimeOptions = []struct {
	label string
	ms    int
}{
	{"10 秒", 10_000},
	{"30 秒", 30_000},
	{"1 分钟", 60_000},
	{"3 分钟", 180_000},
}

// solveDepthOptions 深度选项（SOLVE_DEPTH_OPTIONS；plies=半着数）。
var solveDepthOptions = []struct {
	label string
	plies int
}{
	{"浅（3 着内）", 5},
	{"标准（5 着内）", 9},
	{"深（7 着内，较慢）", 13},
}

// paletteKinds 棋子调色板种类顺序（PALETTE_KINDS）。
var paletteKinds = []rules.Kind{
	rules.King, rules.Advisor, rules.Minister,
	rules.Knight, rules.Rook, rules.Cannon, rules.Pawn,
}

// studioTabs Tab 标签（08 §7：摆盘校验/求解/识图）。
var studioTabs = []string{"摆盘校验", "求解", "识图"}

// StudioHooks 工作室页回调。
type StudioHooks struct {
	OnBack func()
	// OnBattle 进入对战联动（mode + 当前局面 FEN + 玩家执方=求解方）。
	OnBattle func(mode BattleMode, fen, side string)
}

// StudioPage 工作室页 state struct（主 goroutine 独占，铁律 #G3）。
type StudioPage struct {
	env    LlmEnv
	hooks  StudioHooks
	solver *SolverClient

	grid       rules.BoardGrid
	redTurn    bool
	gridVer    int // 摆盘/轮走方变更计数（校验缓存失效）
	problemsAt int // 校验缓存对应的版本（-1 = 未计算）
	problems   []string

	disposed bool

	// Tab（摆盘校验/求解/识图）
	tabIndex  int
	tabClicks [3]widget.Clickable

	// 摆盘校验
	selected     *rules.Piece
	eraser       bool
	paletteRed   [7]widget.Clickable
	paletteBlk   [7]widget.Clickable
	eraserBtn    widget.Clickable
	clearBtn     widget.Clickable
	initialBtn   widget.Clickable
	turnRedBtn   widget.Clickable
	turnBlkBtn   widget.Clickable
	fenEditor    widget.Editor
	loadFenBtn   widget.Clickable
	board        tapBoard
	backBtn      widget.Clickable
	assistantBtn widget.Clickable

	// 求解
	timeChips    [4]widget.Clickable
	depthChips   [3]widget.Clickable
	timeIdx      int
	depthIdx     int
	useLlmChip   widget.Clickable // "大模型辅助"开关（chips 呈现——与限时/深度一致）
	saveBtn      widget.Clickable
	solveBtn     widget.Clickable
	solving      bool
	solveGen     int
	solveStop    chan struct{}
	solveRequest string
	solveFen     string
	solveStart   time.Time
	solveRedTurn bool // 发起时轮走方快照（求解中可改摆盘，结果面板按快照呈现）
	solveTimeMs  int  // 发起时参数快照（限时/深度/裁判口径一致）
	solvePlies   int
	useLlmOn     bool
	saveID       string      // 入库在途标记（RecordSaveDone 新鲜度；空=无在途）
	saveKind     string      // "unsolved" | "solve"
	pendingSheet *solveSheet // 入库回执后呈现

	// 结果面板（置顶关闭）
	sheetOpen bool
	sheet     *solveSheet
	closeBtn  widget.Clickable
	close2Btn widget.Clickable
	battleBtn widget.Clickable
	sheetList layout.List
	tabList   layout.List // Tab 内容区纵向滚动（页级字段——每帧新建清零滚动状态）

	launcher BattleLauncher

	toastText string
	toastSeq  int

	// 识图（T6'.3，08 §7 识图 Tab；K33 无中途取消，按钮 disabled 防重入）
	vision          *VisionClient
	pickBtn         widget.Clickable
	loadPathBtn     widget.Clickable
	visionPathEd    widget.Editor
	reading         bool
	visionElapsed   int
	visionRequest   string
	dialogRequest   string // 文件对话框在途（FilePickDone 关联）
	visionMessage   string
	visionLoaded    bool // 识图结果已载入棋盘（显示校正流按钮）
	visionStop      chan struct{}
	visionTickerGen int
	asstConfig      *llm.LlmEndpointConfig
	blkConfig       *llm.LlmEndpointConfig
	redConfig       *llm.LlmEndpointConfig
	asstLoaded      bool
	blkLoaded       bool
	redLoaded       bool
	corrBtn         widget.Clickable // 摆盘校正（切摆盘校验 Tab 点击纠错）
	revisionBtn     widget.Clickable // 重新识别（重新提交）
	adoptBtn        widget.Clickable // 直接采用（切求解 Tab）

	// 求解辅助（T6'.4，05 §6：提议→裁判→注释，失败不影响求解照常进行）
	assistRequest string
	// assistRunner 提议执行面（nil = 复制物 ProposeSolveFirstMove + 流式
	// transport；测试注入 fake——00 §4"测试可注入 fake"口径）。
	assistRunner  func(requestID string, board *rules.Board, cfg llm.LlmEndpointConfig, authSlot string)
	assistCancel  context.CancelFunc
	verifyRequest string
	assistCode    string
	assistIdea    string
	llmNote       string

	// 研究助手配置弹窗（T6'.4，AssistantConfigDialog 翻译）
	assistantOpen   bool
	assistantCard   *LlmConfigCard
	assistantConfig llm.LlmEndpointConfig
	asstSaveBtn     widget.Clickable
	asstCancelBtn   widget.Clickable
	assistTestID    string
	testLlm         *LlmClient
}

// visionSlotsLoaded 三槽位配置是否齐（DR-009 借用解析前置）。
func (p *StudioPage) visionSlotsLoaded() bool { return p.asstLoaded && p.blkLoaded && p.redLoaded }

// solveSheet 结果面板数据（一次求解一张；EndgameStudioPage SolveSheet）。
type solveSheet struct {
	fen         string
	result      solver.WireSolveResult
	recordSaved bool // 入库回执；false 呈现"已保存到棋谱库失败"
	llmNote     string
	redTurn     bool
}

// NewStudioPage 创建工作室页（env=LlmEnv：求解辅助/识图消费凭据槽位）。
func NewStudioPage(env LlmEnv, hooks StudioHooks) *StudioPage {
	empty := make(rules.BoardGrid, 10)
	for r := range empty {
		empty[r] = make([]*rules.Piece, 9)
	}
	p := &StudioPage{
		env:        env,
		hooks:      hooks,
		grid:       empty,
		redTurn:    true,
		problemsAt: -1,
		sheetList:  layout.List{Axis: layout.Vertical},
		tabList:    layout.List{Axis: layout.Vertical},
	}
	p.solver = NewSolverClient(env.GameEnv, nil)
	p.vision = NewVisionClient(env)
	p.board.onTap = p.onCellTap
	p.board.blocked = p.modalOpen
	// 三槽位配置异步加载（DR-009 借用解析前置；掩码回读——#G7）
	if env.Store != nil && env.NewRequestID != nil {
		env.Store.LoadSlotAsync(env.NewRequestID("cfg-asst"), storage.SlotAssistant)
		env.Store.LoadSlotAsync(env.NewRequestID("cfg-blk"), storage.SlotBlack)
		env.Store.LoadSlotAsync(env.NewRequestID("cfg-red"), storage.SlotRed)
	}
	return p
}

// tapBoard 可点击静态棋盘（摆盘/识图校正共用；命中换算 08 §3.1）。
type tapBoard struct {
	click   gesture.Click
	onTap   func(col, row int)
	blocked func() bool
}

func (b *tapBoard) layout(gtx layout.Context, fen string) layout.Dimensions {
	l := ComputeBoardLayout(float32(gtx.Constraints.Max.X), float32(gtx.Constraints.Max.Y))
	defer clip.Rect{Max: image.Pt(int(l.Width), int(l.Height))}.Push(gtx.Ops).Pop()
	b.click.Add(gtx.Ops)
	for {
		ev, ok := b.click.Update(gtx.Source)
		if !ok {
			break
		}
		if ev.Kind != gesture.KindClick {
			continue
		}
		if b.blocked != nil && b.blocked() {
			continue
		}
		if col, row, ok := HitTest(l, float32(ev.Position.X), float32(ev.Position.Y)); ok && b.onTap != nil {
			b.onTap(col, row)
		}
	}
	return drawStaticBoard(gtx, fen, nil)
}

// ---- 摆盘编辑（EndgameStudioPage handleCellTap 1:1）----

func (p *StudioPage) onCellTap(col, row int) {
	defer p.touchGrid()
	if p.eraser {
		p.grid[row][col] = nil
		return
	}
	if p.selected != nil {
		occupant := p.grid[row][col]
		// 位置合法性：放置时即校验（九宫/士象斜线/兵卒底线等）。
		if issue := state.PlacementIssue(*p.selected, col, row); issue != "" {
			p.showToast(issue)
			return
		}
		// 数量合法性：同格同子为替换（数量不变），否则校验上限。
		count := p.countKind(*p.selected)
		if issue := state.CountIssueForPlacement(*p.selected, count, occupant); issue != "" {
			p.showToast(issue)
			return
		}
		pc := *p.selected
		p.grid[row][col] = &pc
		return
	}
	// 未选棋子：点击已有棋子为取走。
	p.grid[row][col] = nil
}

func (p *StudioPage) touchGrid() { p.gridVer++ }

func (p *StudioPage) countKind(piece rules.Piece) int {
	count := 0
	for _, row := range p.grid {
		for _, q := range row {
			if q != nil && q.Kind == piece.Kind && q.Side == piece.Side {
				count++
			}
		}
	}
	return count
}

// currentFen 当前局面 FEN（含轮走方）。
func (p *StudioPage) currentFen() string { return rules.BuildFen(p.grid, p.redTurn) }

// validate 整体校验五条（缓存：grid/redTurn 未变时复用上次结果）。
func (p *StudioPage) validate() []string {
	if p.problemsAt != p.gridVer {
		p.problems = state.ValidateStudioPosition(p.grid, p.redTurn)
		p.problemsAt = p.gridVer
	}
	return p.problems
}

// ---- 求解与入库（EndgameStudioPage startSolve/confirmSolve/persistRecord）----

func (p *StudioPage) startSolve() {
	if p.solving {
		return // 非阻塞进度条下防重复发起（08 §3.6 优化 2）
	}
	if problems := p.validate(); len(problems) > 0 {
		p.showToast(strings.Join(problems, "；"))
		return
	}
	p.solveFen = p.currentFen()
	p.solveRedTurn = p.redTurn
	p.solveTimeMs = solveTimeOptions[p.timeIdx].ms
	p.solvePlies = solveDepthOptions[p.depthIdx].plies
	p.solving = true
	p.sheetOpen = false
	p.sheet = nil
	p.pendingSheet = nil
	p.solveStart = time.Now()
	p.llmNote = ""
	p.assistCode = ""
	p.assistIdea = ""
	p.solveGen++
	p.startSolveTicker()
	p.beginSolve()
}

// beginSolve 求解编排入口：勾选大模型辅助时先跑辅助段（提议→裁判→注释），
// 失败不影响求解照常进行（05 §6 时序）。
func (p *StudioPage) beginSolve() {
	if p.useLlmOn {
		p.runSolveAssist()
		return
	}
	p.submitSolve()
}

// runSolveAssist LLM 求解辅助段（05 §6）。DR-009：助手槽全空时运行时借用
// 对战配置（黑→红，仅本次请求内存、不写入助手槽），toast 提示来源。
// 提议在编排 goroutine 内直调复制物 ProposeSolveFirstMove（#G3：只 emit 回执），
// 裁判经 solver.Runner solve:win:done 回主循环，注释最终写入棋谱 llmNote。
func (p *StudioPage) runSolveAssist() {
	if !p.visionSlotsLoaded() {
		p.submitSolve() // 配置未就绪：静默跳过辅助
		return
	}
	resolved := state.ResolveAssistantConfig(p.asstConfig, p.blkConfig, p.redConfig)
	if resolved.Source != state.AssistantSourceAssistant && resolved.Source != "" {
		p.showToast(fmt.Sprintf("研究助手未配置，已临时借用%s对战配置", state.AssistantSourceLabel(resolved.Source)))
	}
	if resolved.Config == nil {
		p.submitSolve()
		return
	}
	board, err := rules.FromFen(p.solveFen)
	if err != nil {
		p.submitSolve()
		return
	}
	p.assistRequest = p.newID("assist")
	ctx, cancel := context.WithCancel(context.Background())
	p.assistCancel = cancel
	transport := &moveTransport{
		inner:   llm.NewStreamTransport(p.idleSecs),
		outerID: p.assistRequest,
		resolve: p.resolveKey,
		emit:    p.env.Emit,
	}
	cfg := *resolved.Config
	authSlot := resolved.AuthSlot
	requestID := p.assistRequest
	if p.assistRunner != nil {
		p.assistRunner(requestID, board, cfg, authSlot) // 测试注入：同步回执
		cancel()
		return
	}
	emit := p.env.Emit
	if emit == nil {
		cancel()
		p.submitSolve() // 防御：无事件面（降级环境）直接求解
		return
	}
	go func() {
		defer cancel()
		result, err := llm.ProposeSolveFirstMove(ctx, board, cfg, transport, llm.SolveAssistOptions{AuthSlot: authSlot})
		done := AssistProposalDone{RequestID: requestID, Proposal: result.Proposal, Message: result.Message}
		if err != nil {
			done.Err = err
		}
		emit("", done, nil)
	}()
}

// resolveKey 槽位完整 Key 注入面（仅供传输层；#G7 返回值不进 UI/日志）。
func (p *StudioPage) resolveKey(slot string) string {
	if p.env.Store == nil {
		return ""
	}
	return p.env.Store.ResolveAPIKey(slot)
}

// idleSecs 空闲超时秒数（llm_settings_* 驱动；transport 内部 clamp 5~600）。
func (p *StudioPage) idleSecs() int {
	if p.env.Settings == nil {
		return 0
	}
	return llm.ResolveTimeoutSeconds(p.env.Settings.Get(llm.SettingKeyTimeoutSeconds))
}

// onAssistProposalDone 提议回执（主 goroutine）：注释生成口径 = 上游
// runSolveAssist 逐字；提议有效 → 归一化解码后经求解器裁判（solve:win:done）。
func (p *StudioPage) onAssistProposalDone(ev AssistProposalDone) {
	if ev.RequestID != p.assistRequest || p.disposed {
		return // 迟到/非当前请求（#G5）
	}
	p.assistRequest = ""
	p.assistCancel = nil
	switch {
	case ev.Err != nil:
		p.llmNote = fmt.Sprintf("大模型辅助调用失败：%s", ev.Err.Error())
		p.submitSolve()
	case ev.Proposal.FirstMoveCode == "":
		p.llmNote = fmt.Sprintf("大模型辅助未给出有效提议（%s）", ev.Message)
		p.submitSolve()
	default:
		// extract 返回 "h2-e2" 格式；解析前归一化掉分隔符（upstream llmAssist）。
		normalized := normalizeAssistCode(ev.Proposal.FirstMoveCode)
		if len(normalized) != 4 {
			p.submitSolve() // 无效提议：无注释（upstream 返回 null）
			return
		}
		from := llm.DecodeCell(normalized[:2])
		to := llm.DecodeCell(normalized[2:])
		if from == nil || to == nil {
			p.submitSolve()
			return
		}
		p.assistCode = ev.Proposal.FirstMoveCode
		p.assistIdea = ev.Proposal.Idea
		p.verifyRequest = p.newID("verify")
		p.solver.IsWinningFirstMoveAsync(p.verifyRequest, p.solveFen, rules.Move{From: *from, To: *to},
			p.solvePlies, p.solveTimeMs)
	}
}

// normalizeAssistCode 提议码归一化（仅保留 a-i/0-9，upstream replaceAll）。
func normalizeAssistCode(code string) string {
	var b strings.Builder
	for _, r := range code {
		if (r >= 'a' && r <= 'i') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// onSolveWinDone 裁判回执（主 goroutine）：注释口径 = 上游 runSolveAssist 逐字；
// 裁判失败按"调用失败"生成说明，求解照常进行。
func (p *StudioPage) onSolveWinDone(ev SolveWinDone) {
	if ev.RequestID != p.verifyRequest || p.disposed {
		return
	}
	p.verifyRequest = ""
	suffix := ""
	if p.assistIdea != "" {
		suffix = fmt.Sprintf("；思路: %s", p.assistIdea)
	}
	switch {
	case ev.Err != nil:
		p.llmNote = fmt.Sprintf("大模型辅助调用失败：%s", ev.Err.Error())
	case ev.Win:
		p.llmNote = fmt.Sprintf("大模型首选 %s（已验证为必胜着法）%s", p.assistCode, suffix)
	default:
		p.llmNote = fmt.Sprintf("大模型首选 %s 未通过求解器验证，已忽略%s", p.assistCode, suffix)
	}
	p.assistCode = ""
	p.assistIdea = ""
	p.submitSolve()
}

func (p *StudioPage) submitSolve() {
	p.solveRequest = p.newID("solve")
	p.solver.SolveAsync(p.solveRequest, p.solveFen, p.solveTimeMs, p.solvePlies)
}

// startSolveTicker 求解进度 200ms 节拍（goroutine 只 emit，铁律 #G3；
// 迟到 tick 按 Gen 代次丢弃，#G5）。
func (p *StudioPage) startSolveTicker() {
	gen := p.solveGen
	stop := make(chan struct{})
	p.solveStop = stop
	if p.env.Emit == nil {
		return
	}
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(solveTickInterval):
			}
			p.env.Emit("", SolveTick{Gen: gen}, nil)
		}
	}()
}

func (p *StudioPage) stopSolveTicker() {
	p.solveGen++
	if p.solveStop != nil {
		close(p.solveStop)
		p.solveStop = nil
	}
}

func (p *StudioPage) onSolveDone(ev SolveDone) {
	if ev.RequestID != p.solveRequest || p.disposed {
		return // 迟到/非当前请求（总线已丢取消 id，此处防御）
	}
	p.solveRequest = ""
	p.stopSolveTicker()
	p.solving = false
	if ev.Err != nil {
		p.showToast("求解失败：" + ev.Err.Error())
		return
	}
	if ev.Result == nil {
		p.showToast("求解失败：无结果")
		return
	}
	p.pendingSheet = &solveSheet{
		fen:     p.solveFen,
		result:  *ev.Result,
		llmNote: p.llmNote,
		redTurn: p.solveRedTurn,
	}
	p.persistSolveRecord()
}

// persistSolveRecord 求解结论自动入库（三种结论全部入库，04 §4；upstream persistRecord）。
func (p *StudioPage) persistSolveRecord() {
	if p.pendingSheet == nil {
		return
	}
	sheet := p.pendingSheet
	status := state.SolveStatus(sheet.result.Status)
	label := state.SolveLabelOf(status, len(sheet.result.Solutions))
	var note *string
	if sheet.llmNote != "" {
		note = &sheet.llmNote
	}
	record := state.GameRecordData{
		Title:       state.StudioRecordTitle(label, sheet.redTurn, time.Now()),
		Mode:        "endgame",
		InitialFen:  sheet.fen,
		Moves:       []rules.Move{},
		SolveStatus: status,
		Solutions:   wireSolutionsToIccs(sheet.result),
		LlmNote:     note,
		CreatedAt:   time.Now().UnixMilli(),
	}
	p.saveRecord(record, "solve")
}

// wireSolutionsToIccs 解法序列 → ICCS 码（formatIccs 同语义）。
func wireSolutionsToIccs(result solver.WireSolveResult) [][]string {
	out := make([][]string, 0, len(result.Solutions))
	for _, s := range result.Solutions {
		codes := make([]string, 0, len(s.Moves))
		for _, m := range s.Moves {
			codes = append(codes, parsers.FormatIccs(rules.Pos(m.From.Col, m.From.Row), rules.Pos(m.To.Col, m.To.Row)))
		}
		out = append(out, codes)
	}
	return out
}

// saveUnsolvedRecord 保存棋局（未求解入库 solveStatus=none；upstream saveUnsolvedRecord）。
func (p *StudioPage) saveUnsolvedRecord() {
	if problems := p.validate(); len(problems) > 0 {
		p.showToast(strings.Join(problems, "；"))
		return
	}
	record := state.GameRecordData{
		Title:       state.StudioUnsolvedTitle(p.redTurn, time.Now()),
		Mode:        "endgame",
		InitialFen:  p.currentFen(),
		Moves:       []rules.Move{},
		SolveStatus: state.SolveNone,
		Solutions:   [][]string{},
		CreatedAt:   time.Now().UnixMilli(),
	}
	p.saveRecord(record, "unsolved")
}

func (p *StudioPage) saveRecord(record state.GameRecordData, kind string) {
	if p.env.Records == nil {
		p.showToast("保存失败：本地存储不可用")
		return
	}
	p.saveID = p.newID("recsave")
	p.saveKind = kind
	p.env.Records.RecordsSaveAsync(p.saveID, record)
}

func (p *StudioPage) onRecordSaved(err error) {
	if p.saveID == "" {
		return // 过期回执
	}
	kind := p.saveKind
	p.saveID = ""
	if kind == "solve" {
		sheet := p.pendingSheet
		p.pendingSheet = nil
		if sheet == nil {
			return
		}
		sheet.recordSaved = err == nil
		p.sheet = sheet
		p.sheetOpen = true
		return
	}
	if err != nil {
		p.showToast("保存失败：本地存储不可用")
		return
	}
	p.showToast("棋局已保存到棋谱库（未求解）")
}

// ---- 识图（T6'.3，EndgameStudioPage readImageFile 语义 + 08 §7 人工校正流）----

// startVisionPick 选择棋盘图片（K33：reading 时按钮 disabled——守卫拦截重入）。
func (p *StudioPage) startVisionPick() {
	if p.reading {
		return
	}
	if !p.visionSlotsLoaded() {
		p.visionMessage = "助手配置加载中，请稍后重试"
		return
	}
	p.dialogRequest = p.newID("dialog")
	id := p.dialogRequest
	PickFileAsync(func(path string, err error) {
		p.env.Emit("", FilePickDone{RequestID: id, Path: path, Err: err}, nil)
	})
}

// loadVisionFromPath 从路径直载（D-006 页面内路径输入兜底）。
func (p *StudioPage) loadVisionFromPath() {
	if p.reading {
		return
	}
	path := strings.TrimSpace(p.visionPathEd.Text())
	if path == "" {
		return
	}
	p.beginVisionRead(path)
}

// onFilePicked 文件选择回执（取消=空串无操作）。
func (p *StudioPage) onFilePicked(ev FilePickDone) {
	if ev.RequestID != p.dialogRequest || p.disposed {
		return
	}
	p.dialogRequest = ""
	if ev.Err != nil {
		p.showToast("打开图片失败：" + ev.Err.Error())
		return
	}
	if ev.Path == "" {
		return
	}
	p.beginVisionRead(ev.Path)
}

// beginVisionRead 识图请求发起（DR-009：助手槽全空时运行时借用对战配置 黑→红，
// 仅本次请求内存不写入助手槽；借用时消息区注记来源与识图风险——上游
// readImageFile resolved.source 分支逐字）。
func (p *StudioPage) beginVisionRead(path string) {
	if !p.visionSlotsLoaded() {
		p.visionMessage = "助手配置加载中，请稍后重试"
		return
	}
	resolved := state.ResolveAssistantConfig(p.asstConfig, p.blkConfig, p.redConfig)
	if resolved.Config == nil {
		p.visionMessage = "请先配置研究助手模型（需视觉模型）"
		return
	}
	p.visionMessage = ""
	p.visionLoaded = false
	if resolved.Source != state.AssistantSourceAssistant {
		p.visionMessage = fmt.Sprintf("研究助手未配置，已临时借用%s对战配置（不写入研究助手配置）——对战配置可能不支持识图", state.AssistantSourceLabel(resolved.Source))
	}
	// 计时从图片选定开始（文件浏览期间不计入——upstream readImageFile）
	p.reading = true
	p.visionElapsed = 0
	p.visionRequest = p.newID("vision")
	p.startVisionTicker()
	p.vision.ReadFileAsync(p.visionRequest, path, *resolved.Config, resolved.AuthSlot)
}

// startVisionTicker 识图已用时 1s 节拍（00 §4 timer:tick 页面级计时器共用；
// goroutine 只 emit，#G3；迟到 tick 由 reading 标志守卫）。
func (p *StudioPage) startVisionTicker() {
	p.visionTickerGen++
	stop := make(chan struct{})
	p.visionStop = stop
	if p.env.Emit == nil {
		return
	}
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Second):
			}
			p.env.Emit("", TimerTick{}, nil)
		}
	}()
}

func (p *StudioPage) stopVisionTicker() {
	p.visionTickerGen++
	if p.visionStop != nil {
		close(p.visionStop)
		p.visionStop = nil
	}
}

// onVisionDone 识图回执（主 goroutine；requestId 关联，迟到丢弃 #G5）。
func (p *StudioPage) onVisionDone(ev VisionReadDone) {
	if ev.RequestID != p.visionRequest || p.disposed {
		return
	}
	p.visionRequest = ""
	p.stopVisionTicker()
	p.reading = false
	if ev.Err != nil {
		p.visionMessage = "识图失败：" + ev.Err.Error()
		return
	}
	grid, err := rules.ParseBoardFen(ev.Fen)
	if err != nil {
		p.visionMessage = "识图失败：" + err.Error()
		return
	}
	p.grid = grid
	p.redTurn = rules.ParseTurnFen(ev.Fen)
	p.touchGrid()
	p.visionLoaded = true
	pieceCount := 0
	for _, row := range grid {
		for _, q := range row {
			if q != nil {
				pieceCount++
			}
		}
	}
	// 人工校正流提示（05 §7 管线尾段：识图结果必须人工核对才可求解）
	p.visionMessage = fmt.Sprintf("识别到 %d 枚棋子（%s方行棋），已载入棋盘，请人工核对后再求解",
		pieceCount, turnName(p.redTurn))
}

// onSlotLoaded 凭据槽位掩码回读（DR-009 借用解析输入）。
func (p *StudioPage) onSlotLoaded(ev SecureSlotLoaded) {
	var cfg llm.LlmEndpointConfig
	if ev.Config != nil {
		cfg = *ev.Config
	}
	switch ev.Slot {
	case storage.SlotAssistant:
		p.asstConfig, p.asstLoaded = &cfg, true
	case storage.SlotBlack:
		p.blkConfig, p.blkLoaded = &cfg, true
	case storage.SlotRed:
		p.redConfig, p.redLoaded = &cfg, true
	}
}

// sheetStatusText 结果面板标题（EndgameStudioPage statusText）。
func sheetStatusText(result solver.WireSolveResult) string {
	switch result.Status {
	case "solved":
		if len(result.Solutions) == 1 {
			return "已破解（唯一解）"
		}
		return fmt.Sprintf("已破解（%d 条破解走法）", len(result.Solutions))
	case "noSolution":
		return fmt.Sprintf("无解（%d 半着内已证明）", result.SearchedPlies)
	}
	return "限时内未找到解法"
}

// ---- 事件与页面契约 ----

func (p *StudioPage) newID(prefix string) string {
	if p.env.NewRequestID != nil {
		return p.env.NewRequestID(prefix)
	}
	return prefix + "-test"
}

func (p *StudioPage) showToast(message string) {
	p.toastSeq++
	p.toastText = message
	seq := p.toastSeq
	if p.env.Emit != nil {
		time.AfterFunc(2200*time.Millisecond, func() { p.env.Emit("", ToastHide{Seq: seq}, nil) })
	}
}

// modalOpen 弹窗层是否打开（结果面板/启动器；遮罩拦截底层交互——KG-008 收口）。
func (p *StudioPage) modalOpen() bool { return p.sheetOpen || p.launcher.Opened() || p.assistantOpen }

// OnAppEvent 事件总线回执（主 goroutine 消费）。
func (p *StudioPage) OnAppEvent(payload any) {
	switch ev := payload.(type) {
	case SolveTick:
		if ev.Gen != p.solveGen {
			return // 迟到节拍丢弃（#G5；Emit 已排帧，无额外动作）
		}
	case SolveDone:
		p.onSolveDone(ev)
	case RecordSaveDone:
		p.onRecordSaved(ev.Err)
	case ToastHide:
		if ev.Seq == p.toastSeq {
			p.toastText = ""
		}
	case TimerTick:
		if p.reading {
			p.visionElapsed++ // 识图已用时（K33：无取消入口，仅计时呈现）
		}
	case SecureSlotLoaded:
		p.onSlotLoaded(ev)
	case FilePickDone:
		p.onFilePicked(ev)
	case VisionReadDone:
		p.onVisionDone(ev)
	case AssistProposalDone:
		p.onAssistProposalDone(ev)
	case SolveWinDone:
		p.onSolveWinDone(ev)
	case LlmTestDone:
		if ev.RequestID == p.assistTestID && p.assistantCard != nil {
			p.assistTestID = ""
			p.assistantCard.SetTestResult(ev.OK, ev.Message)
		}
	case PasteTextDone:
		if p.assistantCard != nil {
			p.assistantCard.ApplyPaste(ev.Target, ev.Text, ev.Err)
		}
	case SecureSlotSaved:
		if ev.Slot == storage.SlotAssistant && p.assistantOpen {
			p.onAssistantSaved(ev)
		}
	}
}

// Dispose 页面卸载：取消在途求解（Runner ctx + 总线双收口 #G5）→ 停节拍。
func (p *StudioPage) Dispose() {
	if p.disposed {
		return
	}
	p.disposed = true
	if p.solveRequest != "" {
		p.solver.Cancel(p.solveRequest)
		p.solveRequest = ""
	}
	p.stopSolveTicker()
	p.solving = false
	p.stopVisionTicker()
	// K33：识图请求无中途取消入口——在途回执经 disposed 标志与 requestId
	// 双重丢弃，迟到结果不落盘面。
	p.reading = false
	// 求解辅助：ctx 取消在途提议；裁判请求双收口取消（#G5）
	if p.assistRequest != "" {
		if p.assistCancel != nil {
			p.assistCancel()
		}
		p.assistRequest = ""
	}
	if p.verifyRequest != "" {
		p.solver.Cancel(p.verifyRequest)
		p.verifyRequest = ""
	}
	p.closeAssistantDialog()
	p.launcher.Close()
}

// ---- 布局 ----

func (p *StudioPage) Layout(gtx layout.Context) layout.Dimensions {
	p.handleEvents(gtx)
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	fillRect(gtx.Ops, image.Rectangle{Max: gtx.Constraints.Max}, ThemeSurface)

	return layout.Stack{}.Layout(gtx,
		layout.Expanded(p.layoutContent),
		layout.Stacked(p.layoutOverlays),
	)
}

func (p *StudioPage) layoutContent(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(p.layoutHeader),
		layout.Rigid(p.layoutBoardArea),
		layout.Rigid(p.layoutTabs),
		layout.Flexed(1, p.layoutTabBody),
		layout.Rigid(p.layoutActions),
	)
}

// layoutHeader 头部：返回/标题/研究助手配置（助手弹窗随 T6'.4）。
func (p *StudioPage) layoutHeader(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(8), Bottom: unit.Dp(4), Left: unit.Dp(12), Right: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(studioButton(&p.backBtn, "返回", false)),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					t := material.H6(PageTheme, "残局工作室")
					t.Color = ThemeOnSurface
					return t.Layout(gtx)
				})
			}),
			layout.Rigid(studioButton(&p.assistantBtn, "研究助手模型配置", false)),
		)
	})
}

func (p *StudioPage) layoutBoardArea(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		size := gtx.Constraints.Max.X
		if h := int(float32(gtx.Constraints.Max.Y) * 0.5); h < size {
			size = h // 高度 50% 封顶：Tab 内容与操作行保持可见
		}
		gtx.Constraints = layout.Constraints{Min: image.Pt(size, size), Max: image.Pt(size, size)}
		return p.board.layout(gtx, p.currentFen())
	})
}

func (p *StudioPage) layoutTabs(gtx layout.Context) layout.Dimensions {
	opts := make([]chipOpt, len(studioTabs))
	for i, label := range studioTabs {
		opts[i] = chipOpt{click: &p.tabClicks[i], label: label, selected: p.tabIndex == i}
	}
	return layout.Inset{Top: unit.Dp(6), Left: unit.Dp(12)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layoutOptionChips(gtx, 64, opts...)
	})
}

func (p *StudioPage) layoutTabBody(gtx layout.Context) layout.Dimensions {
	// Tab 内容区纵向滚动（窗口矮/缩放档位高时内容不被裁切——页级列表，
	// 铁律 #G3 主 goroutine 独占）。
	return p.tabList.Layout(gtx, 1, func(gtx layout.Context, _ int) layout.Dimensions {
		switch p.tabIndex {
		case 0:
			return p.layoutSetupTab(gtx)
		case 1:
			return p.layoutSolveTab(gtx)
		default:
			return p.layoutVisionTab(gtx)
		}
	})
}

func (p *StudioPage) layoutSetupTab(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		// 行棋方
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Left: unit.Dp(12), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Rigid(studioLabel("行棋方:", 14)),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
							return layoutOptionChips(gtx, 48,
								chipOpt{click: &p.turnRedBtn, label: "红方", selected: p.redTurn},
								chipOpt{click: &p.turnBlkBtn, label: "黑方", selected: !p.redTurn},
							)
						})
					}),
				)
			})
		}),
		// 调色板两行（红子/黑子）+ 橡皮/清空/初始局面
		layout.Rigid(p.layoutPalette),
		// FEN 导入（上游 FEN 导入 Tab 并入，偏离注记见 PROGRESS）
		layout.Rigid(p.layoutFenImport),
		// 校验结果面板
		layout.Rigid(p.layoutProblems),
	)
}

func (p *StudioPage) layoutPalette(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(12), Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		rows := []struct {
			label  string
			side   rules.Side
			clicks *[7]widget.Clickable
		}{
			{"红子", rules.Red, &p.paletteRed},
			{"黑子", rules.Black, &p.paletteBlk},
		}
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				children := make([]layout.FlexChild, 0, len(rows))
				for _, row := range rows {
					row := row
					children = append(children, layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
							layout.Rigid(studioLabel(row.label, 13)),
							layout.Rigid(func(gtx layout.Context) layout.Dimensions {
								return layout.Inset{Left: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
									opts := make([]chipOpt, len(paletteKinds))
									for i, kind := range paletteKinds {
										piece := rules.Piece{Kind: kind, Side: row.side}
										selected := p.selected != nil && p.selected.Kind == kind && p.selected.Side == row.side && !p.eraser
										opts[i] = chipOpt{click: &row.clicks[i], label: rules.PieceLabel(&piece), selected: selected}
									}
									return layoutOptionChips(gtx, 36, opts...)
								})
							}),
						)
					}))
				}
				return layout.Flex{Axis: layout.Vertical}.Layout(gtx, children...)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutOptionChips(gtx, 56,
						chipOpt{click: &p.eraserBtn, label: "橡皮", selected: p.eraser},
						chipOpt{click: &p.clearBtn, label: "清空棋盘", selected: false},
						chipOpt{click: &p.initialBtn, label: "初始局面", selected: false},
					)
				})
			}),
		)
	})
}

func (p *StudioPage) layoutFenImport(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layoutEditorBox(gtx, &p.fenEditor, "粘贴 FEN（完整 FEN 或仅棋盘字段）")
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(studioSmallButton(&p.loadFenBtn, "解析并载入棋盘")),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							l := material.Body2(PageTheme, fmt.Sprintf("当前 FEN: %s（%s方行棋）", rules.BoardGridToFen(p.grid), turnName(p.redTurn)))
							l.TextSize = unit.Sp(12)
							l.Color = ThemeSeedDark
							return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, l.Layout)
						}),
					)
				})
			}),
		)
	})
}

func (p *StudioPage) layoutProblems(gtx layout.Context) layout.Dimensions {
	problems := p.validate()
	text := "校验通过（可保存 / 可求解）"
	fg := color.NRGBA(ThemeLegalHint)
	if len(problems) > 0 {
		text = "校验未通过：" + strings.Join(problems, "；")
		fg = ThemeError
	}
	return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(PageTheme, text)
		l.TextSize = unit.Sp(12)
		l.Color = fg
		return l.Layout(gtx)
	})
}

// layoutSolveTab 求解参数区（上游求解设置对话框内联化——Gio chips 呈现，偏离注记）。
func (p *StudioPage) layoutSolveTab(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(12), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return studioChoiceRow(gtx, "限时:", p.timeChips[:], p.timeIdx, func(i int) string { return solveTimeOptions[i].label })
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return studioChoiceRow(gtx, "搜索深度（求解方着数）:", p.depthChips[:], p.depthIdx, func(i int) string { return solveDepthOptions[i].label })
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutOptionChips(gtx, 96,
								chipOpt{click: &p.useLlmChip, label: "大模型辅助：开", selected: p.useLlmOn},
								chipOpt{click: &p.useLlmChip, label: "大模型辅助：关", selected: !p.useLlmOn},
							)
						}),
					)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, studioLabel("开启后：模型提议首着，求解器验证通过才写入注释（需先配置研究助手模型；未配置时借用对战配置）", 12))
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, studioLabel(
					"求解期间棋盘可继续操作（底部悬浮条显示进度）；三种结论（含无解/超时）均自动入库为棋谱。", 12))
			}),
		)
	})
}

func (p *StudioPage) layoutVisionTab(gtx layout.Context) layout.Dimensions {
	pickLabel := "选择棋盘图片并识别"
	if p.reading {
		pickLabel = fmt.Sprintf("识别中… 已用时 %d 秒", p.visionElapsed)
	}
	return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// 主入口（K33：识别中 disabled——变灰 + 守卫拦截）
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				btn := material.Button(PageTheme, &p.pickBtn, pickLabel)
				if p.reading {
					btn.Background = ThemeSurfaceDim
					btn.Color = color.NRGBA(ThemeSeedDark)
					btn.Color.A = 120
				}
				return btn.Layout(gtx)
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(2)}.Layout(gtx, studioLabel(
					"一般 5~20 秒；大图或思考型模型会更久，单次超时 120 秒 × 最多 2 次（识别中不可取消）。", 12))
			}),
			// 路径兜底（D-006：页面内路径输入保留为兜底）
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layoutEditorBox(gtx, &p.visionPathEd, "图片路径兜底（对话框不可用时手填）")
						}),
						layout.Rigid(func(gtx layout.Context) layout.Dimensions {
							return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, studioSmallButton(&p.loadPathBtn, "从路径载入并识别"))
						}),
					)
				})
			}),
			// 识图消息
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if p.visionMessage == "" {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					l := material.Body2(PageTheme, p.visionMessage)
					l.TextSize = unit.Sp(12)
					l.Color = ThemeSeedDark
					return l.Layout(gtx)
				})
			}),
			// 人工校正流按钮（识别结果载入后出现——08 §7）
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				if !p.visionLoaded {
					return layout.Dimensions{}
				}
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					return layoutOptionChips(gtx, 72,
						chipOpt{click: &p.corrBtn, label: "去摆盘校正", selected: false},
						chipOpt{click: &p.revisionBtn, label: "重新识别", selected: false},
						chipOpt{click: &p.adoptBtn, label: "直接采用（去求解）", selected: false},
					)
				})
			}),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Top: unit.Dp(6)}.Layout(gtx, studioLabel(
					"识别结果会载入上方棋盘：在摆盘校验页点击纠错（选棋子放置/橡皮清除/取走），核对无误后即可求解。建议使用棋盘截图或正俯拍照片。", 12))
			}),
		)
	})
}

// layoutActions 底部操作行（保存棋局 / AI 求破解——上游全局底部按钮行）。
func (p *StudioPage) layoutActions(gtx layout.Context) layout.Dimensions {
	return layout.Inset{Left: unit.Dp(12), Right: unit.Dp(12), Top: unit.Dp(4), Bottom: unit.Dp(10)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		save := material.Button(PageTheme, &p.saveBtn, "保存棋局")
		save.Background = ThemeSurfaceDim
		save.Color = ThemeSeedDark
		solve := material.Button(PageTheme, &p.solveBtn, "AI 求破解")
		return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
			layout.Flexed(1, save.Layout),
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(12)}.Layout(gtx, solve.Layout)
			}),
		)
	})
}

func (p *StudioPage) handleEvents(gtx layout.Context) {
	// 结果面板/启动器打开：遮罩拦截底层交互（仅消费本帧点击边沿，防穿透——
	// KG-008 收口口径；弹窗本体输入在 layoutOverlays 处理）。
	if p.modalOpen() {
		p.consumeEdges(gtx)
		return
	}
	if p.backBtn.Clicked(gtx) && p.hooks.OnBack != nil {
		p.hooks.OnBack()
		return
	}
	for i := range p.tabClicks {
		if p.tabClicks[i].Clicked(gtx) {
			p.tabIndex = i
			return
		}
	}
	for i := range p.paletteRed {
		if p.paletteRed[i].Clicked(gtx) {
			p.selectPiece(rules.Red, i)
			return
		}
		if p.paletteBlk[i].Clicked(gtx) {
			p.selectPiece(rules.Black, i)
			return
		}
	}
	if p.eraserBtn.Clicked(gtx) {
		p.eraser = !p.eraser
		if p.eraser {
			p.selected = nil
		}
	}
	if p.clearBtn.Clicked(gtx) {
		for r := range p.grid {
			for c := range p.grid[r] {
				p.grid[r][c] = nil
			}
		}
		p.touchGrid()
		p.showToast("已清空棋盘")
	}
	if p.initialBtn.Clicked(gtx) {
		if grid, err := rules.ParseBoardFen(rules.FENInitial); err == nil {
			p.grid = grid
			p.redTurn = true
			p.touchGrid()
		}
	}
	if p.turnRedBtn.Clicked(gtx) {
		p.redTurn = true
		p.touchGrid()
	}
	if p.turnBlkBtn.Clicked(gtx) {
		p.redTurn = false
		p.touchGrid()
	}
	if p.loadFenBtn.Clicked(gtx) {
		p.importFen()
	}
	for i := range p.timeChips {
		if p.timeChips[i].Clicked(gtx) {
			p.timeIdx = i
		}
	}
	for i := range p.depthChips {
		if p.depthChips[i].Clicked(gtx) {
			p.depthIdx = i
		}
	}
	if p.saveBtn.Clicked(gtx) {
		p.saveUnsolvedRecord()
	}
	if p.useLlmChip.Clicked(gtx) {
		p.useLlmOn = !p.useLlmOn // 开关即时生效（下一次求解）
	}
	if p.solveBtn.Clicked(gtx) {
		p.startSolve()
	}
	if p.assistantBtn.Clicked(gtx) {
		p.openAssistantDialog()
	}
	if p.pickBtn.Clicked(gtx) {
		p.startVisionPick()
	}
	if p.loadPathBtn.Clicked(gtx) {
		p.loadVisionFromPath()
	}
	if p.corrBtn.Clicked(gtx) {
		p.tabIndex = 0 // 人工校正：摆盘校验页点击纠错（候选棋子选择器=调色板）
	}
	if p.revisionBtn.Clicked(gtx) {
		p.startVisionPick() // 重新提交
	}
	if p.adoptBtn.Clicked(gtx) {
		p.tabIndex = 1 // 直接采用：进入求解
	}
}

// consumeEdges 弹窗打开时消费底层控件本帧点击边沿（防穿透，不触发动作）。
func (p *StudioPage) consumeEdges(gtx layout.Context) {
	for i := range p.tabClicks {
		p.tabClicks[i].Clicked(gtx)
	}
	for i := range p.paletteRed {
		p.paletteRed[i].Clicked(gtx)
		p.paletteBlk[i].Clicked(gtx)
	}
	p.eraserBtn.Clicked(gtx)
	p.clearBtn.Clicked(gtx)
	p.initialBtn.Clicked(gtx)
	p.turnRedBtn.Clicked(gtx)
	p.turnBlkBtn.Clicked(gtx)
	p.loadFenBtn.Clicked(gtx)
	for i := range p.timeChips {
		p.timeChips[i].Clicked(gtx)
	}
	for i := range p.depthChips {
		p.depthChips[i].Clicked(gtx)
	}
	p.saveBtn.Clicked(gtx)
	p.solveBtn.Clicked(gtx)
	p.useLlmChip.Clicked(gtx)
	p.pickBtn.Clicked(gtx)
	p.loadPathBtn.Clicked(gtx)
	p.corrBtn.Clicked(gtx)
	p.revisionBtn.Clicked(gtx)
	p.adoptBtn.Clicked(gtx)
	p.assistantBtn.Clicked(gtx)
}

func (p *StudioPage) selectPiece(side rules.Side, idx int) {
	piece := rules.Piece{Kind: paletteKinds[idx], Side: side}
	if p.selected != nil && p.selected.Kind == piece.Kind && p.selected.Side == piece.Side {
		p.selected = nil // 再点取消（上游同款）
		return
	}
	p.selected = &piece
	p.eraser = false
}

// importFen FEN 导入（upstream importFen：单字段补轮走方；非法 toast）。
func (p *StudioPage) importFen() {
	fen := strings.TrimSpace(p.fenEditor.Text())
	if fen == "" {
		return
	}
	if len(strings.Fields(fen)) == 1 {
		turn := "b"
		if p.redTurn {
			turn = "w"
		}
		fen = fen + " " + turn
	}
	if !rules.IsValidFen(fen) {
		p.showToast("FEN 无效，请检查格式")
		return
	}
	grid, err := rules.ParseBoardFen(fen)
	if err != nil {
		p.showToast("FEN 无效，请检查格式")
		return
	}
	p.grid = grid
	p.redTurn = rules.ParseTurnFen(fen)
	p.touchGrid()
	p.showToast(fmt.Sprintf("已载入（%s方行棋），可在摆盘页微调", turnName(p.redTurn)))
}

// layoutOverlays 进度悬浮条 + 结果面板 + 启动器 + toast（Stack 顶层）。
func (p *StudioPage) layoutOverlays(gtx layout.Context) layout.Dimensions {
	if p.solving {
		// 非阻塞悬浮条（08 §3.6 优化 2）：复用 toast 绘制（不注册输入事件，
		// 不遮棋盘），无关闭入口——TC-SOL-007；结束自动消失。
		DrawToast(gtx, fmt.Sprintf("求解中… 已用时 %.1fs", time.Since(p.solveStart).Seconds()))
	}
	if p.sheetOpen && p.sheet != nil {
		// 按钮点击消费（先于绘制——Click 在本帧布局中注册、当帧回执边沿）：
		// 渲染不消费=死按钮（M4' 验收修复轮 11 同款教训）。
		switch {
		case p.closeBtn.Clicked(gtx), p.close2Btn.Clicked(gtx):
			p.sheetOpen = false
			p.sheet = nil
		case p.battleBtn.Clicked(gtx):
			p.launcher.Open(nil) // 进入对战联动（起点/执方由 launchBattle 提供）
		}
		// 关闭后本帧跳过绘制（sheet 已 nil——继续绘制= nil deref panic，
		// unwinding 时 defer clip Pop 二次 panic 掩盖原错误，应用退出）。
		if p.sheetOpen && p.sheet != nil {
			p.layoutSheet(gtx)
		}
	}
	if p.assistantOpen && p.assistantCard != nil {
		if p.asstCancelBtn.Clicked(gtx) {
			p.closeAssistantDialog()
		}
		if p.asstSaveBtn.Clicked(gtx) {
			p.saveAssistant()
		}
		// 关闭后本帧跳过绘制（card 已 nil——nil deref panic，与结果面板同款）
		if p.assistantOpen && p.assistantCard != nil {
			p.layoutAssistantDialog(gtx)
		}
	}
	if p.launcher.Opened() && p.sheet != nil {
		p.launcher.Layout(gtx, p.launchBattle)
	}
	if p.toastText != "" {
		DrawToast(gtx, p.toastText)
	}
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

// openAssistantDialog 研究助手配置弹窗（T6'.4，AssistantConfigDialog 翻译：
// 求解辅助与棋盘识图共用的助手槽位，预设为视觉理解模型清单；掩码回显）。
func (p *StudioPage) openAssistantDialog() {
	cfg := llm.LlmEndpointConfig{}
	if p.asstConfig != nil {
		cfg = *p.asstConfig
	}
	p.assistantConfig = cfg
	p.assistantCard = NewLlmConfigCard("求解辅助 / 识图（需视觉理解模型）", storage.SlotAssistant,
		llm.VisionLlmPresets, cfg, func(c llm.LlmEndpointConfig) { p.assistantConfig = c })
	p.assistantCard.OnPaste = p.onAssistantPaste
	p.assistantCard.OnTestConnection = p.onAssistantTest
	p.assistantOpen = true
}

func (p *StudioPage) closeAssistantDialog() {
	p.assistantOpen = false
	p.assistantCard = nil
	p.assistTestID = ""
}

// onAssistantSaved 助手槽写入回执：toast + 重载槽位（掩码回读刷新）+ 关闭。
func (p *StudioPage) onAssistantSaved(ev SecureSlotSaved) {
	if p.env.Store != nil {
		p.env.Store.LoadSlotAsync(p.newID("cfg-asst"), storage.SlotAssistant)
	}
	switch {
	case ev.Err != nil:
		p.showToast("保存失败：本地存储不可用")
	case ev.Stored == "plainFallback":
		p.showToast("模型配置已保存（系统安全存储不可用，已明文保存到本地）")
	default:
		p.showToast("模型配置已保存")
	}
	p.closeAssistantDialog()
}

// onAssistantTest 测试连接（经页内 LlmClient 测试通道；DR-005 关闭参数恒发）。
func (p *StudioPage) onAssistantTest(cfg llm.LlmEndpointConfig, slot string) {
	if p.testLlm == nil {
		p.testLlm = NewLlmClient(p.env.GameEnv, p.resolveKey, p.idleSecs)
	}
	p.assistTestID = p.newID("test")
	p.testLlm.TestConnectionAsync(p.assistTestID, cfg, slot)
}

// onAssistantPaste KG-004 可靠粘贴（目标字段定向回填）。
func (p *StudioPage) onAssistantPaste(target int) {
	pasteFromWindowsAsync(func(text string, err error) {
		p.env.Emit("", PasteTextDone{Target: target, Text: text, Err: err}, nil)
	})
}

// saveAssistant 助手槽保存（掩码合并由存储层承担——防错 #8）。
func (p *StudioPage) saveAssistant() {
	if p.env.Store == nil {
		p.showToast("保存失败：本地存储不可用")
		return
	}
	p.env.Store.SaveSlotAsync(p.newID("save-asst"), storage.SlotAssistant, p.assistantConfig)
}

// layoutAssistantDialog 助手配置弹窗（遮罩 + 面板：配置卡 + 取消/保存；
// KG-009 宏量测居中口径）。
func (p *StudioPage) layoutAssistantDialog(gtx layout.Context) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgba(0x000000, 0.45))

	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	panelW := gtx.Dp(unit.Dp(460))
	if W < panelW {
		panelW = W
	}
	// 量测：内容高度自适应（配置卡 + 按钮行）
	macro := op.Record(gtx.Ops)
	mctx := gtx
	mctx.Constraints = layout.Constraints{Max: image.Point{X: panelW, Y: H}}
	dims := layout.UniformInset(unit.Dp(16)).Layout(mctx, p.assistantPanel)
	call := macro.Stop()

	panelH := dims.Size.Y
	if panelH > H*90/100 {
		panelH = H * 90 / 100
	}
	tr := op.Offset(image.Pt((W-panelW)/2, (H-panelH)/2)).Push(gtx.Ops)
	defer tr.Pop()
	defer clip.UniformRRect(image.Rectangle{Max: image.Point{X: panelW, Y: panelH}}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	call.Add(gtx.Ops)
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

func (p *StudioPage) assistantPanel(gtx layout.Context) layout.Dimensions {
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			t := material.H6(PageTheme, "研究助手模型")
			t.Color = ThemeOnSurface
			return layout.Inset{Bottom: unit.Dp(8)}.Layout(gtx, t.Layout)
		}),
		layout.Rigid(p.assistantCard.Layout),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Inset{Top: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				cancel := material.Button(PageTheme, &p.asstCancelBtn, "取消")
				cancel.Background = ThemeSurfaceDim
				cancel.Color = ThemeSeedDark
				save := material.Button(PageTheme, &p.asstSaveBtn, "保存")
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						return layout.Inset{Right: unit.Dp(10)}.Layout(gtx, cancel.Layout)
					}),
					layout.Flexed(1, save.Layout),
				)
			})
		}),
	)
}

// launchBattle 进入对战联动（结果面板底部按钮；起点=求解局面 FEN，玩家执求解方）。
func (p *StudioPage) launchBattle(mode BattleMode, side string, _ any) {
	if p.hooks.OnBattle != nil && p.sheet != nil {
		if side == "" {
			side = turnName(p.sheet.redTurn) // 人机 AI 缺省执求解方（recordBattle 语料口径同型）
		}
		p.hooks.OnBattle(mode, p.sheet.fen, side)
	}
}

// layoutSheet 结果面板（08 §5 置顶关闭：标题行常驻头部 + × 按钮；
// 底部"关闭"按钮保留兜底；KG-009 手动居中口径）。
func (p *StudioPage) layoutSheet(gtx layout.Context) layout.Dimensions {
	defer clip.Rect{Max: gtx.Constraints.Max}.Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, rgba(0x000000, 0.45))

	W, H := gtx.Constraints.Max.X, gtx.Constraints.Max.Y
	panelW := gtx.Dp(unit.Dp(560))
	if W < panelW {
		panelW = W
	}
	panelH := H * 72 / 100

	tr := op.Offset(image.Pt((W-panelW)/2, (H-panelH)/2)).Push(gtx.Ops)
	defer tr.Pop()
	defer clip.UniformRRect(image.Rectangle{Max: image.Point{X: panelW, Y: panelH}}, gtx.Dp(unit.Dp(12))).Push(gtx.Ops).Pop()
	paint.Fill(gtx.Ops, ThemeSurface)
	gtx.Constraints = layout.Exact(image.Point{X: panelW, Y: panelH})
	layout.UniformInset(unit.Dp(16)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// 置顶头部：标题 + ×（滚动时恒可见——08 §3.6 优化 1）
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						t := material.H6(PageTheme, sheetStatusText(p.sheet.result))
						t.Color = ThemeOnSurface
						return t.Layout(gtx)
					}),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(PageTheme, &p.closeBtn, "×")
						btn.Background = ThemeSurfaceDim
						btn.Color = ThemeSeedDark
						gtx.Constraints.Min.X = gtx.Dp(unit.Dp(40))
						gtx.Constraints.Min.Y = gtx.Dp(unit.Dp(36))
						return btn.Layout(gtx)
					}),
				)
			}),
			// 滚动内容
			layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
				rows := p.sheetRows()
				return p.sheetList.Layout(gtx, len(rows), func(gtx layout.Context, i int) layout.Dimensions {
					l := material.Body2(PageTheme, rows[i].text)
					l.TextSize = rows[i].size
					l.Color = rows[i].color
					return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, l.Layout)
				})
			}),
			// 底部按钮行（进入对战联动 + 关闭兜底）
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(PageTheme, &p.battleBtn, "进入对战")
						btn.Background = ThemeSurfaceDim
						btn.Color = ThemeSeedDark
						return layout.Inset{Right: unit.Dp(10)}.Layout(gtx, btn.Layout)
					}),
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(PageTheme, &p.close2Btn, "关闭")
						btn.Background = ThemeSurfaceDim
						btn.Color = ThemeSeedDark
						return btn.Layout(gtx)
					}),
				)
			}),
		)
	})
	return layout.Dimensions{Size: gtx.Constraints.Max}
}

type sheetRow struct {
	text  string
	size  unit.Sp
	color color.NRGBA
}

// sheetRows 结果面板内容行（upstream SolveResultSheet content 1:1）。
func (p *StudioPage) sheetRows() []sheetRow {
	s := p.sheet
	rows := []sheetRow{}
	savedText := "已保存到棋谱库"
	if !s.recordSaved {
		savedText = "已保存到棋谱库失败"
	}
	rows = append(rows, sheetRow{
		fmt.Sprintf("用时 %.1fs，%s", float64(s.result.Elapsed)/1000, savedText),
		unit.Sp(12), ThemeSeedDark,
	})
	if s.llmNote != "" {
		rows = append(rows, sheetRow{s.llmNote, unit.Sp(12), ThemeSeedDark})
	}
	if s.result.Status == "solved" && len(s.result.Solutions) == 0 {
		rows = append(rows, sheetRow{"对方已被将死/困毙，无需再走。", unit.Sp(14), ThemeOnSurface})
	}
	side := turnName(s.redTurn)
	for i, sol := range s.result.Solutions {
		moves := make([]rules.Move, 0, len(sol.Moves))
		for _, m := range sol.Moves {
			moves = append(moves, m.ToRules())
		}
		notations := state.ChineseNotations(s.fen, moves)
		rows = append(rows, sheetRow{
			fmt.Sprintf("解法 %d（%s方先行）:", i+1, side),
			unit.Sp(13), ThemeOnSurface,
		})
		rows = append(rows, sheetRow{strings.Join(notations, "  "), unit.Sp(14), ThemeOnSurface})
	}
	return rows
}

// ---- 小件 ----

func turnName(red bool) string {
	if red {
		return "红"
	}
	return "黑"
}

func studioLabel(text string, sp unit.Sp) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		l := material.Body2(PageTheme, text)
		l.TextSize = sp
		l.Color = ThemeOnSurface
		return l.Layout(gtx)
	}
}

func studioButton(c *widget.Clickable, label string, primary bool) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(PageTheme, c, label)
		if !primary {
			btn.Background = ThemeSurfaceDim
			btn.Color = ThemeSeedDark
		}
		return layout.Inset{Left: unit.Dp(6)}.Layout(gtx, btn.Layout)
	}
}

func studioSmallButton(c *widget.Clickable, label string) func(gtx layout.Context) layout.Dimensions {
	return func(gtx layout.Context) layout.Dimensions {
		btn := material.Button(PageTheme, c, label)
		btn.Background = ThemeSurfaceDim
		btn.Color = ThemeSeedDark
		btn.TextSize = unit.Sp(13)
		return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, btn.Layout)
	}
}

// studioChoiceRow 标签 + 一行 chips（求解参数区）。
func studioChoiceRow(gtx layout.Context, label string, clicks []widget.Clickable, selected int, nameOf func(int) string) layout.Dimensions {
	return layout.Inset{Top: unit.Dp(4)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
			layout.Rigid(studioLabel(label, 14)),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
					opts := make([]chipOpt, len(clicks))
					for i := range clicks {
						opts[i] = chipOpt{click: &clicks[i], label: nameOf(i), selected: i == selected}
					}
					return layoutOptionChips(gtx, 56, opts...)
				})
			}),
		)
	})
}
