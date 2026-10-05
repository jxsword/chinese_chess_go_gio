package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 语料服务等价用例集（test/features/puzzle/model/corpus_scanner_test.dart 临时目录部分
// + 路径优先级 + test/main/corpus.spec.ts，06 文档 §1）。

func mkdirAll(t *testing.T, elems ...string) string {
	t.Helper()
	p := filepath.Join(elems...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanCorpusIgnoresUnderscoreAndAggregatesFirstLevel(t *testing.T) {
	tmp := t.TempDir()
	// 构造: XQF测试谱/残局/适情雅趣/a.xqf, XQF测试谱/全局/子/b.xqf, _ref/c.xqf
	d1 := mkdirAll(t, tmp, "XQF测试谱", "残局", "适情雅趣")
	writeFile(t, filepath.Join(d1, "a.xqf"), make([]byte, 1100))
	d2 := mkdirAll(t, tmp, "XQF测试谱", "全局", "子")
	writeFile(t, filepath.Join(d2, "b.XQF"), []byte{})
	mkdirAll(t, tmp, "_ref")
	writeFile(t, filepath.Join(tmp, "_ref", "c.xqf"), []byte{})

	scan := ScanCorpus(tmp)
	if !scan.Exists {
		t.Fatal("exists 应为 true")
	}
	if len(scan.Categories) != 1 || scan.Categories[0].Name != "XQF测试谱" {
		t.Fatalf("categories = %+v", scan.Categories)
	}
	if scan.Categories[0].Kind != KindXQFDirectory || scan.Categories[0].Source != "XQF测试谱" {
		t.Fatalf("category = %+v", scan.Categories[0])
	}
}

func TestListXqfEntriesRecursiveSortedWithSource(t *testing.T) {
	tmp := t.TempDir()
	d1 := mkdirAll(t, tmp, "XQF测试谱", "残局", "适情雅趣")
	writeFile(t, filepath.Join(d1, "a.xqf"), []byte{})
	d2 := mkdirAll(t, tmp, "XQF测试谱", "全局", "子")
	writeFile(t, filepath.Join(d2, "b.XQF"), []byte{})

	scan := ScanCorpus(tmp)
	entries := ListXqfEntries(scan.Categories[0].Path, scan.Categories[0].Name)
	if len(entries) != 2 || entries[0].DisplayName != "a" || entries[1].DisplayName != "b" {
		t.Fatalf("entries = %+v", entries)
	}
	if entries[0].Source != "残局/适情雅趣" {
		t.Fatalf("source[0] = %q", entries[0].Source)
	}
	if entries[1].Source != "全局/子" {
		t.Fatalf("source[1] = %q", entries[1].Source)
	}
}

func TestListXqfEntriesFiltersGamebooksLayer(t *testing.T) {
	// ChessQ 的 gamebooks 通用目录层不出现在 source 中。
	tmp := t.TempDir()
	d1 := mkdirAll(t, tmp, "ChessQ-gamebooks", "gamebooks", "杀势集")
	writeFile(t, filepath.Join(d1, "endgame1.xqf"), []byte{})
	scan := ScanCorpus(tmp)
	entries := ListXqfEntries(scan.Categories[0].Path, scan.Categories[0].Name)
	if len(entries) != 1 || entries[0].Source != "杀势集" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestScanCorpusPgnFilePerFileCategory(t *testing.T) {
	// CGLemon-PGN 下的 .pgn/.pgns 每文件一分类，source 取前两级。
	tmp := t.TempDir()
	d1 := mkdirAll(t, tmp, "CGLemon-PGN", "wxf", "ICCS")
	writeFile(t, filepath.Join(d1, "wxf-big.pgns"), []byte{})
	writeFile(t, filepath.Join(d1, "other.txt"), []byte{})
	scan := ScanCorpus(tmp)
	if len(scan.Categories) != 1 {
		t.Fatalf("categories = %d", len(scan.Categories))
	}
	c := scan.Categories[0]
	if c.Kind != KindPgnFile || c.Name != "PGN · wxf-big.pgns（多局合一）" || c.Source != "wxf/ICCS" {
		t.Fatalf("category = %+v", c)
	}
}

func TestScanCorpusEmptyAndMissing(t *testing.T) {
	tmp := t.TempDir()
	scan := ScanCorpus(filepath.Join(tmp, "不存在"))
	if scan.Exists || len(scan.Categories) != 0 {
		t.Fatalf("缺失目录: %+v", scan)
	}
	scan = ScanCorpus(tmp)
	if !scan.Exists || len(scan.Categories) != 0 {
		t.Fatalf("空目录: %+v", scan)
	}
}

func TestDisplayNameOf(t *testing.T) {
	// displayNameOf：去扩展名；无扩展名/点开头原样。
	cases := map[string]string{
		"/a/b/适情雅趣 第1局.xqf": "适情雅趣 第1局",
		"/a/b/README":       "README",
		"/a/b/.hidden.xqf":  ".hidden",
	}
	for in, want := range cases {
		if got := DisplayNameOf(filepath.Base(in)); got != want {
			t.Fatalf("DisplayNameOf(%q) = %q, 期望 %q", filepath.Base(in), got, want)
		}
	}
}

func TestResolveCorpusDirPriority(t *testing.T) {
	// 用户设置 > legacy 相对目录 > 平台默认。
	docs := t.TempDir()
	legacyBase := t.TempDir()
	mkdirAll(t, legacyBase, "corpus")
	// 1. 用户设置优先
	if got := ResolveCorpusDir(CorpusDirOptions{UserSetting: " /tmp/my-corpus ", DocumentsPath: docs, LegacyBasePath: legacyBase}); got != "/tmp/my-corpus" {
		t.Fatalf("user 设置优先: %q", got)
	}
	// 2. legacy 相对目录存在则沿用
	if got := ResolveCorpusDir(CorpusDirOptions{UserSetting: "", DocumentsPath: docs, LegacyBasePath: legacyBase}); got != filepath.Join(legacyBase, "corpus") {
		t.Fatalf("legacy: %q", got)
	}
	// 3. 平台默认 <documents>/ChineseChessUltra/corpus
	want := filepath.Join(docs, "ChineseChessUltra", "corpus")
	if got := ResolveCorpusDir(CorpusDirOptions{UserSetting: "", DocumentsPath: docs, LegacyBasePath: filepath.Join(docs, "nope")}); got != want {
		t.Fatalf("默认: %q", got)
	}
}

func TestReadCorpusFilesFiltersExtsAndMissing(t *testing.T) {
	// readCorpusFiles：只读棋谱扩展名，缺失文件跳过。
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.xqf")
	writeFile(t, a, []byte{1, 2, 3})
	writeFile(t, filepath.Join(tmp, "b.txt"), []byte("nope"))
	files := ReadCorpusFiles([]string{a, filepath.Join(tmp, "b.txt"), filepath.Join(tmp, "missing.xqf")})
	if len(files) != 1 {
		t.Fatalf("files = %d", len(files))
	}
	if files[0].Path != a || len(files[0].Bytes) != 3 || files[0].Bytes[2] != 3 {
		t.Fatalf("bytes = %+v", files[0])
	}
}

func TestScanPgnIndexAndReadPgnGameText(t *testing.T) {
	// scanPgnIndex + readPgnGameText：临时 PGN 索引与单局读取。
	tmp := t.TempDir()
	pgn := filepath.Join(tmp, "multi.pgn")
	writeFile(t, pgn, []byte("[Event \"甲局\"]\n[Red \"红甲\"]\n\n1. 炮二平五 马8进7\n\n[Event \"乙局\"]\n[Red \"红乙\"]\n\n1. 兵七进一 卒7进1\n"))
	index, err := ScanPgnIndex(pgn, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(index) != 2 {
		t.Fatalf("index = %d 局", len(index))
	}
	if index[0].Event == nil || *index[0].Event != "甲局" || index[1].Red == nil || *index[1].Red != "红乙" {
		t.Fatalf("index = %+v", index)
	}
	game2, err := ReadPgnGameText(pgn, index[1])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(game2, "乙局") || !strings.Contains(game2, "兵七进一") {
		t.Fatalf("game2 = %q", game2)
	}
}
