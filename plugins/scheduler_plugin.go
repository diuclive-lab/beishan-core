package plugins

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"beishan/kernel"
)

type scheduledTask struct {
	Name     string
	Interval time.Duration
	CronExpr string
	Workflow string
	stop     chan struct{}
}

type SchedulerPlugin struct {
	kernel *kernel.Kernel
	mu     sync.Mutex
	timers map[string]*scheduledTask
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

type scheduleConfig struct {
	Interval string `json:"interval"`
	CronExpr string `json:"cron"`
	Workflow string `json:"workflow"`
	Name     string `json:"name,omitempty"`
}

func (p *SchedulerPlugin) handleAdd(msg kernel.Message) error {
	var cfg scheduleConfig
	if err := json.Unmarshal(msg.Payload, &cfg); err != nil {
		return fmt.Errorf("scheduler: 参数解析失败: %w", err)
	}
	if cfg.Workflow == "" {
		return fmt.Errorf("scheduler: 需要 workflow 参数")
	}
	if cfg.Interval == "" && cfg.CronExpr == "" {
		return fmt.Errorf("scheduler: 需要 interval 或 cron 参数")
	}
	if cfg.Interval != "" && cfg.CronExpr != "" {
		return fmt.Errorf("scheduler: interval 和 cron 不能同时设置")
	}

	name := cfg.Name
	if name == "" {
		name = cfg.Workflow
	}

	task := &scheduledTask{
		Name:     name,
		Workflow: cfg.Workflow,
		stop:     make(chan struct{}),
	}

	if cfg.Interval != "" {
		dur, err := time.ParseDuration(cfg.Interval)
		if err != nil {
			return fmt.Errorf("scheduler: interval 格式错误 (如 24h, 30m): %w", err)
		}
		task.Interval = dur
	}

	if cfg.CronExpr != "" {
		if _, err := parseCron(cfg.CronExpr); err != nil {
			return fmt.Errorf("scheduler: cron 格式错误: %w", err)
		}
		task.CronExpr = cfg.CronExpr
	}

	p.mu.Lock()
	p.timers[name] = task
	p.mu.Unlock()

	if task.CronExpr != "" {
		go p.runCron(task)
		log.Printf("[调度] 添加定时任务: %s → cron %s 执行 %s", name, task.CronExpr, cfg.Workflow)
	} else {
		go p.runInterval(task)
		log.Printf("[调度] 添加定时任务: %s → 每 %s 执行 %s", name, task.Interval, cfg.Workflow)
	}
	return nil
}

func (p *SchedulerPlugin) runInterval(task *scheduledTask) {
	ticker := time.NewTicker(task.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.triggerWorkflow(task)
		case <-task.stop:
			return
		}
	}
}

func (p *SchedulerPlugin) runCron(task *scheduledTask) {
	for {
		now := time.Now()
		next := cronNext(task.CronExpr, now)
		dur := next.Sub(now)

		log.Printf("[调度] cron %s → 下次触发: %s (还有 %s)", task.Name, next.Format("15:04:05"), dur.Round(time.Second))

		timer := time.NewTimer(dur)
		select {
		case <-timer.C:
			p.triggerWorkflow(task)
		case <-task.stop:
			timer.Stop()
			return
		}
	}
}

func (p *SchedulerPlugin) triggerWorkflow(task *scheduledTask) {
	payload, _ := json.Marshal(map[string]string{
		"workflow": task.Workflow,
	})
	p.kernel.Send(kernel.Message{
		Sender:    "scheduler_plugin",
		Recipient: "workflow_plugin",
		Type:      "workflow_run",
		Payload:   payload,
	})

	if task.CronExpr != "" {
		log.Printf("[调度] 触发工作流: %s (cron %s)", task.Workflow, task.CronExpr)
	} else {
		log.Printf("[调度] 触发工作流: %s (每 %s)", task.Workflow, task.Interval)
	}
}

func (p *SchedulerPlugin) handleRemove(msg kernel.Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	name := string(msg.Payload)
	task, ok := p.timers[name]
	if !ok {
		return fmt.Errorf("scheduler: 找不到任务 %s", name)
	}
	close(task.stop)
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
		if t.CronExpr != "" {
			next := cronNext(t.CronExpr, time.Now())
			fmt.Printf("  - %s → cron %s 执行 %s (下次 %s)\n", t.Name, t.CronExpr, t.Workflow, next.Format("01-02 15:04"))
		} else {
			fmt.Printf("  - %s → 每 %s 执行 %s\n", t.Name, t.Interval, t.Workflow)
		}
	}
}

// ─── 最小 cron 解析器 ──────────────────────────────────────

// cronField 表示标准 5 字段 cron 中的一个字段。
// 5 fields: minute(0-59) hour(0-23) dom(1-31) month(1-12) dow(0-6, 0=Sun)
type cronField struct {
	kind   string // "minute","hour","dom","month","dow"
	all    bool   // *
	values map[int]bool
	step   int
	stepOn bool // */N
}

func parseField(raw string, min, max int) (*cronField, error) {
	f := &cronField{values: make(map[int]bool)}
	raw = strings.TrimSpace(raw)

	if raw == "*" {
		f.all = true
		return f, nil
	}

	// */N
	if strings.HasPrefix(raw, "*/") {
		n, err := strconv.Atoi(raw[2:])
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("步长无效: %s", raw)
		}
		f.stepOn = true
		f.step = n
		return f, nil
	}

	// comma-separated and ranges
	parts := strings.Split(raw, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.Contains(p, "-") {
			// range N-M
			r := strings.SplitN(p, "-", 2)
			lo, err1 := strconv.Atoi(strings.TrimSpace(r[0]))
			hi, err2 := strconv.Atoi(strings.TrimSpace(r[1]))
			if err1 != nil || err2 != nil || lo > hi {
				return nil, fmt.Errorf("范围无效: %s", p)
			}
			for v := lo; v <= hi; v++ {
				if v >= min && v <= max {
					f.values[v] = true
				}
			}
		} else {
			v, err := strconv.Atoi(p)
			if err != nil || v < min || v > max {
				return nil, fmt.Errorf("值无效: %s (范围 %d-%d)", p, min, max)
			}
			f.values[v] = true
		}
	}
	return f, nil
}

type cronExpr struct {
	fields []*cronField // [minute, hour, dom, month, dow]
}

func parseCron(expr string) (*cronExpr, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron 需要 5 个字段，收到 %d 个: %s", len(parts), expr)
	}

	ranges := [][2]int{
		{0, 59}, // minute
		{0, 23}, // hour
		{1, 31}, // dom
		{1, 12}, // month
		{0, 6},  // dow
	}

	e := &cronExpr{}
	for i, p := range parts {
		f, err := parseField(p, ranges[i][0], ranges[i][1])
		if err != nil {
			return nil, fmt.Errorf("字段 %d 解析失败: %w", i, err)
		}
		e.fields = append(e.fields, f)
	}
	return e, nil
}

func (e *cronExpr) match(t time.Time) bool {
	vals := []int{t.Minute(), t.Hour(), t.Day(), int(t.Month()), int(t.Weekday())}
	for i, f := range e.fields {
		if f.all {
			continue
		}
		if f.stepOn {
			if vals[i]%f.step != 0 {
				return false
			}
			continue
		}
		if !f.values[vals[i]] {
			return false
		}
	}
	return true
}

// cronNext 计算从 after 之后的下一个 cron 匹配时间。
func cronNext(expr string, after time.Time) time.Time {
	e, err := parseCron(expr)
	if err != nil {
		// 已经在 handleAdd 验证过，不会到这里
		return after
	}

	// 从 after 的下一分钟开始查找
	t := after.Truncate(time.Minute).Add(time.Minute)

	// 最多查 366 天（跨越闰年），找不到说明表达式不合理
	deadline := t.AddDate(0, 0, 366)

	for t.Before(deadline) {
		if e.match(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return after
}
