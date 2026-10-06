package state

// 全局设置（翻译源 = 上游 frontend/src/stores/globalSettings.ts，38 行；
// 对应 global_settings.dart，07 文档 §3）。应用级设置（非对局状态），
// 允许单例（上游同口径，铁律 #G4 例外面）；对局状态仍必须走 NewGameStore 工厂。
// 持久化经复制物 storage.Settings（07 §5：结构化读写收口，键 global_auto_save）。

import (
	"log"

	"github.com/jxsword/chinese_chess_go_gio/internal/storage"
)

// SettingKeyGlobalUIScale 全局界面缩放键（Gio 版新增，08 §9；复制物
// storage 常量不动——#G2）。值为相对系统缩放的乘数（1.0=跟随系统）。
const SettingKeyGlobalUIScale = "global_ui_scale"

// UIScaleOptions 设置弹窗的缩放档位（相对系统检测值）。
var UIScaleOptions = []float64{1.0, 1.25, 1.5, 1.75, 2.0}

// GlobalSettings 全局设置（上游 useGlobalSettings Zustand 单件的 Go 形态）。
type GlobalSettings struct {
	// AutoSave 离开棋盘/应用切后台时自动保存当前棋局。默认开启（07 §3）。
	AutoSave bool
	// UIScale 界面缩放乘数（1.0=跟随系统检测值；0.10~3.0 clamp）。
	// Gio 版新增（08 §9）：高分屏 Xft.dpi 缺失/仍偏小的场景由用户自调。
	UIScale float64
	// Loaded 是否完成首次加载（设置弹窗每次打开前重新 load，08 §6）。
	Loaded bool

	st *storage.Settings // nil = 存储不可用（读失败按默认值处理）
}

// NewGlobalSettings 创建全局设置（st 为存储实例；可为 nil——存储降级路径）。
func NewGlobalSettings(st *storage.Settings) *GlobalSettings {
	return &GlobalSettings{AutoSave: true, UIScale: 1.0, st: st}
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
	if g.st != nil {
		if f, ok := g.st.Get(SettingKeyGlobalUIScale).(float64); ok && f >= 1.0 && f <= 3.0 {
			g.UIScale = f
		}
	}
	g.Loaded = true
}

// SetUIScale 先改内存再落盘（同 SetAutoSave 语义；clamp 1.0~3.0）。
func (g *GlobalSettings) SetUIScale(value float64) {
	if value < 1.0 {
		value = 1.0
	}
	if value > 3.0 {
		value = 3.0
	}
	g.UIScale = value
	if g.st == nil {
		return
	}
	if err := g.st.Set(SettingKeyGlobalUIScale, value); err != nil {
		log.Println("state: 界面缩放保存失败（保留内存值）:", err)
	}
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
