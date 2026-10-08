package app

// 事件总线（design_docs/00 §3/§4）：后台 goroutine 的结果/进度/错误统一经此回
// UI 主循环（铁律 #G3：禁止后台 goroutine 直写 UI/state）。
//
// requestId 收口（铁律 #G5）：所有异步请求带 requestId；新局/悔棋/离页取消时
// Cancel(id)，迟到结果（含已入队未消费的事件）按 id 在 Drain 时丢弃。

import (
	"log"
	"sync"
	"time"
)

// cancelTTL 取消登记保留时长：迟到结果实际在毫秒级到站，超过 TTL 仍存留的
// 只可能是已消费的陈旧 id——语义不变（#G5），Cancel 时顺带清理防表无限增长
// （M7' 维护轮：原 cancelled 表只增不清，长会话缓慢泄漏）。
const cancelTTL = 5 * time.Minute

// AppEvent 事件总线条目；RequestID 为空 = 无关联事件（直通，不受 Cancel 影响）。
type AppEvent struct {
	RequestID string
	Err       error
	Payload   any
}

// EventBus 事件通道 + requestId 取消注册表。
// Emit/Cancel 可被任意 goroutine 调用；Drain 仅由主循环调用（单消费者）。
type EventBus struct {
	events    chan AppEvent
	mu        sync.Mutex
	cancelled map[string]time.Time
}

// NewEventBus 创建总线（buffer 为通道容量）。
func NewEventBus(buffer int) *EventBus {
	return &EventBus{events: make(chan AppEvent, buffer), cancelled: map[string]time.Time{}}
}

// Emit 提交事件（非阻塞：通道满时丢弃并记日志——后台 goroutine 禁止阻塞等待主循环）。
func (b *EventBus) Emit(ev AppEvent) {
	select {
	case b.events <- ev:
	default:
		log.Println("app: 事件通道满，事件被丢弃（requestId=", ev.RequestID, "）")
	}
}

// Cancel 取消请求：其迟到结果在 Drain 时按 id 丢弃（幂等）；顺带清理过期登记。
func (b *EventBus) Cancel(id string) {
	if id == "" {
		return
	}
	now := time.Now()
	b.mu.Lock()
	b.sweepLocked(now)
	b.cancelled[id] = now
	b.mu.Unlock()
}

// sweepLocked 清理超过 cancelTTL 的陈旧取消登记（调用方持锁）。
func (b *EventBus) sweepLocked(now time.Time) {
	for id, at := range b.cancelled {
		if now.Sub(at) > cancelTTL {
			delete(b.cancelled, id)
		}
	}
}

// Drain 主循环非阻塞取走全部未取消事件。
func (b *EventBus) Drain() []AppEvent {
	var out []AppEvent
	for {
		select {
		case ev := <-b.events:
			if ev.RequestID != "" {
				b.mu.Lock()
				_, dropped := b.cancelled[ev.RequestID]
				b.mu.Unlock()
				if dropped {
					continue // 迟到结果按 requestId 丢弃（#G5）
				}
			}
			out = append(out, ev)
		default:
			return out
		}
	}
}
