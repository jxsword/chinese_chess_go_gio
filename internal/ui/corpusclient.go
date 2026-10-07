package ui

// 语料客户端（T5'.1，design_docs/00 §4 语料行 + 06 附录 §1/§5 消费方式注记）：
// 语料扫描/读文件/解析/索引经复制物（internal/storage + internal/parsers）在
// 后台 goroutine 直调，回执经 app 事件总线回主循环（铁律 #G3 正方向）。
// 解析 Runner 消息形状 {id,type,payload} 在复制物内保留；SSRF 白名单与
// zip-slip 防护为复制物内建，本层零改动（06 附录：Gio 侧零改动）。

import (
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/jxsword/chinese_chess_go_gio/internal/parsers"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// CorpusEnv 语料页环境（app 装配层注入；仅主 goroutine 触达）。
type CorpusEnv struct {
	// Emit 提交事件回主循环（requestId 关联；空 id 直通）。
	Emit func(requestID string, payload any, err error)
	// Cancel 取消在途异步请求（迟到回执按 id 丢弃——铁律 #G5）。
	Cancel func(requestID string)
	// NewRequestID 生成页面内唯一请求 ID。
	NewRequestID func(prefix string) string
	// Root 当前生效语料目录解析（用户设置>legacy>默认，app 注入）。
	Root func() string
	// IO 语料 I/O 注入（nil = 生产复制物实现；性能实测/测试注入）。
	IO state.CorpusIO
	// Settings 设置存储（导入本地语料目录：corpus.userPath 读写；nil = 无设置面）。
	Settings *storage.Settings
}

// goCorpusDriver 生产驱动：fn 在独立 goroutine 执行（阻塞 I/O 不进主循环）。
type goCorpusDriver struct{}

func (goCorpusDriver) Go(fn func()) { go fn() }

// corpusIO state.CorpusIO 生产实现：复制物直调（工作 goroutine 内调用）。
type corpusIO struct {
	root   func() string
	runner *parsers.Runner
	reqSeq atomic.Int64
}

// NewCorpusIO 创建语料 I/O 实现（root = 当前生效语料目录解析器，app 注入
// 用户设置>legacy>默认 口径）。
func NewCorpusIO(root func() string) state.CorpusIO {
	return &corpusIO{root: root, runner: parsers.NewRunner()}
}

func (c *corpusIO) Scan() (storage.CorpusScanResult, error) {
	return storage.ScanCorpus(c.root()), nil
}

func (c *corpusIO) ListEntries(categoryPath, categoryName string) ([]storage.CorpusEntry, error) {
	return storage.ListXqfEntries(categoryPath, categoryName), nil
}

func (c *corpusIO) ReadFiles(paths []string) ([]storage.CorpusFileBytes, error) {
	return storage.ReadCorpusFiles(paths), nil
}

// ParseBatch 批量解析：经复制物 parsers.Runner（goroutine+ctx 内建）。
// 请求 ID 自增（页面操作 id 归 emit 面；批内取消由 Runner ctx 文件边界探针承担）。
func (c *corpusIO) ParseBatch(files []parsers.ParseFileInput) ([]*parsers.ParsedPuzzle, error) {
	id := "corpus-parse-" + strconv.FormatInt(c.reqSeq.Add(1), 10)
	resp := <-c.runner.Submit(parsers.Request{
		ID:      id,
		Type:    parsers.ReqParseBatch,
		Payload: mustJSON(parsers.ParseBatchPayload{Files: files}),
	}, nil)
	if resp.Error != "" {
		return nil, errors.New(resp.Error)
	}
	var result parsers.ParseBatchResult
	if resp.Result == nil {
		return result.Puzzles, nil
	}
	if err := remarshal(resp.Result, &result); err != nil {
		return nil, err
	}
	return result.Puzzles, nil
}

func (c *corpusIO) PgnIndex(path string) ([]storage.PgnIndexEntry, error) {
	return storage.ScanPgnIndex(path, 0)
}

func (c *corpusIO) ReadPgnGame(path string, entry storage.PgnIndexEntry) (string, error) {
	return storage.ReadPgnGameText(path, entry)
}

// CorpusDownloader 语料下载客户端（00 §4 `corpus:progress`/`corpus:download`）：
// 复制物 DownloadCorpus 直调（进度回调经事件总线回主循环；取消=IsCancelled
// 探针 + 总线 Cancel 迟到丢弃双收口，防错 #10——半成品+ETag sidecar 由复制物
// 保留，重试续传）。
type CorpusDownloader struct {
	emit func(requestID string, payload any, err error)
	// root 语料目录解析（上游绑定层 CorpusDownload 语义：targetDir 空 =
	// corpusRoot("")，用户设置>legacy>默认——复制物不做该解析）。
	root func() string
}

// NewCorpusDownloader 创建下载客户端（root = 当前生效语料目录解析器）。
func NewCorpusDownloader(env CorpusEnv) *CorpusDownloader {
	return &CorpusDownloader{emit: env.Emit, root: env.Root}
}

// resolveTargetDir 下载目标目录解析（上游绑定层 CorpusDownload 语义：空 =
// corpusRoot("")）。独立方法便于单测锚定（空串直传复制物会使其 staging 取
// filepath.Dir("")="."、rename 空目标 ENOENT——验收实测缺陷）。
func (d *CorpusDownloader) resolveTargetDir(targetDir string) string {
	if targetDir == "" && d.root != nil {
		return d.root()
	}
	return targetDir
}

// StartAsync 发起下载（阻塞 I/O 在独立 goroutine；goroutine 生存期=请求生存期）。
// isCancelled 为页面注入的取消探针（atomic 标志）。
func (d *CorpusDownloader) StartAsync(requestID, url, targetDir string, isCancelled func() bool) {
	targetDir = d.resolveTargetDir(targetDir)
	go func() {
		_, err := storage.DownloadCorpus(storage.DownloadCorpusOptions{
			URL:       url,
			TargetDir: targetDir,
			OnProgress: func(received, total int64) {
				d.emit(requestID, CorpusDownloadProgress{RequestID: requestID, Received: received, Total: total}, nil)
			},
			IsCancelled: isCancelled,
		})
		// 事件总线 Err 字段不进页面（app.Run 仅分发 Payload），错误内嵌载荷。
		d.emit(requestID, CorpusDownloadDone{RequestID: requestID, Err: err}, nil)
	}()
}
