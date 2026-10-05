// 视觉识图协议（vision.ts / vision_board_reader.dart 1:1 移植，05 文档 §7）。
//
// 只负责"图片 → 候选 FEN"的协议面：提示词强约束输出 JSON、JSON 提取、
// 坐标/棋子字符校验、双王硬校验（识别结果必须经人工核对界面复核后方可
// 进入求解流程，08 §4）。HTTP 部分在 VisionReader（本包，等价 Electron 版
// 主进程 visionReader 服务；DR-004：渲染层零外网直连，铁律 #4）。
//
// 【DR-005 思维链强制关闭】识图请求无 disableThinking 开关——关闭参数按
// 预设映射**无条件注入**（dashscope → "enable_thinking": false；
// glm-4.5v → "thinking": {"type": "disabled"}；其余端点兜底 enable_thinking:false），
// 与对弈通道同源（ThinkingStyleFor，05 §3.1/§7）。
// 纯 Go：禁止 import Wails / frontend（铁律 #1；net/http 仅限本文件 Reader）。
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/rules"
)

// VisionTimeoutMs 单次识图请求超时（视觉模型大图/思考型可能超过 1 分钟，
// 实测关闭思维链 6~14s）。
const VisionTimeoutMs = 120_000

// VisionMaxAttempts 识图最多尝试次数（重试 1 次 = 共 2 次，vision_board_reader.dart:47）。
const VisionMaxAttempts = 2

// VisionTemperature / VisionMaxTokens 识图 temperature/max_tokens（:112-113）。
const (
	VisionTemperature = 0.1
	VisionMaxTokens   = 4096
)

// VisionParseError 识图 JSON 解析异常（等价 Dart FormatException 路径，消息面向用户）。
type VisionParseError struct{ Message string }

func (e *VisionParseError) Error() string { return e.Message }

// VisionSystemPrompt system 提示词（vision_board_reader.dart:99，逐字）。
const VisionSystemPrompt = "你是中国象棋棋盘识别器，只输出约定的 JSON，不输出任何其他文字。"

// visionPromptText 识图提示词：强制输出可程序校验的 JSON（:140-146，逐字）。
const visionPromptText = "识别图中中国象棋棋盘上的所有棋子。" +
	"以 JSON 回复，不要输出任何其他文字：\n" +
	`{"turn":"red或black","pieces":[{"col":"a-i","row":"0-9","piece":"棋子FEN字符"}]}` + "\n" +
	"约定：行 0 为棋盘顶部（黑方底线），行 9 为底部（红方底线）；" +
	"列 a 在左、i 在右。棋子 FEN 字符：红方大写 " +
	"K(帅) A(仕) B(相) N(马) R(车) C(炮) P(兵)，黑方小写 " +
	"k(将) a(士) b(象) n(马) r(车) c(炮) p(卒)。只列实际出现的棋子。"

// VisionPrompt 识图 user 提示词（每次返回同一常量文本）。
func VisionPrompt() string { return visionPromptText }

// BuiltVisionRequest 组装完成的识图请求。
type BuiltVisionRequest struct {
	URL     string
	Headers map[string]string
	Body    string
}

// visionMessageContent 多模态消息 content 两段（字段序 = TS 对象插入序）。
type visionPart struct {
	Type     string          `json:"type"`
	ImageURL *visionImageURL `json:"image_url,omitempty"`
	Text     *string         `json:"text,omitempty"`
}

type visionImageURL struct {
	URL string `json:"url"`
}

type visionMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// visionBody 识图请求体（非流式：无 stream 字段；字段序 = TS 插入序：
// model/messages/temperature/max_tokens/关闭参数）。
type visionBody struct {
	Model       string          `json:"model"`
	Messages    []visionMessage `json:"messages"`
	Temperature float64         `json:"temperature"`
	MaxTokens   int             `json:"max_tokens"`
	// DR-005 恒发关闭参数（按预设映射二选一；omitempty 由指针 nil 控制）。
	EnableThinking *bool           `json:"enable_thinking,omitempty"`
	Thinking       *thinkingConfig `json:"thinking,omitempty"`
}

// BuildVisionRequest 组装一次非流式多模态识图请求（vision_board_reader.dart:82-137）。
// config 为**已解析真实 Key** 的配置（Reader 侧已注入，掩码不进本函数）；
// 未配置（端点/模型缺失）抛 LlmConfigError；空 Key 不带鉴权头（本地网关）。
func BuildVisionRequest(config LlmEndpointConfig, dataURL string) (BuiltVisionRequest, error) {
	if strings.TrimSpace(config.BaseURL) == "" || strings.TrimSpace(config.Model) == "" {
		return BuiltVisionRequest{}, &LlmConfigError{Message: "视觉模型未配置（需填写端点与模型 ID）"}
	}
	text := visionPromptText
	body := visionBody{
		Model: strings.TrimSpace(config.Model),
		Messages: []visionMessage{
			{Role: "system", Content: VisionSystemPrompt},
			{Role: "user", Content: []any{
				visionPart{Type: "image_url", ImageURL: &visionImageURL{URL: dataURL}},
				visionPart{Type: "text", Text: &text},
			}},
		},
		Temperature: VisionTemperature,
		MaxTokens:   VisionMaxTokens,
	}
	// DR-005：无条件注入关闭参数（无任何开关路径）。GLM 形态走 thinking 对象，
	// 其余（含 DashScope 语义与兜底）走顶层布尔——与 BuildChatRequest 同源同映射。
	if ThinkingStyleFor(config.Preset) == ThinkingGLM {
		body.Thinking = &thinkingConfig{Type: "disabled"}
	} else {
		off := false
		body.EnableThinking = &off
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return BuiltVisionRequest{}, &LlmConfigError{Message: "请求体序列化失败：" + err.Error()}
	}

	headers := map[string]string{"Content-Type": "application/json"}
	if key := strings.TrimSpace(config.APIKey); key != "" {
		headers["Authorization"] = "Bearer " + key
	}
	return BuiltVisionRequest{URL: RequestURL(config.BaseURL), Headers: headers, Body: string(raw)}, nil
}

// DetectImageMime 魔数判 MIME：PNG 头 89 50 4E；其余一律按 JPEG（:221-229）。
func DetectImageMime(data []byte) string {
	if len(data) >= 3 && data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4e {
		return "image/png"
	}
	return "image/jpeg"
}

var visionFencePattern = regexp.MustCompile("```[a-zA-Z]*")

// ExtractVisionJSON 剥 markdown 围栏后提取首个 { 到末个 } 的 JSON 对象（:187-197）。
func ExtractVisionJSON(content string) (map[string]any, error) {
	text := visionFencePattern.ReplaceAllString(content, "")
	text = strings.ReplaceAll(text, "```", "")
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return nil, &VisionParseError{Message: "回复中未找到 JSON"}
	}
	text = text[start : end+1]
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, &VisionParseError{Message: "JSON 解析失败：" + err.Error()}
	}
	obj, ok := parsed.(map[string]any)
	if !ok {
		// null / 数组 / 标量均非对象。
		return nil, &VisionParseError{Message: "回复中的 JSON 不是对象"}
	}
	return obj, nil
}

// ParseVisionTurn turn 字段 → 是否红方（解析失败按红方处理，:178-185）。
func ParseVisionTurn(content string) bool {
	json, err := ExtractVisionJSON(content)
	if err != nil {
		return true
	}
	turn, present := json["turn"]
	if !present || turn == nil {
		return true // `${undefined}` → "undefined" ≠ black → 红方
	}
	return !strings.EqualFold(fmt.Sprintf("%v", turn), "black")
}

// visionColIndex 单列字母 a-i → 列下标；非法返回 -1（:215-219）。
// 多字符取首字符（TS charCodeAt(0) 语义）。
func visionColIndex(col any) int {
	s, ok := col.(string)
	if !ok || len(s) == 0 {
		return -1
	}
	runes := []rune(strings.ToLower(s))
	c := int(runes[0]) - 'a'
	if c >= 0 && c <= 8 {
		return c
	}
	return -1
}

// ParseVisionPieces 解析模型 JSON 回复为 10×9 棋盘矩阵（:151-176）。
// JSON 坏损、坐标越界、未知棋子均抛 VisionParseError；通过后执行双王硬校验。
func ParseVisionPieces(content string) (rules.BoardGrid, error) {
	jsonObj, err := ExtractVisionJSON(content)
	if err != nil {
		return nil, err
	}
	pieces, ok := jsonObj["pieces"].([]any)
	if !ok {
		return nil, &VisionParseError{Message: "JSON 缺少 pieces 数组"}
	}
	grid := make(rules.BoardGrid, 10)
	for i := range grid {
		grid[i] = make([]*rules.Piece, 9)
	}
	for _, item := range pieces {
		record, ok := item.(map[string]any)
		if !ok {
			continue // null/标量条目跳过（TS: item 非对象 continue）
		}
		col := visionColIndex(record["col"])
		rowRaw := ""
		if record["row"] != nil {
			rowRaw = fmt.Sprintf("%v", record["row"])
		}
		row := -1
		if isDigits(rowRaw) {
			row = parseIntDecimal(rowRaw)
		}
		piece := (*rules.Piece)(nil)
		if s, ok := record["piece"].(string); ok && len(s) == 1 {
			piece = rules.PieceFromFenChar(s[0])
		}
		if col < 0 || row < 0 || piece == nil {
			raw, _ := json.Marshal(record)
			return nil, &VisionParseError{Message: fmt.Sprintf("非法棋子条目: %s", raw)}
		}
		if row < 0 || row > 9 || col < 0 || col > 8 {
			return nil, &VisionParseError{Message: fmt.Sprintf("坐标越界: col=%v row=%d", record["col"], row)}
		}
		grid[row][col] = piece
	}
	if err := ValidateVisionKings(grid); err != nil {
		return nil, err
	}
	return grid, nil
}

// isDigits /^\d+$/ 等价判定（空串不匹配）。
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// parseIntDecimal 十进制解析（isDigits 已保证合法性）。
func parseIntDecimal(s string) int {
	n := 0
	for _, r := range s {
		n = n*10 + int(r-'0')
	}
	return n
}

// ValidateVisionKings 双王硬校验：红黑王必须各恰一（:199-213）。
func ValidateVisionKings(grid rules.BoardGrid) error {
	redKing, blackKing := 0, 0
	for _, row := range grid {
		for _, piece := range row {
			if piece == nil || piece.Kind != rules.King {
				continue
			}
			if piece.Side == rules.Red {
				redKing++
			} else {
				blackKing++
			}
		}
	}
	if redKing != 1 || blackKing != 1 {
		return &VisionParseError{Message: fmt.Sprintf("双方王数量异常（红 %d / 黑 %d）", redKing, blackKing)}
	}
	return nil
}

// VisionGridToFen 识图成功结果：组装后的完整 FEN（轮走方来自模型识别，
// 可在校正界面修改）。
func VisionGridToFen(grid rules.BoardGrid, isRedTurn bool) string {
	return rules.BuildFen(grid, isRedTurn)
}

// ExcerptVisionBody 截取响应体前 160 字符（:231-234）。
func ExcerptVisionBody(body string) string {
	text := strings.TrimSpace(body)
	if len(text) <= 160 {
		return text
	}
	return text[:160] + "…"
}

// -----------------------------------------------------------------------------
// VisionReader（Electron 版 main/services/visionReader.ts 等价移植）
// -----------------------------------------------------------------------------

// VisionReadBoardRequest 识图请求（00 §3.1；魔数判 MIME 仅 PNG/JPEG，05 §7）。
// AuthSlot（DR-010 同机制）：调用层只见掩码 Key，携带本槽位时由 Reader 注入
// 真实 Authorization；持完整 Key（用户刚输入未回读）时省略本字段。
type VisionReadBoardRequest struct {
	Config      LlmEndpointConfig
	ImageBase64 string
	Mime        string
	AuthSlot    string
}

// VisionReadBoardResult 识图结果：组装 10×9 矩阵后经 BuildFen 得到的盘面。
type VisionReadBoardResult struct {
	Fen string
}

// VisionReaderOptions Reader 选项。
type VisionReaderOptions struct {
	// TimeoutMs 单次请求超时毫秒数（默认 120s；测试注入短超时）。
	TimeoutMs int
	// MaxAttempts 最多尝试次数（默认 2；测试注入 1 以观察错误直通）。
	MaxAttempts int
	// ResolveAPIKey 凭据槽位 → 完整 API Key（authSlot 注入用；无/未配置返回空串）。
	ResolveAPIKey func(slot string) string
}

// VisionTimeoutError 超时内部错误（与普通异常区分，vision_board_reader.dart:71-74）。
type VisionTimeoutError struct {
	TimeoutMs int
}

func (e *VisionTimeoutError) Error() string {
	return fmt.Sprintf("请求超时（%ds）。大模型思维链过慢或网络较差，可重试或更换更快的视觉模型",
		(e.TimeoutMs+500)/1000)
}

// VisionReader 主进程视觉识图服务（vision_board_reader.dart 的 HTTP 部分等价移植）。
//
//   - 非流式 POST OpenAI 兼容 /chat/completions（多模态 image_url + 文本提示词）；
//   - 单次超时 120s（ctx 截止），最多尝试 2 次——HTTP 错误、超时、JSON 坏损、
//     双王校验失败均触发重试（协议/解析复用本文件纯函数）；
//   - Key 注入（DR-010）：调用层只见掩码 Key，携带 authSlot 时由本服务从凭据
//     槽位注入真实 Authorization；持完整 Key 时直接内联。
type VisionReader struct {
	timeoutMs     int
	maxAttempts   int
	resolveAPIKey func(slot string) string
}

// NewVisionReader 构造 Reader（选项零值取默认）。
func NewVisionReader(options VisionReaderOptions) *VisionReader {
	timeoutMs := options.TimeoutMs
	if timeoutMs <= 0 {
		timeoutMs = VisionTimeoutMs
	}
	maxAttempts := options.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = VisionMaxAttempts
	}
	return &VisionReader{
		timeoutMs:     timeoutMs,
		maxAttempts:   maxAttempts,
		resolveAPIKey: options.ResolveAPIKey,
	}
}

// ReadBoard 调用视觉模型识别棋盘，返回组装好的 FEN；全部尝试耗尽抛 LlmApiError。
func (r *VisionReader) ReadBoard(ctx context.Context, req VisionReadBoardRequest) (VisionReadBoardResult, error) {
	// DR-010：掩码 Key（secure.get 回读）→ authSlot 注入真实 Key；完整 Key 直接内联。
	key := strings.TrimSpace(req.Config.APIKey)
	apiKey := key
	if strings.HasPrefix(key, "****") {
		real := ""
		if req.AuthSlot != "" && r.resolveAPIKey != nil {
			real = r.resolveAPIKey(req.AuthSlot)
		}
		apiKey = real
	}

	dataURL := fmt.Sprintf("data:%s;base64,%s", req.Mime, req.ImageBase64)
	config := req.Config
	config.APIKey = apiKey
	built, err := BuildVisionRequest(config, dataURL)
	if err != nil {
		return VisionReadBoardResult{}, err
	}

	lastError := ""
	for attempt := 1; attempt <= r.maxAttempts; attempt++ {
		content, err := r.requestOnce(ctx, built.URL, built.Headers, built.Body)
		if err == nil {
			grid, err := ParseVisionPieces(content)
			if err == nil {
				turn := ParseVisionTurn(content)
				return VisionReadBoardResult{Fen: VisionGridToFen(grid, turn)}, nil
			}
			lastError = err.Error()
			continue
		}
		var timeoutErr *VisionTimeoutError
		if errors.As(err, &timeoutErr) {
			lastError = timeoutErr.Error()
		} else {
			lastError = err.Error()
		}
	}
	return VisionReadBoardResult{}, &LlmApiError{
		Message: fmt.Sprintf("已重试 %d 次仍失败：%s", r.maxAttempts, lastOrUnknown(lastError)),
	}
}

func lastOrUnknown(lastError string) string {
	if lastError == "" {
		return "未知错误"
	}
	return lastError
}

// visionChatResponse /chat/completions 非流式响应的最小解（choices 链）。
type visionChatResponse struct {
	Choices []struct {
		Message struct {
			Content any `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// requestOnce 单次 POST：ctx 截止 = 单次超时；HTTP ≠ 200 截响应体前 160 字符。
func (r *VisionReader) requestOnce(ctx context.Context, url string, headers map[string]string, body string) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(r.timeoutMs)*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(callCtx, http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("连接失败：%s", err.Error())
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// 超时中止与网络错误在此汇合；按 ctx 截止标记区分提示。
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			return "", &VisionTimeoutError{TimeoutMs: r.timeoutMs}
		}
		return "", fmt.Errorf("连接失败：%s", err.Error())
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, ExcerptVisionBody(string(raw)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return "", fmt.Errorf("连接失败：%s", err.Error())
	}
	var payload visionChatResponse
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", errors.New("响应不是合法 JSON")
	}
	if len(payload.Choices) == 0 {
		return "", errors.New("响应缺少 choices")
	}
	content, ok := payload.Choices[0].Message.Content.(string)
	if !ok {
		return "", errors.New("响应缺少正文")
	}
	return content, nil
}
