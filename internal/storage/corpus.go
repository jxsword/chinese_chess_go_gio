package storage

// 语料目录扫描与文件访问（对应 corpus_scanner.dart + corpus_paths.dart，06 文档 §1；
// 逐行翻译 Electron 版 src/main/services/corpus.ts）。
//
// 结构：
// - `XQF-象棋谱大全/<分类>/.../*.xqf` —— 按一级子目录分类，逐文件懒解析；
// - `ChessQ-gamebooks/gamebooks` 下任意层级的 *.xqf —— 残局杀势；
// - `CGLemon-PGN/{wxf,dpxq}/ICCS/*.pgns` —— 多局合一 PGN 大文件，
//   用 parsers.ScanGameOffsets 建立按局索引后分页浏览。
//
// Go 版分工（06 文档：electron-DR-016 的替代）：扫描与流式索引同进程完成，
// 批量解析经 parsers.Runner 在独立 goroutine 执行（铁律 #7）。
// 纯 Go：禁止 import Wails / net/http / 前端符号（铁律 #1）。

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
)

// CorpusKind 语料条目种类（corpus_scanner.dart CorpusKind）。
type CorpusKind string

const (
	KindXQFDirectory CorpusKind = "xqfDirectory"
	KindPgnFile      CorpusKind = "pgnFile"
)

// CorpusCategory 语料分类：目录即分类 / 多局合一 .pgns 每文件一分类（06 文档 §1/§4）。
type CorpusCategory struct {
	Name string     `json:"name"`
	Path string     `json:"path"`
	Kind CorpusKind `json:"kind"`
	// Source 来源标注（相对语料根的前两级路径，作为 ParsedPuzzle.source）。
	Source string `json:"source"`
}

// CorpusScanResult cc:corpus:scan 响应（root 为空时绑定层按 用户设置>legacy>默认 解析）。
type CorpusScanResult struct {
	// Root 解析后的语料目录绝对路径（缺失引导展示用）。
	Root       string           `json:"root"`
	Exists     bool             `json:"exists"`
	Categories []CorpusCategory `json:"categories"`
}

// CorpusEntry XQF 分类下的文件条目（解析前的轻量描述）。
type CorpusEntry struct {
	Path     string `json:"path"`
	Category string `json:"category"`
	// Source 相对分类目录的前两级子目录（如"残局/适情雅趣"）。
	Source      string `json:"source"`
	DisplayName string `json:"displayName"`
}

// CorpusFileBytes cc:corpus:readFiles 条目（Bytes 以 []byte 承载，JSON 线上为 base64）。
type CorpusFileBytes struct {
	Path  string `json:"path"`
	Bytes []byte `json:"bytes"`
}

// PgnIndexEntry 大 PGN 文件单局偏移索引条目（06 文档 §4.4，即 parsers.PgnGameIndex）。
type PgnIndexEntry = parsers.PgnGameIndex

// CorpusDirOptions 目录解析选项（corpus_paths.dart:150-172）。
type CorpusDirOptions struct {
	// UserSetting 用户设置目录（corpus.userPath，空串/空白视为未设置）。
	UserSetting string
	// DocumentsPath 平台默认目录基路径（主进程传 documents；测试传临时目录）。
	DocumentsPath string
	// LegacyBasePath legacy 相对目录基路径（主进程传 cwd；空串不检查）。
	LegacyBasePath string
}

// ResolveCorpusDir 按优先级解析语料目录（可能尚不存在，由引导下载/手动放置创建）：
// 用户设置 > legacy 相对目录 > 平台默认 <documents>/ChineseChessUltra/corpus。
func ResolveCorpusDir(options CorpusDirOptions) string {
	if user := strings.TrimSpace(options.UserSetting); user != "" {
		return user
	}
	if options.LegacyBasePath != "" {
		legacy := filepath.Join(options.LegacyBasePath, "corpus")
		if st, err := os.Stat(legacy); err == nil && st.IsDir() {
			return legacy
		}
	}
	return filepath.Join(options.DocumentsPath, "ChineseChessUltra", "corpus")
}

// DisplayNameOf 文件名去扩展名（CorpusEntry.displayName，corpus_scanner.dart:68-72）。
func DisplayNameOf(p string) string {
	base := filepath.Base(p)
	dot := strings.LastIndex(base, ".")
	if dot > 0 {
		return base[:dot]
	}
	return base
}

func extensionOf(p string) string {
	lower := strings.ToLower(p)
	dot := strings.LastIndex(lower, ".")
	if dot < 0 {
		return ""
	}
	return lower[dot+1:]
}

// listFilesRecursive 递归收集目录下的全部文件（followLinks 语义对齐 corpus_scanner.dart）。
func listFilesRecursive(dir string) []string {
	out := make([]string, 0)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out // 权限等读取失败：该子树跳过
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		st, err := os.Stat(full)
		if err != nil {
			continue
		}
		if st.IsDir() {
			out = append(out, listFilesRecursive(full)...)
		} else if st.Mode().IsRegular() {
			out = append(out, full)
		}
	}
	return out
}

// ScanCorpus 扫描语料分类（corpus_scanner.dart:91-126）：XQF 按一级子目录聚合；
// PGN 大文件每个文件一个分类；忽略 `_` 开头目录。
func ScanCorpus(root string) CorpusScanResult {
	result := CorpusScanResult{Root: root, Exists: false, Categories: []CorpusCategory{}}
	st, err := os.Stat(root)
	if err != nil || !st.IsDir() {
		return result
	}
	result.Exists = true
	categories := make([]CorpusCategory, 0)
	entries, err := os.ReadDir(root)
	if err != nil {
		return result
	}
	for _, e := range entries {
		name := e.Name()
		dirPath := filepath.Join(root, name)
		st, err := os.Stat(dirPath)
		if err != nil || !st.IsDir() {
			continue
		}
		if strings.HasPrefix(name, "_") {
			continue // 忽略 _ref 等辅助目录
		}
		if name == "CGLemon-PGN" {
			// PGN 大文件：递归找 .pgn / .pgns。
			for _, f := range listFilesRecursive(dirPath) {
				ext := extensionOf(f)
				if ext != "pgn" && ext != "pgns" {
					continue
				}
				rel := f[len(root):]
				parts := splitPath(rel)
				if len(parts) > 0 {
					parts = parts[1:] // 去根目录名
				}
				source := strings.Join(firstN(parts, 2), "/")
				categories = append(categories, CorpusCategory{
					Name:   "PGN · " + parts[len(parts)-1] + "（多局合一）",
					Kind:   KindPgnFile,
					Path:   f,
					Source: source,
				})
			}
		} else {
			categories = append(categories, CorpusCategory{
				Name:   name,
				Kind:   KindXQFDirectory,
				Path:   dirPath,
				Source: name,
			})
		}
	}
	sort.Slice(categories, func(i, j int) bool { return categories[i].Name < categories[j].Name })
	result.Categories = categories
	return result
}

// splitPath 按分隔符切相对路径（滤空段）。
func splitPath(rel string) []string {
	parts := strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == filepath.Separator })
	return parts
}

func firstN(xs []string, n int) []string {
	if len(xs) > n {
		return xs[:n]
	}
	return xs
}

// sourceOf 从文件相对路径推导来源标注（前两级子目录，corpus_scanner.dart:151-161）：
// 去文件名、去 ChessQ 的通用目录层 gamebooks。
func sourceOf(categoryPath, filePath string) string {
	rel := filePath[len(categoryPath):]
	parts := splitPath(rel)
	if len(parts) > 0 {
		parts = parts[:len(parts)-1] // 文件名
	}
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "gamebooks" {
			filtered = append(filtered, p)
		}
	}
	return strings.Join(firstN(filtered, 2), "/")
}

// ListXqfEntries 列出 XQF 分类下的全部棋谱文件（不做解析，corpus_scanner.dart:132-149）。
// 来源标注取相对分类目录的前两级子目录（如"残局/适情雅趣"）。
func ListXqfEntries(categoryPath, categoryName string) []CorpusEntry {
	entries := make([]CorpusEntry, 0)
	if _, err := os.Stat(categoryPath); err != nil {
		return entries
	}
	for _, f := range listFilesRecursive(categoryPath) {
		if extensionOf(f) != "xqf" {
			continue
		}
		entries = append(entries, CorpusEntry{
			Path:        f,
			Category:    categoryName,
			Source:      sourceOf(categoryPath, f),
			DisplayName: DisplayNameOf(f),
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].DisplayName < entries[j].DisplayName })
	return entries
}

// corpusReadableExts 渲染层可请求读取的棋谱扩展名（路径安全约束：防任意文件读取）。
var corpusReadableExts = map[string]bool{"xqf": true, "pgn": true, "pgns": true}

// ReadCorpusFiles 批量读取棋谱文件字节（供解析管线；越界/非法扩展名条目跳过）。
func ReadCorpusFiles(paths []string) []CorpusFileBytes {
	out := make([]CorpusFileBytes, 0)
	for _, p := range paths {
		if !corpusReadableExts[extensionOf(p)] {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			continue // 单文件读取失败：跳过（批量解析以 null 结果呈现）
		}
		out = append(out, CorpusFileBytes{Path: p, Bytes: data})
	}
	return out
}

// ScanPgnIndex 扫描大 PGN 文件的按局索引（corpus_scanner.dart:203-208 的 Go 等价：
// 流式扫描，文件读取与字节扫描同 goroutine，内存占用有 1MB 块 + 8MB 单行上限
// 约束，不整读百 MB 文件）。maxGames ≤ 0 表示不限数量。
func ScanPgnIndex(path string, maxGames int) ([]PgnIndexEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close() // 先关 fd 的清理由 defer 收口（09 §2.4 Windows 句柄教训）
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	source := &filePgnSource{f: f, size: st.Size()}
	return parsers.ScanGameOffsets(source, maxGames), nil
}

// ReadPgnGameText 读取索引指向的单局文本并返回（corpus_scanner.dart:210-230）。
func ReadPgnGameText(path string, entry PgnIndexEntry) (string, error) {
	if entry.Offset < 0 || entry.Length < 0 {
		return "", errors.New("PGN 索引越界")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if entry.Offset >= st.Size() {
		return "", errors.New("PGN 索引越界")
	}
	length := entry.Length
	if length > st.Size()-entry.Offset {
		length = st.Size() - entry.Offset
	}
	buf := make([]byte, length)
	if _, err := f.ReadAt(buf, entry.Offset); err != nil && length > 0 {
		return "", err
	}
	return parsers.DecodeUtf8Lossy(buf), nil
}

// filePgnSource os.File 上的 parsers.PgnFileSource 实现。
type filePgnSource struct {
	f    *os.File
	size int64
}

func (s *filePgnSource) ByteLength() int64 { return s.size }

func (s *filePgnSource) Read(offset int64, length int) []byte {
	if offset < 0 || length < 0 || offset >= s.size {
		return []byte{}
	}
	if int64(length) > s.size-offset {
		length = int(s.size - offset)
	}
	buf := make([]byte, length)
	n, err := s.f.ReadAt(buf, offset)
	if n <= 0 {
		return []byte{}
	}
	if err != nil && n < length {
		return buf[:n]
	}
	return buf
}
