// Command poc M0' POC 排险演示入口（design_docs/11 §3 M0' 表 T0'.2~T0'.5）。
//
// 用法：go run ./cmd/poc <demo>
//
//	demo ∈ {board, anim, ime, list}
//
// 各 demo 独立开窗，互不依赖正式页面；POC 实现位于 internal/ui/poc_*.go，
// 正式里程碑重写不继承。
package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"gioui.org/unit"

	"github.com/jxsword/chinese_chess_go_gio/internal/app"
	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	if os.Args[1] == "corpus" {
		demoCorpus()
		return
	}
	if os.Args[1] == "records" {
		demoRecords()
		return
	}
	page, title := demo(os.Args[1])
	if page == nil {
		fmt.Fprintf(os.Stderr, "poc: 未知 demo %q\n", os.Args[1])
		usage()
	}
	w := app.OpenWindow(app.WindowConfig{Title: title, Width: unit.Dp(720), Height: unit.Dp(920)})
	if binder, ok := page.(interface{ BindEvents(func(any)) }); ok {
		binder.BindEvents(func(ev any) { w.Emit(app.AppEvent{Payload: ev}) })
	}
	if err := w.Run(page); err != nil {
		fmt.Fprintln(os.Stderr, "poc:", err)
		os.Exit(1)
	}
}

// demoRecords M5' 记录库页渲染冒烟（POC 专用）：内嵌一条对局 + 一条残局
// （多解）验证列表/详情/线路切换/启动器渲染。
func demoRecords() {
	w := app.OpenWindow(app.WindowConfig{Title: "M5' records", Width: unit.Dp(1024), Height: unit.Dp(768)})
	seq := 0
	repo := &recordsFakeRepo{w: w, seq: &seq, data: recordsSample()}
	page := ui.NewRecordLibraryPage(ui.GameEnv{
		Records: repo,
		Emit: func(id string, payload any, err error) {
			w.Emit(app.AppEvent{RequestID: id, Payload: payload, Err: err})
		},
		Cancel:       w.Cancel,
		NewRequestID: func(prefix string) string { seq++; return fmt.Sprintf("%s-%d", prefix, seq) },
	}, ui.RecordLibraryHooks{})
	if err := w.Run(page); err != nil {
		fmt.Fprintln(os.Stderr, "poc:", err)
		os.Exit(1)
	}
}

// recordsFakeRepo 内嵌样例记录（同步结算 goroutine——回执经 w.Emit）。
type recordsFakeRepo struct {
	w    *app.Window
	seq  *int
	data []storage.GameRecord
}

func (r *recordsFakeRepo) emit(id string, payload any) {
	go func() { r.w.Emit(app.AppEvent{RequestID: id, Payload: payload}) }()
}

func (r *recordsFakeRepo) RecordsListAsync(requestID string) {
	out := make([]storage.GameRecordSummary, 0, len(r.data))
	for i := range r.data {
		d := &r.data[i]
		solved := "solved"
		if d.Mode == "humanVsHuman" {
			solved = "none"
		}
		out = append(out, storage.GameRecordSummary{ID: d.ID, Title: d.Title, Mode: d.Mode, Result: d.Result, SolveStatus: &solved, CreatedAt: d.CreatedAt})
	}
	r.emit(requestID, ui.RecordsListDone{RequestID: requestID, Records: out})
}

func (r *recordsFakeRepo) RecordsGetAsync(requestID string, id int64) {
	for i := range r.data {
		if r.data[i].ID == id {
			r.emit(requestID, ui.RecordGetDone{RequestID: requestID, Record: &r.data[i]})
			return
		}
	}
	r.emit(requestID, ui.RecordGetDone{RequestID: requestID, Err: errors.New("missing")})
}

func (r *recordsFakeRepo) RecordsSaveAsync(requestID string, record state.GameRecordData) {}
func (r *recordsFakeRepo) RecordsDeleteAsync(requestID string, id int64) {
	for i := range r.data {
		if r.data[i].ID == id {
			r.data = append(r.data[:i], r.data[i+1:]...)
			break
		}
	}
	r.emit(requestID, ui.RecordDeleteDone{RequestID: requestID})
}

// demoCorpus M5' 语料库页渲染冒烟（POC 专用，正式页面走 go run .）：
// CC_CORPUS_ROOT 指向语料根目录（空 = 未找到本地语料的下载引导分支）；
// CC_GIO_SYNTH_PGN=N 注入 N 局合成 PGN 索引（长列表性能实测，POC-4 口径）。
func demoCorpus() {
	w := app.OpenWindow(app.WindowConfig{Title: "M5' corpus", Width: unit.Dp(1024), Height: unit.Dp(768)})
	seq := 0
	env := ui.CorpusEnv{
		Emit: func(id string, payload any, err error) {
			w.Emit(app.AppEvent{RequestID: id, Payload: payload, Err: err})
		},
		Cancel:       w.Cancel,
		NewRequestID: func(prefix string) string { seq++; return fmt.Sprintf("%s-%d", prefix, seq) },
		Root:         func() string { return os.Getenv("CC_CORPUS_ROOT") },
	}
	if n := synthCount(); n > 0 {
		env.IO = synthIO{n: n}
	}
	page := ui.NewCorpusPage(env, ui.CorpusHooks{})
	if err := w.Run(page); err != nil {
		fmt.Fprintln(os.Stderr, "poc:", err)
		os.Exit(1)
	}
}

func synthCount() int {
	n, _ := strconv.Atoi(os.Getenv("CC_GIO_SYNTH_PGN"))
	return n
}

// synthIO 合成 PGN 索引（性能实测专用）：单一 PGN 分类 + N 局确定性条目。
type synthIO struct{ n int }

func (s synthIO) Scan() (storage.CorpusScanResult, error) {
	return storage.CorpusScanResult{
		Root:   "/synth",
		Exists: true,
		Categories: []storage.CorpusCategory{
			{Name: "合成 14 万局", Path: "/synth/big.pgns", Kind: storage.KindPgnFile, Source: "synth/ICCS"},
		},
	}, nil
}

func (synthIO) ListEntries(categoryPath, categoryName string) ([]storage.CorpusEntry, error) {
	return nil, nil
}

func (synthIO) ReadFiles(paths []string) ([]storage.CorpusFileBytes, error) {
	return nil, nil
}

func (synthIO) ParseBatch(files []parsers.ParseFileInput) ([]*parsers.ParsedPuzzle, error) {
	return nil, nil
}

func (s synthIO) PgnIndex(path string) ([]storage.PgnIndexEntry, error) {
	out := make([]storage.PgnIndexEntry, s.n)
	for i := range out {
		event := fmt.Sprintf("对局 %d", i)
		red := fmt.Sprintf("红%d", i)
		black := fmt.Sprintf("黑%d", i)
		out[i] = storage.PgnIndexEntry{Offset: int64(i) * 100, Length: 100, Event: &event, Red: &red, Black: &black}
	}
	return out, nil
}

func (synthIO) ReadPgnGame(path string, entry storage.PgnIndexEntry) (string, error) {
	return "", nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "用法: go run ./cmd/poc <board|anim|ime|list>")
	os.Exit(2)
}

func demo(name string) (ui.Page, string) {
	switch name {
	case "board":
		return ui.NewPocBoard(), "POC-1 棋盘自绘"
	case "anim":
		return ui.NewPocAnim(), "POC-2 220ms 飞行动画帧循环"
	case "ime":
		return ui.NewPocIme(), "POC-3 中文 IME + 字体回退链"
	case "list":
		return ui.NewPocList(), "POC-4 长列表虚拟化（14 万局）"
	default:
		return nil, ""
	}
}

// recordsSample 样例记录（对局 2 着 + 残局双解）。
func recordsSample() []storage.GameRecord {
	gameMoves := []storage.RecordMove{
		{F: [2]int{7, 7}, T: [2]int{4, 7}, P: "C"},
		{F: [2]int{7, 0}, T: [2]int{6, 2}, P: "c"},
	}
	solved := "solved"
	var result *string
	return []storage.GameRecord{
		{ID: 1, Title: "2026-10-07 双人对弈", Mode: "humanVsHuman", InitialFen: rules.FENInitial,
			Moves: gameMoves, Result: result, SolveStatus: nil, Solutions: []any{}, CreatedAt: time.Now().UnixMilli()},
		{ID: 2, Title: "残局演示（双解）", Mode: "endgame", InitialFen: "3k5/9/9/9/9/9/9/9/9/4K4 w - - 0 1",
			SolveStatus: &solved, Solutions: []any{[]any{"e0e1"}, []any{"e0d0", "e9d9"}}, CreatedAt: time.Now().UnixMilli()},
	}
}
