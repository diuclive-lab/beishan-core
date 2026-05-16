package plugins

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"beishan/kernel"
)

/* LegalWritePlugin 中国法律文书生成插件（L3）。

   职责：接收条款分析结果，生成标准中文法律文书。

   文书类型：
   1. 合同审查报告（Contract Review Report）
      格式：审查概述 → 逐条分析 → 风险矩阵 → 修改建议
   2. 法律意见书（Legal Opinion）
      格式：事实概要 → 法律分析 → 结论意见
   3. 风险矩阵（Risk Matrix）
      格式：风险项 → 风险等级 → 法律依据 → 建议

   AI 生成标识合规：
   根据《人工智能生成合成内容标识办法》（2025年施行），
   在所有输出中标注 AI 生成身份，确保可追溯。

   参考 claude-for-legal 的输出模板：
   - 工作成果标头（PRIVILEGED & CONFIDENTIAL → 中国版）
   - 审查者注（Reviewer Note）
   - 决策树（Next Steps）
*/
type LegalWritePlugin struct{}

// WriteRequest 文书生成请求。
type WriteRequest struct {
	AnalysisReport json.RawMessage `json:"analysis_report"`     // 条款分析结果
	DocumentType   string          `json:"document_type"`       // contract_review / legal_opinion / risk_matrix
	OutputFormat   string          `json:"output_format"`       // markdown / json
	IncludeMatrix  bool            `json:"include_matrix"`      // 是否包含风险矩阵
}

// GeneratedDocument 生成的文书。
type GeneratedDocument struct {
	Title           string `json:"title"`                      // 文书标题
	DocumentType    string `json:"document_type"`              // 文书类型
	GeneratedAt     string `json:"generated_at"`               // 生成时间
	Content         string `json:"content"`                    // 正文内容（Markdown）
	RiskMatrix      string `json:"risk_matrix,omitempty"`      // 风险矩阵（Markdown 表格）
	NextSteps       string `json:"next_steps,omitempty"`       // 下一步行动建议
	FilePath        string `json:"file_path,omitempty"`        // 如已写入文件，返回路径

	// AI 生成标识（《人工智能生成合成内容标识办法》合规）
	AIGenerated  bool   `json:"ai_generated"`
	AIDisclosure string `json:"ai_disclosure"`     // AI 生成信息披露文本
}

func (p *LegalWritePlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "legal_generate_report":
		return p.handleGenerate(msg)
	case "legal_write_opinion":
		return p.handleWriteOpinion(msg)
	default:
		return kernel.Message{}, fmt.Errorf("legal_write_plugin: 未知消息类型 %s", msg.Type)
	}
}

func (p *LegalWritePlugin) handleGenerate(msg kernel.Message) (kernel.Message, error) {
	// 从 analysis 结果生成完整文书
	var request WriteRequest
	if err := json.Unmarshal(msg.Payload, &request); err != nil {
		return kernel.Message{}, fmt.Errorf("legal_write: payload 解析失败: %w", err)
	}

	// 解析分析报告
	var report AnalysisReport
	if err := json.Unmarshal(request.AnalysisReport, &report); err != nil {
		return kernel.Message{}, fmt.Errorf("legal_write: 分析报告解析失败: %w", err)
	}

	// 选择文书模板
	docType := request.DocumentType
	if docType == "" {
		docType = "contract_review"
	}

	doc := GeneratedDocument{
		DocumentType: docType,
		GeneratedAt:  time.Now().Format("2006-01-02 15:04:05"),
		AIGenerated:  true,
		AIDisclosure: generateAIDisclosure(),
	}

	switch docType {
	case "legal_opinion":
		doc.Title = report.Title
		doc.Content = buildLegalOpinion(report)
	case "risk_matrix":
		doc.Title = report.Title + "——风险矩阵"
		doc.Content = buildRiskMatrixContent(report)
		if request.IncludeMatrix {
			doc.RiskMatrix = buildRiskMatrixTable(report)
		}
	default:
		doc.Title = report.Title
		doc.Content = buildContractReviewReport(report)
		if request.IncludeMatrix {
			doc.RiskMatrix = buildRiskMatrixTable(report)
		}
		doc.NextSteps = buildNextSteps(report.OverallRisk)
	}

	// 构建完整响应
	payload, err := json.Marshal(doc)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("legal_write: 文档序列化失败: %w", err)
	}

	return kernel.Message{
		Type:    "legal_generate_report.result",
		Payload: payload,
	}, nil
}

func (p *LegalWritePlugin) handleWriteOpinion(msg kernel.Message) (kernel.Message, error) {
	return p.handleGenerate(msg)
}

/* buildContractReviewReport 生成合同审查报告。

   模板结构（参考 claude-for-legal 的审查报告格式，
   适配中国合同审查实务）：
*/
func buildContractReviewReport(report AnalysisReport) string {
	content := fmt.Sprintf(`# %s

---

## 审查信息

- **合同类型：** %s
- **整体风险评级：** %s
- **审查时间：** %s

---

## 逐条分析

`, report.Title, report.ContractType, report.OverallRisk,
		time.Now().Format("2006-01-02"))

	for i, analysis := range report.ClauseAnalyses {
		content += fmt.Sprintf(`### %d. %s

**风险评级：** %s

**大前提（法律规则）：**
%s

**小前提（合同约定）：**
%s

**结论：**
%s

`, i+1, analysis.ClauseNumber, analysis.RiskLevel,
			analysis.MajorPremise, analysis.MinorPremise, analysis.Conclusion)

		if analysis.Suggestion != "" {
			content += fmt.Sprintf(`**修改建议：**
%s

`, analysis.Suggestion)
		}
	}

	content += fmt.Sprintf(`## 总体结论

%s

---

## 免责声明

%s

---

*本报告由 AI 辅助生成，生成时间：%s。相关法律法规请以官方公布版本为准。*
`,
		report.Summary, report.Disclaimer,
		time.Now().Format("2006-01-02 15:04:05"))

	return content
}

/* buildLegalOpinion 生成法律意见书。 */
func buildLegalOpinion(report AnalysisReport) string {
	return fmt.Sprintf(`# 法律意见书

**事由：** %s

---

## 一、事实概要

根据提供的合同材料和审查分析，本法律意见书就相关法律问题出具意见。

## 二、法律分析

%s

## 三、结论意见

**整体评价：** %s

%s

---

## 四、注意事项

1. 本法律意见书仅针对提供的合同文本进行分析。
2. 法律适用以分析时的有效法律为准。
3. 如事实情况发生变化，本意见可能需要重新评估。

%s

---

*本法律意见书由 AI 辅助生成，生成时间：%s。*
*根据《人工智能生成合成内容标识办法》，本内容由 AI 生成，使用前请核实。*
`,
		report.Title, report.Summary, report.OverallRisk,
		generateConclusionAdvice(report.OverallRisk),
		report.Disclaimer,
		time.Now().Format("2006-01-02 15:04:05"))
}

/* buildRiskMatrixContent 生成风险矩阵正文。 */
func buildRiskMatrixContent(report AnalysisReport) string {
	content := fmt.Sprintf(`# 风险矩阵

**合同类型：** %s
**分析时间：** %s

---

## 风险总览

整体风险评级：**%s**

---

## 风险项详情

`, report.ContractType, time.Now().Format("2006-01-02 15:04:05"), report.OverallRisk)

	for i, analysis := range report.ClauseAnalyses {
		content += fmt.Sprintf(`### 风险 %d：%s

| 维度 | 内容 |
|---|---|
| **风险等级** | %s |
| **法律依据** | %s |
| **风险描述** | %s |
| **建议** | %s |

`,
			i+1, analysis.ClauseNumber, analysis.RiskLevel,
			analysis.MajorPremise, analysis.MinorPremise+analysis.Conclusion,
			analysis.Suggestion)
	}

	content += fmt.Sprintf(`---

%s

*生成时间：%s*
`,
		report.Disclaimer, time.Now().Format("2006-01-02 15:04:05"))

	return content
}

/* buildRiskMatrixTable 生成风险矩阵表格。

   参考 claude-for-legal 的审查者注（Reviewer Note）格式，
   适配中国合同审查的风险矩阵样式。
*/
func buildRiskMatrixTable(report AnalysisReport) string {
	table := "| 编号 | 条款 | 风险等级 | 法律依据 | 建议 |\n"
	table += "|------|------|----------|----------|------|\n"

	for i, analysis := range report.ClauseAnalyses {
		clause := analysis.ClauseNumber
		if len(clause) > 20 {
			clause = string([]rune(clause)[:20]) + "..."
		}
		law := analysis.MajorPremise
		if len(law) > 30 {
			law = string([]rune(law)[:30]) + "..."
		}
		suggestion := analysis.Suggestion
		if suggestion == "" {
			suggestion = "见正文"
		}
		if len(suggestion) > 25 {
			suggestion = string([]rune(suggestion)[:25]) + "..."
		}

		table += fmt.Sprintf("| %d | %s | %s | %s | %s |\n",
			i+1, clause, analysis.RiskLevel, law, suggestion)
	}

	return table
}

/* buildNextSteps 生成下一步行动建议。

   参考 claude-for-legal 的决策树模式。
*/
func buildNextSteps(risk string) string {
	return fmt.Sprintf(`## 下一步行动

根据审查结果，建议采取以下措施：

1. **高优先级** — 对标记为 🔴 违规的条款进行修改，重新协商
2. **中优先级** — 对标记为 🟡 提示的条款进行内部评估
3. **低优先级** — 审查 🟢 合规条款，确认无误后签署

> 需要进一步协助？我可以：
> - 起草修改建议的条款文本
> - 生成风险矩阵便于内部汇报
> - 对比不同版本合同的改动
`)
}

/* generateConclusionAdvice 根据风险评级生成结论建议。 */
func generateConclusionAdvice(risk string) string {
	switch {
	case contains(risk, "🔴"):
		return "建议：合同存在违反法律强制性规定的条款，不建议在未修改的情况下签署。建议与对方重新协商相关条款。"
	case contains(risk, "🟡"):
		return "建议：合同中的部分条款存在法律风险，建议在签署前咨询法律顾问，或与对方协商修改风险条款。"
	default:
		return "建议：合同整体合规，可按约定签署。建议保留审查记录备查。"
	}
}

/* generateAIDisclosure 生成 AI 生成披露文本。

   根据《人工智能生成合成内容标识办法》要求：
   1. 明确标识为 AI 生成
   2. 不得误导用户认为内容由人类专家出具
   3. 提供对内容准确性的必要警示
*/
func generateAIDisclosure() string {
	return "本内容由人工智能（AI）辅助生成，属于「人工智能生成合成内容」。" +
		"根据《人工智能生成合成内容标识办法》及相关规定，特此标识。" +
		"AI 生成内容不构成法律意见，使用前应由具备执业资格的法律专业人士进行审核和确认。"
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && strings.Contains(s, substr)
}
