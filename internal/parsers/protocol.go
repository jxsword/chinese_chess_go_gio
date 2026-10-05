package parsers

// 解析协议层（06 文档 §6；00 §3.2 通道清单，DR-003 goroutine + ctx）。
//
// Worker 消息形状在内部协议保留（铁律 #7）：
//   - 请求 {id, type: 'parseBatch'|'cancel', payload}
//   - 响应 {id, ok, result?, error?, progress?}
//
// 与 Electron 版 parser.worker 的差异（electron-DR-016 → goroutine 分批）：
// Go 侧每请求独立 goroutine + context 取消（Runner 注册表），分批 ≤128 与
// generation 防陈旧由前端收口；每解析完一个文件经 onProgress 回报
// {done, total}（语料库页进度条 n/m）。
// 纯 Go：禁止 import Wails / net/http / frontend 任何符号（铁律 #1）。

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
)

// RequestType 请求类型（对齐前端 ParserRequestType）。
type RequestType string

const (
	ReqParseBatch RequestType = "parseBatch"
	ReqCancel     RequestType = "cancel"
)

// Request 请求消息 {id, type, payload}。
type Request struct {
	ID      string          `json:"id"`
	Type    RequestType     `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Progress 批量解析进度（{done, total}，语料库页进度条）。
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Response 响应消息 {id, ok, result?, error?, progress?}。
type Response struct {
	ID       string    `json:"id"`
	OK       bool      `json:"ok"`
	Result   any       `json:"result,omitempty"`
	Error    string    `json:"error,omitempty"`
	Progress *Progress `json:"progress,omitempty"`
}

// ErrCanceled 取消错误（文案对齐前端 CANCELED_ERROR = 'canceled'）。
var ErrCanceled = errors.New("canceled")

// ParseFileInput parseBatch 载荷条目（单批 ≤128 个文件，06 文档 §6 分批约定）。
type ParseFileInput struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	// Bytes 文件字节（Wails 通道以 base64 字符串承载，绑定层解码后进入协议层）。
	Bytes []byte `json:"bytes"`
}

// ParseBatchPayload parseBatch 载荷。
type ParseBatchPayload struct {
	Files []ParseFileInput `json:"files"`
}

// ParseBatchResult parseBatch 结果：与 files 等长，损坏/无可演示走法位为 null。
type ParseBatchResult struct {
	Puzzles []*ParsedPuzzle `json:"puzzles"`
}

// Handle 协议处理核心（对齐 parserProtocol.ts dispatch）：单文件失败不中断批次；
// ctx 取消在每文件边界探针处生效（已解析位保持，剩余位为 null，以 canceled 结算）。
// onProgress 非 nil 时每完成一个文件回调 (done, total)。
func Handle(ctx context.Context, req Request, onProgress func(done, total int)) Response {
	switch req.Type {
	case ReqParseBatch:
		var p ParseBatchPayload
		if err := json.Unmarshal(req.Payload, &p); err != nil {
			return Response{ID: req.ID, Error: err.Error()}
		}
		files := p.Files
		puzzles := make([]*ParsedPuzzle, len(files))
		done := 0
		for i, file := range files {
			if ctx.Err() != nil {
				// 批中途被取消：剩余位保持 null，以 canceled 结算。
				return Response{ID: req.ID, Error: ErrCanceled.Error()}
			}
			if p, err := ParsePuzzleFile(file.Name, file.Bytes, file.Source); err == nil && len(p) > 0 {
				puzzles[i] = p[0]
			}
			done++
			if onProgress != nil {
				onProgress(done, len(files))
			}
		}
		return Response{ID: req.ID, OK: true, Result: ParseBatchResult{Puzzles: puzzles}}
	default:
		return Response{ID: req.ID, Error: "Unknown parser request type: " + string(req.Type)}
	}
}

// Runner 请求运行器：每请求独立 goroutine + context 取消注册表（DR-003，
// 与 engine.Runner 同型）。Cancel 对在途请求触发 ctx 取消（批文件边界中止）；
// 对未开始/已结束的请求幂等。响应经带缓冲通道回投，goroutine 不泄漏。
type Runner struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// NewRunner 构造运行器。
func NewRunner() *Runner {
	return &Runner{cancels: make(map[string]context.CancelFunc)}
}

// Submit 受理请求：立即返回响应通道（单请求单 goroutine）；onProgress 经
// 回调转发（绑定层发 parser:progress 事件）。
func (r *Runner) Submit(req Request, onProgress func(done, total int)) <-chan Response {
	ch := make(chan Response, 1)
	if req.Type == ReqCancel {
		r.Cancel(req.ID)
		ch <- Response{ID: req.ID, OK: true}
		return ch
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancels[req.ID] = cancel
	r.mu.Unlock()
	go func() {
		resp := Handle(ctx, req, onProgress)
		r.mu.Lock()
		delete(r.cancels, req.ID)
		r.mu.Unlock()
		cancel() // 释放 ctx 资源
		ch <- resp
	}()
	return ch
}

// Cancel 取消在途请求（幂等；未知 id 无操作）。
func (r *Runner) Cancel(requestID string) {
	r.mu.Lock()
	cancel, ok := r.cancels[requestID]
	delete(r.cancels, requestID)
	r.mu.Unlock()
	if ok {
		cancel()
	}
}
