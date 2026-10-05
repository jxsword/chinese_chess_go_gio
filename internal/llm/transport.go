package llm

// LLM 传输层（05 文档 §3.2；替代 Electron 版 main 进程 LlmProxy，llm-proxy.ts 1:1 移植）。
//
//   - StreamTransport：net/http 流式 POST + bufio.Scanner 按行读（半行跨 TCP
//     分包由 scanner 缓冲重组；Buffer 上限调大防超长行截断）。
//   - 超时模型：空闲超时 = 两次数据块最大间隔（每块重置计时器），默认 60s、
//     clamp 5~600s；总上限 = 空闲×4 独立计时器；二者触发 cancel + error 事件，
//     错误文案逐字沿用（llm-proxy.ts:122/130）。
//   - 取消：Cancel(requestID) → context cancel，幂等，之后无任何事件。
//   - HTTP ≠ 200：截响应体前 160 字符 + AnnotateModelHint（llm-proxy.ts:163-167）。
//   - Proxy：绑定层入口（受理即返回 + 事件回发 + authSlot 注入，DR-010 对应）。
//
// 纯 Go：本文件是 internal/* 唯一对外 HTTP 收口（铁律 #4），
// 禁止 import Wails / frontend。

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
)

// ErrCanceled 取消标记错误（llmPlayer.ts LLM_CANCELED 同文）。
var ErrCanceled = errors.New("llm chat canceled")

// ChatRequest 单次流式对话请求（transport.ts LlmChatWireRequest 同形）。
type ChatRequest struct {
	RequestID string
	URL       string
	Headers   map[string]string
	Body      string
	// AuthSlot 掩码 Key 回读场景：由 Proxy 按槽位注入真实 Authorization（DR-010）。
	AuthSlot string
}

// ChatHandlers 单次 chat 的结局回调（done/error 必有其一，且至多一次；取消时两者皆无）。
type ChatHandlers struct {
	// OnChunk 增量转发（UI 流式展示用；着法管线不依赖）。
	OnChunk func(delta Delta)
	// OnDone 正常结束：全量回复文本（正文为空时为思维链全文）。
	OnDone func(text string)
	// OnError 失败结束：错误消息。
	OnError func(message string)
}

// StreamOptions 计时器注入（测试短超时用；零值 = 按设置解析并 clamp）。
type StreamOptions struct {
	IdleTimeout  time.Duration
	TotalTimeout time.Duration
}

// Transport 传输抽象（packages/llm transport.ts 的 Go 对应；测试注入 fake）。
type Transport interface {
	// Chat 发起一次流式对话，阻塞至结算。结局经 handlers 传递（OnDone/OnError
	// 恰其一）；取消（ctx 或 Cancel(requestID)）后不回调任何函数并返回 ctx 错误。
	Chat(ctx context.Context, req ChatRequest, handlers ChatHandlers) error
	// Cancel 取消在途请求（幂等；取消后不再有任何事件）。
	Cancel(requestID string)
}

// StreamTransport 流式 HTTP 传输（每请求独立计时器与取消注册）。
type StreamTransport struct {
	// GetTimeoutSeconds 空闲超时秒数来源（llm_settings_timeoutSeconds；
	// 未配置回落默认并 clamp 5~600）。nil 时用缺省 60。
	GetTimeoutSeconds func() int

	client *http.Client

	mu     sync.Mutex
	active map[string]*streamCall
}

// NewStreamTransport 构造传输层。
func NewStreamTransport(getTimeoutSeconds func() int) *StreamTransport {
	return &StreamTransport{
		GetTimeoutSeconds: getTimeoutSeconds,
		client:            &http.Client{}, // 无整体超时：空闲/总上限计时器自行管理
		active:            make(map[string]*streamCall),
	}
}

// streamCall 单次在途请求的状态（llm-proxy.ts ActiveRequest 对应）。
type streamCall struct {
	cancel     context.CancelFunc
	idleTimer  *time.Timer
	totalTimer *time.Timer
	cancelled  bool // 仅用户取消（Cancel/上游 ctx）；计时器路径不置位（对齐 llm-proxy.ts）
	settled    bool
}

// Chat 实现 Transport（按设置解析的超时；测试注入短超时用 ChatWithOptions）。
func (t *StreamTransport) Chat(ctx context.Context, req ChatRequest, handlers ChatHandlers) error {
	return t.ChatWithOptions(ctx, req, handlers, StreamOptions{})
}

// ChatWithOptions 带计时器注入的流式对话（Chat 的全参形态）。
func (t *StreamTransport) ChatWithOptions(ctx context.Context, req ChatRequest, handlers ChatHandlers, opts StreamOptions) error {
	idle, total := t.resolveTimeouts(opts)
	idleSec := int(math.Round(idle.Seconds()))
	totalSec := int(math.Round(total.Seconds()))

	callCtx, cancel := context.WithCancel(ctx)
	call := &streamCall{cancel: cancel}
	t.mu.Lock()
	t.active[req.RequestID] = call
	t.mu.Unlock()
	done := make(chan struct{}) // Chat 返回时关闭，回收上游取消守望
	defer func() {
		t.mu.Lock()
		if t.active[req.RequestID] == call {
			delete(t.active, req.RequestID)
		}
		t.mu.Unlock()
		cancel()
		close(done)
	}()

	// 结局结算（llm-proxy.ts fail/succeed 语义）：settled/cancelled 双守卫，
	// 迟到事件与二次结算一律丢弃。
	var settleOnce sync.Once
	settle := func(isError bool, payload string) {
		settleOnce.Do(func() {
			t.mu.Lock()
			blocked := call.settled || call.cancelled
			if !blocked {
				call.settled = true
			}
			stopTimers(call)
			t.mu.Unlock()
			if blocked {
				return
			}
			if isError {
				if handlers.OnError != nil {
					handlers.OnError(payload)
				}
			} else if handlers.OnDone != nil {
				handlers.OnDone(payload)
			}
		})
	}
	fail := func(message string) { settle(true, message) }
	succeed := func(text string) { settle(false, text) }

	// 空闲计时器：每收到一块重置（llm-proxy.ts resetIdle）。触发 = abort + error。
	resetIdle := func() {
		t.mu.Lock()
		if call.idleTimer != nil {
			call.idleTimer.Stop()
		}
		call.idleTimer = time.AfterFunc(idle, func() {
			cancel()
			fail(fmt.Sprintf("空闲超时（%ds 内无响应数据，可在对局设置中调大）", idleSec))
		})
		t.mu.Unlock()
	}

	// 总耗时上限独立计时（05 §3.2：防思维链无限制输出）。
	t.mu.Lock()
	call.totalTimer = time.AfterFunc(total, func() {
		cancel()
		fail(fmt.Sprintf("总耗时超过 %ds（可在对局设置中调大超时，或为 Qwen3 等模型开启「关闭思维链」）", totalSec))
	})
	t.mu.Unlock()

	// 上游 ctx 取消（游戏侧取消语义）：标记 cancelled + 停表 + abort，此后无事件
	//（与 Cancel(requestID) 同路径；计时器路径不置位 cancelled，error 正常回发）。
	go func() {
		select {
		case <-ctx.Done():
			t.markCancelled(call)
		case <-done:
		}
	}()

	httpReq, err := http.NewRequestWithContext(callCtx, http.MethodPost, req.URL, strings.NewReader(req.Body))
	if err != nil {
		fail("连接失败：" + err.Error())
		return nil
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}

	resetIdle()
	resp, err := t.client.Do(httpReq)
	if err != nil {
		// 取消/超时已发事件或不应发事件：静默收尾（llm-proxy.ts:154-161）。
		t.mu.Lock()
		blocked := call.settled || call.cancelled
		t.mu.Unlock()
		if blocked {
			return callCtx.Err()
		}
		fail("连接失败：" + err.Error())
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		body := safeReadBody(resp)
		_ = resp.Body.Close()
		fail(AnnotateModelHint(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, excerpt(body))))
		return nil
	}
	if resp.Body == nil {
		fail("HTTP 200：响应无内容流")
		return nil
	}
	defer resp.Body.Close()

	assembler := &SseAssembler{}
	// 逐块读取 + 自切行（结构对齐 llm-proxy.ts:179-195）：
	//   buffer += chunk；按 \n 切出完整行逐行处理；
	//   每收到一块 → 重置空闲计时器（不是每行——持续吐字不带换行不误判）；
	//   EOF 时残留半行丢弃（对齐 :196-198），以已累积内容结算。
	// 半行跨 TCP 分包由 buf 拼接重组；行长无上限（对齐 TS string 累积，05 §3.2）。
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(tmp)
		if n > 0 {
			resetIdle() // 每收到一块 → 重置空闲计时器（05 §3.2）
			buf = append(buf, tmp[:n]...)
			for {
				idx := bytes.IndexByte(buf, '\n')
				if idx < 0 {
					break
				}
				// 先拷贝再挪移：line 与 buf 同底层数组，直接 append(buf[:0],…)
				// 会覆盖 line 数据（实测 chunk 全部损坏）。
				line := trimEOL(append([]byte(nil), buf[:idx]...))
				buf = append(buf[:0], buf[idx+1:]...)
				t.mu.Lock()
				blocked := call.settled || call.cancelled
				t.mu.Unlock()
				if blocked {
					return callCtx.Err()
				}
				result, herr := assembler.HandleLine(string(line))
				if herr != nil {
					fail(herr.Error())
					return nil
				}
				t.mu.Lock()
				blocked = call.settled || call.cancelled
				t.mu.Unlock()
				if blocked {
					return callCtx.Err()
				}
				if result.Delta != nil && handlers.OnChunk != nil {
					handlers.OnChunk(*result.Delta)
				}
				if result.Ended {
					succeed(assembler.PickAnswer())
					return nil
				}
			}
		}
		if errors.Is(rerr, io.EOF) {
			break // 残留半行丢弃，流自然结束
		}
		if rerr != nil {
			t.mu.Lock()
			blocked := call.settled || call.cancelled
			t.mu.Unlock()
			if blocked {
				return callCtx.Err()
			}
			fail("连接中断：" + rerr.Error())
			return nil
		}
	}
	// 流自然结束：以已累积内容结算。
	succeed(assembler.PickAnswer())
	return nil
}

// trimEOL 去掉行尾 \n 与 \r\n（TS 版按 indexOf('\n') 切分后不额外剥 \r，
// 但 SseAssembler 的 TrimSpace 会吃掉行尾空白，两者行为一致；此处剥 \r 仅
// 为减少一次拷贝前的字节量）。
func trimEOL(line []byte) []byte {
	for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
		line = line[:len(line)-1]
	}
	return line
}

// Cancel 取消在途请求（幂等）。取消后不再有任何事件（迟到丢弃由状态位收口）。
func (t *StreamTransport) Cancel(requestID string) {
	t.mu.Lock()
	call, ok := t.active[requestID]
	t.mu.Unlock()
	if ok {
		t.markCancelled(call)
	}
}

// markCancelled 用户取消收口：标记 cancelled、停表、abort（此后 settle 全被守卫）。
func (t *StreamTransport) markCancelled(call *streamCall) {
	t.mu.Lock()
	call.cancelled = true
	stopTimers(call)
	t.mu.Unlock()
	call.cancel()
}

// resolveTimeouts 计时器解析：注入优先，否则设置解析（空闲默认 60s clamp 5~600，
// 总上限 = 空闲×4）。
func (t *StreamTransport) resolveTimeouts(opts StreamOptions) (idle, total time.Duration) {
	if opts.IdleTimeout > 0 {
		if opts.TotalTimeout > 0 {
			return opts.IdleTimeout, opts.TotalTimeout
		}
		return opts.IdleTimeout, 4 * opts.IdleTimeout
	}
	seconds := ResolveTimeoutSeconds(nil)
	if t.GetTimeoutSeconds != nil {
		seconds = ResolveTimeoutSeconds(t.GetTimeoutSeconds())
	}
	idle = time.Duration(seconds) * time.Second
	return idle, 4 * idle
}

// stopTimers 停止并清空计时器（调用方持锁）。
func stopTimers(call *streamCall) {
	if call.idleTimer != nil {
		call.idleTimer.Stop()
		call.idleTimer = nil
	}
	if call.totalTimer != nil {
		call.totalTimer.Stop()
		call.totalTimer = nil
	}
}

// safeReadBody 响应体安全读取：超长截断由 excerpt 负责（160 字符），
// 读取上限 2MB 纯防御（Go 版增强：TS response.text() 无上限）。
func safeReadBody(resp *http.Response) string {
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return ""
	}
	return string(raw)
}

// excerpt 截取响应体前 160 字符（llm_move_source.dart:482-486；按 rune 计数，
// ASCII 与 TS 逐字符一致，中文不产生半字节截断）。
func excerpt(body string) string {
	text := strings.TrimSpace(body)
	runes := []rune(text)
	if len(runes) <= 160 {
		return text
	}
	return string(runes[:160]) + "…"
}

// ---------------------------------------------------------------------------
// Proxy：绑定层入口（llm-proxy.ts 的 sender 事件形态）
// ---------------------------------------------------------------------------

// ProxySender 事件回发接口（Wails 绑定以 EventsEmit 实现；测试以收集器实现）。
type ProxySender interface {
	SendChunk(requestID string, delta Delta)
	SendDone(requestID string, text string)
	SendError(requestID string, message string)
}

// ProxyOptions Proxy 装配选项。
type ProxyOptions struct {
	// GetTimeoutSeconds 空闲超时秒数来源（llm_settings_timeoutSeconds）。
	GetTimeoutSeconds func() int
	// ResolveAPIKey 凭据槽位 → 完整 API Key（authSlot 注入用；无/未配置返回空串）。
	ResolveAPIKey func(slot string) string
}

// Proxy 绑定层代理：受理即返回、结局经 sender 事件回传、authSlot 注入鉴权。
type Proxy struct {
	transport *StreamTransport
	options   ProxyOptions
}

// NewProxy 构造绑定层代理。
func NewProxy(options ProxyOptions) *Proxy {
	return &Proxy{transport: NewStreamTransport(options.GetTimeoutSeconds), options: options}
}

// ActiveCount 在途请求数（测试观察用）。
func (p *Proxy) ActiveCount() int {
	p.transport.mu.Lock()
	defer p.transport.mu.Unlock()
	return len(p.transport.active)
}

// Chat 发起一次流式对话（受理即返回，goroutine 内阻塞至结算）。
// 返回通道在处理完毕（正常/出错/取消）后关闭——绑定层可忽略，测试借此等待。
// 结局一律经 sender 事件传递；取消（Cancel）后不再有任何事件。
func (p *Proxy) Chat(req ChatRequest, sender ProxySender) <-chan struct{} {
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		// DR-010：调用方持掩码 Key，经 authSlot 注入真实鉴权头。
		headers := make(map[string]string, len(req.Headers))
		for k, v := range req.Headers {
			headers[k] = v
		}
		if req.AuthSlot != "" && p.options.ResolveAPIKey != nil {
			if realKey := p.options.ResolveAPIKey(req.AuthSlot); realKey != "" {
				headers["Authorization"] = "Bearer " + realKey
			} else {
				delete(headers, "Authorization")
			}
		}
		inner := ChatRequest{RequestID: req.RequestID, URL: req.URL, Headers: headers, Body: req.Body}
		_ = p.transport.Chat(context.Background(), inner, ChatHandlers{
			OnChunk: func(delta Delta) { sender.SendChunk(req.RequestID, delta) },
			OnDone:  func(text string) { sender.SendDone(req.RequestID, text) },
			OnError: func(message string) { sender.SendError(req.RequestID, message) },
		})
	}()
	return finished
}

// Cancel 取消在途请求（幂等）。
func (p *Proxy) Cancel(requestID string) {
	p.transport.Cancel(requestID)
}

// TestConnectionResult 配置卡"测试连接"结果（ipc/types LlmTestConnectionResult 同形）。
type TestConnectionResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// TestConnection 配置卡"测试连接"（单次最小流式请求，收集结局返回结果；
// llm_move_source.dart:488-497 语义）。authSlot：掩码 Key 场景按槽位注入。
func (p *Proxy) TestConnection(config LlmEndpointConfig, authSlot string) TestConnectionResult {
	built, err := BuildTestConnectionChat(config, authSlot)
	if err != nil {
		// 端点/模型未配置等构建期错误：直接作为连接失败消息（不产生 requestId）。
		return TestConnectionResult{OK: false, Message: "连接失败：" + err.Error()}
	}
	headers := built.Headers
	if built.AuthSlot != "" && p.options.ResolveAPIKey != nil {
		headers = make(map[string]string, len(built.Headers))
		for k, v := range built.Headers {
			headers[k] = v
		}
		if realKey := p.options.ResolveAPIKey(built.AuthSlot); realKey != "" {
			headers["Authorization"] = "Bearer " + realKey
		} else {
			delete(headers, "Authorization")
		}
	}
	type outcome struct {
		done bool
		text string
		msg  string
	}
	result := make(chan outcome, 1)
	requestID := fmt.Sprintf("test-conn-%d", time.Now().UnixNano())
	go func() {
		_ = p.transport.Chat(context.Background(), ChatRequest{
			RequestID: requestID,
			URL:       built.URL,
			Headers:   headers,
			Body:      built.Body,
		}, ChatHandlers{
			OnDone:  func(text string) { result <- outcome{done: true, text: text} },
			OnError: func(message string) { result <- outcome{msg: message} },
		})
	}()
	out := <-result
	if out.done {
		return TestConnectionResult{OK: true, Message: fmt.Sprintf("连接成功，模型 %s 响应正常", config.Model)}
	}
	return TestConnectionResult{OK: false, Message: "连接失败：" + AnnotateModelHint(out.msg)}
}
