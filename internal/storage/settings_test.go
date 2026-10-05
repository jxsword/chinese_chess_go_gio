package storage

import (
	"os"
	"path/filepath"
	"testing"
)

// 等价集：Electron 版 test/main/settings.spec.ts（T2.2 验收，07 文档 §3/§5）。
// electron-store 语义：缺省 global_auto_save=true；set/get 往返；实例间持久化。

func openTestSettings(t *testing.T) *Settings {
	t.Helper()
	s, err := OpenSettings(t.TempDir())
	if err != nil {
		t.Fatalf("OpenSettings: %v", err)
	}
	return s
}

func TestSettingsGlobalAutoSaveDefaultsTrue(t *testing.T) {
	s := openTestSettings(t)
	if s.Get(SettingKeyGlobalAutoSave) != true {
		t.Fatalf("global_auto_save 默认 = %v, want true", s.Get(SettingKeyGlobalAutoSave))
	}
}

func TestSettingsSetGetRoundTrip(t *testing.T) {
	s := openTestSettings(t)
	if s.Get("corpus.userPath") != nil {
		t.Fatalf("缺键应返回 nil，got %v", s.Get("corpus.userPath"))
	}
	if err := s.Set(SettingKeyGlobalAutoSave, false); err != nil {
		t.Fatalf("set: %v", err)
	}
	if s.Get(SettingKeyGlobalAutoSave) != false {
		t.Fatalf("global_auto_save = %v, want false", s.Get(SettingKeyGlobalAutoSave))
	}
	if err := s.Set("corpus.userPath", "/tmp/corpus"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if s.Get("corpus.userPath") != "/tmp/corpus" {
		t.Fatalf("corpus.userPath = %v", s.Get("corpus.userPath"))
	}
}

func TestSettingsPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	first, err := OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Set(SettingKeyGlobalAutoSave, false); err != nil {
		t.Fatal(err)
	}
	second, err := OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second.Get(SettingKeyGlobalAutoSave) != false {
		t.Fatalf("重开实例 global_auto_save = %v, want false（已存值不被缺省覆盖）", second.Get(SettingKeyGlobalAutoSave))
	}
}

func TestSettingsFileShape(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(SettingKeyGlobalAutoSave, false); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, settingsFilename))
	if err != nil {
		t.Fatal(err)
	}
	// 扁平 JSON 对象 {key: value}（electron-store 等价形状）
	if string(raw) != `{"global_auto_save":false}` {
		t.Fatalf("settings.json = %s", raw)
	}
}

// clamp（05 §8 范围表；llmSettingsFromRaw 语义）：越界回落/夹取在 load 时生效。
func TestSettingsClampOnLoad(t *testing.T) {
	dir := t.TempDir()
	// 手工写入越界配置（模拟旧版本/手改文件）
	bad := `{
		"llm_settings_timeoutSeconds": 0,
		"llm_settings_maxAttempts": 99,
		"llm_settings_fallbackIndex": 5,
		"llm_settings_intervalSeconds": -3,
		"llm_settings_advisorModeIndex": 9,
		"llm_settings_strengthBlend": 200,
		"llm_settings_advisorDifficulty": 0,
		"llm_settings_redSideType": 3
	}`
	if err := os.WriteFile(filepath.Join(dir, settingsFilename), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]int{
		keyTimeoutSeconds:    5,   // 0 → clamp 下界
		keyMaxAttempts:       10,  // 99 → clamp 上界
		keyFallbackIndex:     0,   // 5 越界 → 默认 builtinAi
		keyIntervalSeconds:   0,   // -3 → clamp 下界
		keyAdvisorModeIndex:  1,   // 9 越界 → 默认 candidate
		keyStrengthBlend:     100, // 200 → clamp 上界
		keyAdvisorDifficulty: 1,   // 0 → clamp 下界
		keyRedSideType:       0,   // 3 越界 → 默认 llm
	}
	for key, want := range cases {
		if got := s.Get(key); got != want {
			t.Fatalf("%s = %v, want %v", key, got, want)
		}
	}
}

// 红黑强度缺省回落 strengthBlend 原始值再 clamp（与 TS fromRaw 一致；
// 缺省键不注入——electron-store 虚拟缺省语义，Get 返回 nil 由渲染层 fromRaw 兜底）。
func TestSettingsStrengthBlendFallbackDefault(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, settingsFilename),
		[]byte(`{"llm_settings_strengthBlend": 200, "llm_settings_redStrengthBlend": 300, "llm_settings_timeoutSeconds": "abc"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get(keyRedStrengthBlend); got != 100 {
		t.Fatalf("redStrengthBlend = %v, want 100（越界 clamp）", got)
	}
	if got := s.Get(keyBlackStrengthBlend); got != nil {
		t.Fatalf("blackStrengthBlend = %v, want nil（缺省键不注入）", got)
	}
	if got := s.Get(keyStrengthBlend); got != 100 {
		t.Fatalf("strengthBlend = %v, want 100", got)
	}
	if got := s.Get(keyTimeoutSeconds); got != 60 {
		t.Fatalf("timeoutSeconds = %v, want 60（非数值回落该键默认）", got)
	}

	dir2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir2, settingsFilename),
		[]byte(`{"llm_settings_strengthBlend": 30, "llm_settings_blackStrengthBlend": 7.9}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s2, err := OpenSettings(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if got := s2.Get(keyStrengthBlend); got != 30 {
		t.Fatalf("strengthBlend = %v, want 30（范围内不动）", got)
	}
	if got := s2.Get(keyBlackStrengthBlend); got != 7 {
		t.Fatalf("blackStrengthBlend = %v, want 7（范围内数值截断取整）", got)
	}
}

// 损坏 settings.json 视为空设置（后续写入重建）。
func TestSettingsCorruptFileResets(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, settingsFilename), []byte("{corrupt"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := OpenSettings(dir)
	if err != nil {
		t.Fatalf("损坏文件不应报错: %v", err)
	}
	if s.Get(SettingKeyGlobalAutoSave) != true {
		t.Fatal("损坏文件后应按缺省处理")
	}
	if err := s.Set(SettingKeyGlobalAutoSave, false); err != nil {
		t.Fatalf("损坏后写入重建: %v", err)
	}
	reopened, err := OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Get(SettingKeyGlobalAutoSave) != false {
		t.Fatal("重建后应读到新值")
	}
}
