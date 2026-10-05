package storage

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
)

// 设置存储（07 文档 §5）：JSON 文件 <userData>/settings.json 替代 electron-store，
// 键沿用原版 shared_preferences（global_auto_save / corpus.userPath / llm_settings_*）。
//
//   - global_auto_save 缺省 true（07 §3 默认值）；
//   - llm_settings_* 越界值在 load 时 clamp 回默认/范围（05 §8 表；语义逐字对照
//     packages/llm/settings.ts llmSettingsFromRaw——枚举 index 越界回落默认、数值越界 clamp）；
//   - Set 写入原值（electron-store 语义；渲染层保存前已 clamp，load 时再兜底）。
type Settings struct {
	mu   sync.Mutex
	path string
	data map[string]any
}

// SettingKeyGlobalAutoSave 自动保存开关（shared/constants.ts SETTING_KEYS.globalAutoSave）。
const SettingKeyGlobalAutoSave = "global_auto_save"

// settingsFilename JSON 设置文件名（electron-store name="settings" 等价）。
const settingsFilename = "settings.json"

// OpenSettings 打开（或初始化）设置文件；目录不存在时创建。损坏文件视为空设置
// （后续写入重建，对齐 Electron 版"读失败按未配置/缺省"语义）。
func OpenSettings(userDataDir string) (*Settings, error) {
	if err := os.MkdirAll(userDataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create userData dir: %w", err)
	}
	s := &Settings{path: filepath.Join(userDataDir, settingsFilename), data: map[string]any{}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// load 读取并 clamp；文件缺失/损坏按空设置处理。
func (s *Settings) load() error {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil || data == nil {
		return nil // 损坏视为无设置
	}
	clampLLMSettings(data)
	s.data = data
	return nil
}

// Get 读取设置键；未设置返回 nil（契约面 null）；global_auto_save 缺省 true。
func (s *Settings) Get(key string) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.data[key]
	if !ok {
		if key == SettingKeyGlobalAutoSave {
			return true
		}
		return nil
	}
	return v
}

// Set 写入设置键并落盘（原子写 tmp+rename；非敏感文件 0644）。
func (s *Settings) Set(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return s.writeLocked()
}

// Delete 删除设置键（回落默认/缺省语义）。
func (s *Settings) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return s.writeLocked()
}

func (s *Settings) writeLocked() error {
	raw, err := json.Marshal(s.data)
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// ---------------------------------------------------------------------------
// llm_settings_* 越界 clamp（llm_settings.dart:85-118 / settings.ts llmSettingsFromRaw）
// ---------------------------------------------------------------------------

const llmSettingsPrefix = "llm_settings_"

// LLM 设置键（settings.ts LLM_SETTING_KEYS；持久化为枚举 index 整数）。
const (
	keyTimeoutSeconds     = llmSettingsPrefix + "timeoutSeconds"
	keyMaxAttempts        = llmSettingsPrefix + "maxAttempts"
	keyFallbackIndex      = llmSettingsPrefix + "fallbackIndex"
	keyIntervalSeconds    = llmSettingsPrefix + "intervalSeconds"
	keyAdvisorModeIndex   = llmSettingsPrefix + "advisorModeIndex"
	keyStrengthBlend      = llmSettingsPrefix + "strengthBlend"
	keyAdvisorDifficulty  = llmSettingsPrefix + "advisorDifficulty"
	keyRedStrengthBlend   = llmSettingsPrefix + "redStrengthBlend"
	keyBlackStrengthBlend = llmSettingsPrefix + "blackStrengthBlend"
	keyRedSideType        = llmSettingsPrefix + "redSideType"
	keyBlackSideType      = llmSettingsPrefix + "blackSideType"
	keyHumanVsLlmOpponent = llmSettingsPrefix + "humanVsLlmOpponentType"
)

// clampLLMSettings 对已存在的 llm_settings_* 键原地 clamp（05 §8 范围表）；
// 缺省键不注入（electron-store 缺省是读取期虚拟值，不落盘也不进内存表）。
// 红黑强度缺省回落 strengthBlend 原始值（可能越界，再 clamp），与 TS fromRaw 一致。
func clampLLMSettings(data map[string]any) {
	strengthRaw := 50
	if v, ok := data[keyStrengthBlend]; ok {
		strengthRaw = numberOr(v, 50)
	}
	clamp := func(key string, dflt, lo, hi int) {
		if _, present := data[key]; present {
			data[key] = clampInt(numberOr(data[key], dflt), lo, hi)
		}
	}
	pick := func(key string, values, dflt int) {
		if _, present := data[key]; present {
			data[key] = pickEnum(data[key], values, dflt)
		}
	}
	clamp(keyTimeoutSeconds, 60, 5, 600)
	clamp(keyMaxAttempts, 3, 1, 10)
	pick(keyFallbackIndex, 2, 0)
	clamp(keyIntervalSeconds, 1, 0, 60)
	pick(keyAdvisorModeIndex, 3, 1)
	clamp(keyStrengthBlend, 50, 0, 100)
	clamp(keyAdvisorDifficulty, 5, 1, 5)
	if _, present := data[keyRedStrengthBlend]; present {
		data[keyRedStrengthBlend] = clampInt(numberOr(data[keyRedStrengthBlend], strengthRaw), 0, 100)
	}
	if _, present := data[keyBlackStrengthBlend]; present {
		data[keyBlackStrengthBlend] = clampInt(numberOr(data[keyBlackStrengthBlend], strengthRaw), 0, 100)
	}
	pick(keyRedSideType, 2, 0)
	pick(keyBlackSideType, 2, 0)
	pick(keyHumanVsLlmOpponent, 2, 0)
}

// numberOr 数值读取：有限数值 → 截断取整；否则默认值（TS intOr：Number.isFinite 判定）。
func numberOr(v any, dflt int) int {
	f, ok := v.(float64)
	if !ok || math.IsInf(f, 0) || math.IsNaN(f) {
		return dflt
	}
	return int(f)
}

// clampInt 数值越界 clamp（settings.ts clampInt）。
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// pickEnum 枚举 index 还原：非整数/越界一律回落默认（settings.ts pickEnum；
// JSON 数值恒为整数语义，非负整数且 < values 才有效）。
func pickEnum(v any, values, dflt int) int {
	f, ok := v.(float64)
	if !ok || f != math.Trunc(f) || f < 0 || int(f) >= values {
		return dflt
	}
	return int(f)
}
