package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zalando/go-keyring"
)

// 凭据存储（07 文档 §4；对应 Electron 版 llm_config_store.dart / credentials.ts）：
//
// - 三槽位 llm_config_red / llm_config_black / llm_config_assistant；
// - 槽位 JSON {baseUrl, apiKey, model, preset}（无 disableThinking 字段——DR-005，
//   思维链关闭参数在请求构造层恒发，M4 接入）；
// - 主存 OS keyring（service=chinese-chess-ultra-go，user=槽位名）；
// - 回退（沿 electron-DR-011）：keyring 不可用（无 secret service/凭据库锁定）→
//   明文写 <userData>/credentials.enc 权限 0600，UI 如实回报"已用未加密本地文件存储"；
//   读优先 keyring、回退文件；删除双向清理；
// - 掩码语义（electron-DR-013）：回读给 UI 的 Key 一律 ****+末 4 位；保存时掩码
//   字符串不覆盖真实 Key（回写合并）；完整 Key 不进日志/异常消息/渲染层。

// KeyringServiceName keyring service 名（07 §4）。
const KeyringServiceName = "chinese-chess-ultra-go"

// 凭据三槽位名（shared/ipc/types.SecureSlot）。
const (
	SlotRed       = "llm_config_red"
	SlotBlack     = "llm_config_black"
	SlotAssistant = "llm_config_assistant"
)

// CredentialsFallbackFilename 回退文件名（07 §4：<userData>/credentials.enc；
// 0600 明文本就是回退的目的）。
const CredentialsFallbackFilename = "credentials.enc"

// SlotConfig 单槽位凭据（07 §4 槽位 JSON；preset=使用预设名，自定义/缺省为空串）。
type SlotConfig struct {
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Model   string `json:"model"`
	Preset  string `json:"preset,omitempty"`
}

// Keyring OS keyring 抽象：测试注入伪实现（(credentials.spec.ts fakeCryptor 同位)）。
type Keyring interface {
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
	Delete(service, user string) error
}

// OSKeyring zalando/go-keyring 默认实现。
type OSKeyring struct{}

// Get 读取 keyring 条目；ErrNotFound 表示槽位不存在。
func (OSKeyring) Get(service, user string) (string, error) { return keyring.Get(service, user) }

// Set 写入 keyring 条目（OS 凭据库加密）。
func (OSKeyring) Set(service, user, secret string) error { return keyring.Set(service, user, secret) }

// Delete 删除 keyring 条目。
func (OSKeyring) Delete(service, user string) error { return keyring.Delete(service, user) }

// SecureSetResult 写入结果（shared/ipc/types.SecureSetResult）：
// encrypted = keyring 落盘；plainFallback = 系统安全存储不可用，明文回退（0600）。
type SecureSetResult struct {
	Stored string `json:"stored"`
}

const (
	storedEncrypted     = "encrypted"
	storedPlainFallback = "plainFallback"
)

// Credentials 凭据服务（keyring 主存 + 明文回退文件 + 掩码/合并）。
type Credentials struct {
	mu           sync.Mutex // 回退文件读改写互斥（Wails 绑定方法并发进入）
	keyring      Keyring
	fallbackPath string // <userData>/credentials.enc
}

// NewCredentials 构造；fallbackPath 为 <userData>/credentials.enc。
func NewCredentials(kr Keyring, fallbackPath string) *Credentials {
	return &Credentials{keyring: kr, fallbackPath: fallbackPath}
}

// MaskApiKey API Key 掩码（shared/mask.ts maskApiKey，对齐 llm_config.dart:80-84）：
// 空 → ""；长度 ≤4 → "****"；否则 "****"+末 4 位。完整 Key 不得进入日志/异常消息/UI。
func MaskApiKey(apiKey string) string {
	if len(apiKey) == 0 {
		return ""
	}
	runes := []rune(apiKey)
	if len(runes) <= 4 {
		return "****"
	}
	return "****" + string(runes[len(runes)-4:])
}

// Get 读取槽位：apiKey 已掩码；未配置/损坏返回 nil（llm_config_store.dart:42-51）。
// 读取顺序：keyring → 明文回退文件（DR-011）。
func (c *Credentials) Get(slot string) *SlotConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	cfg := c.getRawLocked(slot)
	if cfg == nil {
		return nil
	}
	masked := *cfg
	masked.APIKey = MaskApiKey(masked.APIKey)
	return &masked
}

// GetRaw 读取槽位完整配置（apiKey 不掩码）——仅供后端内部使用（electron-DR-010：
// M4 请求构造注入真实 Authorization），任何路径不得把返回值发给渲染层或写日志。
// keyring 优先，明文回退文件兜底。
func (c *Credentials) GetRaw(slot string) *SlotConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.getRawLocked(slot)
}

// getRaw 读取语义（credentials.ts readEncrypted/readPlain 映射）：
//   - keyring 命中但解析失败（密文损坏/格式非法）→ 未配置（nil，与"解密失败按未配置"一致）；
//   - keyring 槽位缺失或 keyring 不可用 → 回退文件（07 §4：keyring 不可用 → 回退）；
//   - 回退文件该槽位缺失/字段非法 → 未配置（nil）。
//
// 调用方须持 c.mu（Set 的掩码合并路径复用）。
func (c *Credentials) getRawLocked(slot string) *SlotConfig {
	if secret, err := c.keyring.Get(KeyringServiceName, slot); err == nil {
		cfg, ok := parseSlotJSON(secret)
		if !ok {
			return nil
		}
		return cfg
	}
	return c.readPlain(slot)
}

// Set 整体写入槽位；keyring 不可用时走明文回退文件（DR-011）。
// 返回实际落盘方式，供界面如实告知用户。
//
// 掩码合并（electron-DR-013）：渲染层回读的 apiKey 是掩码（****+末 4 位），页面
// 防抖保存/卸载回写会把整个配置原样写回——若不合并，掩码字符串会覆盖真实 Key
// （重启后 Key 失效）。apiKey 呈掩码形态时保留存储中的原 Key，仅更新其余字段；
// 无原 Key 可恢复时按空 Key 处理。
func (c *Credentials) Set(slot string, payload SlotConfig) (SecureSetResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	merged := c.mergeMaskedKeyLocked(slot, payload)
	if err := c.keyring.Set(KeyringServiceName, slot, marshalSlot(merged)); err == nil {
		return SecureSetResult{Stored: storedEncrypted}, nil
	}
	file := c.readPlainFile()
	file[slot] = json.RawMessage(marshalSlot(merged))
	if err := c.writePlainFile(file); err != nil {
		return SecureSetResult{}, err
	}
	return SecureSetResult{Stored: storedPlainFallback}, nil
}

// Delete 删除槽位（keyring 与回退文件双向清理；幂等）。
func (c *Credentials) Delete(slot string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.keyring.Delete(KeyringServiceName, slot) // 槽位可能不存在/keyring 不可用：忽略
	file := c.readPlainFile()
	if _, ok := file[slot]; ok {
		delete(file, slot)
		return c.writePlainFile(file)
	}
	return nil
}

// mergeMaskedKeyLocked 掩码 Key 合并：掩码形态（**** 前缀，先去空白）→ 沿用存储中的原
// Key；无原 Key → 空串（不落掩码字符串）。调用方须持 c.mu。
func (c *Credentials) mergeMaskedKeyLocked(slot string, payload SlotConfig) SlotConfig {
	if !strings.HasPrefix(strings.TrimSpace(payload.APIKey), "****") {
		return payload
	}
	existing := c.getRawLocked(slot)
	if existing != nil {
		payload.APIKey = existing.APIKey
	} else {
		payload.APIKey = ""
	}
	return payload
}

// parseSlotJSON 槽位密文/明文解析：baseUrl 与 model 必须为字符串（credentials.ts
// readEncrypted 的字段校验；损坏/缺失字段按未配置处理）。
func parseSlotJSON(secret string) (*SlotConfig, bool) {
	var cfg struct {
		BaseURL *string `json:"baseUrl"`
		APIKey  *string `json:"apiKey"`
		Model   *string `json:"model"`
		Preset  *string `json:"preset"`
	}
	if err := json.Unmarshal([]byte(secret), &cfg); err != nil {
		return nil, false
	}
	if cfg.BaseURL == nil || cfg.Model == nil {
		return nil, false
	}
	out := SlotConfig{BaseURL: *cfg.BaseURL, Model: *cfg.Model}
	if cfg.APIKey != nil {
		out.APIKey = *cfg.APIKey
	}
	if cfg.Preset != nil {
		out.Preset = *cfg.Preset
	}
	return &out, true
}

func marshalSlot(cfg SlotConfig) string {
	raw, err := json.Marshal(cfg)
	if err != nil {
		// SlotConfig 全为字符串字段，marshal 不可失败；防御返回空对象
		return "{}"
	}
	return string(raw)
}

// readPlainFile 读取回退文件整体（文件层 JSON 透传：缺失/损坏 → 空表，后续写入重建；
// 各槽位的字段校验在单槽位读取时进行，与 credentials.ts readPlainFile/readPlain 分层一致）。
func (c *Credentials) readPlainFile() map[string]json.RawMessage {
	file := map[string]json.RawMessage{}
	raw, err := os.ReadFile(c.fallbackPath)
	if err != nil {
		return file
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		return map[string]json.RawMessage{}
	}
	return file
}

// readPlain 读取回退文件单槽位；缺失/字段非法 → nil（未配置）。
func (c *Credentials) readPlain(slot string) *SlotConfig {
	raw, ok := c.readPlainFile()[slot]
	if !ok {
		return nil
	}
	cfg, ok := parseSlotJSON(string(raw))
	if !ok {
		return nil
	}
	return cfg
}

// writePlainFile 原子写回退文件：临时文件 + rename，权限 0600（仅当前用户可读）。
func (c *Credentials) writePlainFile(file map[string]json.RawMessage) error {
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(c.fallbackPath), 0o700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	tmp := c.fallbackPath + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.fallbackPath); err != nil {
		return errors.Join(err, os.Remove(tmp))
	}
	return nil
}
