package llm

// 请求构建用例（T4.1，05 §3.1 请求格式 + DR-005 恒发关闭参数；
// Electron 版 test/llm/config.spec.ts 移植，关闭参数断言按 Go 版预设映射改写）。

import (
	"encoding/json"
	"reflect"
	"testing"
)

// 测试配置（preset 缺省 → 兜底形态 enable_thinking:false）。
func cfg(over map[string]string) LlmEndpointConfig {
	c := LlmEndpointConfig{BaseURL: "https://api.example.com/v1", APIKey: "", Model: "test-model", Preset: ""}
	for k, v := range over {
		switch k {
		case "baseUrl":
			c.BaseURL = v
		case "apiKey":
			c.APIKey = v
		case "model":
			c.Model = v
		case "preset":
			c.Preset = v
		}
	}
	return c
}

func mustParse(t *testing.T, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("请求体不是合法 JSON：%v\n%s", err, body)
	}
	return m
}

func assertNoAuth(t *testing.T, built BuiltChatRequest) {
	t.Helper()
	if _, ok := built.Headers["Authorization"]; ok {
		t.Fatalf("不应携带鉴权头，实际 %q", built.Headers["Authorization"])
	}
}

func TestRequestURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://a.com/v1", "https://a.com/v1/chat/completions"},
		{"https://a.com", "https://a.com/chat/completions"},
		{"https://a.com/v1/", "https://a.com/v1/chat/completions"},
		{"https://a.com/v1///", "https://a.com/v1/chat/completions"},
		{"https://a.com/v1/chat/completions", "https://a.com/v1/chat/completions"},
		{"https://a.com/v1/chat/completions/", "https://a.com/v1/chat/completions"},
	}
	for _, c := range cases {
		if got := RequestURL(c.in); got != c.want {
			t.Errorf("RequestURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsConfigured(t *testing.T) {
	if IsConfigured(cfg(map[string]string{"baseUrl": " ", "model": "m"})) {
		t.Error("空端点应不可调用")
	}
	if IsConfigured(cfg(map[string]string{"baseUrl": "https://a.com", "model": "  "})) {
		t.Error("空模型应不可调用")
	}
	if !IsConfigured(cfg(nil)) {
		t.Error("端点与模型齐备应可调用（Key 可空）")
	}
}

// 请求体快照（含 DR-005 各预设关闭参数断言）。
func TestBuildChatRequestBodySnapshot(t *testing.T) {
	// v1：temperature 0.3 / max_tokens 4096 / stream true / enable_thinking false（兜底恒发）
	built, err := BuildChatRequest(cfg(nil), "SYS", "USR", BuildChatOptions{UseV2: false})
	if err != nil {
		t.Fatalf("构建失败：%v", err)
	}
	if built.URL != "https://api.example.com/v1/chat/completions" {
		t.Fatalf("URL = %q", built.URL)
	}
	want := mustParse(t, `{
		"model": "test-model",
		"messages": [
			{"role": "system", "content": "SYS"},
			{"role": "user", "content": "USR"}
		],
		"temperature": 0.3,
		"max_tokens": 4096,
		"stream": true,
		"enable_thinking": false
	}`)
	if !reflect.DeepEqual(mustParse(t, built.Body), want) {
		t.Fatalf("v1 请求体快照不符：\n%s", built.Body)
	}

	// v2：max_tokens 8192
	built, _ = BuildChatRequest(cfg(nil), "S", "U", BuildChatOptions{UseV2: true})
	if got := mustParse(t, built.Body)["max_tokens"]; got != float64(MaxTokensV2) {
		t.Fatalf("v2 max_tokens = %v", got)
	}
}

// 【DR-005 硬性要求】预设映射：构造层恒发关闭参数、无任何开关。
func TestThinkingOffParamsByPreset(t *testing.T) {
	cases := []struct {
		name       string
		preset     string
		wantEnable bool // 顶层 enable_thinking:false
		wantGLM    bool // thinking:{type:disabled}
	}{
		{"智谱 GLM（glm 形态）", "智谱 GLM", false, true},
		{"DeepSeek（兜底）", "DeepSeek", true, false},
		{"Kimi（兜底）", "Kimi（Moonshot）", true, false},
		{"OpenAI（兜底）", "OpenAI", true, false},
		{"OpenRouter（兜底）", "OpenRouter", true, false},
		{"自定义（兜底）", "自定义", true, false},
		{"未知/缺省（兜底）", "", true, false},
		{"DashScope 识图预设（布尔形态）", "通义千问 VL（阿里云百炼）", true, false},
		{"智谱 GLM-4.5V 识图预设（glm 形态）", "智谱 GLM-4.5V（视觉）", false, true},
	}
	for _, c := range cases {
		built, err := BuildChatRequest(cfg(map[string]string{"preset": c.preset}), "S", "U", BuildChatOptions{})
		if err != nil {
			t.Fatalf("%s：构建失败 %v", c.name, err)
		}
		m := mustParse(t, built.Body)
		enable, hasEnable := m["enable_thinking"]
		glm, hasGLM := m["thinking"]
		if c.wantEnable {
			if !hasEnable || enable != false {
				t.Errorf("%s：应恒发 enable_thinking:false，实际 %v", c.name, m)
			}
		} else if hasEnable {
			t.Errorf("%s：不应携带 enable_thinking（glm 形态独占）", c.name)
		}
		if c.wantGLM {
			want := map[string]any{"type": "disabled"}
			if !hasGLM || !reflect.DeepEqual(glm, want) {
				t.Errorf("%s：应恒发 thinking:{type:disabled}，实际 %v", c.name, m)
			}
		} else if hasGLM {
			t.Errorf("%s：不应携带 thinking 对象", c.name)
		}
	}
	// ThinkingStyleFor 单元口径。
	if ThinkingStyleFor("智谱 GLM") != ThinkingGLM ||
		ThinkingStyleFor("智谱 GLM-4.5V（视觉）") != ThinkingGLM ||
		ThinkingStyleFor("通义千问 VL（阿里云百炼）") != ThinkingDashScope ||
		ThinkingStyleFor("DeepSeek") != ThinkingDefault ||
		ThinkingStyleFor("未登记端点") != ThinkingDefault {
		t.Error("ThinkingStyleFor 预设映射不符")
	}
}

func TestBuildChatRequestAuth(t *testing.T) {
	// 空 Key：不携带鉴权头（本地网关）
	built, err := BuildChatRequest(cfg(map[string]string{"apiKey": "  "}), "S", "U", BuildChatOptions{})
	if err != nil {
		t.Fatal(err)
	}
	assertNoAuth(t, built)
	if built.AuthSlot != "" {
		t.Error("空 Key 不应带 AuthSlot")
	}
	if built.Headers["Content-Type"] != "application/json" || built.Headers["Accept"] != "text/event-stream" {
		t.Fatalf("公共头不符：%v", built.Headers)
	}

	// 完整 Key：内联 Bearer
	built, _ = BuildChatRequest(cfg(map[string]string{"apiKey": "sk-full-1234"}), "S", "U", BuildChatOptions{})
	if built.Headers["Authorization"] != "Bearer sk-full-1234" || built.AuthSlot != "" {
		t.Fatalf("完整 Key 鉴权不符：%v / %q", built.Headers, built.AuthSlot)
	}

	// 掩码 Key（secure 回读）：无鉴权头 + AuthSlot 待注入
	built, _ = BuildChatRequest(cfg(map[string]string{"apiKey": "****1234"}), "S", "U", BuildChatOptions{AuthSlot: "llm_config_black"})
	assertNoAuth(t, built)
	if built.AuthSlot != "llm_config_black" {
		t.Fatalf("AuthSlot = %q", built.AuthSlot)
	}

	// 掩码 Key 但未提供槽位 → 报错（防裸掩码上外网）
	_, err = BuildChatRequest(cfg(map[string]string{"apiKey": "****1234"}), "S", "U", BuildChatOptions{})
	if _, ok := err.(*LlmConfigError); !ok {
		t.Fatalf("应返回 LlmConfigError，实际 %v", err)
	}

	// 未配置端点 → LlmConfigError（等价 LlmConfigException）
	_, err = BuildChatRequest(cfg(map[string]string{"baseUrl": "", "model": ""}), "S", "U", BuildChatOptions{})
	if err == nil || err.Error() != "模型端点未配置（需填写端点与模型 ID）" {
		t.Fatalf("未配置错误文案不符：%v", err)
	}
}

func TestBuildTestConnectionChat(t *testing.T) {
	built, err := BuildTestConnectionChat(cfg(nil), "")
	if err != nil {
		t.Fatal(err)
	}
	m := mustParse(t, built.Body)
	wantMessages := mustParse(t, `{"m":[{"role":"system","content":"你是一个连通性测试助手。"},{"role":"user","content":"请回复：ok"}]}`)["m"]
	if !reflect.DeepEqual(m["messages"], wantMessages) {
		t.Fatalf("测试连接 messages 不符：%v", m["messages"])
	}
	if m["max_tokens"] != float64(MaxTokensV1) || m["stream"] != true {
		t.Fatalf("测试连接预算/流式不符：%v", m)
	}
}

// 双方共用模型：空侧运行时跟随对方（DR-012）。
func TestResolveLlmSideConfig(t *testing.T) {
	const redSlot = "llm_config_red"
	const blackSlot = "llm_config_black"
	empty := LlmEndpointConfig{}
	other := cfg(map[string]string{"baseUrl": "https://a.com/v1", "apiKey": "sk-x", "model": "m"})

	if !IsEmptyLlmConfig(empty) {
		t.Error("全空应判空")
	}
	if !IsEmptyLlmConfig(LlmEndpointConfig{BaseURL: " "}) {
		t.Error("纯空白应判空")
	}
	r := ResolveLlmSideConfig(empty, other, redSlot, blackSlot)
	if r.Config != other || r.AuthSlot != blackSlot {
		t.Fatalf("空侧应镜像对方配置与对方槽位：%+v", r)
	}

	// 填了任一字段（哪怕只填 Key）→ 不镜像，槽位为自身
	own := LlmEndpointConfig{APIKey: "sk-only-key"}
	r = ResolveLlmSideConfig(own, other, redSlot, blackSlot)
	if r.Config != own || r.AuthSlot != redSlot {
		t.Fatalf("非空侧应用自身配置：%+v", r)
	}
	if IsConfigured(r.Config) {
		t.Error("独立无效配置应由开始校验拦截")
	}

	// 双方都配了同一个模型 → 各用各的
	a := cfg(map[string]string{"apiKey": "sk-a", "model": "same-model"})
	b := cfg(map[string]string{"apiKey": "sk-b", "model": "same-model"})
	if ResolveLlmSideConfig(a, b, redSlot, blackSlot).AuthSlot != redSlot {
		t.Error("红方应使用红方槽位")
	}
	if ResolveLlmSideConfig(b, a, blackSlot, redSlot).AuthSlot != blackSlot {
		t.Error("黑方应使用黑方槽位")
	}

	// 双方都空 → 相互镜像后仍为空（开始校验拦截）
	r = ResolveLlmSideConfig(empty, empty, redSlot, blackSlot)
	if IsConfigured(r.Config) {
		t.Error("双方全空镜像后仍应不可调用")
	}
}

// 端点预设（仅公开地址与示例模型 ID）。
func TestPresets(t *testing.T) {
	wantNames := []string{"智谱 GLM", "DeepSeek", "Kimi（Moonshot）", "OpenRouter", "OpenAI", "自定义"}
	got := make([]string, len(LlmPresets))
	for i, p := range LlmPresets {
		got[i] = p.Name
	}
	if !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("对话预设顺序不符：%v", got)
	}
	first := LlmPresets[0]
	if first.BaseURL != "https://open.bigmodel.cn/api/paas/v4" || first.ExampleModel != "glm-4-flash" {
		t.Fatalf("智谱预设不符：%+v", first)
	}
	if LlmPresets[5].BaseURL != "" {
		t.Error("自定义预设端点应为空")
	}
	hasVisionModel := func(model string) bool {
		for _, p := range VisionLlmPresets {
			if p.ExampleModel == model {
				return true
			}
		}
		return false
	}
	if !hasVisionModel("qwen-vl-max") || !hasVisionModel("glm-4.5v") {
		t.Error("视觉预设应含 qwen-vl-max / glm-4.5v（M6 使用）")
	}
}
