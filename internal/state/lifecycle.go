package state

// 生命周期事件分发注册表（翻译源 = 上游 frontend/src/stores/lifecycleRegistry.ts，
// 22 行；07 文档 §2 映射表的渲染侧收口）。
//
// 页面经 GameAutoSave.Attach 订阅、Dispose 注销；internal/app 把窗口失焦/最小化
// 相位广播给所有订阅者（对局页的自动保存）。close 相位不走本总线——由页面
// OnClose 同步保存承接（K16 有界等待，07 §2 落地口径）。
// 免锁：主 goroutine 独占（订阅发生在页面装配、Notify 发生在事件循环，07 §6.2）。

// LifecycleBus 生命周期订阅注册表（仅持"保存"回调，不持任何对局状态——上游同注）。
type LifecycleBus struct {
	listeners []func()
}

// Subscribe 注册监听，返回反注册函数（对应上游 subscribeLifecycle）。
func (b *LifecycleBus) Subscribe(fn func()) (unsubscribe func()) {
	b.listeners = append(b.listeners, fn)
	idx := len(b.listeners) - 1
	return func() {
		// 惰性删除：置 nil 保序（Dispose 后下次 Notify 跳过）
		b.listeners[idx] = nil
	}
}

// Notify 广播生命周期相位（对应上游 notifyLifecycle；blur/minimize 统一触发保存）。
func (b *LifecycleBus) Notify() {
	for _, l := range append([]func(){}, b.listeners...) {
		if l != nil {
			l()
		}
	}
}
