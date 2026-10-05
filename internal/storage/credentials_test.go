package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// 等价集：Electron 版 test/main/credentials.spec.ts（T2.2 验收，07 文档 §4）：
// 掩码不泄 Key；读失败按未配置；keyring 不可用 → 明文回退 0600（electron-DR-011）；
// 掩码 Key 回写合并（electron-DR-013）。keyring 以伪实现注入（fakeCryptor 同位）。

var testConfig = SlotConfig{
	BaseURL: "https://open.bigmodel.cn/api/paas/v4",
	APIKey:  "sk-secret-1234567890abcd",
	Model:   "glm-4-flash",
	Preset:  "智谱",
}

// fakeKeyring 伪 keyring：broken=true 模拟系统安全存储不可用（Set/Get/Delete 报错）；
// corrupt 秘密用于模拟密文损坏。
type fakeKeyring struct {
	broken bool
	store  map[string]string
}

func newFakeKeyring(broken bool) *fakeKeyring {
	return &fakeKeyring{broken: broken, store: map[string]string{}}
}

func (f *fakeKeyring) Get(service, user string) (string, error) {
	if f.broken {
		return "", errKeyringUnavailable
	}
	v, ok := f.store[service+"/"+user]
	if !ok {
		return "", errKeyringNotFound
	}
	return v, nil
}

func (f *fakeKeyring) Set(service, user, secret string) error {
	if f.broken {
		return errKeyringUnavailable
	}
	f.store[service+"/"+user] = secret
	return nil
}

func (f *fakeKeyring) Delete(service, user string) error {
	if f.broken {
		return errKeyringUnavailable
	}
	delete(f.store, service+"/"+user)
	return nil
}

// 测试用错误（与 zalando/go-keyring 的错误类型同位：找不到 vs 不可用）
var (
	errKeyringNotFound    = errors.New("secret not found")
	errKeyringUnavailable = errors.New("keyring unavailable")
)

func newTestCredentials(t *testing.T, kr Keyring) (*Credentials, string) {
	t.Helper()
	dir := t.TempDir()
	fallback := filepath.Join(dir, CredentialsFallbackFilename)
	return NewCredentials(kr, fallback), fallback
}

func assertMaskedGet(t *testing.T, got *SlotConfig) {
	t.Helper()
	if got == nil {
		t.Fatal("get = nil, want config")
	}
	if got.BaseURL != testConfig.BaseURL || got.Model != testConfig.Model {
		t.Fatalf("got = %+v", got)
	}
	if got.APIKey != "****abcd" {
		t.Fatalf("apiKey = %q, want 掩码 ****abcd", got.APIKey)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), testConfig.APIKey) {
		t.Fatalf("掩码回读泄漏完整 Key：%s", raw)
	}
}

func TestMaskApiKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"abc", "****"},
		{"abcd", "****"},
		{"abcde", "****bcde"},
		{testConfig.APIKey, "****abcd"},
	}
	for _, c := range cases {
		if got := MaskApiKey(c.in); got != c.want {
			t.Fatalf("MaskApiKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCredentialsSetThenGetMasksKey(t *testing.T) {
	svc, _ := newTestCredentials(t, newFakeKeyring(false))
	if _, err := svc.Set(SlotRed, testConfig); err != nil {
		t.Fatalf("set: %v", err)
	}
	assertMaskedGet(t, svc.Get(SlotRed))
}

func TestCredentialsEncryptedSetLeavesNoPlaintextFile(t *testing.T) {
	svc, fallback := newTestCredentials(t, newFakeKeyring(false))
	if _, err := svc.Set(SlotBlack, testConfig); err != nil {
		t.Fatalf("set: %v", err)
	}
	// keyring 命中时不得落任何明文回退文件（等价 Electron"落盘文件不含明文 Key"，
	// Go 语义更强：主存加密则根本无明文文件）
	if _, err := os.Stat(fallback); !os.IsNotExist(err) {
		t.Fatalf("keyring 可用时不应产生回退文件: %v", err)
	}
}

func TestCredentialsMissingSlotIsUnconfigured(t *testing.T) {
	svc, _ := newTestCredentials(t, newFakeKeyring(false))
	if svc.Get(SlotRed) != nil {
		t.Fatal("空存储应返回 nil")
	}
	if _, err := svc.Set(SlotBlack, testConfig); err != nil {
		t.Fatal(err)
	}
	if svc.Get(SlotRed) != nil {
		t.Fatal("未写入槽位应返回 nil")
	}
	if svc.Get(SlotBlack) == nil {
		t.Fatal("已写入槽位不应返回 nil")
	}
}

func TestCredentialsCorruptSecretIsUnconfigured(t *testing.T) {
	kr := newFakeKeyring(false)
	svc, _ := newTestCredentials(t, kr)
	if _, err := svc.Set(SlotAssistant, testConfig); err != nil {
		t.Fatal(err)
	}
	// 模拟密文损坏（解密/解析失败按未配置——对齐原版"读失败按未配置"）
	kr.store[KeyringServiceName+"/"+SlotAssistant] = "@@@not-a-slot-json@@@"
	if svc.Get(SlotAssistant) != nil {
		t.Fatal("损坏密文应按未配置")
	}
}

func TestCredentialsCorruptFallbackFileIsUnconfigured(t *testing.T) {
	svc, fallback := newTestCredentials(t, newFakeKeyring(true))
	if err := os.WriteFile(fallback, []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	if svc.Get(SlotRed) != nil {
		t.Fatal("回退文件损坏应按未配置（不抛错）")
	}
	// 损坏后写入重建
	res, err := svc.Set(SlotRed, testConfig)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if res.Stored != storedPlainFallback {
		t.Fatalf("stored = %s, want plainFallback", res.Stored)
	}
	assertMaskedGet(t, svc.Get(SlotRed))
}

func TestCredentialsShortAndEmptyKeys(t *testing.T) {
	svc, _ := newTestCredentials(t, newFakeKeyring(false))
	if _, err := svc.Set(SlotRed, SlotConfig{BaseURL: testConfig.BaseURL, Model: testConfig.Model, APIKey: "abc"}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Get(SlotRed).APIKey; got != "****" {
		t.Fatalf("短 Key 掩码 = %q, want ****", got)
	}
	if _, err := svc.Set(SlotRed, SlotConfig{BaseURL: testConfig.BaseURL, Model: testConfig.Model, APIKey: ""}); err != nil {
		t.Fatal(err)
	}
	if got := svc.Get(SlotRed).APIKey; got != "" {
		t.Fatalf("空 Key 掩码 = %q, want 空串", got)
	}
}

func TestCredentialsDeleteSlot(t *testing.T) {
	svc, _ := newTestCredentials(t, newFakeKeyring(false))
	if _, err := svc.Set(SlotRed, testConfig); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(SlotBlack, testConfig); err != nil {
		t.Fatal(err)
	}
	if err := svc.Delete(SlotRed); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if svc.Get(SlotRed) != nil {
		t.Fatal("删除后应未配置")
	}
	if svc.Get(SlotBlack) == nil {
		t.Fatal("其余槽位不受影响")
	}
	if err := svc.Delete(SlotRed); err != nil { // 重复 delete 幂等
		t.Fatalf("重复 delete: %v", err)
	}
}

func TestCredentialsPlainFallbackWhenKeyringUnavailable(t *testing.T) {
	svc, fallback := newTestCredentials(t, newFakeKeyring(true))
	res, err := svc.Set(SlotRed, testConfig)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if res.Stored != storedPlainFallback {
		t.Fatalf("stored = %s, want plainFallback（DR-011）", res.Stored)
	}
	// 回退文件路径与内容（明文本就是回退的目的，含完整 Key）
	raw, err := os.ReadFile(fallback)
	if err != nil {
		t.Fatalf("read fallback: %v", err)
	}
	if !strings.Contains(string(raw), testConfig.APIKey) {
		t.Fatal("回退文件应含完整 Key（明文回退的目的）")
	}
	// 权限 0600（仅当前用户可读）。Windows 无 POSIX 权限位（Chmod 仅置只读位，
	// Stat 恒报 0666，"仅当前用户可读"由 NTFS ACL 承载）——非 Windows 断言。
	if runtime.GOOS != "windows" {
		info, err := os.Stat(fallback)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("回退文件权限 = %v, want 0600", info.Mode().Perm())
		}
	}
	// 渲染层仍只见掩码
	assertMaskedGet(t, svc.Get(SlotRed))
	// getRaw（后端注入鉴权用）拿到完整 Key
	if got := svc.GetRaw(SlotRed); got == nil || got.APIKey != testConfig.APIKey {
		t.Fatalf("getRaw apiKey = %v", got)
	}
}

func TestCredentialsKeyringPriorityOverFallback(t *testing.T) {
	fallbackDir := t.TempDir()
	fallback := filepath.Join(fallbackDir, CredentialsFallbackFilename)
	kr := newFakeKeyring(true) // 安全存储不可用
	// 先明文保存
	plainSvc := NewCredentials(kr, fallback)
	plainCfg := testConfig
	plainCfg.Model = "plain-model"
	if res, err := plainSvc.Set(SlotRed, plainCfg); err != nil || res.Stored != storedPlainFallback {
		t.Fatalf("plain set: %+v, %v", res, err)
	}
	// 再加密保存同槽位（安全存储恢复可用：同一 keyring 实例翻转状态）
	kr.broken = false
	encSvc := NewCredentials(kr, fallback)
	encCfg := testConfig
	encCfg.Model = "enc-model"
	if res, err := encSvc.Set(SlotRed, encCfg); err != nil || res.Stored != storedEncrypted {
		t.Fatalf("enc set: %+v, %v", res, err)
	}
	// 读取以 keyring 为准
	reader := NewCredentials(kr, fallback)
	if got := reader.Get(SlotRed); got == nil || got.Model != "enc-model" {
		t.Fatalf("get model = %+v, want enc-model", got)
	}
	// delete 清理双存储
	if err := reader.Delete(SlotRed); err != nil {
		t.Fatal(err)
	}
	if reader.Get(SlotRed) != nil {
		t.Fatal("delete 后应未配置")
	}
	// 明文回退路径同样贯通（black 槽位：keyring 再次不可用 → 回退文件）
	kr.broken = true
	if res, err := plainSvc.Set(SlotBlack, testConfig); err != nil || res.Stored != storedPlainFallback {
		t.Fatalf("plain set black: %+v, %v", res, err)
	}
	kr.broken = false
	if got := reader.Get(SlotBlack); got == nil || got.Model != testConfig.Model {
		t.Fatalf("get black = %+v", got)
	}
	if err := reader.Delete(SlotBlack); err != nil {
		t.Fatal(err)
	}
	if reader.Get(SlotBlack) != nil {
		t.Fatal("delete black 后应未配置")
	}
}

func TestCredentialsMaskedKeyMerge(t *testing.T) {
	svc, _ := newTestCredentials(t, newFakeKeyring(false))
	if _, err := svc.Set(SlotRed, testConfig); err != nil {
		t.Fatal(err)
	}
	// 模拟页面回读掩码后整配置回写（防抖保存/卸载回写路径）
	maskedReadback := svc.Get(SlotRed)
	if maskedReadback.APIKey != "****abcd" {
		t.Fatalf("回读掩码 = %q", maskedReadback.APIKey)
	}
	if _, err := svc.Set(SlotRed, SlotConfig{BaseURL: maskedReadback.BaseURL, APIKey: maskedReadback.APIKey, Model: "renamed-model", Preset: maskedReadback.Preset}); err != nil {
		t.Fatal(err)
	}
	// 真实 Key 保留，其余字段已更新
	if got := svc.GetRaw(SlotRed); got.APIKey != testConfig.APIKey {
		t.Fatalf("真实 Key 被掩码覆盖: %q", got.APIKey)
	}
	if got := svc.Get(SlotRed); got.Model != "renamed-model" {
		t.Fatalf("model = %q, want renamed-model", got.Model)
	}
	// 明文回退路径同样合并
	plainSvc, _ := newTestCredentials(t, newFakeKeyring(true))
	if _, err := plainSvc.Set(SlotRed, testConfig); err != nil {
		t.Fatal(err)
	}
	writeBack := SlotConfig{BaseURL: maskedReadback.BaseURL, APIKey: maskedReadback.APIKey, Model: "again", Preset: maskedReadback.Preset}
	if _, err := plainSvc.Set(SlotRed, writeBack); err != nil {
		t.Fatal(err)
	}
	if got := plainSvc.GetRaw(SlotRed); got.APIKey != testConfig.APIKey {
		t.Fatalf("明文路径真实 Key 被覆盖: %q", got.APIKey)
	}
}

func TestCredentialsMaskedKeyWithoutExisting(t *testing.T) {
	svc, _ := newTestCredentials(t, newFakeKeyring(false))
	// 掩码 Key 但存储中无原 Key → 按空 Key 处理（不落掩码字符串）
	noKey := testConfig
	noKey.APIKey = "****abcd"
	if _, err := svc.Set(SlotRed, noKey); err != nil {
		t.Fatal(err)
	}
	if got := svc.GetRaw(SlotRed); got.APIKey != "" {
		t.Fatalf("apiKey = %q, want 空串", got.APIKey)
	}
}

func TestCredentialsFallbackPlainReadKeyringPriority(t *testing.T) {
	// keyring 不可用时读取回退文件；keyring 恢复后回读优先 keyring（不存在 → 回退文件兜底）
	fallbackDir := t.TempDir()
	fallback := filepath.Join(fallbackDir, CredentialsFallbackFilename)
	writer := NewCredentials(newFakeKeyring(true), fallback)
	if _, err := writer.Set(SlotAssistant, testConfig); err != nil {
		t.Fatal(err)
	}
	reader := NewCredentials(newFakeKeyring(false), fallback) // keyring 可用但槽位空
	assertMaskedGet(t, reader.Get(SlotAssistant))
}

// 并发写入不同槽位：回退文件读改写互斥（-race 覆盖；Wails 绑定方法并发进入）。
func TestCredentialsConcurrentSet(t *testing.T) {
	svc, _ := newTestCredentials(t, newFakeKeyring(true))
	var wg sync.WaitGroup
	for i, slot := range []string{SlotRed, SlotBlack, SlotAssistant} {
		wg.Add(1)
		go func(slot string, i int) {
			defer wg.Done()
			cfg := testConfig
			cfg.Model = cfg.Model + string(rune('0'+i))
			if _, err := svc.Set(slot, cfg); err != nil {
				t.Errorf("set %s: %v", slot, err)
			}
		}(slot, i)
	}
	wg.Wait()
	for _, slot := range []string{SlotRed, SlotBlack, SlotAssistant} {
		if svc.Get(slot) == nil {
			t.Fatalf("并发写入后 %s 应可读", slot)
		}
	}
}
