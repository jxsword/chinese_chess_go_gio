package state

// 语料库浏览 store（翻译源 = 上游 frontend/src/stores/corpusBrowser.ts，372 行；
// 对应 corpus_browser_vm.dart，06 文档 §6）。
//
// 职责：扫描语料分类、按分类懒解析 XQF 文件（goroutine 分批 128 + 进度）、
// PGN 大文件按局索引分页浏览、搜索/难度筛选/排序。generation 计数防旧任务
// 覆盖新分类状态（corpusBrowser.ts:75 对齐上游 _generation）。
//
// Go 形态（00 §4 语料行，M5'）：本 struct 仅主 goroutine 读写（铁律 #G3）；
// 阻塞 I/O（扫描/读文件/解析/索引）经注入的 CorpusIO 在后台 goroutine 执行
//（驱动面 CorpusDriver 注入，生产 = go fn()），完成回执经 emit 回主循环
// Apply（生产 = app 事件总线；00 §4 `corpus:scan` 等事件行）——迟到回执按
// 代次丢弃（铁律 #G5），长批次间隙以 done 通道协作取消。
// 应用级数据（非对局状态），允许单例；对局状态仍必须走工厂（铁律 #G4）。

import (
	"sort"
	"strconv"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// CorpusSortMode 列表排序方式（corpus_browser_vm.dart:16）。
type CorpusSortMode string

const (
	SortName       CorpusSortMode = "name"
	SortMoves      CorpusSortMode = "moves"
	SortDifficulty CorpusSortMode = "difficulty"
)

// PgnPageSize PGN 大文件分页大小（06 文档 §4.4：渲染层分页列表每页 50）。
// Gio UI 以虚拟化连续滚动呈现（08 §8 落地注记），分页语义保留于状态层。
const PgnPageSize = 50

// ParseBatchSize 批量解析分批大小（06 文档 §6：每批 128 个文件）。
const ParseBatchSize = 128

// ParsedPuzzleView 解析结果的 UI 投影（corpusTypes.ts）：预计算 moveCount 与
// isEndgamePuzzle（避免渲染期重复判定）。
type ParsedPuzzleView struct {
	ID            string
	InitialFen    string
	SolutionMoves []string
	Title         *string
	Description   *string
	Source        string
	Format        string
	Difficulty    int
	MoveCount     int
	// Endgame 残局/排局题 = true；全局对局 = false（puzzle_data.dart:74-86）。
	Endgame bool
}

// VisibleItem 筛选排序后的可见条目（含原 entries 下标，openXqfPuzzle 用）。
type VisibleItem struct {
	Index  int
	Entry  storage.CorpusEntry
	Puzzle ParsedPuzzleView
}

// ToView 把解析结果投影为视图模型（isEndgamePuzzle 预计算；
// corpusBrowser.ts toView）。
func ToView(p *parsers.ParsedPuzzle, fallbackSource string) *ParsedPuzzleView {
	if p == nil {
		return nil
	}
	source := p.Source
	if source == "" {
		source = fallbackSource
	}
	return &ParsedPuzzleView{
		ID:            p.ID,
		InitialFen:    p.InitialFen,
		SolutionMoves: p.SolutionMoves,
		Title:         p.Title,
		Description:   p.Description,
		Source:        source,
		Format:        p.Format,
		Difficulty:    p.Difficulty,
		MoveCount:     len(p.SolutionMoves),
		Endgame:       parsers.IsEndgamePuzzle(source, p.InitialFen),
	}
}

// VisibleItems 搜索 + 难度筛选 + 排序后的可见列表（仅已解析成功的条目，
// corpus_browser_vm.dart:71-101）。
func VisibleItems(entries []storage.CorpusEntry, puzzles []*ParsedPuzzleView, query string, onlyEndgame bool, difficultyFilter int, sortMode CorpusSortMode) []VisibleItem {
	query = strings.TrimSpace(query)
	items := []VisibleItem{}
	for i := range entries {
		if i >= len(puzzles) {
			break // 上游 undefined 语义：越界位不可见
		}
		puzzle := puzzles[i]
		if puzzle == nil {
			continue
		}
		if difficultyFilter != 0 && puzzle.Difficulty != difficultyFilter {
			continue
		}
		if onlyEndgame && !puzzle.Endgame {
			continue
		}
		if len(query) > 0 && !strings.Contains(deref(puzzle.Title, entries[i].DisplayName), query) {
			continue
		}
		items = append(items, VisibleItem{Index: i, Entry: entries[i], Puzzle: *puzzle})
	}
	switch sortMode {
	case SortName:
		// 上游为 UTF-16 码元序（对齐 Dart compareTo）；Go 字符串比较为 UTF-8
		// 字节序——BMP 内常用 CJK 两编码均为码点序，语义一致。
		sort.SliceStable(items, func(a, b int) bool {
			return items[a].Entry.DisplayName < items[b].Entry.DisplayName
		})
	case SortMoves:
		sort.SliceStable(items, func(a, b int) bool {
			return items[a].Puzzle.MoveCount < items[b].Puzzle.MoveCount
		})
	case SortDifficulty:
		sort.SliceStable(items, func(a, b int) bool {
			d := items[a].Puzzle.Difficulty - items[b].Puzzle.Difficulty
			if d != 0 {
				return d < 0
			}
			return items[a].Puzzle.MoveCount < items[b].Puzzle.MoveCount
		})
	}
	return items
}

// PgnPage PGN 索引的搜索 + 分页切片结果（corpus_pgn_browser_page.dart:172-223）。
type PgnPage struct {
	Slice      []storage.PgnIndexEntry
	TotalPages int
	Total      int
}

// PgnFilter PGN 索引的搜索过滤（event/red/black 包含语义，contains）。
func PgnFilter(index []storage.PgnIndexEntry, query string) []storage.PgnIndexEntry {
	q := strings.TrimSpace(query)
	if len(q) == 0 {
		return index
	}
	filtered := make([]storage.PgnIndexEntry, 0, len(index))
	for _, e := range index {
		if (e.Event != nil && strings.Contains(*e.Event, q)) ||
			(e.Red != nil && strings.Contains(*e.Red, q)) ||
			(e.Black != nil && strings.Contains(*e.Black, q)) {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

// PgnPageSlice 分页切片与总页数（越界页收敛到最后一页）。
func PgnPageSlice(index []storage.PgnIndexEntry, query string, page int) PgnPage {
	filtered := PgnFilter(index, query)
	totalPages := (len(filtered) + PgnPageSize - 1) / PgnPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	safePage := page
	if safePage < 0 {
		safePage = 0
	}
	if safePage > totalPages-1 {
		safePage = totalPages - 1
	}
	lo := safePage * PgnPageSize
	if lo > len(filtered) {
		lo = len(filtered)
	}
	hi := lo + PgnPageSize
	if hi > len(filtered) {
		hi = len(filtered)
	}
	return PgnPage{Slice: filtered[lo:hi], TotalPages: totalPages, Total: len(filtered)}
}

func deref(s *string, dflt string) string {
	if s == nil {
		return dflt
	}
	return *s
}

// pgnGameFileName 单局解析的虚拟文件名（corpusBrowser.ts `game-${offset}.pgn`）。
func pgnGameFileName(offset int64) string {
	return "game-" + strconv.FormatInt(offset, 10) + ".pgn"
}

// ---------------------------------------------------------------------------
// 异步编排（load/selectCategory/openPgnCategory/openPgnGame 的 Go 形态）
// ---------------------------------------------------------------------------

// CorpusIO 阻塞 I/O 面（复制物直调；必须在工作 goroutine 调用，禁止主循环）。
// 生产 = internal/ui corpusclient 包装 storage/parsers；测试 = 同步 fake。
type CorpusIO interface {
	// Scan 扫描语料分类（root 为空串 = 用户设置>legacy>默认 解析）。
	Scan() (storage.CorpusScanResult, error)
	// ListEntries 列出 XQF 分类下全部 .xqf 文件（不解析）。
	ListEntries(categoryPath, categoryName string) ([]storage.CorpusEntry, error)
	// ReadFiles 批量读取棋谱文件字节（坏路径跳过）。
	ReadFiles(paths []string) ([]storage.CorpusFileBytes, error)
	// ParseBatch 批量解析（结果与 files 等长，失败位为 nil）。
	ParseBatch(files []parsers.ParseFileInput) ([]*parsers.ParsedPuzzle, error)
	// PgnIndex 大 PGN 文件按局偏移索引（流式扫描）。
	PgnIndex(path string) ([]storage.PgnIndexEntry, error)
	// ReadPgnGame 读取索引指向的单局文本。
	ReadPgnGame(path string, entry storage.PgnIndexEntry) (string, error)
}

// CorpusDriver 后台驱动面：Go 在独立 goroutine 执行 fn（生产 = go fn()；
// 测试 = 同步执行，集成用例确定性）。
type CorpusDriver interface {
	Go(fn func())
}

// CorpusEvent 完成回执事件（00 §4 `corpus:*` 事件行；经 app 事件总线回主循环
// Apply——requestId 关联取消与迟到丢弃，代次二次收口）。
type CorpusEvent interface{ corpusEvent() }

// CorpusScanDone 语料扫描回执（`corpus:scan`）。
type CorpusScanDone struct {
	Gen  int
	Scan storage.CorpusScanResult
	Err  error
}

func (CorpusScanDone) corpusEvent() {}

// CorpusEntriesDone XQF 分类条目回执（`corpus:entries`）。
type CorpusEntriesDone struct {
	Gen     int
	Entries []storage.CorpusEntry
	Err     error
}

func (CorpusEntriesDone) corpusEvent() {}

// CorpusBatchDone 单批解析回执（`corpus:batch`）：Views 与该批 entries 对齐
// （失败位 nil）；Start = 批起始下标。进度由 Apply 按覆盖范围推进。
type CorpusBatchDone struct {
	Gen        int
	Start      int
	Views      []*ParsedPuzzleView
	BatchCount int
}

func (CorpusBatchDone) corpusEvent() {}

// CorpusPgnIndexDone PGN 按局索引回执（`corpus:pgnindex`）。
type CorpusPgnIndexDone struct {
	Gen   int
	Index []storage.PgnIndexEntry
	Err   error
}

func (CorpusPgnIndexDone) corpusEvent() {}

// CorpusPgnGameDone PGN 单局读取+解析回执（`corpus:pgngame`）；View = nil 表示
// 该局解析失败或无可演示走法。
type CorpusPgnGameDone struct {
	Gen  int
	View *ParsedPuzzleView
	Err  error
}

func (CorpusPgnGameDone) corpusEvent() {}

// CorpusBrowser 语料库浏览状态（主 goroutine 独占）。
type CorpusBrowser struct {
	io     CorpusIO
	driver CorpusDriver
	emit   func(requestID string, ev CorpusEvent)

	// CorpusExists 语料目录是否存在（不存在/为空时展示下载引导）。
	CorpusExists bool
	// CorpusPath 解析后的语料目录绝对路径（缺失引导展示/下载用）。
	CorpusPath       string
	Categories       []storage.CorpusCategory
	SelectedCategory int // -1 = 未选择
	Entries          []storage.CorpusEntry
	// Puzzles 与 Entries 对齐的解析结果（解析中/失败为 nil）。
	Puzzles []*ParsedPuzzleView
	// Progress 批量解析进度（0.0-1.0）；-1 表示不在解析中。
	Progress         float64
	Query            string
	OnlyEndgame      bool
	DifficultyFilter int
	SortMode         CorpusSortMode
	// PgnPath 当前打开的 PGN 大文件分类路径（空 = 非 PGN 视图）。
	PgnPath    string
	PgnSource  string
	PgnIndex   []storage.PgnIndexEntry
	PgnPage    int
	PgnLoading bool
	PgnQuery   string
	// 详情重放视图（XQF 条目或 PGN 单局解析结果）；nil = 列表视图。
	ViewingPuzzle  *ParsedPuzzleView
	ViewingLoading bool
	ViewingError   string

	// gen 代次（上游 generation）：每次新操作递增，迟到回执按代数丢弃。
	gen int
	// opDone 当前操作协作取消通道（beginOp 时关闭旧通道——工作 goroutine 在
	// 批间隙探针处退出；铁律 #G5 的 state 侧收口）。
	opDone chan struct{}
	// opID 当前在途操作 requestId（页面 Dispose 经总线 Cancel 用）。
	opID string
}

// NewCorpusBrowser 创建语料库浏览 store（io/driver/emit 均必传）。
func NewCorpusBrowser(io CorpusIO, driver CorpusDriver, emit func(requestID string, ev CorpusEvent)) *CorpusBrowser {
	return &CorpusBrowser{
		io:               io,
		driver:           driver,
		emit:             emit,
		CorpusExists:     true,
		SelectedCategory: -1,
		Progress:         -1,
		SortMode:         SortName,
	}
}

// InFlightID 当前在途操作 requestId（无在途操作返回空串）。
func (s *CorpusBrowser) InFlightID() string { return s.opID }

// beginOp 开启新操作：递增代次、关闭旧取消通道、记当前 requestId
// （corpusBrowser.ts `const myGen = ++generation`）。
func (s *CorpusBrowser) beginOp(requestID string) {
	s.gen++
	if s.opDone != nil {
		close(s.opDone)
	}
	s.opDone = make(chan struct{})
	s.opID = requestID
}

// staleChan 协作取消探针（工作 goroutine 用值捕获的通道——beginOp 在主
// goroutine close+重建通道字段，goroutine 不得再读字段，-race 口径）。
func staleChan(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

// Load 扫描语料目录（root 空串 = 按 用户设置>legacy>默认 解析）；目录缺失或
// 为空视同缺失，回到下载引导（corpusBrowser.ts load）。
func (s *CorpusBrowser) Load(requestID string) {
	s.beginOp(requestID)
	gen := s.gen
	done := s.opDone
	s.driver.Go(func() {
		scan, err := s.io.Scan()
		if staleChan(done) {
			return
		}
		if err != nil {
			scan = storage.CorpusScanResult{}
		}
		s.emit(requestID, CorpusScanDone{Gen: gen, Scan: scan})
	})
}

// SelectCategory 选择分类：XQF 分类清视图状态并分批懒解析；PGN 分类只记录
// 选中（页面层跳转 openPgnCategory）（corpusBrowser.ts selectCategory）。
func (s *CorpusBrowser) SelectCategory(requestID string, index int) {
	if index < 0 || index >= len(s.Categories) {
		return
	}
	category := s.Categories[index]
	s.beginOp(requestID)
	gen := s.gen
	// 切回 XQF 分类必须清掉 PGN 大文件视图状态，否则右侧面板停留在 PGN 视图
	//（pgnPath 残留）——上游"1/4 分类切换后不刷新"回归修复语义。
	s.SelectedCategory = index
	s.Entries = nil
	s.Puzzles = nil
	s.Progress = -1
	s.PgnPath = ""
	s.PgnIndex = nil
	s.PgnPage = 0
	s.ViewingPuzzle = nil
	if category.Kind != storage.KindXQFDirectory {
		// PGN 大文件分类不批量解析。
		return
	}
	done := s.opDone
	s.driver.Go(func() {
		entries, err := s.io.ListEntries(category.Path, category.Name)
		if staleChan(done) {
			return
		}
		if err != nil {
			entries = []storage.CorpusEntry{}
		}
		s.emit(requestID, CorpusEntriesDone{Gen: gen, Entries: entries})
		// 分批解析：每批 128，渐进展示（corpus_browser_vm.dart:226-245）。
		total := len(entries)
		puzzles := make([]*ParsedPuzzleView, total)
		for start := 0; start < total; start += ParseBatchSize {
			if staleChan(done) {
				return
			}
			end := start + ParseBatchSize
			if end > total {
				end = total
			}
			chunk := entries[start:end]
			views := s.parseChunk(requestID, gen, chunk)
			if staleChan(done) {
				return
			}
			for i, v := range views {
				if v != nil {
					puzzles[start+i] = v
				}
			}
			s.emit(requestID, CorpusBatchDone{Gen: gen, Start: start, Views: views, BatchCount: len(chunk)})
		}
	})
}

// parseChunk 读文件 + 批量解析一批（readFiles 失败 = 空字节表；parseBatch
// 失败 = 该批保持 nil）——批次推进不因错误中断（corpusBrowser.ts catch 语义）。
func (s *CorpusBrowser) parseChunk(requestID string, gen int, chunk []storage.CorpusEntry) []*ParsedPuzzleView {
	views := make([]*ParsedPuzzleView, len(chunk))
	paths := make([]string, len(chunk))
	for i, e := range chunk {
		paths[i] = e.Path
	}
	bytes, err := s.io.ReadFiles(paths)
	if err != nil || len(bytes) == 0 {
		return views
	}
	byteByPath := make(map[string][]byte, len(bytes))
	for _, b := range bytes {
		byteByPath[b.Path] = b.Bytes
	}
	files := make([]parsers.ParseFileInput, 0, len(chunk))
	fileChunkIndex := make([]int, 0, len(chunk))
	for ci, e := range chunk {
		if b, ok := byteByPath[e.Path]; ok {
			files = append(files, parsers.ParseFileInput{Name: e.Path, Source: e.Source, Bytes: b})
			fileChunkIndex = append(fileChunkIndex, ci)
		}
	}
	if len(files) == 0 {
		return views
	}
	parsed, err := s.io.ParseBatch(files)
	if err != nil {
		return views // 批次失败：该批保持 nil，继续下一批
	}
	// 结果按 files 顺序回填到 entries 对应下标（ReadFiles 可能跳过坏路径）。
	for i, p := range parsed {
		ci := fileChunkIndex[i]
		views[ci] = ToView(p, chunk[ci].Source)
	}
	return views
}

// OpenPgnCategory 打开 PGN 大文件分类：建立按局索引（后台流式扫描）
// （corpusBrowser.ts openPgnCategory）。
func (s *CorpusBrowser) OpenPgnCategory(requestID string, index int) {
	if index < 0 || index >= len(s.Categories) {
		return
	}
	category := s.Categories[index]
	if category.Kind != storage.KindPgnFile {
		return
	}
	s.beginOp(requestID)
	gen := s.gen
	s.SelectedCategory = index
	s.PgnPath = category.Path
	s.PgnSource = category.Source
	s.PgnIndex = nil
	s.PgnPage = 0
	s.PgnLoading = true
	s.ViewingPuzzle = nil
	done := s.opDone
	s.driver.Go(func() {
		index, err := s.io.PgnIndex(category.Path)
		if staleChan(done) {
			return
		}
		if err != nil {
			index = []storage.PgnIndexEntry{}
		}
		s.emit(requestID, CorpusPgnIndexDone{Gen: gen, Index: index})
	})
}

// OpenPgnGame 打开 PGN 大文件中索引指向的单局：读取 + 解析进详情；切分类/
// 换局后迟到结果按代数丢弃（corpusBrowser.ts openPgnGame）。
func (s *CorpusBrowser) OpenPgnGame(requestID string, entry storage.PgnIndexEntry) {
	if s.PgnPath == "" {
		return
	}
	pgnPath, pgnSource := s.PgnPath, s.PgnSource
	s.beginOp(requestID) // 迟到结果按代数丢弃（00 §3.2）
	gen := s.gen
	s.ViewingLoading = true
	s.ViewingError = ""
	s.ViewingPuzzle = nil
	done := s.opDone
	s.driver.Go(func() {
		text, err := s.io.ReadPgnGame(pgnPath, entry)
		if staleChan(done) {
			return
		}
		if err != nil {
			s.emit(requestID, CorpusPgnGameDone{Gen: gen, View: nil, Err: err})
			return
		}
		parsed, err := s.io.ParseBatch([]parsers.ParseFileInput{{
			Name:   pgnGameFileName(entry.Offset),
			Source: pgnSource,
			Bytes:  []byte(text),
		}})
		if staleChan(done) {
			return
		}
		var view *ParsedPuzzleView
		if err == nil && len(parsed) > 0 {
			view = ToView(parsed[0], pgnSource)
		}
		s.emit(requestID, CorpusPgnGameDone{Gen: gen, View: view})
	})
}

// OpenXqfPuzzle 以 entries/puzzles 下标打开详情（解析失败位不打开）。
func (s *CorpusBrowser) OpenXqfPuzzle(index int) {
	if index < 0 || index >= len(s.Entries) {
		return
	}
	if s.Puzzles[index] == nil {
		return
	}
	s.ViewingPuzzle = s.Puzzles[index]
	s.ViewingError = ""
	s.ViewingLoading = false
}

// ClosePuzzle 关闭详情回列表。
func (s *CorpusBrowser) ClosePuzzle() {
	s.ViewingPuzzle = nil
	s.ViewingError = ""
	s.ViewingLoading = false
}

// SetQuery 搜索（XQF 列表）。
func (s *CorpusBrowser) SetQuery(q string) { s.Query = q }

// SetOnlyEndgame 仅看残局开关。
func (s *CorpusBrowser) SetOnlyEndgame(v bool) { s.OnlyEndgame = v }

// SetDifficultyFilter 难度筛选（0 = 全部）。
func (s *CorpusBrowser) SetDifficultyFilter(d int) { s.DifficultyFilter = d }

// SetSortMode 排序方式。
func (s *CorpusBrowser) SetSortMode(m CorpusSortMode) { s.SortMode = m }

// SetPgnQuery PGN 搜索（重置回第一页）。
func (s *CorpusBrowser) SetPgnQuery(q string) {
	s.PgnQuery = q
	s.PgnPage = 0
}

// Apply 完成回执应用（主 goroutine；代次不匹配 = 迟到回执，丢弃）。
func (s *CorpusBrowser) Apply(ev CorpusEvent) {
	switch e := ev.(type) {
	case CorpusScanDone:
		if e.Gen != s.gen {
			return
		}
		s.CorpusPath = e.Scan.Root
		s.Categories = e.Scan.Categories
		s.SelectedCategory = -1
		s.Entries = nil
		s.Puzzles = nil
		s.Progress = -1
		s.PgnPath = ""
		s.PgnIndex = nil
		s.PgnPage = 0
		if !e.Scan.Exists || len(e.Scan.Categories) == 0 {
			// 目录缺失或为空（下载失败残留半成品等）：视同缺失，回到下载引导。
			s.CorpusExists = false
			return
		}
		s.CorpusExists = true
		// 上游 load 尾部 await selectCategory(0)。
		s.SelectCategory(s.opID, 0)
	case CorpusEntriesDone:
		if e.Gen != s.gen {
			return
		}
		s.Entries = e.Entries
		s.Puzzles = make([]*ParsedPuzzleView, len(e.Entries))
		if len(e.Entries) > 0 {
			s.Progress = 0
		} else {
			s.Progress = -1
		}
	case CorpusBatchDone:
		if e.Gen != s.gen {
			return
		}
		for i, v := range e.Views {
			if v != nil {
				s.Puzzles[e.Start+i] = v
			}
		}
		if len(s.Entries) > 0 {
			s.Progress = float64(e.Start+e.BatchCount) / float64(len(s.Entries))
			if e.Start+e.BatchCount >= len(s.Entries) {
				s.Progress = -1
			}
		}
	case CorpusPgnIndexDone:
		if e.Gen != s.gen {
			return
		}
		s.PgnIndex = e.Index
		s.PgnLoading = false
	case CorpusPgnGameDone:
		if e.Gen != s.gen {
			return
		}
		if e.View == nil {
			s.ViewingLoading = false
			if e.Err != nil {
				s.ViewingError = "单局读取失败"
			} else {
				s.ViewingError = "该局解析失败或无可演示走法"
			}
			return
		}
		s.ViewingPuzzle = e.View
		s.ViewingLoading = false
	}
}
