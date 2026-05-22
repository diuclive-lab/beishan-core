package workflow

import "gopkg.in/yaml.v3"

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
	ElapsedMs int64 `json:"ElapsedMs,omitempty"` // 步骤耗时（毫秒）
}

/* WorkflowResult 整个工作流的执行结果。 */
type WorkflowResult struct {
	WorkflowID  string       `json:"WorkflowID"`
	Steps       []StepResult `json:"Steps"`
	FinalStep   string       `json:"FinalStep"`
	Success     bool         `json:"Success"`
	Error       string       `json:"Error"`
	FinalOutput string       `json:"FinalOutput,omitempty"` // 最后一步的输出，用于嵌套工作流
	TotalMs     int64        `json:"TotalMs,omitempty"`     // 总耗时（毫秒）
}
