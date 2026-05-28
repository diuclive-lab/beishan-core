package workflow

import (
	"time"

	"gopkg.in/yaml.v3"
)

/*
WorkflowDef 工作流定义，从 YAML 加载。

	示例：
	- id: cold_start
	  plugin: cold_start_plugin
	  type: cold_start
	  next: legal_search
*/
type WorkflowDef struct {
	ID            string    `yaml:"id"`
	Steps         []StepDef `yaml:"steps"`
	MaxIterations int       `yaml:"max_iterations,omitempty"` // 全局循环上限，默认 200

	// ── 输出路由 ────────────────────────────────────────────────
	// OutputTarget 声明工作流最终输出的目标渠道，引擎执行完成后按此分发。
	//
	// 可选值：
	//   chat        （默认）回复到当前对话窗口
	//   dashboard   推送到 Web 仪表盘（TODO: 需要 WebSocket/SSE 实装后激活）
	//   notify      通过 notify_plugin 发送通知（邮件/Slack/企业微信）
	//   knowledge   将结果存入知识库（knowledge_remember）
	//
	// 扩展说明：未来可支持多目标，字段改为 []string，向后兼容。
	OutputTarget string `yaml:"output_target,omitempty"`

	// ── 触发机制 ────────────────────────────────────────────────
	// Trigger 声明工作流的触发方式，不填默认为 manual（用户主动请求）。
	// 设计目标是支持三种触发：手动 / 定时 / 事件驱动。
	// 当前 event 类型预留接口，引擎加载时打印警告，不执行。
	//
	// Trigger 已支持三种类型：manual / scheduled / event。
	// event 类型由 Engine.InitEventSubscriptions() 在启动时扫描注册，
	// 收到匹配 observatory 事件时自动执行工作流（main.go:283 已调用）。
	Trigger *TriggerDef `yaml:"trigger,omitempty"`
}

// TriggerDef 工作流触发方式。
type TriggerDef struct {
	// Type: manual（默认，用户主动请求）/ scheduled（定时，由 scheduler_plugin 管理）
	//       event（事件驱动，当前预留，尚未实装）
	Type string `yaml:"type"`

	// EventType 仅 type=event 时有效。
	// 格式与 observatory.Event.Type 一致，例如 "agent.failed" / "knowledge.added"。
	// EventType 格式与 observatory.Event.Type 一致，如 "agent.failed" / "knowledge.added"。
	// Engine.InitEventSubscriptions() 读取此字段并调用 observatory.Subscribe。
	EventType string `yaml:"event_type,omitempty"`

	// Cron 仅 type=scheduled 时有效，标准 5 段 cron 表达式（由 scheduler_plugin 解析）。
	Cron string `yaml:"cron,omitempty"`
}

/*
StepDef 工作流中的单个步骤。

	next 支持两种格式：
	  字符串: next: legal_search
	  列表:   next:
	            - if: "..."
	              goto: clause_analysis
	            - default: write_report

	on_error: 失败后继续到指定步骤（不设置则终止工作流）
	retry_delay: 重试间隔秒数，默认 1
*/
type StepDef struct {
	ID            string                 `yaml:"id"`
	Plugin        string                 `yaml:"plugin"`
	Type          string                 `yaml:"type"`
	Provider      string                 `yaml:"provider,omitempty"`      // 可选，指定 LLM provider（local/deepseek/xiaomi）
	Inputs        map[string]interface{} `yaml:"inputs,omitempty"`
	Timeout       int                    `yaml:"timeout,omitempty"`        // 秒，默认 120
	Retry         int                    `yaml:"retry,omitempty"`          // 失败重试次数，默认 0
	RetryDelay    int                    `yaml:"retry_delay,omitempty"`    // 重试间隔秒数，默认 1
	OnError       string                 `yaml:"on_error,omitempty"`       // 失败后继续到指定步骤
	SkipIf        string                 `yaml:"skip_if,omitempty"`        // 条件表达式，成立时跳过本步骤
	Effort        string                 `yaml:"effort,omitempty"`         // quick/standard/deep，控制 max_tokens
	Batch         *BatchDef              `yaml:"batch,omitempty"`          // 批量循环执行
	ParallelSteps []StepDef              `yaml:"steps,omitempty"`          // 并行子步骤
	Next          NextList               `yaml:"next,omitempty"`
}

// BatchDef 批量循环定义。对 foreach 数组中的每个元素，执行 step 中的 plugin:action。
// 当前元素可通过 ctx["item"] 访问。
type BatchDef struct {
	Foreach     string `yaml:"foreach"`               // 表达式，求值为数组（如 "${input}"）
	Parallel    bool   `yaml:"parallel,omitempty"`    // 是否并发执行（默认 false 串行）
	Concurrency int    `yaml:"concurrency,omitempty"` // 并发数上限，默认 5。仅 parallel=true 时生效
}

/* NextList 支持 next 字段的字符串和列表两种格式。 */
type NextList []NextDef

/* NextDef 步骤间的路由规则。 */
type NextDef struct {
	If      string `yaml:"if,omitempty"`
	Goto    string `yaml:"goto"`
	Default bool   `yaml:"default,omitempty"`
}

/* UnmarshalYAML 自定义解析，兼容 next: string 和 next: [...] 两种格式。 */
func (n *NextList) UnmarshalYAML(value *yaml.Node) error {
	// 先试列表格式
	var list []NextDef
	if err := value.Decode(&list); err == nil {
		*n = list
		return nil
	}
	// 再试字符串格式
	var s string
	if err := value.Decode(&s); err == nil {
		*n = []NextDef{{Goto: s}}
		return nil
	}
	// 都不行才报错
	return value.Decode(&list)
}

/* StepResult 单步执行结果，用于上下文传递。 */
type StepResult struct {
	ID        string
	Output    string
	Error     string
	ElapsedMs int64                      `json:"ElapsedMs,omitempty"` // 步骤耗时（毫秒）
	Data      map[string]interface{}     `json:"-"`                  // Go-DSL 结构化数据（YAML 引擎不用）
	SubResults []StepResult              `json:"-"`                  // 子步骤结果（chain/parallel）
}

/* ═══════════════════════════════════════════════════════════
   Go-DSL 工作流类型 — 编译时安全的静态硬化链
   与上文的 StepResult/WorkflowResult 共享状态类型
   ═══════════════════════════════════════════════════════════ */

// StepStatus 执行状态（双引擎通用）
type StepStatus string

const (
	StatusPending  StepStatus = "pending"
	StatusRunning  StepStatus = "running"
	StatusSuccess  StepStatus = "success"
	StatusError    StepStatus = "error"
	StatusSkipped  StepStatus = "skipped"
)

// GoStepType 步骤类型
type GoStepType string

const (
	GoStepTool      GoStepType = "tool"      // → kernel.Call(L3 插件)，零校验
	GoStepPlugin    GoStepType = "plugin"    // → kernel.Call(指定插件)
	GoStepChain     GoStepType = "chain"     // 顺序子步骤
	GoStepParallel  GoStepType = "parallel"  // 并发子步骤
	GoStepTransform GoStepType = "transform" // TransformFn 纯数据变换
)

// ErrorStrategy 步骤失败时的处理策略
type ErrorStrategy string

const (
	ErrorFailStep     ErrorStrategy = "fail_step"     // 标记错误，继续执行
	ErrorFailWorkflow ErrorStrategy = "fail_workflow" // 立即终止
	ErrorContinue     ErrorStrategy = "continue"      // 跳过，继续
)

// GoStepInput 如何为步骤构建输入参数
type GoStepInput struct {
	Merge  []GoInputSource          `json:"merge,omitempty"`
	From   string                   `json:"from,omitempty"` // 引用另一步骤全部输出
	Static map[string]interface{}   `json:"static,omitempty"`

	// RawInputKeys 从原始输入映射字段：{"contract": "input"}
	// 等价于 InputSource{Key:"contract", Value:"${input}"}
	RawInputKeys map[string]string `json:"raw_input_keys,omitempty"`
}

// GoInputSource 合并输入源中的单个条目
type GoInputSource struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"` // 模板引用 ${input} / ${steps.x.output.y}
	Step  string `json:"step,omitempty"`  // 步骤输出中提取
	Field string `json:"field,omitempty"`
}

// GoStep Go-DSL 工作流中的单个步骤
type GoStep struct {
	ID   string     `json:"id"`
	Type GoStepType `json:"type"`
	Name string     `json:"name,omitempty"`

	// 工具步骤 — 通过 kernel.Call 找宿主插件执行，零校验
	Tool          string        `json:"tool,omitempty"`

	// 插件步骤 — 直接 kernel.Call 到指定插件
	Recipient     string        `json:"recipient,omitempty"`
	MsgType       string        `json:"msg_type,omitempty"`
	PluginTimeout time.Duration `json:"timeout,omitempty"`

	// 变换步骤 — 纯 Go 函数，约定不做 I/O
	TransformFn func(ctx GoContext, input map[string]interface{}) (map[string]interface{}, error) `json:"-"`

	// 🛡️ 中间件钩子 — 包裹在任何 StepType 前后（约定不做 I/O）
	BeforeExecute func(ctx GoContext, input map[string]interface{}) (map[string]interface{}, error) `json:"-"`
	AfterExecute  func(ctx GoContext, input map[string]interface{}, result *StepResult) (*StepResult, error) `json:"-"`

	// 输入构建
	Input *GoStepInput `json:"input,omitempty"`

	// 状态注册
	OutputVar string `json:"output_var,omitempty"`

	// 韧性策略
	MaxRetries int           `json:"max_retries,omitempty"`
	RetryDelay time.Duration `json:"retry_delay,omitempty"`
	Fallback   string        `json:"fallback,omitempty"` // 主工具失败后尝试的降级工具
	OnError    ErrorStrategy `json:"on_error,omitempty"`

	// 嵌套子步骤
	SubSteps []GoStep `json:"sub_steps,omitempty"`
}

// GoWorkflow Go-DSL 工作流定义
type GoWorkflow struct {
	Name       string        `json:"name"`
	Steps      []GoStep      `json:"steps"`
	Timeout    time.Duration `json:"timeout,omitempty"` // 全局超时
	ResultStep string        `json:"result_step,omitempty"` // 取哪步输出为最终结果，空=最后一步
}

// GoContext 执行时注入的环境
type GoContext struct {
	WorkflowName string
	StepID       string
	Kernel       interface{} // *kernel.Kernel，运行时注入
}
type WorkflowResult struct {
	WorkflowID  string       `json:"WorkflowID"`
	Steps       []StepResult `json:"Steps"`
	FinalStep   string       `json:"FinalStep"`
	Success     bool         `json:"Success"`
	Error       string       `json:"Error"`
	FinalOutput string       `json:"FinalOutput,omitempty"` // 最后一步的输出，用于嵌套工作流
	TotalMs     int64        `json:"TotalMs,omitempty"`     // 总耗时（毫秒）

	// ── 人工确认支持 ─────────────────────────────────────────────
	// NeedsConfirm=true 时，工作流在 human_confirm 步骤处暂停。
	// 引擎不等待，而是将"等待确认"状态返回给调用方，
	// 由 think_plugin 将 ConfirmMessage 展示给用户。
	//
	// 轻量版设计（当前）：
	//   工作流被拆成"确认前"和"确认后"两段。
	//   用户回复"确认"后，think_plugin 触发 ConfirmWorkflow。
	//   不保存执行状态，ConfirmWorkflow 重新接收用户输入从头跑。
	//
	// TODO(workflow-pause): 完整的"暂停/恢复"需要序列化 ctx 到持久化存储，
	//   再由 think_plugin confirm 路径反序列化并继续执行。
	//   当前先用拆两段的方式，够用，待需求明确后升级。
	NeedsConfirm    bool   `json:"NeedsConfirm,omitempty"`
	ConfirmMessage  string `json:"ConfirmMessage,omitempty"`  // 展示给用户的确认问题
	ConfirmWorkflow string `json:"ConfirmWorkflow,omitempty"` // 用户确认后触发的工作流 ID
}
