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

	"beishan/internal/tools"
	"beishan/kernel"
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
		respType := msg.Type + ".result"
		if !result.Success {
			respType = msg.Type + ".error"
		}
		var respPayload json.RawMessage
		output := result.Output
		if len(output) > 0 && output[0] == '{' && json.Valid([]byte(output)) {
			respPayload = json.RawMessage(output)
		} else {
			respPayload, _ = json.Marshal(output)
		}
		return kernel.Message{Type: respType, Payload: respPayload}, nil

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

// ─── skill_list / skill_view / skill_delete ───────────────

func (p *SkillFactoryPlugin) handleList() (kernel.Message, error) {
	entries, err := os.ReadDir(p.workflows)
	if err != nil {
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
		// D03: skill_factory_plugin 是特权插件，允许直接删除 workflows/ 目录文件。
		// 参见 docs/reports/boundary_debt_register.md#D03
		return kernel.Message{}, fmt.Errorf("skill_factory: 删除失败: %w", err)
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
