package storage

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 下载器等价用例集（test/features/puzzle/model/corpus_downloader_test.dart 11 条
// + corpusDownloader.spec.ts 全表，06 文档 §5）。

func bytesOf(s string) []byte { return []byte(s) }

// ---- 构造测试 zip（对齐 corpusZip.ts buildTestZip：store/deflate + 可选 Unix mode）----

type testZipEntry struct {
	name     string
	content  []byte
	method   uint16
	unixMode uint32 // 0 = 默认 0o644 文件；0xa1ff = 符号链接
}

func buildTestZip(t *testing.T, entries []testZipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: e.method, CreatorVersion: 3 << 8}
		if e.unixMode != 0 {
			h.ExternalAttrs = e.unixMode << 16
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(e.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// ---- SSRF 白名单全表 ----

func TestIsDownloadURLAllowedPublicHTTPS(t *testing.T) {
	// 允许 https 公网地址。
	if !IsDownloadURLAllowed("https://github.com/jxsword/qp-corpus/releases/latest/download/qp-corpus.zip") {
		t.Fatal("公网 https 应放行")
	}
	if !IsDownloadURLAllowed("https://github.com/jxsword/qp-corpus/releases/download/v1/x.zip") {
		t.Fatal("公网 https 应放行")
	}
}

func TestIsDownloadURLAllowedRejectsNonHTTPSAndInvalid(t *testing.T) {
	// 拒绝非 https 与非法 URL。
	for _, u := range []string{
		"http://github.com/jxsword/qp-corpus/releases/download/v1/x.zip",
		"ftp://example.com/x.zip",
		"not a url",
		"",
	} {
		if IsDownloadURLAllowed(u) {
			t.Fatalf("%q 应拒绝", u)
		}
	}
}

func TestIsDownloadURLAllowedRejectsBlockedHosts(t *testing.T) {
	// 拒绝 localhost / 环回 / 私有 / 保留地址 / mDNS。
	for _, host := range []string{
		"localhost",
		"127.0.0.1",
		"10.0.0.1",
		"192.168.1.1",
		"172.16.0.1",
		"169.254.1.1",
		"0.0.0.0",
		"224.0.0.1",
		"[::1]",
		"[fe80::1]",
		"[fd00::1]",
		"nas.local",
	} {
		if IsDownloadURLAllowed(fmt.Sprintf("https://%s/x.zip", host)) {
			t.Fatalf("https://%s 应拒绝", host)
		}
	}
}

func TestIsDownloadURLAllowedRejectsBypassForms(t *testing.T) {
	// 拒绝 IPv4-mapped IPv6 / 整数 IP / 八进制分段等绕过形式（P2-1）。
	for _, u := range []string{
		"https://[::ffff:127.0.0.1]/x.zip", // mapped 点分
		"https://[::ffff:7f00:1]/x.zip",    // mapped 十六进制
		"https://[::ffff:10.0.0.1]/x.zip",  // mapped 私网
		"https://2130706433/x.zip",         // 纯十进制整数 IP
		"https://0x7f000001/x.zip",         // 十六进制整数 IP
		"https://2887685888/x.zip",         // 超出 v4 范围的整数 host
		"https://0177.0.0.1/x.zip",         // 八进制分段（前导 0 歧义）
	} {
		if IsDownloadURLAllowed(u) {
			t.Fatalf("%s 应拒绝", u)
		}
	}
	// mapped 公网地址仍放行（行为对齐：校验的是内网/保留段）。
	if !IsDownloadURLAllowed("https://[::ffff:8.8.8.8]/x.zip") {
		t.Fatal("mapped 公网应放行")
	}
}

// ---- extractZip（本地构造包，不走网络）----

func writeTestZip(t *testing.T, dir, name string, entries []testZipEntry) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, buildTestZip(t, entries), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExtractZipNormalEntries(t *testing.T) {
	// 正常解压：文件与子目录落位，返回文件数。
	tmp := t.TempDir()
	zipPath := writeTestZip(t, tmp, "ok.zip", []testZipEntry{
		{name: "XQF-象棋谱大全/残局/适情雅趣/a.xqf", content: bytesOf("data-a")},
		{name: "XQF-象棋谱大全/全局/b.xqf", content: bytesOf("data-b")},
		{name: "README.md", content: bytesOf("readme")},
	})
	target := filepath.Join(tmp, "corpus")
	count, err := extractZip(zipPath, target)
	if err != nil {
		t.Fatal(err)
	}
	if count.Extracted != 3 || count.Skipped != 0 {
		t.Fatalf("result = %+v", count)
	}
	if _, err := os.Stat(filepath.Join(target, "XQF-象棋谱大全", "残局", "适情雅趣", "a.xqf")); err != nil {
		t.Fatalf("解压落位失败: %v", err)
	}
}

func TestExtractZipDeflateEntries(t *testing.T) {
	// deflate 压缩条目正确解压。
	tmp := t.TempDir()
	zipPath := writeTestZip(t, tmp, "deflate.zip", []testZipEntry{
		{name: "data.bin", content: bytesOf("compress-me-compress-me-compress-me"), method: zip.Deflate},
	})
	target := filepath.Join(tmp, "corpus")
	count, err := extractZip(zipPath, target)
	if err != nil {
		t.Fatal(err)
	}
	if count.Extracted != 1 {
		t.Fatalf("result = %+v", count)
	}
}

func TestExtractZipSlipBasic(t *testing.T) {
	// zip-slip 防护：.. 越界、绝对路径、盘符条目被跳过。
	tmp := t.TempDir()
	zipPath := writeTestZip(t, tmp, "evil.zip", []testZipEntry{
		{name: "../escape.txt", content: bytesOf("evil")},
		{name: "/abs/evil.txt", content: bytesOf("evil")},
		{name: "C:/evil.txt", content: bytesOf("evil")},
		{name: "XQF-象棋谱大全/安全.xqf", content: bytesOf("safe")},
	})
	target := filepath.Join(tmp, "corpus")
	count, err := extractZip(zipPath, target)
	if err != nil {
		t.Fatal(err)
	}
	if count.Extracted != 1 {
		t.Fatalf("只有安全条目应被解压: %+v", count)
	}
	if _, err := os.Stat(filepath.Join(target, "XQF-象棋谱大全", "安全.xqf")); err != nil {
		t.Fatal("安全条目缺失")
	}
	if _, err := os.Stat(filepath.Join(tmp, "escape.txt")); !os.IsNotExist(err) {
		t.Fatal("越界文件不存在于临时目录")
	}
}

func TestExtractZipSlipProbes(t *testing.T) {
	// zip-slip 探针：反斜杠归一、嵌套 ..、UNC、符号链接全拦截。
	tmp := t.TempDir()
	zipPath := writeTestZip(t, tmp, "probes.zip", []testZipEntry{
		{name: `..\escape.txt`, content: bytesOf("evil")},
		{name: "a/../../escape2.txt", content: bytesOf("evil2")},
		{name: `\\evil\share\f.txt`, content: bytesOf("evil3")},
		{name: "link/evil.xqf", content: bytesOf("evil"), unixMode: 0xa1ff}, // S_IFLNK
		{name: "ok.txt", content: bytesOf("hi")},
	})
	target := filepath.Join(tmp, "corpus")
	count, err := extractZip(zipPath, target)
	if err != nil {
		t.Fatal(err)
	}
	if count.Extracted != 1 {
		t.Fatalf("只允许 ok.txt 落地: %+v", count)
	}
	if _, err := os.Stat(filepath.Join(target, "ok.txt")); err != nil {
		t.Fatal("ok.txt 缺失")
	}
	for _, p := range []string{
		filepath.Join(tmp, "escape.txt"),
		filepath.Join(tmp, "escape2.txt"),
		filepath.Join(tmp, "evil"),
		filepath.Join(target, "link"),
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s 不应存在", p)
		}
	}
}

func TestExtractZipReservedNamesAndTrailingDots(t *testing.T) {
	// Windows 保留名（含扩展名形式）与尾随点/空格条目被跳过。
	tmp := t.TempDir()
	zipPath := writeTestZip(t, tmp, "reserved.zip", []testZipEntry{
		{name: "CON", content: bytesOf("x")},
		{name: "NUL.txt", content: bytesOf("x")},
		{name: "com1", content: bytesOf("x")},
		{name: "aux/inner.txt", content: bytesOf("x")},
		{name: "LPT2", content: bytesOf("x")},
		{name: "bad.", content: bytesOf("x")},
		{name: "bad. ", content: bytesOf("x")},
		{name: "good.txt", content: bytesOf("ok")},
	})
	target := filepath.Join(tmp, "corpus")
	result, err := extractZip(zipPath, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Extracted != 1 || result.Skipped != 7 {
		t.Fatalf("result = %+v, 期望 1/7", result)
	}
	if _, err := os.Stat(filepath.Join(target, "good.txt")); err != nil {
		t.Fatal("good.txt 缺失")
	}
}

func TestExtractZipNameConflictSkipsWithoutAbort(t *testing.T) {
	// 同名冲突（先文件后目录）跳过冲突条目，不中断整体解压。
	tmp := t.TempDir()
	zipPath := writeTestZip(t, tmp, "conflict.zip", []testZipEntry{
		{name: "conflict", content: bytesOf("file")},
		{name: "conflict/inner.txt", content: bytesOf("dir-entry")},
		{name: "ok.txt", content: bytesOf("ok")},
	})
	target := filepath.Join(tmp, "corpus")
	result, err := extractZip(zipPath, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Extracted != 2 || result.Skipped != 1 {
		t.Fatalf("result = %+v, 期望 2/1", result)
	}
	if _, err := os.Stat(filepath.Join(target, "ok.txt")); err != nil {
		t.Fatal("ok.txt 缺失")
	}
}

// ---- ExtractZipAtomic ----

func TestExtractZipAtomicFailureCleansStaging(t *testing.T) {
	// 失败时清理临时目录且旧目录不被破坏。
	parent := t.TempDir()
	target := filepath.Join(parent, "corpus")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 非 zip 垃圾字节：解压必然抛异常（先过魔数校验才到解压，直接测 atomic 对坏 zip 的容错）。
	corrupt := filepath.Join(parent, "corrupt.zip")
	if err := os.WriteFile(corrupt, []byte{0x50, 0x4b, 0x03, 0x04, 0xff, 0xff, 0xff, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ExtractZipAtomic(corrupt, target, nil); err == nil {
		t.Fatal("坏 zip 应报错")
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); err != nil {
		t.Fatal("旧目录不应被破坏")
	}
	leftovers := listDirPrefix(t, parent, "corpus.tmp")
	if len(leftovers) != 0 {
		t.Fatalf("失败的临时解压目录应被整体删除: %v", leftovers)
	}
}

func TestExtractZipAtomicSuccessReplacesTarget(t *testing.T) {
	// 成功后原子替换旧目录。
	parent := t.TempDir()
	target := filepath.Join(parent, "corpus")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "old.txt"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipPath := writeTestZip(t, parent, "replace.zip", []testZipEntry{
		{name: "new.txt", content: bytesOf("new")},
	})
	result, err := ExtractZipAtomic(zipPath, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Extracted != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(target, "new.txt")); err != nil {
		t.Fatal("new.txt 缺失")
	}
	if _, err := os.Stat(filepath.Join(target, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("旧目录应被整体替换")
	}
	if leftovers := listDirPrefix(t, parent, "corpus.tmp"); len(leftovers) != 0 {
		t.Fatalf("临时解压目录残留: %v", leftovers)
	}
}

func listDirPrefix(t *testing.T, dir, prefix string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := []string{}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), prefix) {
			out = append(out, e.Name())
		}
	}
	return out
}

// ---- VerifyZipIntegrity（魔数 + 大小区间）----

func TestVerifyZipIntegrityBadMagic(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "bad.zip")
	if err := os.WriteFile(f, make([]byte, 2<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	err := VerifyZipIntegrity(f)
	if err == nil || !strings.Contains(err.Error(), "魔数") {
		t.Fatalf("err = %v", err)
	}
}

func TestVerifyZipIntegritySizeBounds(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "small.zip")
	if err := os.WriteFile(f, []byte{0x50, 0x4b, 0x03, 0x04}, 0o644); err != nil {
		t.Fatal(err)
	}
	err := VerifyZipIntegrity(f)
	if err == nil || !strings.Contains(err.Error(), "大小异常") {
		t.Fatalf("err = %v", err)
	}
}

// ---- Range 续传（06 §5 Electron 增强项：If-Range + Range，失败安全回退整体重下）----

const testETag = `"corpus-etag-v1"`

// serveZip 支持单段 Range/If-Range 的最小静态服务器；记录收到的请求头。
func serveZip(t *testing.T, body []byte, mode string) (url string, seenHeaders *[]map[string]string, close func()) {
	t.Helper()
	seen := &[]map[string]string{}
	handler := func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, map[string]string{"if-range": r.Header.Get("If-Range"), "range": r.Header.Get("Range")})
		match := regexpFindRange(r.Header.Get("Range"))
		if mode == "range" && match != "" {
			var start int64
			fmt.Sscanf(match, "bytes=%d-", &start)
			slice := body[start:]
			w.Header().Set("Content-Length", fmt.Sprint(len(slice)))
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(body)-1, len(body)))
			w.Header().Set("ETag", testETag)
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(slice)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("ETag", testETag)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}
	server := httptest.NewServer(http.HandlerFunc(handler))
	return server.URL + "/qp-corpus.zip", seen, server.Close
}

func regexpFindRange(rangeHeader string) string {
	if rangeHeader == "" {
		return ""
	}
	if strings.HasPrefix(rangeHeader, "bytes=") {
		return rangeHeader
	}
	return ""
}

func TestDownloadZipResumeWithMatchingETag(t *testing.T) {
	// 半成品 + ETag 匹配：206 续写剩余字节，完成后清 sidecar。
	zipBody := buildTestZip(t, []testZipEntry{
		{name: "XQF-象棋谱大全/a.xqf", content: bytesOf("data-a")},
		{name: "README.md", content: bytesOf("readme")},
	})
	url, seenHeaders, closeServer := serveZip(t, zipBody, "range")
	defer closeServer()

	// 模拟上次中断：半成品（前 10 字节）+ sidecar。
	tempFile := CorpusTempZipPath(url)
	if err := os.WriteFile(tempFile, zipBody[:10], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempFile+".etag", []byte(testETag), 0o644); err != nil {
		t.Fatal(err)
	}

	progress := make([][2]int64, 0)
	out, err := downloadZip(url, func(received, total int64) {
		progress = append(progress, [2]int64{received, total})
	}, nil, func(string) bool { return true }) // 测试本地 http；生产恒为 SSRF 校验
	if err != nil {
		t.Fatal(err)
	}
	if out != tempFile {
		t.Fatalf("out = %q", out)
	}
	if _, err := os.Stat(tempFile + ".etag"); !os.IsNotExist(err) {
		t.Fatal("完成后应清 sidecar")
	}
	// 服务器收到了 If-Range + Range 头（ETag 一致性校验）。
	if (*seenHeaders)[0]["if-range"] != testETag || (*seenHeaders)[0]["range"] != "bytes=10-" {
		t.Fatalf("headers = %+v", (*seenHeaders)[0])
	}
	// 续传后完整文件可解压（字节齐全）。
	target := tempFile + ".extract-check"
	result, err := extractZip(out, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Extracted != 2 {
		t.Fatalf("extracted = %d", result.Extracted)
	}
	_ = os.RemoveAll(target)
	// 进度总量含已下载前缀。
	last := progress[len(progress)-1]
	if last[0] != int64(len(zipBody)) || last[1] != int64(len(zipBody)) {
		t.Fatalf("progress 末帧 = %v", last)
	}
	_ = os.Remove(out)
}

func TestDownloadZipServerIgnoresRangeFallsBackToFull(t *testing.T) {
	// 服务器忽略 Range（资源变更）：200 整体重下，不残留半成品。
	zipBody := buildTestZip(t, []testZipEntry{{name: "b.xqf", content: bytesOf("data-b")}})
	url, _, closeServer := serveZip(t, zipBody, "full")
	defer closeServer()

	tempFile := CorpusTempZipPath(url)
	if err := os.WriteFile(tempFile, bytes.Repeat([]byte{0xab}, 10), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempFile+".etag", []byte(`"old-etag"`), 0o644); err != nil {
		t.Fatal(err)
	}

	progress := make([][2]int64, 0)
	out, err := downloadZip(url, func(r, total int64) { progress = append(progress, [2]int64{r, total}) }, nil, func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	// 服务器带 If-Range 仍回 200 → 整体覆盖，续传后完整可解压。
	target := out + ".extract-check"
	result, err := extractZip(out, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Extracted != 1 {
		t.Fatalf("extracted = %d", result.Extracted)
	}
	_ = os.RemoveAll(target)
	if progress[len(progress)-1][1] != int64(len(zipBody)) {
		t.Fatalf("progress 总量 = %d", progress[len(progress)-1][1])
	}
	_ = os.Remove(out)
}

func TestDownloadCorpusRejectsIllegalURL(t *testing.T) {
	// 下载入口：非 https 直接拒绝（SSRF 门）。
	if _, err := DownloadCorpus(DownloadCorpusOptions{URL: "http://127.0.0.1/x.zip", TargetDir: t.TempDir()}); err == nil ||
		!strings.Contains(err.Error(), "下载地址不合法") {
		t.Fatalf("err = %v", err)
	}
}

func TestExtractZipCorruptEntrySkippedNotAborts(t *testing.T) {
	// P1 修复回归：单条目内容损坏（CRC 校验失败）跳过并计数，不中断整体解压
	// （对齐 TS 版逐条目 try/catch；标准库 CRC 校验使坏条目被跳过而非照写坏数据）。
	tmp := t.TempDir()
	good := buildTestZip(t, []testZipEntry{
		{name: "a.xqf", content: bytesOf("data-a")},
		{name: "b.xqf", content: bytesOf("data-b")},
	})
	// 破坏第一个条目 payload 中间一个字节（LFH 30 字节 + 文件名 5 字节之后）。
	corrupt := append([]byte{}, good...)
	corrupt[30+5+1] ^= 0xff
	zipPath := filepath.Join(tmp, "corrupt-entry.zip")
	if err := os.WriteFile(zipPath, corrupt, 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, "corpus")
	result, err := extractZip(zipPath, target)
	if err != nil {
		t.Fatalf("坏条目不应中止整体解压: %v", err)
	}
	if result.Extracted != 1 || result.Skipped != 1 {
		t.Fatalf("result = %+v, 期望 1/1", result)
	}
	if _, err := os.Stat(filepath.Join(target, "b.xqf")); err != nil {
		t.Fatal("完条目 b.xqf 应落盘")
	}
}

func TestExtractZipDirectoryEntriesMaterialized(t *testing.T) {
	// P2 修复回归：目录条目落盘建目录（不计 extracted/skipped；失败才 skip）。
	tmp := t.TempDir()
	zipPath := writeTestZip(t, tmp, "dirs.zip", []testZipEntry{
		{name: "XQF-象棋谱大全/残局/", content: nil},
		{name: "XQF-象棋谱大全/残局/a.xqf", content: bytesOf("data-a")},
	})
	target := filepath.Join(tmp, "corpus")
	result, err := extractZip(zipPath, target)
	if err != nil {
		t.Fatal(err)
	}
	if result.Extracted != 1 || result.Skipped != 0 {
		t.Fatalf("result = %+v, 期望 1/0（目录不计数）", result)
	}
	st, err := os.Stat(filepath.Join(target, "XQF-象棋谱大全", "残局"))
	if err != nil || !st.IsDir() {
		t.Fatalf("目录条目应落盘: %v", err)
	}
}
