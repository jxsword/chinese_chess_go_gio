package storage

// 棋谱语料包下载器（对应 corpus_downloader.dart，06 文档 §5 安全清单；
// 逐行翻译 Electron 版 src/main/services/corpusDownloader.ts）。
//
// 安全约束：
// - 下载前用 IsDownloadURLAllowed 校验 URL（仅 https 公网地址，SSRF 防护）；
// - 重定向手动跟随（≤5 跳），每一跳都重新校验；
// - zip 解压防路径穿越：符号链接/绝对路径/../盘符/Windows 保留名条目跳过
//   （zip-slip 防护）；
// - zip 魔数 PK\x03\x04 与大小区间（1MB~512MB）校验。
//
// 健壮性约束：
// - 连接 15s 超时 + 响应流 30s 块间停滞超时（context 取消，绑定层计时）；
// - 临时 zip 写系统临时目录；解压先落 `corpus.tmp-<ts>` 临时目录，
//   全部成功后原子替换目标目录，失败整体清理，不残留半成品；
// - Range 续传（06 §5 Electron 增强项）：临时文件名对 URL 确定性命名（SHA-1），
//   中断残留的半成品与 ETag sidecar 配对；重试时带 `If-Range: <etag>` + `Range`，
//   服务器资源未变则 206 续写，否则按 200 整体重下（失败安全保留原语义）。
// - 临时目录清理先关 fd 再删 + 带退避重试（09 §2.4 Windows 句柄滞后教训）。

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ConnectTimeoutMS TCP 连接与响应头等待超时（corpus_downloader.dart:48）。
const ConnectTimeoutMS = 15_000

// ChunkTimeoutMS 响应流块间超时：超过该时长无新数据即判定下载停滞（corpus_downloader.dart:51）。
const ChunkTimeoutMS = 30_000

// MinZipBytes / MaxZipBytes 语料 zip 合法大小下限/上限（当前包约 45.8MB，留足余量防炸弹/空文件）。
const (
	MinZipBytes = 1 << 20
	MaxZipBytes = 512 << 20
)

// maxRedirects 最大重定向跳数（含 0 号初始请求共 6 次机会）。
const maxRedirects = 5

// reservedSegment Windows 保留设备名（不区分大小写，含 `CON.txt` 扩展名形式）。
var reservedSegment = regexp.MustCompile(`^(?i)(CON|PRN|AUX|NUL|COM[1-9]|LPT[1-9])(\..*)?$`)

// CorpusDownloadCancelled 用户取消下载时抛出（corpus_downloader.dart:26-31）。
var CorpusDownloadCancelled = errors.New("下载已取消")

// CorpusDownloadResult 下载解压结果：解压成功的文件数与跳过的条目数。
type CorpusDownloadResult struct {
	Extracted int `json:"extracted"`
	Skipped   int `json:"skipped"`
}

// CorpusTempZipPath 下载临时 zip 的确定性路径（URL SHA-1 命名，Range 续传依赖同名可寻）。
func CorpusTempZipPath(url string) string {
	sum := sha1.Sum([]byte(url))
	return filepath.Join(os.TempDir(), "corpus-download-"+hex.EncodeToString(sum[:])+".zip")
}

// ---------------------------------------------------------------------------
// SSRF 校验（corpus_paths.dart:41-148 逐条等价）
// ---------------------------------------------------------------------------

// IsDownloadURLAllowed 校验下载 URL 是否允许（仅 https；拒绝 localhost/环回/私有/保留地址/mDNS）。
func IsDownloadURLAllowed(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	if strings.ToLower(u.Scheme) != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return false
	}
	return !isBlockedHost(host)
}

func isBlockedHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return true
	}
	// IPv4 字面量：拒绝环回/私有/链路本地/保留段。
	ipv4Match := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)\.(\d+)$`).FindStringSubmatch(host)
	if ipv4Match != nil {
		groups := ipv4Match[1:]
		// 八进制分段（多段前导 0，如 0177.0.0.1）语义有歧义，一律拒绝。
		for _, g := range groups {
			if len(g) > 1 && strings.HasPrefix(g, "0") {
				return true
			}
		}
		octets := make([]int, 4)
		for i, g := range groups {
			v, err := strconv.Atoi(g)
			if err != nil || v < 0 || v > 255 {
				return true
			}
			octets[i] = v
		}
		return isBlockedV4(octets)
	}
	// 纯十进制 / 0x 十六进制整数形式的 IPv4（如 2130706433 / 0x7f000001）。
	if asInt, ok := parseIntegerHost(host); ok {
		if asInt > 0xffffffff {
			return true
		}
		return isBlockedV4([]int{
			int(asInt>>24) & 0xff, int(asInt>>16) & 0xff, int(asInt>>8) & 0xff, int(asInt) & 0xff,
		})
	} // IPv6 字面量：拒绝环回、链路本地（fe80::/10）、唯一本地（fc00::/7）。
	// （Go url.Hostname() 已去 []，h 即裸地址。）
	if strings.Contains(host, ":") {
		h := host
		if h == "::" || h == "::1" {
			return true
		}
		// IPv4-mapped IPv6（::ffff:0:0/96）：还原成 v4 判段。
		if strings.HasPrefix(strings.ToLower(h), "::ffff:") {
			mapped, ok := parseMappedV4(h[len("::ffff:"):])
			if !ok {
				return true // 形式存疑的 mapped 段一律拒绝
			}
			return isBlockedV4(mapped)
		}
		lower := strings.ToLower(h)
		for _, p := range []string{"fe8", "fe9", "fea", "feb"} {
			if strings.HasPrefix(lower, p) {
				return true
			}
		}
		if strings.HasPrefix(lower, "fc") || strings.HasPrefix(lower, "fd") {
			return true
		}
	}
	return false
}

// isBlockedV4 IPv4 段判定（环回/私有/链路本地/组播与保留段，corpus_paths.dart:107-115）。
func isBlockedV4(octets []int) bool {
	a, b := octets[0], octets[1]
	if a == 0 || a == 10 || a == 127 {
		return true
	}
	if a == 169 && b == 254 {
		return true
	}
	if a == 172 && b >= 16 && b <= 31 {
		return true
	}
	if a == 192 && b == 168 {
		return true
	}
	if a >= 224 {
		return true
	}
	return false
}

// parseIntegerHost 解析十进制/十六进制整数形式的 host；非整数形式返回 false（corpus_paths.dart:118-129）。
// 匹配整数形式但溢出 64 位时返回 MaxUint64（> 0xffffffff → 拒绝，对齐 TS
// parseInt 得大数后的拒判定——SSRF 纵深）。
func parseIntegerHost(host string) (uint64, bool) {
	if regexp.MustCompile(`^\d+$`).MatchString(host) {
		n, err := strconv.ParseUint(host, 10, 64)
		if err != nil {
			return ^uint64(0), true
		}
		return n, true
	}
	lower := strings.ToLower(host)
	if len(lower) > 2 && strings.HasPrefix(lower, "0x") && regexp.MustCompile(`^0x[0-9a-f]+$`).MatchString(lower) {
		n, err := strconv.ParseUint(lower[2:], 16, 64)
		if err != nil {
			return ^uint64(0), true
		}
		return n, true
	}
	return 0, false
}

// parseMappedV4 解析 `::ffff:` 后的 IPv4 部分（点分十进制或两组十六进制，corpus_paths.dart:132-148）。
func parseMappedV4(rest string) ([]int, bool) {
	dotted := regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)\.(\d+)$`).FindStringSubmatch(rest)
	if dotted != nil {
		octets := make([]int, 4)
		for i, g := range dotted[1:] {
			v, err := strconv.Atoi(g)
			if err != nil || v > 255 {
				return nil, false
			}
			octets[i] = v
		}
		return octets, true
	}
	hex2 := regexp.MustCompile(`^([0-9a-fA-F]{1,4}):([0-9a-fA-F]{1,4})$`).FindStringSubmatch(rest)
	if hex2 != nil {
		hi, err1 := strconv.ParseUint(hex2[1], 16, 32)
		lo, err2 := strconv.ParseUint(hex2[2], 16, 32)
		if err1 != nil || err2 != nil {
			return nil, false
		}
		return []int{int(hi>>8) & 0xff, int(hi) & 0xff, int(lo>>8) & 0xff, int(lo) & 0xff}, true
	}
	return nil, false
}

// ---------------------------------------------------------------------------
// 下载 + 校验 + 解压
// ---------------------------------------------------------------------------

// DownloadCorpusOptions 下载选项（onProgress/isCancelled 与 Dart 版对齐）。
type DownloadCorpusOptions struct {
	URL         string
	TargetDir   string
	OnProgress  func(received, total int64)
	IsCancelled func() bool
}

// DownloadCorpus 下载 url 指向的语料 zip 并解压到 targetDir（corpus_downloader.dart:68-108）。
// 任一步失败/取消整体清理并返回错误；成功返回解压统计。
func DownloadCorpus(options DownloadCorpusOptions) (CorpusDownloadResult, error) {
	if !IsDownloadURLAllowed(options.URL) {
		return CorpusDownloadResult{}, errors.New("下载地址不合法（仅允许 https 公网地址）")
	}
	if err := throwIfCancelled(options.IsCancelled); err != nil {
		return CorpusDownloadResult{}, err
	}
	zipFile, err := downloadZip(options.URL, options.OnProgress, options.IsCancelled, IsDownloadURLAllowed)
	if err != nil {
		return CorpusDownloadResult{}, err
	}
	defer func() {
		// 临时 zip 清理失败不掩盖主流程结果。
		_ = os.Remove(zipFile)
	}()
	if err := VerifyZipIntegrity(zipFile); err != nil {
		return CorpusDownloadResult{}, err
	}
	if err := throwIfCancelled(options.IsCancelled); err != nil {
		return CorpusDownloadResult{}, err
	}

	// Windows legacy 目录联接：不删除/替换联接本身（保留开发期行为），退化为直接解压。
	if isSymlink(options.TargetDir) {
		return extractZip(zipFile, options.TargetDir)
	}
	return ExtractZipAtomic(zipFile, options.TargetDir, options.IsCancelled)
}

func isSymlink(path string) bool {
	st, err := os.Lstat(path)
	return err == nil && st.Mode()&os.ModeSymlink != 0
}

func throwIfCancelled(isCancelled func() bool) error {
	if isCancelled != nil && isCancelled() {
		return CorpusDownloadCancelled
	}
	return nil
}

// downloadZip 下载 zip 到系统临时目录；手动跟随重定向并逐跳校验（corpus_downloader.dart:144-211）。
// 块间停滞超时经 context 取消实现（绑定层计时，00 文档 TIMER 职责铁律）。
// urlValidator 供测试注入（本地 http 服务器）；生产恒为 IsDownloadURLAllowed。
func downloadZip(rawURL string, onProgress func(received, total int64), isCancelled func() bool, urlValidator func(string) bool) (string, error) {
	// 续传状态：半成品 + ETag sidecar（仅服务器返回强 ETag 时启用）。
	tempFile := CorpusTempZipPath(rawURL)
	etagFile := tempFile + ".etag"
	resumeOffset := int64(0)
	var resumeEtag string
	if etag, err := os.ReadFile(etagFile); err == nil {
		if st, err := os.Stat(tempFile); err == nil {
			resumeEtag = string(etag)
			resumeOffset = st.Size()
		}
	} else {
		_ = os.Remove(tempFile) // 残留清理失败：200 路径会整体覆盖
	}

	client := &http.Client{
		// 手动跟随重定向：3xx 原样返回给循环逐跳校验。
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   ConnectTimeoutMS * time.Millisecond,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout:   ConnectTimeoutMS * time.Millisecond,
			ResponseHeaderTimeout: ConnectTimeoutMS * time.Millisecond,
		},
	}

	current := rawURL
	for redirect := 0; redirect <= maxRedirects; redirect++ {
		if err := throwIfCancelled(isCancelled); err != nil {
			return "", err
		}
		if !urlValidator(current) {
			return "", errors.New("重定向地址不合法（仅允许 https 公网地址）")
		}
		req, err := http.NewRequest(http.MethodGet, current, nil)
		if err != nil {
			return "", err
		}
		if resumeEtag != "" && resumeOffset > 0 {
			req.Header.Set("If-Range", resumeEtag)
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeOffset))
		}
		// 块间超时经 ctx 取消实现：停滞/取消时中止阻塞中的 Read（对齐 TS 版
		// AbortController，主进程侧计时——00 文档 TIMER 职责铁律）。
		ctx, cancelCtx := context.WithCancel(context.Background())
		defer cancelCtx()
		req = req.WithContext(ctx)
		resp, err := client.Do(req)
		if err != nil {
			return "", errors.New("连接超时（15 秒无响应）")
		}

		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			location := resp.Header.Get("Location")
			if location == "" {
				resp.Body.Close()
				return "", errors.New("重定向缺少 Location 头")
			}
			base, err := url.Parse(current)
			if err != nil {
				resp.Body.Close()
				return "", err
			}
			reference, err := url.Parse(location)
			if err != nil {
				resp.Body.Close()
				return "", err
			}
			current = base.ResolveReference(reference).String()
			resp.Body.Close()
			continue
		}
		// 206 = 续传命中；200 = 服务器忽略 Range 或资源已变（If-Range 未通过）→ 整体重下。
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
			resp.Body.Close()
			return "", fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
		}
		resuming := resp.StatusCode == http.StatusPartialContent && resumeOffset > 0
		if !resuming {
			resumeOffset = 0
			_ = os.Remove(tempFile) // 旧半成品无法清理：写入模式会覆盖
		}
		// 服务器返回新 ETag 且本次为整段下载：登记 sidecar 供中断后续传。
		etag := resp.Header.Get("ETag")
		if etag != "" && !resuming {
			if err := os.WriteFile(etagFile, []byte(etag), 0o644); err == nil {
				resumeEtag = etag
			}
			// sidecar 写失败：仅失去续传能力
		}

		contentLength := int64(-1)
		if h := resp.Header.Get("Content-Length"); h != "" {
			if v, err := strconv.ParseInt(h, 10, 64); err == nil {
				contentLength = v
			}
		}
		total := contentLength
		if resuming && contentLength >= 0 {
			total = resumeOffset + contentLength
		}

		flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		if resuming {
			flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
		}
		sink, err := os.OpenFile(tempFile, flags, 0o644)
		if err != nil {
			resp.Body.Close()
			return "", err
		}
		// 块间超时：每收到一块重置；停滞即 cancel 中止 Read（stalled 标记区分错误文案）。
		stalled := false
		chunkTimer := time.AfterFunc(ChunkTimeoutMS*time.Millisecond, func() {
			stalled = true
			cancelCtx()
		})
		armChunkTimer := func() { chunkTimer.Reset(ChunkTimeoutMS * time.Millisecond) }

		received := int64(0)
		buf := make([]byte, 64<<10)
		readErr := error(nil)
		for {
			if err := throwIfCancelled(isCancelled); err != nil {
				readErr = err
				break
			}
			armChunkTimer()
			n, readE := resp.Body.Read(buf)
			if n > 0 {
				received += int64(n)
				if _, werr := sink.Write(buf[:n]); werr != nil {
					readErr = werr
					break
				}
				if onProgress != nil {
					onProgress(resumeOffset+received, total)
				}
			}
			if readE != nil {
				if readE == io.EOF {
					readErr = nil
				} else if readErr == nil {
					readErr = readE
				}
				break
			}
		}
		chunkTimer.Stop()
		closeErr := sink.Close()
		resp.Body.Close()
		cancelCtx()
		if readErr == nil && closeErr != nil {
			readErr = closeErr
		}
		if readErr == nil {
			// 下载完成：不再续传（本次将完整消费该 zip），清 sidecar。
			_ = os.Remove(etagFile)
			return tempFile, nil
		}
		// 中断：保留半成品 + sidecar（若服务器未给 ETag 则无从续传，清残留）。
		if resumeEtag == "" {
			_ = os.Remove(tempFile)
		}
		if stalled {
			return "", errors.New("下载停滞（30 秒无新数据）")
		}
		if errors.Is(readErr, CorpusDownloadCancelled) {
			return "", readErr
		}
		return "", readErr
	}
	return "", errors.New("重定向次数过多")
}

// VerifyZipIntegrity 校验下载产物完整性：zip 魔数 + 大小区间（corpus_downloader.dart:215-236）。
func VerifyZipIntegrity(zipPath string) error {
	st, err := os.Stat(zipPath)
	if err != nil {
		return err
	}
	if st.Size() < MinZipBytes || st.Size() > MaxZipBytes {
		return fmt.Errorf(
			"语料包大小异常（%d 字节，允许 %d-%d），可能不是有效的语料包", st.Size(), MinZipBytes, MaxZipBytes)
	}
	head, err := readHeadBytes(zipPath, 4)
	if err != nil {
		return err
	}
	isZip := len(head) == 4 && head[0] == 0x50 && head[1] == 0x4b && head[2] == 0x03 && head[3] == 0x04
	if !isZip {
		return errors.New("语料包格式异常（缺少 zip 魔数 PK\\x03\\x04）")
	}
	return nil
}

func readHeadBytes(path string, length int) ([]byte, error) {
	// 读取文件前 length 字节（corpus_downloader.dart 用 RAF；此处等效轻实现）。
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, length)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:n], nil
}

// ExtractZipAtomic 把 zipPath 解压并原子替换 targetDir（corpus_downloader.dart:116-141）。
// 先解压到目标目录旁的 `corpus.tmp-<ts>` 临时目录（同 parent，rename 不跨设备），
// 全部成功后删除旧目标目录并 rename 替换；任何失败整体删除临时目录。
func ExtractZipAtomic(zipPath, targetDir string, isCancelled func() bool) (CorpusDownloadResult, error) {
	stagingDir := filepath.Join(filepath.Dir(targetDir), fmt.Sprintf("corpus.tmp-%d", time.Now().UnixMilli()))
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return CorpusDownloadResult{}, err
	}
	counts, err := extractZip(zipPath, stagingDir)
	if err == nil {
		err = throwIfCancelled(isCancelled)
	}
	if err == nil {
		if err = os.RemoveAll(targetDir); err == nil {
			err = os.Rename(stagingDir, targetDir)
		}
	}
	if err != nil {
		removeDirRetry(stagingDir) // 临时解压目录清理失败不掩盖原始异常
		return CorpusDownloadResult{}, err
	}
	return counts, nil
}

// removeDirRetry 目录删除（先关 fd 再删已由调用方保证 fd 生命周期；带退避重试
// 兜底 Windows 句柄滞后/AV 短暂持句——09 §2.4 CI 平台性教训）。
func removeDirRetry(path string) {
	for attempt := 0; ; attempt++ {
		if err := os.RemoveAll(path); err == nil {
			return
		} else if attempt >= 5 {
			return // 清理失败不掩盖主流程
		}
		time.Sleep(time.Duration(50*(attempt+1)) * time.Millisecond)
	}
}

// extractZip 解压本地 zip 文件到 targetDir（含 zip-slip 防护，corpus_downloader.dart:253-311）。
// Windows 保留名/尾随点空格条目、符号链接条目与写入异常条目跳过并计数，
// 不中断整体解压；目录条目落盘建目录（失败才计 skip）；单条目内容损坏
// （CRC/截断）跳过并计数，不中断整体（对齐 TS 逐条目 try/catch）。
func extractZip(zipPath, targetDir string) (CorpusDownloadResult, error) {
	count := 0
	skipped := 0
	skip := func() { skipped++ }
	err := zipEntries(zipPath, func(entry ZipEntryLite, data []byte, entryErr error) error {
		if entryErr != nil {
			skip() // 条目内容损坏：跳过并计数
			return nil
		}
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		// 符号链接条目一律拒绝；绝对路径与 .. 段一律拒绝。
		if entry.IsSymlink || strings.HasPrefix(name, "/") || containsSegment(name, "..") {
			skip()
			return nil
		}
		segments := splitNonEmpty(name, '/')
		if len(segments) == 0 {
			skip()
			return nil
		}
		for _, s := range segments {
			if strings.Contains(s, ":") || s == "." {
				skip()
				return nil
			}
		}
		// Windows 保留设备名与尾随 `.`/空格：写入必失败，直接跳过。
		for _, s := range segments {
			if strings.HasSuffix(s, ".") || strings.HasSuffix(s, " ") || reservedSegment.MatchString(s) {
				skip()
				return nil
			}
		}
		// 词法重组（已拒绝 .. / 盘符，不可能逃出目标目录）。
		outPath := filepath.Join(targetDir, filepath.Join(segments...))
		if entry.IsDirectory {
			// 目录条目：落盘建目录（成功不计 skipped；对齐 TS mkdirSync 语义）。
			if err := os.MkdirAll(outPath, 0o755); err != nil {
				skip()
			}
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
			skip() // 同名冲突（先文件后目录）、权限、磁盘满等单条目异常：跳过并计数
			return nil
		}
		if err := os.WriteFile(outPath, data, 0o644); err != nil {
			skip()
			return nil
		}
		count++
		return nil
	})
	if err != nil {
		return CorpusDownloadResult{}, err
	}
	return CorpusDownloadResult{Extracted: count, Skipped: skipped}, nil
}

func containsSegment(name, seg string) bool {
	for _, s := range splitNonEmpty(name, '/') {
		if s == seg {
			return true
		}
	}
	return false
}

func splitNonEmpty(s string, sep byte) []string {
	parts := strings.Split(s, string(sep))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
