package plugins

// PRIVILEGED PLUGIN: skill_factory manages YAML workflow files directly.
// These filesystem operations are inherent to its function as a workflow editor.
// See docs/reports/boundary_debt_register.md#D03

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"beishan/internal/llm"
	"beishan/internal/tools"
	"beishan/kernel"
	"gopkg.in/yaml.v3"
)

var ErrSkillExists = errors.New("skill already exists")

/* SkillFactoryPlugin （技能工场插件）

   根据自然语言描述，用 DeepSeek 生成标准 YAML 工作流并保存到 workflows/。
   让用户不需要手写 YAML，说一句"把这个变成一个技能"就能生成可复用的工作流。

   消息类型：
   - skill_create:  根据描述生成 YAML 工作流并保存
   - skill_preview: 根据描述生成 YAML，返回预览，不写入磁盘
   - skill_list:    列出所有已有 skill
   - skill_view:    查看某个 skill 的 YAML 内容
   - skill_delete:  删除一个 skill
*/
type SkillFactoryPlugin struct {
	kernel    *kernel.Kernel
	workflows string // workflows/ 目录的绝对路径
}

func NewSkillFactory(k *kernel.Kernel, workflowsDir string) *SkillFactoryPlugin {
	return &SkillFactoryPlugin{
		kernel:    k,
		workflows: workflowsDir,
	}
}

func (p *SkillFactoryPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "skill_evaluate":
		result := tools.ValidateAndExecute(msg.Type, msg.Payload)
		fmt.Printf("[技能评估] %s\n", result.Output[:min(len(result.Output), 200)])
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: msg.Type + ".result", Payload: respPayload}, nil

	case "skill_create":
		return p.handleCreate(msg)
	case "skill_list":
		return p.handleList()
	case "skill_view":
		return p.handleView(msg)
		case "skill_delete":
			return p.handleDelete(msg)
		case "skill_preview":
			return p.handlePreview(msg)
	default:
		return kernel.Message{}, fmt.Errorf("skill_factory: 未知类型 %s", msg.Type)
	}
}

// ─── skill_create ─────────────────────────────────────────

type createRequest struct {
	Description string `json:"description"`
	Name        string `json:"name,omitempty"`  // 可选，不提供则由 DeepSeek 生成
	Force       bool   `json:"force,omitempty"` // 同名文件存在时是否覆盖
	Preview     bool   `json:"preview,omitempty"` // true=仅预览不写入
}

func (p *SkillFactoryPlugin) handleCreate(msg kernel.Message) (kernel.Message, error) {
	var req createRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 参数解析失败: %w", err)
	}
	if req.Description == "" {
		return kernel.Message{}, fmt.Errorf("skill_factory: 需要 description 参数")
	}

	// 1. 用 DeepSeek 生成 YAML
	yamlContent, err := p.generateWorkflow(req.Description, req.Name)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 生成失败: %w", err)
	}

	// 2. 硬化层验证：YAML 能解析为合法的 WorkflowDef
		name, err := p.validateAndSave(yamlContent, req.Force)
		if errors.Is(err, ErrSkillExists) {
			payload, _ := json.Marshal(map[string]string{
				"name":   name,
				"status": "exists",
				"note":   fmt.Sprintf("工作流 %s.yaml 已存在，如需覆盖请设置 force:true", name),
			})
			return kernel.Message{Type: "skill.result", Payload: payload}, nil
		}
		if err != nil {
			return kernel.Message{}, fmt.Errorf("skill_factory: 验证失败: %w", err)
		}

		payload, _ := json.Marshal(map[string]string{
			"name":   name,
			"status": "created",
			"note":   fmt.Sprintf("工作流 %s.yaml 已创建，可通过 workflow_plugin 执行", name),
		})
		return kernel.Message{Type: "skill.result", Payload: payload}, nil
	}

func (p *SkillFactoryPlugin) handlePreview(msg kernel.Message) (kernel.Message, error) {
	var req createRequest
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 参数解析失败: %w", err)
	}
	if req.Description == "" {
		return kernel.Message{}, fmt.Errorf("skill_factory: 需要 description 参数")
	}

	yamlContent, err := p.generateWorkflow(req.Description, req.Name)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 生成失败: %w", err)
	}

	// 只验证不保存
	if _, err := p.validateOnly(yamlContent); err != nil {
		payload, _ := json.Marshal(map[string]string{
			"status": "preview_invalid",
			"note":   fmt.Sprintf("YAML 验证未通过: %s", err),
			"yaml":   yamlContent,
		})
		return kernel.Message{Type: "skill.preview", Payload: payload}, nil
	}

	payload, _ := json.Marshal(map[string]string{
		"status": "preview_ok",
		"yaml":   yamlContent,
		"note":   "预览通过四层验证，使用 skill_create 确认创建",
	})
	return kernel.Message{Type: "skill.preview", Payload: payload}, nil
}

// generateWorkflow 构造 DeepSeek 提示词，生成合法 YAML 工作流。


// ─── 工作流模板系统 ──────────────────────────────────────

type WorkflowTemplate struct {
	ID          string // 模板标识
	Name        string // 显示名
	Description string // 适用场景描述
	Steps       string // YAML 步骤模板，用 {{.Name}} {{.Step1Prompt}} 等变量填充
}

var workflowTemplates = []WorkflowTemplate{
	{
		ID:          "review",
		Name:        "内容审核模板",
		Description: "获取内容 → 分类/分析 → 评估 → 报告。适用于 URL 审核、文档审查、内容评估。",
		Steps: `steps:
  - id: fetch
    plugin: search_plugin
    type: web_render
    timeout: 60
    inputs:
      url: "${input}"
      wait: 5
    next: analyze

  - id: analyze
    plugin: think_plugin
    type: chat
    timeout: 120
    inputs:
      mode: "no_retrieval"
      message: |
        {{.Step2Prompt}}
        内容：${steps.fetch.output}
    next: evaluate

  - id: evaluate
    plugin: think_plugin
    type: chat
    timeout: 60
    inputs:
      mode: "no_retrieval"
      message: |
        {{.Step3Prompt}}
        分析结果：${steps.analyze.output}
    # next 省略 → 自动结束`,
	},
	{
		ID:          "search_summarize",
		Name:        "搜索汇总模板",
		Description: "搜索 → 分析 → 汇总。适用于信息搜索、资料调研、竞品分析。",
		Steps: `steps:
  - id: search
    plugin: search_plugin
    type: web_search
    timeout: 30
    inputs:
      query: "${input}"
    next: analyze

  - id: analyze
    plugin: think_plugin
    type: chat
    timeout: 120
    inputs:
      mode: "no_retrieval"
      message: |
        {{.Step2Prompt}}
        搜索结果：${steps.search.output}
    # next 省略 → 自动结束`,
	},
	{
		ID:          "ingest",
		Name:        "内容入库模板",
		Description: "获取/解析内容 → 提炼知识 → 入库。适用于文章导入、文档入库、笔记整理。",
		Steps: `steps:
  - id: fetch
    plugin: search_plugin
    type: web_render
    timeout: 60
    inputs:
      url: "${input}"
      wait: 5
    next: extract

  - id: extract
    plugin: think_plugin
    type: chat
    timeout: 120
    inputs:
      mode: "no_retrieval"
      message: |
        {{.Step2Prompt}}
        内容：${steps.fetch.output}

        输出 JSON：{"title":"...","summary":"...","tags":["..."],"insights":["..."]}
    next: store

  - id: store
    plugin: memory_plugin
    type: knowledge_remember
    inputs:
      title: "${steps.extract.output.title}"
      summary: "${steps.extract.output.summary}"
    # next 省略 → 自动结束`,
	},
	{
		ID:          "multi_source",
		Name:        "多源汇总模板",
		Description: "并行搜索多个来源 → 汇总分析。适用于多维度调研、对比分析。",
		Steps: `steps:
  - id: search
    plugin: search_plugin
    type: web_search
    timeout: 30
    inputs:
      query: "${input}"
    next: kb_check

  - id: kb_check
    plugin: memory_plugin
    type: knowledge_search
    timeout: 10
    inputs:
      keyword: "${input}"
    next: analyze

  - id: analyze
    plugin: think_plugin
    type: chat
    timeout: 120
    inputs:
      mode: "no_retrieval"
      message: |
        {{.Step2Prompt}}
        网络搜索：${steps.search.output}
        知识库：${steps.kb_check.output}
    # next 省略 → 自动结束`,
	},
}

// classifyTemplate 用 LLM 选择最匹配的模板，返回模板 ID 和变量填充。
func (p *SkillFactoryPlugin) classifyTemplate(description string) (string, map[string]string, error) {
	var templateDescs []string
	for i, t := range workflowTemplates {
		templateDescs = append(templateDescs, fmt.Sprintf("%d. [%s] %s — %s", i+1, t.ID, t.Name, t.Description))
	}

	prompt := fmt.Sprintf(`根据用户描述，选择最合适的工作流模板并填充变量。

可用模板：
%s

用户描述：%s

输出 JSON（不要其他文字）：
{
  "template": "模板ID",
  "name": "工作流名称（英文短横线分隔）",
  "variables": {
    "Step2Prompt": "第二步的提示词",
    "Step3Prompt": "第三步的提示词（如模板需要）"
  }
}`, strings.Join(templateDescs, "\n"), description)

	content, usage, err := llm.ChatCompletionWithUsage(
		[]llm.ChatMessage{
			{Role: "system", Content: "你是工作流模板分类器。只输出 JSON，不要其他文字。"},
			{Role: "user", Content: prompt},
		},
		30*time.Second,
	)
	llm.RecordUsage("skill_factory", usage)
	if err != nil {
		return "", nil, err
	}

	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var result struct {
		Template  string            `json:"template"`
		Name      string            `json:"name"`
		Variables map[string]string `json:"variables"`
	}
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return "", nil, fmt.Errorf("模板分类结果解析失败: %w", err)
	}
	return result.Template, result.Variables, nil
}

// applyTemplate 用变量填充模板生成 YAML。
func (p *SkillFactoryPlugin) applyTemplate(tmplID, name string, vars map[string]string) (string, error) {
	for _, t := range workflowTemplates {
		if t.ID == tmplID {
			yaml := t.Steps
			// 简单变量替换
			for k, v := range vars {
				yaml = strings.ReplaceAll(yaml, "{{."+k+"}}", v)
			}
			// 加 id 头
			return fmt.Sprintf("id: %s\n\n%s", name, yaml), nil
		}
	}
	return "", fmt.Errorf("模板 %s 未找到", tmplID)
}

func (p *SkillFactoryPlugin) generateWorkflow(description, preferredName string) (string, error) {

	// 1. 尝试模板匹配
	tmplID, vars, err := p.classifyTemplate(description)
	if err == nil && tmplID != "" {
		name := preferredName
		if name == "" {
			if n, ok := vars["name"]; ok {
				name = n
			}
		}
		if name == "" {
			name = "generated_workflow"
		}
		yaml, err := p.applyTemplate(tmplID, name, vars)
		if err == nil && yaml != "" {
			fmt.Printf("[技能工场] 使用模板 %s 生成工作流 %s\n", tmplID, name)
			return yaml, nil
		}
	}

	// 2. 降级：从零生成
	pluginList := p.buildPluginList()

	nameHint := ""
	if preferredName != "" {
		nameHint = fmt.Sprintf("工作流 id 请使用: %s", preferredName)
	}

	prompt := fmt.Sprintf(`You are a YAML workflow generator for the beishan-core microkernel system.

Output ONLY valid YAML. No explanations, no markdown, no code fences.

The YAML must be a valid beishan-core workflow definition with this exact structure:
id: <workflow_name>
steps:
  - id: <step_id>
    plugin: <plugin_name>
    type: <message_type>
    timeout: <seconds>
    inputs:
      <key>: <value_with_optional_interpolation>
    next: <next_step_id_or_conditional>

Rules:
1. Each step MUST reference one of the available plugins listed below.
2. The first step runs first. Use "next" to chain steps. Omit "next" on the last step.
3. Support interpolation between steps: ${steps.<step_id>.output}
4. The "inputs" values are Go template strings. Use ${input} for user input, ${steps.<id>.output} for step results.
5. Keep timeout reasonable: 30s for search, 10s for memory/todo, 120s for think_plugin chat.
6. Generate 3-8 steps for a typical workflow.
7. The workflow id should be a concise hyphen-separated name.

%s

Available plugins:
%s

User request: %s`, nameHint, pluginList, description)

	content, usage, err := llm.ChatCompletionWithUsage(
		[]llm.ChatMessage{
			{Role: "system", Content: "You generate beishan-core workflow YAML files. Output ONLY valid YAML. No explanations, no markdown fences, no extra text."},
			{Role: "user", Content: prompt},
		},
		90*time.Second,
	)
	llm.RecordUsage("skill_factory", usage)
	if err != nil {
		return "", fmt.Errorf("API 调用失败: %w", err)
	}

	// 清理可能的 markdown 标记
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```yaml")
	content = strings.TrimPrefix(content, "```yml")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	return content, nil
}

// ─── 硬化层验证 ─────────────────────────────────────────

// workflowDef 用于 YAML 验证的最小结构
type workflowDef struct {
	ID    string     `yaml:"id"`
	Steps []stepDef  `yaml:"steps"`
}
type stepDef struct {
	ID      string `yaml:"id"`
	Plugin  string `yaml:"plugin"`
	Type    string `yaml:"type"`
	Timeout int    `yaml:"timeout,omitempty"`
	Retry   int    `yaml:"retry,omitempty"`
}

func (p *SkillFactoryPlugin) validateOnly(yamlContent string) (string, error) {
	var def workflowDef
	if err := yaml.Unmarshal([]byte(yamlContent), &def); err != nil {
		return "", fmt.Errorf("YAML 解析失败: %w", err)
	}
	if def.ID == "" {
		return "", fmt.Errorf("工作流缺少 id 字段")
	}
	if len(def.Steps) == 0 {
		return "", fmt.Errorf("工作流没有步骤")
	}

	// 2. 验证插件都存在
	known := p.kernel.KnownPlugins()
	pluginSet := make(map[string]bool, len(known))
	for _, name := range known {
		pluginSet[name] = true
	}
	metas := p.kernel.KnownPluginsMeta()

	for _, step := range def.Steps {
		if step.ID == "" {
			return "", fmt.Errorf("步骤缺少 id 字段")
		}
		if step.Plugin == "" {
			return "", fmt.Errorf("步骤 %s 缺少 plugin 字段", step.ID)
		}
		if !pluginSet[step.Plugin] {
			return "", fmt.Errorf("步骤 %s 引用了未注册插件: %s（可用: %s）",
				step.ID, step.Plugin, strings.Join(known, ", "))
		}
		if step.Type == "" {
			return "", fmt.Errorf("步骤 %s 缺少 type 字段", step.ID)
		}
		// 3. 验证 type 在插件支持的 types 列表内
		if m, ok := metas[step.Plugin]; ok && len(m.Types) > 0 {
			valid := false
			for _, t := range m.Types {
				if t == step.Type {
					valid = true
					break
				}
			}
			if !valid {
				return "", fmt.Errorf("步骤 %s 的 type %q 不在插件 %s 支持的类型列表中: %v",
					step.ID, step.Type, step.Plugin, m.Types)
			}
		}
	}
	return def.ID, nil
}

func (p *SkillFactoryPlugin) validateAndSave(yamlContent string, force bool) (string, error) {
	name, err := p.validateOnly(yamlContent)
	if err != nil {
		return "", err
	}

	// 4. 检查文件名冲突
	path := filepath.Join(p.workflows, name+".yaml")
	if _, err := os.Stat(path); err == nil {
		if !force {
			return name, ErrSkillExists
		}
		fmt.Printf("[skill_factory] 覆盖已有工作流: %s\n", name)
	}

	// 5. 写入文件
	fullContent := fmt.Sprintf("# Generated by skill_factory_plugin\n# %s\n\n%s",
		time.Now().Format("2006-01-02 15:04:05"),
		yamlContent)
	if err := os.MkdirAll(p.workflows, 0755); err != nil {
	// D03: workflow manager needs to create directory		return "", fmt.Errorf("创建目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(fullContent), 0644); err != nil {
	// D03: workflow manager needs to write files		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	return name, nil
}

// ─── skill_list / skill_view / skill_delete ───────────────

func (p *SkillFactoryPlugin) handleList() (kernel.Message, error) {
	entries, err := os.ReadDir(p.workflows)
	if err != nil {
	// D03: workflow manager needs to list files
		return kernel.Message{}, fmt.Errorf("skill_factory: 读取工作流目录失败: %w", err)
	}

	type skillInfo struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	var skills []skillInfo

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".yaml")
		skills = append(skills, skillInfo{Name: name, Path: e.Name()})
	}

	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Name < skills[j].Name
	})

	payload, _ := json.Marshal(skills)
	fmt.Printf("[skill_factory] 列出 %d 个 skill\n", len(skills))
	return kernel.Message{Type: "skill.list", Payload: payload}, nil
}

func (p *SkillFactoryPlugin) handleView(msg kernel.Message) (kernel.Message, error) {
	name := strings.Trim(string(msg.Payload), `"`)
	path := filepath.Join(p.workflows, name+".yaml")

	data, err := os.ReadFile(path)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("skill_factory: 找不到 skill %s", name)
	}

	payload, _ := json.Marshal(map[string]string{
		"name":    name,
		"content": string(data),
	})
	return kernel.Message{Type: "skill.view", Payload: payload}, nil
}

func (p *SkillFactoryPlugin) handleDelete(msg kernel.Message) (kernel.Message, error) {
	name := strings.Trim(string(msg.Payload), `"`)
	path := filepath.Join(p.workflows, name+".yaml")

	if err := os.Remove(path); err != nil {
	// D03: workflow manager needs to delete files		return kernel.Message{}, fmt.Errorf("skill_factory: 删除失败: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{
		"name":   name,
		"status": "deleted",
	})
	fmt.Printf("[skill_factory] 删除 skill: %s\n", name)
	return kernel.Message{Type: "skill.result", Payload: payload}, nil
}

// ─── 工具函数 ──────────────────────────────────────────────

func (p *SkillFactoryPlugin) buildPluginList() string {
	metas := p.kernel.KnownPluginsMeta()

	var names []string
	for name := range metas {
		names = append(names, name)
	}
	sort.Strings(names)

	var sb strings.Builder
	for _, name := range names {
		if name == "scheduler_plugin" || name == "workflow_plugin" || name == "skill_factory_plugin" {
			continue
		}
		m := metas[name]
		sb.WriteString(fmt.Sprintf("- %s: %s", name, m.Description))
		if len(m.Types) > 0 {
			sb.WriteString(fmt.Sprintf(" (types: %s)", strings.Join(m.Types, ", ")))
		}
		sb.WriteString("\n")
	}
	return sb.String()

}
