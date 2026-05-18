package plugins

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"beishan/kernel"
)

type scheduledTask struct {
	Name     string
	Interval time.Duration
	Workflow string
	Ticker   *time.Ticker
}

type SchedulerPlugin struct {
	kernel  *kernel.Kernel
	mu      sync.Mutex
	timers  map[string]*scheduledTask
}

func NewScheduler(k *kernel.Kernel) *SchedulerPlugin {
	return &SchedulerPlugin{
		kernel: k,
		timers: make(map[string]*scheduledTask),
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
	var cfg struct {
		Interval string `json:"interval"`
		Workflow string `json:"workflow"`
		Name     string `json:"name,omitempty"`
	}
	if err := json.Unmarshal(msg.Payload, &cfg); err != nil {
		return fmt.Errorf("scheduler: 参数解析失败: %w", err)
	}
	if cfg.Interval == "" || cfg.Workflow == "" {
		return fmt.Errorf("scheduler: 需要 interval 和 workflow 参数")
	}

	dur, err := time.ParseDuration(cfg.Interval)
	if err != nil {
		return fmt.Errorf("scheduler: interval 格式错误 (如 24h, 30m): %w", err)
	}

	name := cfg.Name
	if name == "" {
		name = cfg.Workflow
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	task := &scheduledTask{
		Name:     name,
		Interval: dur,
		Workflow: cfg.Workflow,
		Ticker:   time.NewTicker(dur),
	}
	p.timers[name] = task

	go func(t *scheduledTask) {
		for range t.Ticker.C {
			payload, _ := json.Marshal(map[string]string{
				"workflow": t.Workflow,
			})
			p.kernel.Send(kernel.Message{
				Sender:    "scheduler_plugin",
				Recipient: "workflow_plugin",
				Type:      "workflow_run",
				Payload:   payload,
			})
			log.Printf("[调度] 触发工作流: %s (每 %s)", t.Workflow, t.Interval)
		}
	}(task)

	log.Printf("[调度] 添加定时任务: %s → 每 %s 执行 %s", name, dur, cfg.Workflow)
	return nil
}

func (p *SchedulerPlugin) handleRemove(msg kernel.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	name := string(msg.Payload)
	task, ok := p.timers[name]
	if !ok {
		return fmt.Errorf("scheduler: 找不到任务 %s", name)
	}
	task.Ticker.Stop()
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
	for _, t := range p.timers {
		fmt.Printf("  - %s → 每 %s 执行 %s\n", t.Name, t.Interval, t.Workflow)
	}
}
