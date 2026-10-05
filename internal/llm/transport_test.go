package llm

// 主进程传输层集成测试（T4.1，09 §2.3/§2.4；test/main/llmProxy.spec.ts 移植）：
// mock SSE 服务器 + 真实 net/http 流式路径——
// 块序完整 / 跨分包行重组 / 空闲计时器重置 / 总上限 / cancel → abort /
// HTTP≠200 + AnnotateModelHint / chunk.error / 思维链退回 / authSlot 注入。

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// collector 事件收集器（llmProxy.spec makeCollector）。
type collector struct {
	mu     sync.Mutex
	chunks []Delta
	done   []string
	errors []string
}

func (c *collector) SendChunk(_ string, delta Delta) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chunks = append(c.chunks, delta)
}

func (c *collector) SendDone(_ string, text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.done = append(c.done, text)
}

func (c *collector) SendError(_ string, message string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.errors = append(c.errors, message)
}

func (c *collector) allDone() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.done...)
}

func (c *collector) allErrors() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.errors...)
}

func (c *collector) chunkTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.chunks))
	for _, d := range c.chunks {
		if d.Content != nil {
			out = append(out, *d.Content)
		} else {
			out = append(out, "")
		}
	}
	return out
}

func (c *collector) reasoningTexts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, 0, len(c.chunks))
	for _, d := range c.chunks {
		if d.Reasoning != nil {
			out = append(out, *d.Reasoning)
		}
	}
	return out
}

// makeProxy 构造绑定层代理（空闲超时来源 60s；计时器场景经 ChatWithOptions 注入）。
func makeProxy(resolveApiKey func(string) string) *Proxy {
	return NewProxy(ProxyOptions{
		GetTimeoutSeconds: func() int { return 60 },
		ResolveAPIKey:     resolveApiKey,
	})
}

// handlersFrom 收集器 → ChatHandlers（StreamTransport 直连形态）。
func handlersFrom(c *collector) ChatHandlers {
	return ChatHandlers{
		OnChunk: func(d Delta) { c.SendChunk("", d) },
		OnDone:  func(text string) { c.SendDone("", text) },
		OnError: func(msg string) { c.SendError("", msg) },
	}
}

func chatReq(url, body string) ChatRequest {
	return ChatRequest{
		RequestID: "r1",
		URL:       url,
		Headers:   map[string]string{"Content-Type": "application/json", "Accept": "text/event-stream"},
		Body:      body,
	}
}

func TestProxyNormalStream(t *testing.T) {
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Write(SSEData(`{"choices":[{"delta":{"content":"着法"}}]}`))
		api.Write(SSEData(`{"choices":[{"delta":{"content":": b2-e2"}}]}`))
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	<-proxy.Chat(chatReq(server.URL+"/v1/chat/completions", "{}"), c)
	if errs := c.allErrors(); len(errs) != 0 {
		t.Fatalf("不应有错误：%v", errs)
	}
	if got := strings.Join(c.chunkTexts(), ""); got != "着法: b2-e2" {
		t.Fatalf("chunk 序列 = %q", got)
	}
	if done := c.allDone(); len(done) != 1 || done[0] != "着法: b2-e2" {
		t.Fatalf("done = %v", done)
	}
	if proxy.ActiveCount() != 0 {
		t.Fatalf("在途请求应清零，实际 %d", proxy.ActiveCount())
	}
}

func TestProxyLineReassembly(t *testing.T) {
	// 一行拆三段写（含中文多字节边界），最后补 \n
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Write(`data: {"choices":[{"del`)
		api.Write(`ta":{"content":"炮二平五，马8进7，用半个`)
		api.Write("「着法: h2-e2」\"}}]}\n\n")
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	<-proxy.Chat(chatReq(server.URL, "{}"), c)
	if errs := c.allErrors(); len(errs) != 0 {
		t.Fatalf("不应有错误：%v", errs)
	}
	if got := strings.Join(c.chunkTexts(), ""); got != "炮二平五，马8进7，用半个「着法: h2-e2」" {
		t.Fatalf("重组文本 = %q", got)
	}
	if len(c.allDone()) != 1 || !strings.Contains(c.allDone()[0], "炮二平五") {
		t.Fatalf("done = %v", c.allDone())
	}
}

func TestProxyIdleTimerResetPerChunk(t *testing.T) {
	// 块间 80ms < 空闲 150ms：计时器逐块重置，不误判；总上限 600ms > 全程。
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		for i := 0; i < 4; i++ {
			sleep(80)
			api.Write(SSEData(fmt.Sprintf(`{"choices":[{"delta":{"content":"块%d"}}]}`, i)))
		}
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	transport := NewStreamTransport(func() int { return 60 })
	c := &collector{}
	err := transport.ChatWithOptions(t.Context(), chatReq(server.URL, "{}"), handlersFrom(c), StreamOptions{IdleTimeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatalf("Chat err = %v", err)
	}
	if errs := c.allErrors(); len(errs) != 0 {
		t.Fatalf("不应有错误：%v", errs)
	}
	if len(c.chunkTexts()) != 4 {
		t.Fatalf("chunk 数 = %d", len(c.chunkTexts()))
	}
	if done := c.allDone(); len(done) != 1 || done[0] != "块0块1块2块3" {
		t.Fatalf("done = %v", done)
	}
}

func TestProxyIdleTimeout(t *testing.T) {
	// 块间最大间隔超限 → error 含「空闲超时」并中止。
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Write(SSEData(`{"choices":[{"delta":{"content":"首块"}}]}`))
		sleep(500) // 远超空闲 120ms
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	transport := NewStreamTransport(func() int { return 60 })
	c := &collector{}
	_ = transport.ChatWithOptions(t.Context(), chatReq(server.URL, "{}"), handlersFrom(c), StreamOptions{IdleTimeout: 120 * time.Millisecond})
	if len(c.allDone()) != 0 {
		t.Fatalf("不应有 done：%v", c.allDone())
	}
	errs := c.allErrors()
	if len(errs) != 1 {
		t.Fatalf("应恰好一次 error：%v", errs)
	}
	for _, frag := range []string{"空闲超时", "内无响应数据", "可在对局设置中调大"} {
		if !strings.Contains(errs[0], frag) {
			t.Fatalf("错误文案缺 %q：%q", frag, errs[0])
		}
	}
}

func TestProxyTotalLimit(t *testing.T) {
	// 每块间隔 50ms（< 空闲 100ms，不触发空闲超时），共 12 块 ≈ 600ms；
	// 总上限 400ms 将先触发（总秒数 round 后 = 0）。
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		for i := 0; i < 12; i++ {
			sleep(50)
			api.Write(SSEData(fmt.Sprintf(`{"choices":[{"delta":{"content":"%d"}}]}`, i)))
		}
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	transport := NewStreamTransport(func() int { return 60 })
	c := &collector{}
	_ = transport.ChatWithOptions(t.Context(), chatReq(server.URL, "{}"), handlersFrom(c), StreamOptions{IdleTimeout: 100 * time.Millisecond})
	if len(c.allDone()) != 0 {
		t.Fatalf("不应有 done：%v", c.allDone())
	}
	errs := c.allErrors()
	if len(errs) != 1 {
		t.Fatalf("应恰好一次 error：%v", errs)
	}
	if !strings.Contains(errs[0], "总耗时超过 0s") || !strings.Contains(errs[0], "关闭思维链") {
		t.Fatalf("总上限文案不符：%q", errs[0])
	}
}

func TestProxyCancelNoLateEvents(t *testing.T) {
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		for i := 0; i < 10; i++ {
			sleep(70)
			api.Write(SSEData(fmt.Sprintf(`{"choices":[{"delta":{"content":"%d"}}]}`, i)))
		}
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	finished := proxy.Chat(chatReq(server.URL, "{}"), c)
	sleep(120) // 收到 1~2 块
	if len(c.chunkTexts()) < 1 {
		t.Fatal("取消前应至少收到一块")
	}
	proxy.Cancel("r1")
	<-finished
	countAtCancel := len(c.chunkTexts())
	sleep(250)
	if len(c.chunkTexts()) != countAtCancel {
		t.Fatalf("取消后不应有迟到块：%d → %d", countAtCancel, len(c.chunkTexts()))
	}
	if len(c.allDone()) != 0 || len(c.allErrors()) != 0 {
		t.Fatalf("取消后不应有任何结局：done=%v errors=%v", c.allDone(), c.allErrors())
	}
	if proxy.ActiveCount() != 0 {
		t.Fatalf("在途请求应清零，实际 %d", proxy.ActiveCount())
	}
}

func TestProxyCancelUnknownID(t *testing.T) {
	proxy := makeProxy(nil)
	proxy.Cancel("nope") // 幂等：不 panic
}

func TestProxyHTTP500WithModelHint(t *testing.T) {
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Respond(500, map[string]string{"Content-Type": "application/json"})
		api.End(`{"error":"Streaming translation is not supported"}`)
	})
	defer server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	<-proxy.Chat(chatReq(server.URL, "{}"), c)
	if len(c.allDone()) != 0 {
		t.Fatal("不应有 done")
	}
	errs := c.allErrors()
	if len(errs) != 1 {
		t.Fatalf("应恰好一次 error：%v", errs)
	}
	for _, frag := range []string{"HTTP 500", "Streaming translation is not supported", "翻译模型（qwen-mt-* 系列）"} {
		if !strings.Contains(errs[0], frag) {
			t.Fatalf("错误文案缺 %q：%q", frag, errs[0])
		}
	}
}

func TestProxyHTTP400LongBodyExcerpt(t *testing.T) {
	longBody := strings.Repeat("x", 300) + "marker-that-must-not-appear"
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Respond(400, map[string]string{"Content-Type": "text/plain"})
		api.End(longBody)
	})
	defer server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	<-proxy.Chat(chatReq(server.URL, "{}"), c)
	errs := c.allErrors()
	if len(errs) != 1 {
		t.Fatalf("应恰好一次 error：%v", errs)
	}
	if !strings.Contains(errs[0], "HTTP 400") {
		t.Fatalf("应含状态码：%q", errs[0])
	}
	if len(errs[0]) >= len(longBody) || strings.Contains(errs[0], "marker-that-must-not-appear") {
		t.Fatalf("应截断长响应体：%q", errs[0])
	}
	if !strings.HasSuffix(errs[0], "…") {
		t.Fatalf("应以省略号结尾：%q", errs[0])
	}
}

func TestProxyStreamChunkError(t *testing.T) {
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Write(SSEData(`{"choices":[{"delta":{"content":"ok"}}]}`))
		api.Write(SSEData(`{"error":{"message":"server exploded"}}`))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	<-proxy.Chat(chatReq(server.URL, "{}"), c)
	errs := c.allErrors()
	if len(errs) != 1 {
		t.Fatalf("应恰好一次 error：%v", errs)
	}
	if !strings.Contains(errs[0], "流式响应错误") || !strings.Contains(errs[0], "server exploded") {
		t.Fatalf("错误文案不符：%q", errs[0])
	}
	if len(c.allDone()) != 0 {
		t.Fatal("不应有 done")
	}
}

func TestProxyReasoningFallbackDone(t *testing.T) {
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Write(SSEData(`{"choices":[{"delta":{"reasoning_content":"先看中路…"}}]}`))
		api.Write(SSEData(`{"choices":[{"delta":{"reasoning_content":"着法: b2-e2"}}]}`))
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	<-proxy.Chat(chatReq(server.URL, "{}"), c)
	if errs := c.allErrors(); len(errs) != 0 {
		t.Fatalf("不应有错误：%v", errs)
	}
	if done := c.allDone(); len(done) != 1 || done[0] != "先看中路…着法: b2-e2" {
		t.Fatalf("done = %v", done)
	}
	if got := strings.Join(c.reasoningTexts(), ""); got != "先看中路…着法: b2-e2" {
		t.Fatalf("reasoning 增量 = %q", got)
	}
}

func TestProxyNaturalStreamEnd(t *testing.T) {
	// 无 [DONE] 流自然结束：仍以已累积内容结算 done。
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Write(SSEData(`{"choices":[{"delta":{"content":"着法: e3-e0"}}]}`))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	<-proxy.Chat(chatReq(server.URL, "{}"), c)
	if errs := c.allErrors(); len(errs) != 0 {
		t.Fatalf("不应有错误：%v", errs)
	}
	if done := c.allDone(); len(done) != 1 || done[0] != "着法: e3-e0" {
		t.Fatalf("done = %v", done)
	}
}

func TestProxyConnectionRefused(t *testing.T) {
	// 占住一个端口再关掉：拿到"确定没人听"的端口。
	server := StartMockSseServer(func(_ MockSseRequest, _ MockSseAPI) {})
	deadURL := server.URL
	server.Close()

	proxy := makeProxy(nil)
	c := &collector{}
	<-proxy.Chat(chatReq(deadURL, "{}"), c)
	if len(c.allDone()) != 0 {
		t.Fatal("不应有 done")
	}
	errs := c.allErrors()
	if len(errs) != 1 || !strings.Contains(errs[0], "连接失败") {
		t.Fatalf("应报连接失败：%v", errs)
	}
}

func TestProxyConcurrentRequestIsolation(t *testing.T) {
	server := StartMockSseServer(func(req MockSseRequest, api MockSseAPI) {
		who := "黑方: h2-e2"
		if strings.Contains(req.URL, "red") {
			who = "红方: b2-e2"
		}
		api.Write(SSEData(fmt.Sprintf(`{"choices":[{"delta":{"content":"%s"}}]}`, who)))
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(nil)
	cA, cB := &collector{}, &collector{}
	reqA, reqB := chatReq(server.URL+"/red", "{}"), chatReq(server.URL+"/black", "{}")
	reqA.RequestID, reqB.RequestID = "red", "black"
	fA, fB := proxy.Chat(reqA, cA), proxy.Chat(reqB, cB)
	<-fA
	<-fB
	if done := cA.allDone(); len(done) != 1 || done[0] != "红方: b2-e2" {
		t.Fatalf("红方串流：%v", done)
	}
	if done := cB.allDone(); len(done) != 1 || done[0] != "黑方: h2-e2" {
		t.Fatalf("黑方串流：%v", done)
	}
	if len(cA.allErrors()) != 0 || len(cB.allErrors()) != 0 {
		t.Fatal("不应有错误")
	}
}

func TestProxyAuthSlotInjection(t *testing.T) {
	var mu sync.Mutex
	var seenAuth []string
	server := StartMockSseServer(func(req MockSseRequest, api MockSseAPI) {
		mu.Lock()
		seenAuth = append(seenAuth, req.Headers.Get("Authorization"))
		mu.Unlock()
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(func(slot string) string {
		if slot == "llm_config_black" {
			return "sk-real-9999"
		}
		return ""
	})
	cA := &collector{}
	reqA := chatReq(server.URL+"/a", "{}")
	reqA.RequestID = "a"
	reqA.AuthSlot = "llm_config_black"
	reqA.Headers = map[string]string{"Content-Type": "application/json"}
	<-proxy.Chat(reqA, cA)

	cB := &collector{}
	reqB := chatReq(server.URL+"/b", "{}")
	reqB.RequestID = "b"
	reqB.AuthSlot = "llm_config_red" // 槽位无 Key
	reqB.Headers = map[string]string{"Content-Type": "application/json", "Authorization": "Bearer ****abcd"}
	<-proxy.Chat(reqB, cB)

	mu.Lock()
	defer mu.Unlock()
	if seenAuth[0] != "Bearer sk-real-9999" {
		t.Fatalf("应注入真实鉴权头：%q", seenAuth[0])
	}
	if seenAuth[1] != "" {
		t.Fatalf("槽位无 Key 时应剔除掩码头：%q", seenAuth[1])
	}
	if len(cA.allErrors()) != 0 || len(cB.allErrors()) != 0 {
		t.Fatal("不应有错误")
	}
	if len(cA.allDone()) != 2-len(cB.allDone()) {
		// 两个请求各一次 done（见下断言）
	}
	if len(cA.allDone())+len(cB.allDone()) != 2 {
		t.Fatalf("应各结算一次 done：%v + %v", cA.allDone(), cB.allDone())
	}
}

func TestProxyInlineKeyPassthrough(t *testing.T) {
	// 渲染层自持完整 Key（无 authSlot）：鉴权头原样透传。
	var mu sync.Mutex
	var seenAuth string
	server := StartMockSseServer(func(req MockSseRequest, api MockSseAPI) {
		mu.Lock()
		seenAuth = req.Headers.Get("Authorization")
		mu.Unlock()
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(func(string) string { return "should-not-be-used" })
	c := &collector{}
	req := chatReq(server.URL, "{}")
	req.Headers = map[string]string{"Authorization": "Bearer sk-inline"}
	<-proxy.Chat(req, c)
	mu.Lock()
	defer mu.Unlock()
	if seenAuth != "Bearer sk-inline" {
		t.Fatalf("鉴权头应透传：%q", seenAuth)
	}
}

func TestProxyTestConnection(t *testing.T) {
	// 错误消息为真实错误而非 requestId（遮蔽回归）。
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Respond(400, map[string]string{"Content-Type": "text/plain"})
		api.End("Streaming translation is not supported")
	})
	defer server.Close()

	proxy := makeProxy(func(string) string { return "sk-real" })
	res := proxy.TestConnection(
		LlmEndpointConfig{BaseURL: server.URL, APIKey: "****abcd", Model: "m", Preset: ""},
		"llm_config_black",
	)
	if res.OK {
		t.Fatal("应失败")
	}
	if !strings.Contains(res.Message, "HTTP 400") || !strings.Contains(res.Message, "翻译模型") {
		t.Fatalf("失败消息不符：%q", res.Message)
	}
	if strings.Contains(res.Message, "test-conn") {
		t.Fatalf("失败消息不应含 requestId：%q", res.Message)
	}

	// 端点未配置 → 构建期错误直接作为失败消息。
	res2 := proxy.TestConnection(LlmEndpointConfig{}, "")
	if res2.OK || !strings.Contains(res2.Message, "模型端点未配置") {
		t.Fatalf("未配置消息不符：%+v", res2)
	}
}

func TestProxyTestConnectionSuccess(t *testing.T) {
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Write(SSEData(`{"choices":[{"delta":{"content":"ok"}}]}`))
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	proxy := makeProxy(nil)
	res := proxy.TestConnection(
		LlmEndpointConfig{BaseURL: server.URL, APIKey: "", Model: "test-model", Preset: ""},
		"",
	)
	if !res.OK || res.Message != "连接成功，模型 test-model 响应正常" {
		t.Fatalf("成功消息不符：%+v", res)
	}
}

func TestExcerpt(t *testing.T) {
	if got := excerpt("  short  "); got != "short" {
		t.Fatalf("excerpt trim = %q", got)
	}
	long := strings.Repeat("汉", 200)
	got := excerpt(long)
	if n := len([]rune(strings.TrimSuffix(got, "…"))); n != 160 {
		t.Fatalf("应截 160 字符，实际 %d", n)
	}
}

func TestProxyIdleResetPerChunkNoNewline(t *testing.T) {
	// 复审修复回归（R1-P3b）：空闲计时器按"块"重置而非按完整行——
	// 持续到达但不带换行的半行流不误触空闲超时（对齐 llm-proxy.ts:179-195）。
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		// 每段 80ms、共 6 段 ≈ 480ms > 空闲 150ms——若按"行"重置必然误判。
		for i := 0; i < 6; i++ {
			sleep(80)
			api.Write(fmt.Sprintf(`{"choices":[{"delta":{"content":"段%d",`, i))
		}
		api.Write("}}]}\n\n") // 补全最后一行
		api.Write(SSEData("[DONE]"))
		api.End()
	})
	defer server.Close()

	transport := NewStreamTransport(func() int { return 60 })
	c := &collector{}
	err := transport.ChatWithOptions(t.Context(), chatReq(server.URL, "{}"), handlersFrom(c),
		StreamOptions{IdleTimeout: 150 * time.Millisecond})
	if err != nil {
		t.Fatalf("Chat err = %v", err)
	}
	if errs := c.allErrors(); len(errs) != 0 {
		t.Fatalf("半行慢流不应触发空闲超时：%v", errs)
	}
	if len(c.allDone()) != 1 {
		t.Fatalf("应正常结算 done：%v", c.allDone())
	}
}

func TestProxyEOFResidualHalfLineDropped(t *testing.T) {
	// 复审修复回归（R1-P3c）：流自然结束（无 [DONE]、末行无 \n）时残留
	// 半行丢弃（对齐 llm-proxy.ts:196-198——以已结算行的累积内容为准）。
	server := StartMockSseServer(func(_ MockSseRequest, api MockSseAPI) {
		api.Write(SSEData(`{"choices":[{"delta":{"content":"着法: b2-e2"}}]}`))
		api.Write(`data: {"choices":[{"del`) // 残留半行，无换行直接 EOF
		api.End()
	})
	defer server.Close()

	transport := NewStreamTransport(func() int { return 60 })
	c := &collector{}
	_ = transport.Chat(t.Context(), chatReq(server.URL, "{}"), handlersFrom(c))
	if errs := c.allErrors(); len(errs) != 0 {
		t.Fatalf("不应有错误：%v", errs)
	}
	if done := c.allDone(); len(done) != 1 || done[0] != "着法: b2-e2" {
		t.Fatalf("残留半行应丢弃，done = %v", done)
	}
}
