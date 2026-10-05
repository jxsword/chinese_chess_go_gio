package llm

// 可编程 mock SSE 服务器（09 文档 §2.3；test/main/helpers/mockLlmServer.ts 的 Go 移植）：
// StreamTransport 走真实 net/http 路径连 127.0.0.1，本服务器按脚本逐块推送。

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"
)

// MockSseRequest 捕获的请求。
type MockSseRequest struct {
	Method  string
	URL     string
	Headers http.Header
	Body    string
}

// MockSseAPI 写出/收尾 API：默认 200 + text/event-stream 头在首次 write 懒发送；
// 需要自定义状态码（如 HTTP 4xx/5xx 场景）时在首次 write 前调用 Respond。
type MockSseAPI struct {
	write   func(text string)
	end     func(data ...string)
	respond func(status int, headers map[string]string)
}

// Write 推送一段文本（自动 flush）。
func (a MockSseAPI) Write(text string) { a.write(text) }

// End 结束响应；可附带最终数据（如非 200 响应体）。
func (a MockSseAPI) End(data ...string) { a.end(data...) }

// Respond 自定义响应头/状态码（非 200 场景）。
func (a MockSseAPI) Respond(status int, headers map[string]string) {
	a.respond(status, headers)
}

// MockSseServer mock 服务器句柄。
type MockSseServer struct {
	// URL 形如 http://127.0.0.1:PORT 的根地址。
	URL      string
	requests []MockSseRequest
	mu       sync.Mutex
	srv      *httptest.Server
}

// Requests 捕获的请求序列。
func (s *MockSseServer) Requests() []MockSseRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]MockSseRequest(nil), s.requests...)
}

// Close 关闭服务器。
func (s *MockSseServer) Close() { s.srv.Close() }

// MockSseHandler 请求处理脚本（可阻塞以控制块间节奏）。
type MockSseHandler func(req MockSseRequest, api MockSseAPI)

// StartMockSseServer 启动 mock SSE 服务器。
func StartMockSseServer(handler MockSseHandler) *MockSseServer {
	s := &MockSseServer{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		req := MockSseRequest{Method: r.Method, URL: r.URL.String(), Headers: r.Header, Body: string(raw)}
		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()

		responded := false
		ensureHead := func() {
			if !responded {
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Cache-Control", "no-cache")
				w.WriteHeader(http.StatusOK)
				responded = true
			}
		}
		// 取消/超时用例会中止客户端连接：写已断开连接的错误对断言无意义
		//（协议断言在客户端侧），静默吞掉防未处理错误（Electron 版 CI 教训）。
		api := MockSseAPI{
			write: func(text string) {
				ensureHead()
				select {
				case <-r.Context().Done():
					return // 客户端已断开：best-effort，直接结算
				default:
				}
				_, _ = w.Write([]byte(text))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			},
			end: func(data ...string) {
				ensureHead()
				if len(data) > 0 {
					_, _ = w.Write([]byte(strings.Join(data, "")))
				}
			},
			respond: func(status int, headers map[string]string) {
				for k, v := range headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(status)
				responded = true
			},
		}
		func() {
			defer func() {
				// handler 内写已断开连接的 panic 防御（对齐 TS 版 catch 收尾）。
				_ = recover()
			}()
			handler(req, api)
		}()
	}))
	s.URL = s.srv.URL
	return s
}

// SSEData SSE data 行便捷构造（mockLlmServer.ts sseData）。
func SSEData(payload string) string { return "data: " + payload + "\n\n" }

// sleep 延时工具（脚本节奏控制）。
func sleep(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }
