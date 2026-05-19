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
	ID    string    `yaml:"id"`
	Steps []StepDef `yaml:"steps"`
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
	ID         string                 `yaml:"id"`
	Plugin     string                 `yaml:"plugin"`
	Type       string                 `yaml:"type"`
	Inputs     map[string]interface{} `yaml:"inputs,omitempty"`
	Timeout    int                    `yaml:"timeout,omitempty"`     // 秒，默认 120
	Retry      int                    `yaml:"retry,omitempty"`       // 失败重试次数，默认 0
	RetryDelay int                    `yaml:"retry_delay,omitempty"` // 重试间隔秒数，默认 1
	OnError    string                 `yaml:"on_error,omitempty"`    // 失败后继续到指定步骤
	Next       NextList               `yaml:"next,omitempty"`
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
	ID     string
	Output string
	Error  string
}

/* WorkflowResult 整个工作流的执行结果。 */
type WorkflowResult struct {
	WorkflowID string
	Steps      []StepResult
	FinalStep  string
	Success    bool
	Error      string
}
