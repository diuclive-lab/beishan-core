package observatory

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// Pulse is a lightweight system health snapshot.
type Pulse struct {
	Timestamp     time.Time `json:"timestamp"`
	Provider      string    `json:"provider"`       // "healthy", "degraded", "down"
	ToolCount     int       `json:"tool_count"`
	KnowledgeSize int       `json:"knowledge_size"` // stored entries
	TraceCount    int              `json:"trace_count"`
	Failure       FailureTaxonomy  `json:"failure,omitempty"`    // decision traces recorded
	LastError     string    `json:"last_error,omitempty"`
	UptimeHours   float64   `json:"uptime_hours"`
}

func Check(providerOK bool, toolCount, knowledgeSize, traceCount int, lastErr string, uptime time.Duration, metrics ...map[string]float64) Pulse {
	var ft FailureTaxonomy
	var m map[string]float64
	if len(metrics) > 0 {
		m = metrics[0]
		ft = Classify(m)
	}
	provider := "healthy"
	if !providerOK {
		provider = "down"
	}
	if m != nil {
		if v, ok := m["api_reachable"]; ok && v == 0 {
			provider = "degraded"
		}
		if v, ok := m["local_model_available"]; ok && v > 0 && !providerOK {
			provider = "degraded (local fallback)"
		}
	}
	return Pulse{
		Timestamp:     time.Now().UTC(),
		Provider:      provider,
		ToolCount:     toolCount,
		KnowledgeSize: knowledgeSize,
		TraceCount:    traceCount,
		LastError:     lastErr,
		UptimeHours:   uptime.Hours(),
		Failure:       ft,
	}
}

func (p Pulse) Markdown() string {
	var b strings.Builder
	b.WriteString("# System Pulse\n\n")
	b.WriteString(fmt.Sprintf("- **Provider:** %s\n", p.Provider))
	b.WriteString(fmt.Sprintf("- **Tools:** %d\n", p.ToolCount))
	b.WriteString(fmt.Sprintf("- **Knowledge base:** %d entries\n", p.KnowledgeSize))
	b.WriteString(fmt.Sprintf("- **Traces recorded:** %d\n", p.TraceCount))
	b.WriteString(fmt.Sprintf("- **Uptime:** %.1f hours\n", p.UptimeHours))
	if p.LastError != "" {
		b.WriteString(fmt.Sprintf("- **Last error:** %s\n", p.LastError))
	}
	return b.String()
}

// ── 启动期降级登记（非核心模块启动失败时用，替代 panic 崩库）──────────────────
//
// 原则：非核心组件（如某个 Go-DSL 工作流）初始化失败时，不再 panic 掀翻整个进程，
// 而是登记一条降级、跳过该组件、daemon 照常服务其余功能。/health 随之报 "degraded"，
// 让降级状态对外可见（curl 可见、可被 scripts/daemon_drift.sh 监控）。
// 详见 DESIGN_PRINCIPLES.md「核心 fail-fast，非核心降级」。

// Degradation 是一条非致命的启动/运行期降级记录。
type Degradation struct {
	Component string    `json:"component"`
	Reason    string    `json:"reason"`
	At        time.Time `json:"at"`
}

// EventDegraded 在每次 RecordDegradation 时发布，供订阅者告警。
const EventDegraded = "system.degraded"

var (
	degMu        sync.Mutex
	degradations []Degradation
)

// RecordDegradation 登记一条非致命降级（线程安全）：记日志 + 发 EventDegraded 事件 +
// 计入 /health 的 degraded 状态。非核心模块启动/初始化失败时调用它，而非 panic。
func RecordDegradation(component, reason string) {
	degMu.Lock()
	degradations = append(degradations, Degradation{Component: component, Reason: reason, At: time.Now().UTC()})
	degMu.Unlock()
	log.Printf("[degraded] %s: %s", component, reason)
	PublishEvent(Event{Type: EventDegraded, Data: map[string]string{"component": component, "reason": reason}})
}

// Degradations 返回当前已登记降级的快照副本（线程安全）。空切片 = 无降级 = 健康。
func Degradations() []Degradation {
	degMu.Lock()
	defer degMu.Unlock()
	out := make([]Degradation, len(degradations))
	copy(out, degradations)
	return out
}
