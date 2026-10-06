package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

func TestGlobalSettingsUIScalePersist(t *testing.T) {
	dir := t.TempDir()
	st, err := storage.OpenSettings(dir)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGlobalSettings(st)
	g.Load()
	if g.UIScale != 1.0 {
		t.Fatalf("default = %v", g.UIScale)
	}
	g.SetUIScale(1.5)
	if g.UIScale != 1.5 {
		t.Fatalf("memory = %v", g.UIScale)
	}
	// 重新打开读回
	st2, _ := storage.OpenSettings(dir)
	g2 := NewGlobalSettings(st2)
	g2.Load()
	if g2.UIScale != 1.5 {
		t.Fatalf("reload = %v, want 1.5", g2.UIScale)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	t.Logf("settings.json: %s", raw)
}
