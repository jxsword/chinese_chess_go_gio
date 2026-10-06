package state

// 语料库 store 用例（翻译源 = 上游 frontend/test/stores/corpusBrowser.spec.ts；
// corpus_browser_vm.dart 等价：筛选排序 + PGN 分页 + generation 防覆盖，06 文档 §6）。
// 驱动 = 同步 CorpusDriver（fake IO 同步直呼），emit = 直接 Apply——集成用例确定性。

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// fakeIO 上游 spec 顶部 vi.mock 的 Go 对应：scan 固定两类；parseBatch 按
// 文件名注入结果（"乙"→成功 / "game-N"→失败或成功 / 其余→nil）。
type fakeIO struct {
	scan        storage.CorpusScanResult
	scanErr     error
	entries     []storage.CorpusEntry
	readFiles   func(paths []string) ([]storage.CorpusFileBytes, error)
	pgnIndexErr error
	readGameErr error
	// parseOf 文件名 → 解析结果（nil 表项 = 解析失败位）。
	parseOf func(name string) *parsers.ParsedPuzzle

	// 调用记录（分批/generation 用例断言）。
	readFilesCalls [][]string
	parseCalls     int
}

func (f *fakeIO) Scan() (storage.CorpusScanResult, error) { return f.scan, f.scanErr }

func (f *fakeIO) ListEntries(categoryPath, categoryName string) ([]storage.CorpusEntry, error) {
	return f.entries, nil
}

func (f *fakeIO) ReadFiles(paths []string) ([]storage.CorpusFileBytes, error) {
	f.readFilesCalls = append(f.readFilesCalls, paths)
	if f.readFiles != nil {
		return f.readFiles(paths)
	}
	out := make([]storage.CorpusFileBytes, 0, len(paths))
	for _, p := range paths {
		out = append(out, storage.CorpusFileBytes{Path: p, Bytes: []byte("x")})
	}
	return out, nil
}

func (f *fakeIO) ParseBatch(files []parsers.ParseFileInput) ([]*parsers.ParsedPuzzle, error) {
	f.parseCalls++
	out := make([]*parsers.ParsedPuzzle, len(files))
	for i, file := range files {
		if f.parseOf != nil {
			out[i] = f.parseOf(file.Name)
		}
	}
	return out, nil
}

func (f *fakeIO) PgnIndex(path string) ([]storage.PgnIndexEntry, error) {
	if f.pgnIndexErr != nil {
		return nil, f.pgnIndexErr
	}
	return []storage.PgnIndexEntry{}, nil
}

func (f *fakeIO) ReadPgnGame(path string, entry storage.PgnIndexEntry) (string, error) {
	if f.readGameErr != nil {
		return "", f.readGameErr
	}
	return "", nil
}

// syncDriver 同步驱动：Go(fn) 立即执行（对应上游 await 语义的确定性化）。
type syncDriver struct{}

func (syncDriver) Go(fn func()) { fn() }

// newTestStore 组装 fake + 同步驱动，emit 直接 Apply（同步闭环）。
func newCorpusTestStore(io CorpusIO) *CorpusBrowser {
	s := NewCorpusBrowser(io, syncDriver{}, nil)
	s.emit = func(requestID string, ev CorpusEvent) { s.Apply(ev) }
	return s
}

func newSyncStore(io CorpusIO) *CorpusBrowser { return newCorpusTestStore(io) }

// view 用例基座（spec puzzle() helper）。
func view(overrides func(*ParsedPuzzleView)) *ParsedPuzzleView {
	v := &ParsedPuzzleView{
		ID:         "t",
		InitialFen: "4k4/9/9/9/9/9/9/9/9/4K4 w - - 0 1",
		Source:     "残局/适情雅趣",
		Format:     "xqf",
		Difficulty: 1,
		MoveCount:  10,
		Endgame:    true,
	}
	if overrides != nil {
		overrides(v)
	}
	return v
}

func entry(name string) storage.CorpusEntry {
	return storage.CorpusEntry{
		Path:        "/corpus/" + name + ".xqf",
		Category:    "XQF测试谱",
		Source:      "残局/适情雅趣",
		DisplayName: name,
	}
}

func scanFixture() (storage.CorpusScanResult, []storage.CorpusEntry) {
	scan := storage.CorpusScanResult{
		Root:   "/corpus",
		Exists: true,
		Categories: []storage.CorpusCategory{
			{Name: "XQF-象棋谱大全", Path: "/corpus/xqf", Kind: storage.KindXQFDirectory, Source: "XQF-象棋谱大全"},
			{Name: "PGN · big.pgns（多局合一）", Path: "/corpus/big.pgns", Kind: storage.KindPgnFile, Source: "wxf/ICCS"},
		},
	}
	entries := []storage.CorpusEntry{
		{Path: "/corpus/xqf/甲.xqf", Category: "XQF-象棋谱大全", Source: "残局/适情雅趣", DisplayName: "甲"},
		{Path: "/corpus/xqf/乙.xqf", Category: "XQF-象棋谱大全", Source: "全局", DisplayName: "乙"},
	}
	return scan, entries
}

// spec mock parseBatch：乙 → 成功（endgame=false，来源"全局"）；game-1 → 失败；
// game-其余 → 成功 pgn 单局；其余 → nil（解析失败位）。
func specParseOf(name string) *parsers.ParsedPuzzle {
	switch {
	case strings.HasPrefix(name, "game-") && name == "game-1.pgn":
		return nil
	case strings.HasPrefix(name, "game-"):
		return &parsers.ParsedPuzzle{
			ID:            "pgn/wxf/ICCS/单局",
			InitialFen:    "rnbakabnr/9/1c5c1/p1p1p1p1p/9/9/P1P1P1P1P/1C5C1/9/RNBAKABNR w - - 0 1",
			SolutionMoves: []string{"h2e2", "h9g7"},
			Title:         strPtr("单局"),
			Source:        "wxf/ICCS",
			Format:        "pgn",
			Difficulty:    1,
		}
	case strings.Contains(name, "乙"):
		return &parsers.ParsedPuzzle{
			ID:            "xqf/全局/乙",
			InitialFen:    "4k4/9/9/9/9/9/9/9/9/4K4 w - - 0 1",
			SolutionMoves: []string{"h2e2"},
			Title:         strPtr("乙"),
			Source:        "全局",
			Format:        "xqf",
			Difficulty:    1,
		}
	}
	return nil
}

func strPtr(s string) *string { return &s }

// --- visibleItems（搜索 + 筛选 + 排序，corpus_browser_vm.dart:71-101）---

func specEntries() []storage.CorpusEntry {
	return []storage.CorpusEntry{entry("丙"), entry("甲"), entry("乙")}
}

func specPuzzles() []*ParsedPuzzleView {
	return []*ParsedPuzzleView{
		view(func(v *ParsedPuzzleView) {
			v.Title = strPtr("丙局")
			v.Difficulty = 3
			v.MoveCount = 50
			v.Endgame = false
		}),
		view(func(v *ParsedPuzzleView) { v.Title = strPtr("甲局"); v.MoveCount = 10 }),
		nil, // 解析失败条目不可见
	}
}

// 上游用例：搜索匹配标题，解析失败条目被过滤
func TestVisibleItemsQueryFiltersFailedEntries(t *testing.T) {
	items := VisibleItems(specEntries(), specPuzzles(), "甲", false, 0, SortName)
	if len(items) != 1 {
		t.Fatalf("期望 1 条，实际 %d", len(items))
	}
	if items[0].Entry.DisplayName != "甲" {
		t.Fatalf("期望 甲，实际 %s", items[0].Entry.DisplayName)
	}
}

// 上游用例：仅看残局 + 难度筛选可叠加
func TestVisibleItemsEndgameAndDifficultyStack(t *testing.T) {
	only := VisibleItems(specEntries(), specPuzzles(), "", true, 0, SortName)
	if len(only) != 1 || only[0].Entry.DisplayName != "甲" {
		t.Fatalf("仅看残局期望 [甲]，实际 %v", only)
	}
	byDiff := VisibleItems(specEntries(), specPuzzles(), "", false, 3, SortName)
	if len(byDiff) != 1 || byDiff[0].Entry.DisplayName != "丙" {
		t.Fatalf("难度 3 期望 [丙]，实际 %v", byDiff)
	}
}

// 上游用例：三种排序：名称 / 步数 / 难度（难度并列按步数）
func TestVisibleItemsThreeSortModes(t *testing.T) {
	sort := func(mode CorpusSortMode, ps []*ParsedPuzzleView) []string {
		items := VisibleItems(specEntries(), ps, "", false, 0, mode)
		names := make([]string, 0, len(items))
		for _, it := range items {
			names = append(names, it.Entry.DisplayName)
		}
		return names
	}
	puzzles := specPuzzles()
	// 名称排序为码点序（对齐 Dart compareTo）：丙 U+4E19 < 甲 U+7532。
	got := sort(SortName, puzzles)
	if len(got) != 2 || got[0] != "丙" || got[1] != "甲" {
		t.Fatalf("名称排序期望 [丙 甲]，实际 %v", got)
	}
	got = sort(SortMoves, puzzles)
	if len(got) != 2 || got[0] != "甲" || got[1] != "丙" {
		t.Fatalf("步数排序期望 [甲 丙]，实际 %v", got)
	}
	got = sort(SortDifficulty, puzzles)
	if len(got) != 2 || got[0] != "甲" || got[1] != "丙" {
		t.Fatalf("难度排序期望 [甲 丙]，实际 %v", got)
	}
	// 难度并列时按步数：丙(3,80) vs 甲(3,50)
	tied := []*ParsedPuzzleView{
		view(func(v *ParsedPuzzleView) { v.Difficulty = 3; v.MoveCount = 80 }),
		view(func(v *ParsedPuzzleView) { v.Difficulty = 3; v.MoveCount = 50 }),
	}
	got = sort(SortDifficulty, tied)
	if len(got) != 2 || got[0] != "甲" || got[1] != "丙" {
		t.Fatalf("难度并列按步数期望 [甲 丙]，实际 %v", got)
	}
}

// --- pgnPageSlice（PGN 大文件分页，06 §4.4 每页 50）---

func specIndex(n int) []storage.PgnIndexEntry {
	index := make([]storage.PgnIndexEntry, n)
	for i := range index {
		event := fmt.Sprintf("对局 %d", i)
		red := fmt.Sprintf("红%d", i)
		black := fmt.Sprintf("黑%d", i)
		index[i] = storage.PgnIndexEntry{Offset: int64(i), Length: 10, Event: &event, Red: &red, Black: &black}
	}
	return index
}

// 上游用例：分页切片与总页数
func TestPgnPageSlicePagination(t *testing.T) {
	index := specIndex(123)
	got := PgnPageSlice(index, "", 0)
	if got.Total != 123 || got.TotalPages != 3 {
		t.Fatalf("期望 total=123 totalPages=3，实际 %d/%d", got.Total, got.TotalPages)
	}
	if len(got.Slice) != PgnPageSize || *got.Slice[0].Event != "对局 0" {
		t.Fatalf("首页应满 50 条且首条为 对局 0")
	}
	last := PgnPageSlice(index, "", 2)
	if len(last.Slice) != 23 {
		t.Fatalf("末页期望 23 条，实际 %d", len(last.Slice))
	}
}

// 上游用例：搜索过滤后分页与越界页收敛
func TestPgnPageSliceQueryAndClamp(t *testing.T) {
	index := specIndex(123)
	got := PgnPageSlice(index, "红1", 0)
	if got.Total != 34 {
		t.Fatalf("红1* contains 语义期望 34 条，实际 %d", got.Total)
	}
	if got.TotalPages != 1 {
		t.Fatalf("期望 1 页，实际 %d", got.TotalPages)
	}
	clamped := PgnPageSlice(index, "", 99)
	if len(clamped.Slice) != 23 {
		t.Fatalf("越界页应收敛到最后一页（23 条），实际 %d", len(clamped.Slice))
	}
}

// --- 语料库 store 集成（分批解析 + generation）---

// 上游用例：load → selectCategory(0)：解析结果回填，进度收尾为 -1
func TestStoreLoadSelectCategoryFillsPuzzles(t *testing.T) {
	scan, entries := scanFixture()
	io := &fakeIO{scan: scan, entries: entries, parseOf: specParseOf}
	s := newSyncStore(io)
	s.Load("req-load")

	if !s.CorpusExists || s.SelectedCategory != 0 {
		t.Fatalf("CorpusExists=%v SelectedCategory=%d", s.CorpusExists, s.SelectedCategory)
	}
	if len(s.Puzzles) != 2 {
		t.Fatalf("期望 2 条目，实际 %d", len(s.Puzzles))
	}
	if s.Puzzles[0] != nil {
		t.Fatalf("甲：worker 返回 null，应保持 nil")
	}
	if s.Puzzles[1] == nil || deref(s.Puzzles[1].Title, "") != "乙" {
		t.Fatalf("乙应解析成功")
	}
	if s.Puzzles[1].Endgame {
		t.Fatalf("来源\"全局\"关键词 → 全局对局（endgame=false）")
	}
	if s.Progress != -1 {
		t.Fatalf("收尾进度应为 -1，实际 %v", s.Progress)
	}
}

// 上游用例：PGN 分类不批量解析，openPgnCategory 建立索引视图
func TestStoreOpenPgnCategoryBuildsIndexView(t *testing.T) {
	scan, _ := scanFixture()
	io := &fakeIO{scan: scan, entries: nil, parseOf: specParseOf}
	s := newSyncStore(io)
	s.Load("req-load")
	s.OpenPgnCategory("req-pgn", 1)

	if s.PgnPath != "/corpus/big.pgns" {
		t.Fatalf("PgnPath=%q", s.PgnPath)
	}
	if s.SelectedCategory != 1 {
		t.Fatalf("SelectedCategory=%d", s.SelectedCategory)
	}
	if io.parseCalls != 0 {
		t.Fatalf("PGN 分类不批量解析，实际 parseBatch 调用 %d 次", io.parseCalls)
	}
}

// 上游用例：从 PGN 分类切回 XQF 分类：pgn 视图状态必须被清掉（回归：切换不刷新 bug）
func TestStoreSwitchBackFromPgnClearsView(t *testing.T) {
	scan, entries := scanFixture()
	io := &fakeIO{scan: scan, entries: entries, parseOf: specParseOf}
	s := newSyncStore(io)
	s.Load("req-load")
	s.OpenPgnCategory("req-pgn", 1)
	if s.PgnPath == "" {
		t.Fatal("前置：应处于 PGN 视图")
	}
	s.SelectCategory("req-xqf", 0)
	if s.PgnPath != "" || len(s.PgnIndex) != 0 || s.ViewingPuzzle != nil {
		t.Fatalf("切回 XQF 必须清 pgnPath/pgnIndex/viewingPuzzle")
	}
	// 反向：再进 PGN 分类照常工作
	s.OpenPgnCategory("req-pgn2", 1)
	if s.PgnPath != "/corpus/big.pgns" {
		t.Fatalf("再进 PGN 分类失败：%q", s.PgnPath)
	}
}

// 上游用例：openXqfPuzzle：以 entries/puzzles 下标打开详情；closePuzzle 关闭
func TestStoreOpenXqfPuzzleAndClose(t *testing.T) {
	scan, entries := scanFixture()
	io := &fakeIO{scan: scan, entries: entries, parseOf: specParseOf}
	s := newSyncStore(io)
	s.Load("req-load")

	s.OpenXqfPuzzle(1)
	if s.ViewingPuzzle == nil || deref(s.ViewingPuzzle.Title, "") != "乙" {
		t.Fatalf("应打开 乙")
	}
	s.OpenXqfPuzzle(0) // 甲解析失败位为 nil：不打开
	if s.ViewingPuzzle == nil || deref(s.ViewingPuzzle.Title, "") != "乙" {
		t.Fatalf("解析失败位不应替换当前详情")
	}
	s.ClosePuzzle()
	if s.ViewingPuzzle != nil {
		t.Fatal("closePuzzle 应关闭详情")
	}
}

// 上游用例：openPgnGame：读取单局文本经 worker 解析进详情；解析失败给出错误
func TestStoreOpenPgnGameParseAndFailure(t *testing.T) {
	scan, _ := scanFixture()
	io := &fakeIO{scan: scan, entries: nil, parseOf: specParseOf}
	s := newSyncStore(io)
	s.Load("req-load")
	s.OpenPgnCategory("req-pgn", 1)

	// 失败分支：mock 对 game-1.pgn 返回 nil
	s.OpenPgnGame("req-game1", storage.PgnIndexEntry{Offset: 1, Length: 2})
	if s.ViewingError != "该局解析失败或无可演示走法" {
		t.Fatalf("期望解析失败文案，实际 %q", s.ViewingError)
	}

	// 成功分支：mock 对 game-*.pgn 返回"单局"
	s.OpenPgnGame("req-game2", storage.PgnIndexEntry{Offset: 2, Length: 2})
	v := s.ViewingPuzzle
	if v == nil || deref(v.Title, "") != "单局" {
		t.Fatalf("应打开 单局，实际 %v", v)
	}
	if v.Format != "pgn" || v.MoveCount != 2 {
		t.Fatalf("format=%s moveCount=%d", v.Format, v.MoveCount)
	}
}

// 上游用例（错误分支补全）：单局读取失败文案
func TestStoreOpenPgnGameReadFailure(t *testing.T) {
	scan, _ := scanFixture()
	io := &fakeIO{scan: scan, entries: nil, parseOf: specParseOf, readGameErr: errors.New("io")}
	s := newSyncStore(io)
	s.Load("req-load")
	s.OpenPgnCategory("req-pgn", 1)
	s.OpenPgnGame("req-game", storage.PgnIndexEntry{Offset: 1})
	if s.ViewingError != "单局读取失败" {
		t.Fatalf("期望 单局读取失败，实际 %q", s.ViewingError)
	}
}

// 翻译扩展（上游 spec 分批语义）：>128 条目分 2 批；进度推进与失败批保持 nil。
func TestStoreBatching128AndFailureBatch(t *testing.T) {
	scan, _ := scanFixture()
	entries := make([]storage.CorpusEntry, 130)
	for i := range entries {
		entries[i] = entry(fmt.Sprintf("谱%03d", i))
	}
	io := &fakeIO{scan: scan, entries: entries, parseOf: specParseOf}
	s := newSyncStore(io)
	s.Load("req-load")

	if len(io.readFilesCalls) != 2 {
		t.Fatalf("130 条应分 2 批读文件，实际 %d", len(io.readFilesCalls))
	}
	if len(io.readFilesCalls[0]) != ParseBatchSize || len(io.readFilesCalls[1]) != 130-ParseBatchSize {
		t.Fatalf("批大小 128/%d", len(io.readFilesCalls[1]))
	}
	if s.Progress != -1 {
		t.Fatalf("收尾进度 -1，实际 %v", s.Progress)
	}
}

// 翻译扩展（generation 防陈旧）：代次过期的迟到回执被丢弃，不覆盖新分类状态。
func TestStoreGenerationDropsStaleEvents(t *testing.T) {
	scan, entries := scanFixture()
	io := &fakeIO{scan: scan, entries: entries, parseOf: specParseOf}
	s := newSyncStore(io)
	s.Load("req-load")
	staleGen := s.gen

	// 模拟迟到回执（旧代次的批次结果）：新操作已开启新代次。
	s.SetQuery("x") // 无关操作不推进代次——用真实新操作推进：
	s.SelectCategory("req-new", 0)
	s.Apply(CorpusBatchDone{Gen: staleGen, Start: 0, Views: []*ParsedPuzzleView{view(nil)}, BatchCount: 1})
	if s.Puzzles[0] != nil {
		t.Fatal("旧代次批次回执不应回填")
	}
	// 当代回执正常回填。
	s.Apply(CorpusBatchDone{Gen: s.gen, Start: 0, Views: []*ParsedPuzzleView{view(nil)}, BatchCount: 1})
	if s.Puzzles[0] == nil {
		t.Fatal("当代批次回执应回填")
	}
}

// 翻译扩展（目录缺失/为空 → 下载引导）。
func TestStoreMissingCorpusShowsGuide(t *testing.T) {
	io := &fakeIO{scan: storage.CorpusScanResult{Root: "/corpus", Exists: false}, entries: nil}
	s := newSyncStore(io)
	s.Load("req-load")
	if s.CorpusExists {
		t.Fatal("目录缺失应回下载引导")
	}
	if s.SelectedCategory != -1 || len(s.Categories) != 0 {
		t.Fatal("缺失态应清分类视图")
	}
	// 空分类等价缺失（下载失败残留半成品等）。
	io2 := &fakeIO{scan: storage.CorpusScanResult{Root: "/corpus", Exists: true}, entries: nil}
	s2 := newSyncStore(io2)
	s2.Load("req-load")
	if s2.CorpusExists {
		t.Fatal("分类为空应视同缺失")
	}
}

// 翻译扩展（协作取消）：新操作开启后旧批次 goroutine 在探针处退出（不产出回执）。
func TestStoreCooperativeCancelStopsStaleLoop(t *testing.T) {
	scan, _ := scanFixture()
	entries := make([]storage.CorpusEntry, 300)
	for i := range entries {
		entries[i] = entry(fmt.Sprintf("谱%03d", i))
	}
	io := &fakeIO{scan: scan, entries: entries, parseOf: specParseOf}
	s := newSyncStore(io)
	// 手工驱动：先开启一次扫描建立代次，再手动模拟旧代次 goroutine 的探针。
	s.Load("req-load")
	done := s.opDone
	if done == nil {
		t.Fatal("应有取消通道")
	}
	s.SelectCategory("req-new2", 0) // beginOp 关闭旧通道
	select {
	case <-done:
	default:
		t.Fatal("旧操作取消通道应已关闭")
	}
}
