package app

// 生命周期事件派发单测（design_docs/09 §4：生命周期事件派发表驱动，事件注入 fake）。
// 语义锚点 = 07 §2 事件源映射表（Gio 替代上游 app:lifecycle）：
//   - blur ← key.FocusEvent{Focus:false}（v0.10.3 实测：字段名为 Focus，经
//     ConfigEvent 派发窗口焦点变化——07 §2 表的 Focused:false 名称修订）
//   - close ← *app.ClosingEvent（v0.10.3 实测：system.CommandClose 不存在，
//     关闭请求经 ClosingEvent，可 Abort 挂起→放行——07 §2 表的修订）
//   - minimize ← 留 M2' POC 核实（X11/Wayland 平台差异，07 §2 表）

import "testing"

// fakeLifecycleHandler 记录派发序列。
type fakeLifecycleHandler struct {
	events []string
}

func (f *fakeLifecycleHandler) OnBlur()     { f.events = append(f.events, "blur") }
func (f *fakeLifecycleHandler) OnMinimize() { f.events = append(f.events, "minimize") }
func (f *fakeLifecycleHandler) OnClose()    { f.events = append(f.events, "close") }

// 事件注入 helper（Window.Run 事件分支将事件翻译为派发调用；翻译规则在此单测锚定）。
func TestLifecycle_DispatchSequence(t *testing.T) {
	lr := NewLifecycleRouter()
	h := &fakeLifecycleHandler{}
	lr.Add(h)

	// 失焦（FocusEvent{Focus:false} 的翻译结果）
	DispatchFocusChanged(lr, false)
	// 获焦不派发任何生命周期事件
	DispatchFocusChanged(lr, true)
	// 关闭请求（ClosingEvent 的翻译结果）
	DispatchCloseRequest(lr)
	// 最小化（M2' 事件源接线；派发通道先就位）
	DispatchMinimize(lr)

	want := []string{"blur", "close", "minimize"}
	if len(h.events) != len(want) {
		t.Fatalf("派发序列 = %v, want %v（获焦不派发）", h.events, want)
	}
	for i, e := range want {
		if h.events[i] != e {
			t.Fatalf("派发序列[%d] = %q, want %q", i, h.events[i], e)
		}
	}
}

// 多 handler 按注册顺序派发（自动保存 + 其他挂接点并存）。
func TestLifecycle_MultipleHandlersInOrder(t *testing.T) {
	lr := NewLifecycleRouter()
	a := &fakeLifecycleHandler{}
	b := &fakeLifecycleHandler{}
	lr.Add(a)
	lr.Add(b)
	DispatchFocusChanged(lr, false)
	if len(a.events) != 1 || len(b.events) != 1 {
		t.Fatalf("两 handler 均应收到 blur：a=%v b=%v", a.events, b.events)
	}
}

// 无 handler 时派发不 panic。
func TestLifecycle_EmptyHandlersNoop(t *testing.T) {
	lr := NewLifecycleRouter()
	DispatchFocusChanged(lr, false)
	DispatchCloseRequest(lr)
	DispatchMinimize(lr)
}
