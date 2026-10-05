package llm

// 视觉识图协议测试（T6.3，vision_board_reader 15 用例等价集，09 §1；
// 逐项对齐 Electron 版 test/llm/vision.spec.ts）：
// JSON 提取（围栏/杂质）/ 坐标与棋子校验 / 双王硬校验 / turn 解析 / MIME 魔数 /
// 请求体组装（多模态消息、非流式参数、DR-005 恒发关闭参数）。
//
// DR-005 Go 版差异（05 §7）：识图请求无 disableThinking 开关——关闭参数按
// 预设恒发（dashscope→enable_thinking:false；glm→thinking:{type:disabled}）。

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const okContentRed = `{"turn":"red","pieces":[{"col":"d","row":"0","piece":"k"},{"col":"e","row":"9","piece":"K"},{"col":"a","row":"4","piece":"R"}]}`

func okContent(turn string) string {
	return `{"turn":"` + turn + `","pieces":[{"col":"d","row":"0","piece":"k"},{"col":"e","row":"9","piece":"K"},{"col":"a","row":"4","piece":"R"}]}`
}

func TestVisionPromptSnapshot(t *testing.T) {
	// ① 提示词含 JSON 模板与坐标/棋子约定（快照关键片段，逐字）。
	prompt := VisionPrompt()
	for _, frag := range []string{
		`{"turn":"red或black","pieces":[{"col":"a-i","row":"0-9","piece":"棋子FEN字符"}]}`,
		"行 0 为棋盘顶部（黑方底线）",
		"K(帅) A(仕) B(相) N(马) R(车) C(炮) P(兵)",
		"只列实际出现的棋子",
	} {
		if !strings.Contains(prompt, frag) {
			t.Errorf("visionPrompt 缺少片段 %q", frag)
		}
	}
	if VisionSystemPrompt != "你是中国象棋棋盘识别器，只输出约定的 JSON，不输出任何其他文字。" {
		t.Errorf("VISION_SYSTEM_PROMPT 逐字不符: %q", VisionSystemPrompt)
	}
}

func TestParseVisionPiecesJSONExtraction(t *testing.T) {
	// ② 纯 JSON 直接解析。
	grid, err := ParseVisionPieces(okContentRed)
	if err != nil {
		t.Fatal(err)
	}
	if p := grid[0][3]; p == nil || p.Side != "black" { // k (d,0)
		t.Errorf("grid[0][3] 应为黑 k: %+v", p)
	}
	if p := grid[9][4]; p == nil || p.Side != "red" { // K (e,9)
		t.Errorf("grid[9][4] 应为红 K: %+v", p)
	}
	if p := grid[4][0]; p == nil || p.Kind != "rook" { // R (a,4)
		t.Errorf("grid[4][0] 应为车: %+v", p)
	}

	// ③ markdown 围栏包裹：剥 ``` 后解析成功。
	wrapped := "```json\n" + okContent("black") + "\n```"
	if ParseVisionTurn(wrapped) {
		t.Errorf("围栏 black 应解析为黑方")
	}
	grid, err = ParseVisionPieces(wrapped)
	if err != nil {
		t.Fatal(err)
	}
	if p := grid[0][3]; p == nil || p.Kind != "king" {
		t.Errorf("围栏内 k 应解析成功: %+v", p)
	}

	// ④ 前后杂质文本：取首个 { 到末个 }。
	noisy := "好的，识别结果如下：\n" + okContentRed + "\n以上。"
	grid, err = ParseVisionPieces(noisy)
	if err != nil {
		t.Fatal(err)
	}
	if p := grid[9][4]; p == nil || p.Kind != "king" {
		t.Errorf("杂质文本应正确提取 JSON: %+v", p)
	}

	// ⑤ 无 JSON（未找到 {）：报"回复中未找到 JSON"。
	_, err = ParseVisionPieces("抱歉，我无法识别该图片")
	if err == nil || !strings.Contains(err.Error(), "回复中未找到 JSON") {
		t.Errorf("应报'回复中未找到 JSON', got %v", err)
	}
}

func TestParseVisionPiecesEntryValidation(t *testing.T) {
	// ⑥ 坐标越界：row=10 报"坐标越界"；列字母非法（j）按"非法棋子条目"拦截。
	badRow := `{"turn":"red","pieces":[{"col":"d","row":"10","piece":"k"},{"col":"e","row":"9","piece":"K"}]}`
	_, err := ParseVisionPieces(badRow)
	if err == nil || !strings.Contains(err.Error(), "坐标越界") {
		t.Errorf("row=10 应报'坐标越界', got %v", err)
	}
	badCol := `{"turn":"red","pieces":[{"col":"j","row":"0","piece":"k"},{"col":"e","row":"9","piece":"K"}]}`
	_, err = ParseVisionPieces(badCol)
	if err == nil || !strings.Contains(err.Error(), "非法棋子条目") {
		t.Errorf("col=j 应报'非法棋子条目', got %v", err)
	}

	// ⑦ 未知棋子/缺失字段：报"非法棋子条目"。
	badPiece := `{"turn":"red","pieces":[{"col":"d","row":"0","piece":"x"},{"col":"e","row":"9","piece":"K"}]}`
	if _, err = ParseVisionPieces(badPiece); err == nil || !strings.Contains(err.Error(), "非法棋子条目") {
		t.Errorf("未知棋子应报'非法棋子条目', got %v", err)
	}
	missing := `{"turn":"red","pieces":[{"col":"d","row":"0"},{"col":"e","row":"9","piece":"K"}]}`
	if _, err = ParseVisionPieces(missing); err == nil || !strings.Contains(err.Error(), "非法棋子条目") {
		t.Errorf("缺失字段应报'非法棋子条目', got %v", err)
	}

	// ⑧ 缺少 pieces 数组：报"JSON 缺少 pieces 数组"。
	if _, err = ParseVisionPieces(`{"turn":"red"}`); err == nil || !strings.Contains(err.Error(), "JSON 缺少 pieces 数组") {
		t.Errorf("缺 pieces 应报'JSON 缺少 pieces 数组', got %v", err)
	}

	// ⑨ 行号数字型（0-9 数值而非字符串）兼容解析。
	numeric := `{"turn":"red","pieces":[{"col":"d","row":0,"piece":"k"},{"col":"e","row":9,"piece":"K"}]}`
	grid, err := ParseVisionPieces(numeric)
	if err != nil {
		t.Fatal(err)
	}
	if p := grid[0][3]; p == nil || p.Kind != "king" {
		t.Errorf("数字行号应兼容解析: %+v", p)
	}
}

func TestValidateVisionKings(t *testing.T) {
	// ⑩ 双王非各恰一：硬校验失败（红 1 / 黑 0）。
	grid, err := ParseVisionPieces(`{"turn":"red","pieces":[{"col":"e","row":"9","piece":"K"},{"col":"d","row":"0","piece":"k"},{"col":"a","row":"0","piece":"r"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	// 人工构造"黑王丢失"的矩阵（识别漏检将的典型场景）。
	grid[0][3] = nil
	err = ValidateVisionKings(grid)
	if err == nil || !strings.Contains(err.Error(), "双方王数量异常（红 1 / 黑 0）") {
		t.Errorf("黑王丢失应硬校验失败, got %v", err)
	}
	// 识别管线整体也在 parse 尾部执行该校验。
	_, err = ParseVisionPieces(`{"turn":"red","pieces":[{"col":"e","row":"9","piece":"K"},{"col":"a","row":"0","piece":"r"}]}`)
	if err == nil || !strings.Contains(err.Error(), "双方王数量异常") {
		t.Errorf("parse 尾部应执行双王校验, got %v", err)
	}

	// ⑪ 双红王 / 三王同样拒绝。
	twoRed := `{"turn":"red","pieces":[{"col":"e","row":"9","piece":"K"},{"col":"e","row":"8","piece":"K"},{"col":"d","row":"0","piece":"k"}]}`
	_, err = ParseVisionPieces(twoRed)
	if err == nil || !strings.Contains(err.Error(), "双方王数量异常（红 2 / 黑 1）") {
		t.Errorf("双红王应拒绝, got %v", err)
	}
}

func TestParseVisionTurn(t *testing.T) {
	// ⑫ red/缺失/坏 JSON → 红方；black → 黑方（大小写不敏感）。
	if !ParseVisionTurn(okContent("red")) {
		t.Errorf("red 应为红方")
	}
	if ParseVisionTurn(okContent("black")) {
		t.Errorf("black 应为黑方")
	}
	if ParseVisionTurn(okContent("BLACK")) {
		t.Errorf("大小写不敏感")
	}
	if !ParseVisionTurn(`{"pieces":[]}`) {
		t.Errorf("缺 turn → 红方")
	}
	if !ParseVisionTurn("不是 JSON") {
		t.Errorf("解析失败 → 红方兜底")
	}
}

func TestDetectImageMime(t *testing.T) {
	// ⑬ PNG 头 89 50 4E → image/png；其余一律 image/jpeg。
	if DetectImageMime([]byte{0x89, 0x50, 0x4e, 0x47, 0x0d}) != "image/png" {
		t.Errorf("PNG 魔数应判 png")
	}
	if DetectImageMime([]byte{0xff, 0xd8, 0xff, 0xe0}) != "image/jpeg" {
		t.Errorf("JPEG 应判 jpeg")
	}
	if DetectImageMime([]byte{0x00, 0x01, 0x02}) != "image/jpeg" {
		t.Errorf("其他应判 jpeg")
	}
	if DetectImageMime([]byte{0x89}) != "image/jpeg" {
		t.Errorf("不足 3 字节应判 jpeg")
	}
}

func TestBuildVisionRequest(t *testing.T) {
	// ⑭ 多模态消息 + 非流式参数 + DR-005 关闭参数 + Bearer 头 + URL 补全。
	config := LlmEndpointConfig{
		BaseURL: "https://api.example.com/v1/",
		APIKey:  "sk-test",
		Model:   "glm-4.5v",
		Preset:  "智谱 GLM-4.5V（视觉）",
	}
	built, err := BuildVisionRequest(config, "data:image/png;base64,AAA")
	if err != nil {
		t.Fatal(err)
	}
	if built.URL != "https://api.example.com/v1/chat/completions" {
		t.Errorf("URL 补全不符: %s", built.URL)
	}
	if built.Headers["Authorization"] != "Bearer sk-test" {
		t.Errorf("Bearer 头不符: %q", built.Headers["Authorization"])
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(built.Body), &body); err != nil {
		t.Fatal(err)
	}
	if _, hasStream := body["stream"]; hasStream {
		t.Errorf("识图请求应非流式（无 stream 字段）")
	}
	if body["temperature"] != 0.1 {
		t.Errorf("temperature = %v, want 0.1", body["temperature"])
	}
	if body["max_tokens"] != float64(4096) {
		t.Errorf("max_tokens = %v, want 4096", body["max_tokens"])
	}
	// DR-005：glm-4.5v 预设恒发 thinking:{type:disabled}，不发送 enable_thinking。
	if body["thinking"] == nil {
		t.Errorf("glm 预设应恒发 thinking 关闭参数: %v", body)
	}
	if _, has := body["enable_thinking"]; has {
		t.Errorf("glm 预设不应发送 enable_thinking")
	}
	messages, ok := body["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages 应为 2 条: %v", body["messages"])
	}
	sys, _ := messages[0].(map[string]any)
	if sys["role"] != "system" {
		t.Errorf("messages[0].role = %v", sys["role"])
	}
	user, _ := messages[1].(map[string]any)
	content, ok := user["content"].([]any)
	if !ok || len(content) != 2 {
		t.Fatalf("user content 应为多模态两段: %v", user["content"])
	}
	imagePart, _ := content[0].(map[string]any)
	if imagePart["type"] != "image_url" {
		t.Errorf("content[0].type = %v", imagePart["type"])
	}
	imageURL, _ := imagePart["image_url"].(map[string]any)
	if imageURL["url"] != "data:image/png;base64,AAA" {
		t.Errorf("image_url.url = %v", imageURL["url"])
	}
	textPart, _ := content[1].(map[string]any)
	if textPart["type"] != "text" || textPart["text"] != VisionPrompt() {
		t.Errorf("content[1] 应为提示词文本: %v", textPart)
	}

	// ⑮ DR-005 恒发（无开关路径）：
	//   - dashscope 预设 → enable_thinking:false；
	//   - OpenAI/自定义/未知 → enable_thinking:false 兜底；
	//   - 任意预设下二参数至多出现其一（不存在双发/漏发）。
	dash, err := BuildVisionRequest(LlmEndpointConfig{
		BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		Model:   "qwen-vl-max",
		Preset:  "通义千问 VL（阿里云百炼）",
	}, "x")
	if err != nil {
		t.Fatal(err)
	}
	var dashBody map[string]any
	_ = json.Unmarshal([]byte(dash.Body), &dashBody)
	if dashBody["enable_thinking"] != false {
		t.Errorf("dashscope 预设应恒发 enable_thinking:false: %v", dashBody["enable_thinking"])
	}
	if _, has := dashBody["thinking"]; has {
		t.Errorf("dashscope 预设不应发送 thinking 对象")
	}

	// 空 Key 不带鉴权头。
	noKey, err := BuildVisionRequest(LlmEndpointConfig{
		BaseURL: "https://api.example.com/v1/",
		Model:   "glm-4.5v",
		Preset:  "智谱 GLM-4.5V（视觉）",
	}, "data:image/jpeg;base64,BBB")
	if err != nil {
		t.Fatal(err)
	}
	if _, hasAuth := noKey.Headers["Authorization"]; hasAuth {
		t.Errorf("空 Key 不应携带鉴权头")
	}

	// 端点/模型缺失抛 LlmConfigError。
	_, err = BuildVisionRequest(LlmEndpointConfig{BaseURL: " ", Model: ""}, "x")
	var cfgErr *LlmConfigError
	if !errors.As(err, &cfgErr) {
		t.Errorf("未配置应抛 LlmConfigError, got %v", err)
	}
	if cfgErr != nil && cfgErr.Error() != "视觉模型未配置（需填写端点与模型 ID）" {
		t.Errorf("配置错误文案逐字不符: %q", cfgErr.Error())
	}
}

func TestVisionGridToFen(t *testing.T) {
	// 识图矩阵 → 完整 FEN（轮走方随识别结果）。
	grid, err := ParseVisionPieces(okContent("black"))
	if err != nil {
		t.Fatal(err)
	}
	if fen := VisionGridToFen(grid, false); fen != "3k5/9/9/9/R8/9/9/9/9/4K4 b - - 0 1" {
		t.Errorf("FEN 组装不符: %s", fen)
	}
}

func TestExcerptVisionBody(t *testing.T) {
	// 超 160 字符截断加省略号。
	long := strings.Repeat("x", 200)
	out := ExcerptVisionBody(long)
	if len([]rune(out)) != 161 || !strings.HasSuffix(out, "…") {
		t.Errorf("截断应为 160 字符+省略号: %d", len([]rune(out)))
	}
	if ExcerptVisionBody("short") != "short" {
		t.Errorf("短文本原样返回")
	}
}

// -----------------------------------------------------------------------------
// VisionReader（主进程识图服务等价移植）：httptest mock 多模态端点
// -----------------------------------------------------------------------------

// visionServerOK 标准 200 响应（choices[0].message.content）。
func visionServerOK(t *testing.T, capture *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if capture != nil {
			*capture = req
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{
				"message": map[string]any{"content": okContentRed},
			}},
		})
	}))
}

func visionConfig(server *httptest.Server) LlmEndpointConfig {
	return LlmEndpointConfig{
		BaseURL: server.URL,
		APIKey:  "",
		Model:   "qwen-vl-max",
		Preset:  "通义千问 VL（阿里云百炼）",
	}
}

func TestVisionReaderSuccess(t *testing.T) {
	var captured map[string]any
	server := visionServerOK(t, &captured)
	defer server.Close()
	reader := NewVisionReader(VisionReaderOptions{ResolveAPIKey: func(string) string { return "" }})
	result, err := reader.ReadBoard(context.Background(), VisionReadBoardRequest{
		Config:      visionConfig(server),
		ImageBase64: "AAA",
		Mime:        "image/png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Fen == "" || !strings.Contains(result.Fen, "3k5") {
		t.Fatalf("应返回组装 FEN: %+v", result)
	}
	// data URL 前缀随 mime。
	messages := captured["messages"].([]any)
	user := messages[1].(map[string]any)
	content := user["content"].([]any)
	imagePart := content[0].(map[string]any)
	imageURL := imagePart["image_url"].(map[string]any)
	if imageURL["url"] != "data:image/png;base64,AAA" {
		t.Errorf("data URL 不符: %v", imageURL["url"])
	}
	// DR-005：识图请求恒发关闭参数（dashscope 预设）。
	if captured["enable_thinking"] != false {
		t.Errorf("识图请求应恒发 enable_thinking:false: %v", captured["enable_thinking"])
	}
}

func TestVisionReaderMaskedKeyInjection(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": okContentRed}}},
		})
	}))
	defer server.Close()
	reader := NewVisionReader(VisionReaderOptions{ResolveAPIKey: func(slot string) string {
		if slot != "llm_config_assistant" {
			t.Errorf("resolveApiKey 槽位不符: %s", slot)
		}
		return "sk-real-key"
	}})
	// DR-010：掩码 Key（secure.get 回读）→ authSlot 注入真实 Key。
	_, err := reader.ReadBoard(context.Background(), VisionReadBoardRequest{
		Config:      LlmEndpointConfig{BaseURL: server.URL, APIKey: "****abcd", Model: "m", Preset: ""},
		ImageBase64: "AAA",
		Mime:        "image/jpeg",
		AuthSlot:    "llm_config_assistant",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer sk-real-key" {
		t.Errorf("掩码 Key 应由槽位注入真实鉴权: %q", authHeader)
	}
	// 完整 Key 直接内联。
	_, err = reader.ReadBoard(context.Background(), VisionReadBoardRequest{
		Config:      LlmEndpointConfig{BaseURL: server.URL, APIKey: "sk-inline", Model: "m", Preset: ""},
		ImageBase64: "AAA",
		Mime:        "image/jpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "Bearer sk-inline" {
		t.Errorf("完整 Key 应直接内联: %q", authHeader)
	}
	// 无 Key：不带鉴权头。
	_, err = reader.ReadBoard(context.Background(), VisionReadBoardRequest{
		Config:      LlmEndpointConfig{BaseURL: server.URL, APIKey: "", Model: "m", Preset: ""},
		ImageBase64: "AAA",
		Mime:        "image/jpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authHeader != "" {
		t.Errorf("空 Key 不应带鉴权头: %q", authHeader)
	}
}

func TestVisionReaderHTTPErrorRetryExhausted(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "server exploded", http.StatusInternalServerError)
	}))
	defer server.Close()
	reader := NewVisionReader(VisionReaderOptions{ResolveAPIKey: func(string) string { return "" }})
	_, err := reader.ReadBoard(context.Background(), VisionReadBoardRequest{
		Config:      visionConfig(server),
		ImageBase64: "AAA",
		Mime:        "image/jpeg",
	})
	if err == nil {
		t.Fatal("HTTP 错误耗尽重试后应失败")
	}
	if !strings.Contains(err.Error(), "已重试 2 次仍失败") || !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("错误文案不符: %v", err)
	}
	if calls.Load() != 2 {
		t.Errorf("应尝试 2 次, got %d", calls.Load())
	}
}

func TestVisionReaderBadJSONRetry(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if calls.Load() == 1 {
			// 第一次：JSON 坏损（双王校验不过，等价路径）。
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"不是 JSON"}}]}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": okContentRed}}},
		})
	}))
	defer server.Close()
	reader := NewVisionReader(VisionReaderOptions{ResolveAPIKey: func(string) string { return "" }})
	result, err := reader.ReadBoard(context.Background(), VisionReadBoardRequest{
		Config:      visionConfig(server),
		ImageBase64: "AAA",
		Mime:        "image/jpeg",
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("坏损回复应重试一次, got %d", calls.Load())
	}
	if result.Fen == "" {
		t.Errorf("重试后应成功")
	}
}

func TestVisionReaderMissingChoices(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"object":"chat.completion"}`))
	}))
	defer server.Close()
	reader := NewVisionReader(VisionReaderOptions{MaxAttempts: 1, ResolveAPIKey: func(string) string { return "" }})
	_, err := reader.ReadBoard(context.Background(), VisionReadBoardRequest{
		Config:      visionConfig(server),
		ImageBase64: "AAA",
		Mime:        "image/jpeg",
	})
	if err == nil || !strings.Contains(err.Error(), "响应缺少 choices") {
		t.Errorf("缺 choices 应报错: %v", err)
	}
}

func TestVisionReaderTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer server.Close()
	reader := NewVisionReader(VisionReaderOptions{
		TimeoutMs:     50,
		MaxAttempts:   1,
		ResolveAPIKey: func(string) string { return "" },
	})
	_, err := reader.ReadBoard(context.Background(), VisionReadBoardRequest{
		Config:      visionConfig(server),
		ImageBase64: "AAA",
		Mime:        "image/jpeg",
	})
	if err == nil {
		t.Fatal("超时应报错")
	}
	want := "请求超时（0s）。大模型思维链过慢或网络较差，可重试或更换更快的视觉模型"
	if strings.Contains(err.Error(), "已重试") {
		// 耗尽重试包装内含超时文案。
		if !strings.Contains(err.Error(), "可重试或更换更快的视觉模型") {
			t.Errorf("超时文案不符: %v", err)
		}
		return
	}
	if err.Error() != want {
		t.Errorf("超时文案不符: %v", err)
	}
}
