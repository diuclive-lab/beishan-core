package plugins

import (
	"encoding/json"
	"fmt"

	"beishan/kernel"
)

/* ClauseAnalyzerPlugin 中国法律条款分析插件（L3）。

   使用 三段论（大前提-小前提-结论）法律推理方法，
   替代西方 IRAC（Issue-Rule-Analysis-Conclusion）框架。

   三段论推理结构：
   1. 大前提（Major Premise）：中国法律法规的明确规定
      — "根据《民法典》第XXX条，..."
   2. 小前提（Minor Premise）：合同条款的具体内容
      — "本案合同第X条约定：..."
   3. 结论（Conclusion）：法律评价
      — "该条款因...而无效/有效/存在风险"

   参考 claude-for-legal 的风险评级体系：
   - 🟢 合规（无法律风险）
   - 🟡 提示（存在风险，需关注）
   - 🔴 违规（违反法律强制性规定）

   输入：三合一数据包（合同原文 + 法律画像 + 检索结果）
   输出：逐条分析结果，含三段论推理过程和风险评级
*/
type ClauseAnalyzerPlugin struct{}

// AnalysisInput 条款分析输入：legal_review_plugin 打包的三合一数据。
type AnalysisInput struct {
	Contract string `json:"contract"`            // 合同原文
	Profile  string `json:"profile"`             // 冷启动法律画像（JSON 字符串）
	Laws     string `json:"laws"`                // 法律检索结果（JSON 字符串）
}

// ClauseAnalysis 单一条款的分析结果。
type ClauseAnalysis struct {
	ClauseNumber string `json:"clause_number"`          // 条款编号，如"第X条"或"第X.X条"
	ClauseText   string `json:"clause_text,omitempty"`  // 条款原文摘要

	// 三段论推理
	MajorPremise  string `json:"major_premise"`   // 大前提：法律规则
	MinorPremise  string `json:"minor_premise"`   // 小前提：合同约定
	Conclusion    string `json:"conclusion"`       // 结论：法律评价

	// 风险评级（参考 claude-for-legal 三档制）
	RiskLevel   string `json:"risk_level"`             // 🟢 合规 / 🟡 提示 / 🔴 违规
	RiskSummary string `json:"risk_summary,omitempty"` // 风险概述

	// 修改建议
	Suggestion string `json:"suggestion,omitempty"`  // 如果存在风险，建议的修改方案
}

// AnalysisReport 完整分析报告。
type AnalysisReport struct {
	Title            string            `json:"title"`                         // 审查标题
	ContractType     string            `json:"contract_type"`                 // 合同类型
	OverallRisk      string            `json:"overall_risk"`                  // 整体风险评级
	ClauseAnalyses   []ClauseAnalysis `json:"clause_analyses,omitempty"`     // 逐条分析
	Summary          string            `json:"summary"`                       // 总体审查结论

	// AI 生成标识（《人工智能生成合成内容标识办法》合规）
	AIGenerated  bool   `json:"ai_generated"`    // 是否 AI 生成
	Disclaimer   string `json:"disclaimer"`      // 免责声明
}

func (p *ClauseAnalyzerPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "clause_analysis":
		return p.handleAnalysis(msg)
	default:
		return kernel.Message{}, fmt.Errorf("clause_analyzer_plugin: 未知消息类型 %s", msg.Type)
	}
}

func (p *ClauseAnalyzerPlugin) handleAnalysis(msg kernel.Message) (kernel.Message, error) {
	var input AnalysisInput
	if err := json.Unmarshal(msg.Payload, &input); err != nil {
		return kernel.Message{}, fmt.Errorf("clause_analysis: payload 解析失败: %w", err)
	}

	// 解包各数据源
	contract := input.Contract

	// 解析法律画像
	profile, err := p.parseProfile(input.Profile)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("clause_analysis: 画像解析失败: %w", err)
	}

	// 解析法律检索结果
	laws, err := p.parseLaws(input.Laws)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("clause_analysis: 法律检索结果解析失败: %w", err)
	}

	// 执行三段论分析
	report := p.applySyllogism(contract, profile, laws)

	payload, err := json.Marshal(report)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("clause_analysis: 报告序列化失败: %w", err)
	}

	return kernel.Message{
		Type:    "clause_analysis.result",
		Payload: payload,
	}, nil
}

/* parseProfile 解析法律画像。

   从 cold_start_plugin 输出的 JSON 字符串中提取合同类型和法律领域。
*/
func (p *ClauseAnalyzerPlugin) parseProfile(profileJSON string) (ColdStartProfile, error) {
	var profile ColdStartProfile
	if err := json.Unmarshal([]byte(profileJSON), &profile); err != nil {
		return ColdStartProfile{}, err
	}
	return profile, nil
}

/* parseLaws 解析法律检索结果。

   从 legal_search_plugin 输出的 JSON 字符串中提取法律引用。
*/
func (p *ClauseAnalyzerPlugin) parseLaws(lawsJSON string) (LegalSearchResult, error) {
	var result LegalSearchResult
	if err := json.Unmarshal([]byte(lawsJSON), &result); err != nil {
		// 如果解析失败，返回空结果
		return LegalSearchResult{}, nil
	}
	return result, nil
}

/* applySyllogism 使用三段论执行中国法律条款分析。

   三段论推理框架（替代 IRAC）：

   步骤1：识别可适用的法律规则 → 大前提
     - 遍历 contract 的约定事项
     - 匹配 legal_search 返回的法律法规
     - 提取相关法条原文作为大前提

   步骤2：提取合同具体约定 → 小前提
     - 定位具体条款的约定内容
     - 提取关键词和关键数据（金额、期限、条件等）

   步骤3：比较并得出结论 → 结论
     - 将合同约定与法律规定比对
     - 判断是否违反强制性规定
     - 判断是否存在法律风险
     - 给出风险评级和修改建议
*/
func (p *ClauseAnalyzerPlugin) applySyllogism(contract string, profile ColdStartProfile, laws LegalSearchResult) AnalysisReport {
	report := AnalysisReport{
		Title:        profile.ContractType + "法律审查报告",
		ContractType: profile.ContractType,
		OverallRisk:  "待评估",
		Summary:      "完成逐条分析后生成综合结论",
		AIGenerated:  true,
		Disclaimer:   generateDisclaimer(profile.UserRole),
	}

	// 构建三段论分析
	// 实际实现中，以下逻辑应由 LLM 或规则引擎完成
	// 当前骨架展示三段论的结构和风险识别框架

	// 步骤1：收集大前提（法律规则）
	majorPremises := p.collectMajorPremises(laws)

	// 步骤2：提取小前提（合同约定）
	minorPremises := p.collectMinorPremises(contract)

	// 步骤3：逐条推理
	for _, mp := range majorPremises {
		for _, np := range minorPremises {
			analysis := p.constructSyllogism(mp, np, contract)
			report.ClauseAnalyses = append(report.ClauseAnalyses, analysis)
		}
	}

	// 如果没有提取出具体条款，至少输出一个总体分析
	if len(report.ClauseAnalyses) == 0 {
		report.ClauseAnalyses = append(report.ClauseAnalyses, ClauseAnalysis{
			ClauseNumber: "总体",
			MajorPremise: "依据《中华人民共和国民法典》合同编相关规定",
			MinorPremise: contractSummary(contract),
			Conclusion:   "请提供合同全文以进行逐条分析",
			RiskLevel:    "🟡 提示",
		})
	}

	// 计算整体风险
	report.OverallRisk = computeOverallRisk(report.ClauseAnalyses)

	// 生成总体结论
	if len(report.ClauseAnalyses) > 0 {
		report.Summary = buildSummary(report.ClauseAnalyses, report.OverallRisk)
	}

	return report
}

/* collectMajorPremises 从法律检索结果中收集大前提。

   大前提 = 法律规则：
   - 效力性强制性规定（导致合同无效）
   - 管理性强制性规定（不影响合同效力但有行政责任）
   - 任意性规定（可被合同约定排除）
   - 指导性案例裁判规则
*/
func (p *ClauseAnalyzerPlugin) collectMajorPremises(laws LegalSearchResult) []string {
	var premises []string
	for _, statute := range laws.Statutes {
		if statute.Title != "" {
			premises = append(premises, statute.Title)
		}
	}
	if len(premises) == 0 {
		return []string{"《中华人民共和国民法典》合同编"}
	}
	return premises
}

/* collectMinorPremises 从合同文本中提取小前提。

   小前提 = 合同约定事实：
   - 违约责任条款
   - 损害赔偿限额
   - 管辖权条款
   - 保密条款
   - 知识产权条款
   - 终止条款
*/
func (p *ClauseAnalyzerPlugin) collectMinorPremises(contract string) []string {
	// TBD: 接入规则引擎或 LLM 做条款提取
	// 当前骨架返回空，由总体分析覆盖
	return nil
}

/* constructSyllogism 构建单条三段论。

   推理过程：
   大前提：法律规定 → "根据《民法典》第XXX条，违约金的约定不得超过实际损失的30%"
   小前提：合同约定 → "本合同约定违约金为合同金额的50%"
   结论：法律评价 → "该违约金条款可能因过高而被法院调减（🔴 违规）"
*/
func (p *ClauseAnalyzerPlugin) constructSyllogism(majorPremise, minorPremise, contract string) ClauseAnalysis {
	return ClauseAnalysis{
		ClauseNumber: "待识别",
		MajorPremise: majorPremise,
		MinorPremise: minorPremise,
		Conclusion:   "待分析",
		RiskLevel:    "🟡 提示",
	}
}

/* computeOverallRisk 综合评估整体风险。

   规则：存在任何 🔴 违规条款 → 整体违规
         存在 🟡 提示条款 → 整体提示
         全部 🟢 → 整体合规
*/
func computeOverallRisk(analyses []ClauseAnalysis) string {
	hasRed := false
	hasYellow := false

	for _, a := range analyses {
		switch a.RiskLevel {
		case "🔴 违规":
			hasRed = true
		case "🟡 提示":
			hasYellow = true
		}
	}

	switch {
	case hasRed:
		return "🔴 违规（存在违反法律强制性规定的条款）"
	case hasYellow:
		return "🟡 提示（存在需要注意的法律风险）"
	default:
		return "🟢 合规（未发现法律风险）"
	}
}

/* buildSummary 生成总体审查结论。 */
func buildSummary(analyses []ClauseAnalysis, risk string) string {
	redCount := 0
	yellowCount := 0
	greenCount := 0

	for _, a := range analyses {
		switch a.RiskLevel {
		case "🔴 违规":
			redCount++
		case "🟡 提示":
			yellowCount++
		default:
			greenCount++
		}
	}

	summary := fmt.Sprintf("审查结果：共分析 %d 项。", len(analyses))
	if redCount > 0 {
		summary += fmt.Sprintf("其中 %d 项违规（需修改），", redCount)
	}
	if yellowCount > 0 {
		summary += fmt.Sprintf("%d 项存在风险（建议关注），", yellowCount)
	}
	if greenCount > 0 {
		summary += fmt.Sprintf("%d 项合规。", greenCount)
	}
	summary += " " + risk
	summary += "。本报告由 AI 辅助生成，仅供法律专业人员参考，不构成法律意见。"

	return summary
}

/* generateDisclaimer 根据用户角色生成免责声明。

   参考 claude-for-legal 的输出角色分级模式。
   适配中国法律背景下 AI 辅助生成的法律文书的免责要求。
*/
func generateDisclaimer(userRole string) string {
	switch userRole {
	case "律师":
		return "本报告由 AI 辅助生成，供执业律师参考使用。使用前请核实法律条文的最新有效性。" +
			"AI 生成内容不代表律师的专业法律意见，律师应对本报告内容进行独立判断和核实。"
	case "法务":
		return "本报告由 AI 辅助生成，供企业法务参考使用。涉及重大法律决策时，建议咨询外部律师。" +
			"AI 生成内容不构成法律意见。"
	case "个人":
		return "本报告由 AI 辅助生成，仅供参考，不构成法律意见。涉及具体法律纠纷时，建议咨询执业律师。" +
			"本报告不得作为诉讼或仲裁依据。根据《人工智能生成合成内容标识办法》，本内容由 AI 生成。"
	default:
		return "本报告由 AI 辅助生成，仅供法律专业人员参考，不构成法律意见。" +
			"使用前请核实相关法律条文的最新状态。根据《人工智能生成合成内容标识办法》，本内容由 AI 生成。"
	}
}

/* contractSummary 截取合同文本摘要。 */
func contractSummary(contract string) string {
	runes := []rune(contract)
	if len(runes) > 200 {
		return string(runes[:200]) + "..."
	}
	return contract
}
