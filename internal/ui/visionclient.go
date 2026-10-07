package ui

// 视觉识图客户端（T6'.3，design_docs/05 附录 §7 消费方式注记 + 00 §4
// cc:vision:readBoard 行）：goroutine 直调复制物 VisionReader（单次 ≤120s×2，
// **无中途取消入口**——K33 口径，页面按钮 disabled 防重入），结果经 app 事件
// 总线回主循环（铁律 #G3 正方向）；迟到回执按 requestId 丢弃（铁律 #G5）。
//
// 文件读取与 base64 编码同在请求 goroutine 内完成（I/O 不进主循环）；
// 掩码 Key（DR-010）：请求携带 AuthSlot，Reader 按槽位注入真实 Authorization。

import (
	"context"
	"encoding/base64"
	"os"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
)

// VisionClient 识图客户端（每页一实例；构造期写定 emit，goroutine 只读）。
type VisionClient struct {
	emit func(requestID string, payload any, err error)
	// resolve 槽位完整 Key 注入面（app 注入，仅后台 goroutine 调用——#G7）。
	resolve func(slot string) string
}

// NewVisionClient 创建客户端（resolve 经 env 注入）。
func NewVisionClient(env LlmEnv) *VisionClient {
	var resolve func(slot string) string
	if env.Store != nil {
		resolve = env.Store.ResolveAPIKey
	}
	return &VisionClient{emit: env.Emit, resolve: resolve}
}

// ReadFileAsync 图片载入全链路（后台 goroutine：读文件 → MIME 魔数判别 →
// base64 → VisionReader.ReadBoard → 回执 ui.VisionReadDone）。
func (c *VisionClient) ReadFileAsync(requestID, path string, config llm.LlmEndpointConfig, authSlot string) {
	go func() {
		data, err := os.ReadFile(path)
		if err != nil {
			c.emit(requestID, VisionReadDone{RequestID: requestID, Err: err}, nil)
			return
		}
		c.readAsync(requestID, data, config, authSlot)
	}()
}

// ReadDataAsync 图片字节直载（测试/剪贴板等来源）。
func (c *VisionClient) ReadDataAsync(requestID string, data []byte, config llm.LlmEndpointConfig, authSlot string) {
	go c.readAsync(requestID, data, config, authSlot)
}

func (c *VisionClient) readAsync(requestID string, data []byte, config llm.LlmEndpointConfig, authSlot string) {
	reader := llm.NewVisionReader(llm.VisionReaderOptions{ResolveAPIKey: c.resolve})
	result, err := reader.ReadBoard(context.Background(), llm.VisionReadBoardRequest{
		Config:      config,
		ImageBase64: base64.StdEncoding.EncodeToString(data),
		Mime:        llm.DetectImageMime(data),
		AuthSlot:    authSlot,
	})
	done := VisionReadDone{RequestID: requestID}
	if err == nil {
		done.Fen = result.Fen
	} else {
		done.Err = err
	}
	c.emit(requestID, done, nil)
}
