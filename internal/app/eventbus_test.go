package app

// 事件总线单测（design_docs/09 §4：纯 Go 表驱动，不渲染）。
// 语义锚点 = 00 §3：后台 goroutine 经事件通道回主循环；requestId 取消与迟到结果
// 丢弃（铁律 #G5，上游 00 §3.3 规范照搬）；通道满非阻塞丢弃。

import "testing"

// Emit→Drain 基本往返：事件按序投递一次。
func TestEventBus_DrainDeliversOnceInOrder(t *testing.T) {
	bus := NewEventBus(16)
	bus.Emit(AppEvent{RequestID: "r1", Payload: "a"})
	bus.Emit(AppEvent{RequestID: "r2", Payload: "b"})
	got := bus.Drain()
	if len(got) != 2 || got[0].RequestID != "r1" || got[1].RequestID != "r2" {
		t.Fatalf("drain = %+v, want [r1 r2] 按序一次", got)
	}
	if again := bus.Drain(); len(again) != 0 {
		t.Fatalf("二次 drain = %+v, want 空", again)
	}
}

// 迟到结果按 requestId 丢弃（#G5）：Cancel 后再 Emit → 不投递。
func TestEventBus_CancelDropsLateEvents(t *testing.T) {
	bus := NewEventBus(16)
	bus.Cancel("gone")
	bus.Emit(AppEvent{RequestID: "gone", Payload: "late"})
	bus.Emit(AppEvent{RequestID: "alive", Payload: "fresh"})
	bus.Emit(AppEvent{Payload: "no-request-id"})
	got := bus.Drain()
	if len(got) != 2 || got[0].Payload != "fresh" || got[1].Payload != "no-request-id" {
		t.Fatalf("drain = %+v, want 仅 alive 与无 requestId 两事件", got)
	}
}

// Cancel 前已入队未消费的事件同属迟到结果 → drain 时丢弃。
func TestEventBus_CancelDropsQueuedEvents(t *testing.T) {
	bus := NewEventBus(16)
	bus.Emit(AppEvent{RequestID: "r1", Payload: "queued"})
	bus.Cancel("r1")
	if got := bus.Drain(); len(got) != 0 {
		t.Fatalf("drain = %+v, want 空（已取消请求的在队事件被丢弃）", got)
	}
}

// 空 RequestID 直通（无关联语义，不受任何 Cancel 影响）。
func TestEventBus_EmptyRequestIDAlwaysDelivered(t *testing.T) {
	bus := NewEventBus(16)
	bus.Cancel("")
	bus.Emit(AppEvent{Payload: "system"})
	bus.Emit(AppEvent{RequestID: "", Payload: "lifecycle"})
	got := bus.Drain()
	if len(got) != 2 {
		t.Fatalf("drain = %+v, want 2（空 RequestID 直通）", got)
	}
}

// 通道满：Emit 非阻塞、多余事件丢弃并记日志（后台 goroutine 禁止阻塞等待主循环）。
func TestEventBus_EmitNeverBlocksWhenFull(t *testing.T) {
	bus := NewEventBus(4)
	for i := 0; i < 32; i++ {
		bus.Emit(AppEvent{RequestID: "bulk", Payload: i})
	}
	if got := bus.Drain(); len(got) != 4 {
		t.Fatalf("drain = %d 事件, want 4（容量上限，超出丢弃）", len(got))
	}
}

// Cancel 覆盖多个 requestId；重复 Cancel 幂等。
func TestEventBus_CancelMultipleAndIdempotent(t *testing.T) {
	bus := NewEventBus(16)
	bus.Cancel("a")
	bus.Cancel("a")
	bus.Cancel("b")
	bus.Emit(AppEvent{RequestID: "a"})
	bus.Emit(AppEvent{RequestID: "b"})
	if got := bus.Drain(); len(got) != 0 {
		t.Fatalf("drain = %+v, want 空", got)
	}
}
