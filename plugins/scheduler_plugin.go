package plugins

import (
	"beishan/kernel"
	"fmt"
	"log"
	"sync"
	"time"
)

/* SchedulerPlugin 定时任务插件。

   到点触发 schedule_tick，由内核决定谁处理。
   不编排任何人，只发提醒。
*/
type SchedulerPlugin struct {
	kernel *kernel.Kernel
	timers map[string]*time.Ticker
	mu     sync.Mutex
}

func NewScheduler(k *kernel.Kernel) *SchedulerPlugin {
	return &SchedulerPlugin{
		kernel: k,
		timers: make(map[string]*time.Ticker),
	}
}

func (p *SchedulerPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "schedule_add":
		return kernel.Message{}, p.handleAdd(msg)
	case "schedule_remove":
		return kernel.Message{}, p.handleRemove(msg)
	case "schedule_list":
		p.handleList()
		return kernel.Message{}, nil
	default:
		return kernel.Message{}, fmt.Errorf("scheduler_plugin: 未知消息类型 %s", msg.Type)
	}
}

func (p *SchedulerPlugin) handleAdd(msg kernel.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	name := fmt.Sprintf("task_%d", len(p.timers)+1)
	ticker := time.NewTicker(10 * time.Second)
	p.timers[name] = ticker

	go func() {
		for range ticker.C {
			p.kernel.Send(kernel.Message{
				Sender:  "scheduler_plugin",
				Type:    "schedule_tick",
				Payload: []byte(fmt.Sprintf(`{"task":"%s"}`, name)),
			})
			log.Printf("[调度] 触发: %s", name)
		}
	}()

	log.Printf("[调度] 添加任务: %s", name)
	return nil
}

func (p *SchedulerPlugin) handleRemove(msg kernel.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	name := string(msg.Payload)
	ticker, ok := p.timers[name]
	if !ok {
		return fmt.Errorf("scheduler: 找不到任务 %s", name)
	}
	ticker.Stop()
	delete(p.timers, name)
	log.Printf("[调度] 移除任务: %s", name)
	return nil
}

func (p *SchedulerPlugin) handleList() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.timers) == 0 {
		fmt.Println("[调度] 无定时任务")
		return
	}
	fmt.Printf("[调度] 当前 %d 个任务:\n", len(p.timers))
	for name := range p.timers {
		fmt.Printf("  - %s\n", name)
	}
}
