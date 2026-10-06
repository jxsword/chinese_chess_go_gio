package app

// LLM 页存储代理（T4'.2，design_docs/00 §4 `secure:slot:get/set` 行）：
// 复制物 storage.Credentials / state.SaveLlmSettings 的 I/O 一律后台 goroutine
//（铁律 #G3），结果经事件总线回主循环；完整 API Key 只经 ResolveAPIKey 供给
// 传输层注入（DR-010），掩码回读给 UI（铁律 #G7，防错 #8）。

import (
	"errors"
	"log"

	"github.com/jxsword/chinese_chess_go_gio/internal/llm"
	"github.com/jxsword/chinese_chess_go_gio/internal/state"
	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
	"github.com/jxsword/chinese_chess_go_gio/internal/ui"
)

// llmStore ui.LlmStore 的 app 实现（每窗口一个）。
type llmStore struct {
	store *DataStore
	emit  func(requestID string, payload any, err error)
}

func newLlmStore(store *DataStore, emit func(requestID string, payload any, err error)) *llmStore {
	return &llmStore{store: store, emit: emit}
}

// errStorageUnavailable 存储降级口径（与"本地存储不可用"同文案族）。
var errStorageUnavailable = errors.New("app: 本地存储不可用")

// slotToLlm 复制物槽位形状 → LLM 端点配置（同形字段直映）。
func slotToLlm(cfg *storage.SlotConfig) *llm.LlmEndpointConfig {
	if cfg == nil {
		return nil
	}
	return &llm.LlmEndpointConfig{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model, Preset: cfg.Preset}
}

// LoadSlotAsync 实现 ui.LlmStore（掩码回读——Credentials.Get 已掩码，#G7）。
func (s *llmStore) LoadSlotAsync(requestID, slot string) {
	go func() {
		creds := s.store.Credentials()
		if creds == nil {
			s.emit(requestID, ui.SecureSlotLoaded{Slot: slot}, nil)
			return
		}
		s.emit(requestID, ui.SecureSlotLoaded{Slot: slot, Config: slotToLlm(creds.Get(slot))}, nil)
	}()
}

// SaveSlotAsync 实现 ui.LlmStore（掩码合并落盘——UI 持掩码 Key，复制物 Set
// 不让掩码串覆盖真实 Key，防错 #8）。
func (s *llmStore) SaveSlotAsync(requestID, slot string, cfg llm.LlmEndpointConfig) {
	go func() {
		creds := s.store.Credentials()
		if creds == nil {
			s.emit(requestID, ui.SecureSlotSaved{Slot: slot, Err: errStorageUnavailable}, nil)
			return
		}
		res, err := creds.Set(slot, storage.SlotConfig{BaseURL: cfg.BaseURL, APIKey: cfg.APIKey, Model: cfg.Model, Preset: cfg.Preset})
		if err != nil {
			log.Println("app: 模型配置保存失败:", err)
		}
		stored := ""
		if err == nil {
			stored = res.Stored
		}
		s.emit(requestID, ui.SecureSlotSaved{Slot: slot, Stored: stored}, err)
	}()
}

// SaveSettings 实现 ui.LlmStore（fire-and-forget：错误记日志不回执，上游
// 防抖保存静默同语义；读侧 LlmEnv.Settings 为内存态不受影响）。
func (s *llmStore) SaveSettings(settings state.LlmGameSettings, fields ...state.LlmSettingsField) {
	go func() {
		if err := state.SaveLlmSettings(s.store.Settings(), settings, fields...); err != nil {
			log.Println("app: 对局设置保存失败:", err)
		}
	}()
}

// ResolveAPIKey 实现 ui.LlmStore（完整 Key；仅供传输层注入 Authorization——
// DR-010。返回值不得进入 UI/日志/异常消息，铁律 #G7）。仅后台 goroutine 调用。
func (s *llmStore) ResolveAPIKey(slot string) string {
	creds := s.store.Credentials()
	if creds == nil {
		return ""
	}
	raw := creds.GetRaw(slot)
	if raw == nil {
		return ""
	}
	return raw.APIKey
}
