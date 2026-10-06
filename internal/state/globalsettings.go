package state

// 全局设置（翻译源 = 上游 frontend/src/stores/globalSettings.ts，38 行；
// 对应 global_settings.dart，07 文档 §3）。应用级设置（非对局状态），
// 允许单例（上游同口径，铁律 #G4 例外面）；对局状态仍必须走 NewGameStore 工厂。
// 持久化经复制物 storage.Settings（07 §5：结构化读写收口，键 global_auto_save）。

import "github.com/jxsword/chinese_chess_go_gio/internal/storage"

// GlobalSettings 全局设置（上游 useGlobalSettings Zustand 单件的 Go 形态）。
type GlobalSettings struct {
	// AutoSave 离开棋盘/应用切后台时自动保存当前棋局。默认开启（07 §3）。
	AutoSave bool
	// Loaded 是否完成首次加载（设置弹窗每次打开前重新 load，08 §6）。
	Loaded bool

	st *storage.Settings // nil = 存储不可用（读失败按默认值处理）
}

// NewGlobalSettings 创建全局设置（st 为存储实例；可为 nil——存储降级路径）。
func NewGlobalSettings(st *storage.Settings) *GlobalSettings {
	return &GlobalSettings{AutoSave: true, st: st}
}

// Load 读取持久化值（global_settings.dart:22-27：读失败/缺省按默认值开启）。
func (g *GlobalSettings) Load() {
	value := true
	if g.st != nil {
		if stored, ok := g.st.Get(storage.SettingKeyGlobalAutoSave).(bool); ok {
			value = stored
		}
	}
	g.AutoSave = value
	g.Loaded = true
}

// SetAutoSave 先改内存再落盘（global_settings.dart:30-35：写失败保留内存值，
// 下次进入设置弹窗会重新 load；发即忘不阻塞交互）。
func (g *GlobalSettings) SetAutoSave(value bool) {
	g.AutoSave = value
	if g.st == nil {
		return
	}
	_ = g.st.Set(storage.SettingKeyGlobalAutoSave, value)
}
