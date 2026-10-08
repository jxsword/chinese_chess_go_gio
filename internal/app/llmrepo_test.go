package app

// T4'.2 测试：ui.LlmStore 的 app 实现（凭据槽位掩码回读/掩码合并落盘/降级路径）。
// 复制物 Credentials 掩码语义已被 credentials_test 锚定；此处锚定代理层契约：
// 回执形状（SecureSlotLoaded/Saved）、掩码不进 UI、ResolveAPIKey 只供注入。

import (
	"sync"
	"testing"
	"time"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// llmStoreFixture 收集代理回执（后台 goroutine → 通道）。
type llmStoreFixture struct {
	store    *DataStore
	receipts chan llmReceipt
}

func newLlmStoreFixture(t *testing.T, dir string) *llmStoreFixture {
	t.Helper()
	f := &llmStoreFixture{store: OpenDataStore(dir), receipts: make(chan llmReceipt, 8)}
	return f
}

func (f *llmStoreFixture) emit(_ string, payload any, err error) {
	f.receipts <- llmReceipt{payload: payload, err: err}
}

type llmReceipt struct {
	payload any
	err     error
}

func (f *llmStoreFixture) wait(t *testing.T) llmReceipt {
	t.Helper()
	select {
	case r := <-f.receipts:
		return r
	case <-time.After(2 * time.Second):
		t.Fatal("回执超时")
		return llmReceipt{}
	}
}

// 槽位 roundtrip：保存（掩码合并落盘）→ 回读掩码 Key；ResolveAPIKey 返回完整 Key。
func TestLlmStoreSlotRoundtripMasked(t *testing.T) {
	dir := t.TempDir()
	f := newLlmStoreFixture(t, dir)
	proxy := newLlmStore(f.store, f.emit, &sync.WaitGroup{})

	cfg := llm.LlmEndpointConfig{BaseURL: "https://api.test/v1", APIKey: "sk-real-secret-9999", Model: "test-model", Preset: "DeepSeek"}
	proxy.SaveSlotAsync("save-1", storage.SlotRed, cfg)
	r := f.wait(t)
	saved := r.payload.(ui.SecureSlotSaved)
	if r.err != nil || saved.Stored == "" {
		t.Fatalf("save failed: err=%v stored=%q", r.err, saved.Stored)
	}
	if saved.RequestID != "save-1" {
		t.Fatalf("save receipt requestId = %q, want save-1（#G5 载荷带 id）", saved.RequestID)
	}

	proxy.LoadSlotAsync("load-1", saved.Slot)
	loaded := f.wait(t).payload.(ui.SecureSlotLoaded)
	if loaded.RequestID != "load-1" {
		t.Fatalf("load receipt requestId = %q, want load-1（#G5 载荷带 id）", loaded.RequestID)
	}
	if loaded.Config.APIKey != "****9999" {
		t.Fatalf("masked key = %q, want ****9999（铁律 #G7）", loaded.Config.APIKey)
	}
	if loaded.Config.BaseURL != cfg.BaseURL || loaded.Config.Model != cfg.Model || loaded.Config.Preset != cfg.Preset {
		t.Fatalf("non-key fields altered: %+v", loaded.Config)
	}

	// ResolveAPIKey：完整 Key 仅注入面（模拟 DR-010 传输层）。
	if got := proxy.ResolveAPIKey(saved.Slot); got != "sk-real-secret-9999" {
		t.Fatalf("resolve = %q", got)
	}

	// 脏存盘清洗：存储中的历史脏配置（模型 ID 夹带 NUL）加载回读即净
	//（走子管线/测试连接/展示三路一致——M4' 验收反馈修复轮）。
	// 实测存储形态（用户存档）：model = "qwen3.8-max" + 4×NUL
	dirty := llm.LlmEndpointConfig{BaseURL: "https://dashscope.aliyuncs.com/compatible-mode/v1",
		APIKey: "sk-dirty", Model: "qwen3.8-max\x00\x00\x00\x00"}
	proxy.SaveSlotAsync("save-dirty", storage.SlotBlack, dirty)
	f.wait(t)
	if got := proxy.ResolveAPIKey(storage.SlotBlack); got != "sk-dirty" {
		t.Fatalf("dirty save resolve = %q", got)
	}
	proxy.LoadSlotAsync("load-dirty", storage.SlotBlack)
	loadedDirty := f.wait(t).payload.(ui.SecureSlotLoaded)
	if loadedDirty.Config == nil || loadedDirty.Config.Model != "qwen3.8-max" {
		t.Fatalf("dirty load = %+v, want model cleaned to qwen3.8-max", loadedDirty.Config)
	}
	if loadedDirty.Config.APIKey != "****irty" {
		t.Fatalf("dirty load key must be masked, got %q", loadedDirty.Config.APIKey)
	}

	// 掩码回写不覆盖真实 Key（防错 #8，代理层透传复制物合并语义）。
	maskedWrite := cfg
	maskedWrite.APIKey = "****9999"
	proxy.SaveSlotAsync("save-2", saved.Slot, maskedWrite)
	f.wait(t)
	if got := proxy.ResolveAPIKey(saved.Slot); got != "sk-real-secret-9999" {
		t.Fatalf("masked write must not clobber real key, got %q", got)
	}
}

// 降级路径：数据目录不可用 → 加载 nil 配置、保存错误回执、ResolveAPIKey 空串。
func TestLlmStoreDegrade(t *testing.T) {
	f := newLlmStoreFixture(t, "")
	proxy := newLlmStore(f.store, f.emit, &sync.WaitGroup{})

	proxy.LoadSlotAsync("load-1", "llm_config_red")
	loaded := f.wait(t).payload.(ui.SecureSlotLoaded)
	if loaded.Config != nil {
		t.Fatal("degraded load must be nil config")
	}

	proxy.SaveSlotAsync("save-1", "llm_config_red", llm.LlmEndpointConfig{Model: "m"})
	r := f.wait(t)
	saved := r.payload.(ui.SecureSlotSaved)
	if saved.Err == nil {
		t.Fatal("degraded save must error")
	}
	if got := proxy.ResolveAPIKey(storage.SlotRed); got != "" {
		t.Fatalf("degraded resolve = %q", got)
	}
}
