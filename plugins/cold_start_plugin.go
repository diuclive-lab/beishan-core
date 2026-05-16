package plugins

import (
	"encoding/json"
	"fmt"

	"beishan/kernel"
)

/* ColdStartPlugin 冷启动访谈插件（L3）。

   职责：接收用户输入的合同原文或法律需求，识别以下维度：
   - 合同类型（买卖合同、服务合同、租赁合同等）
   - 法律领域（合同法、劳动法、知识产权法等）
   - 当事人角色（甲方/乙方、权利人/义务人）
   - 管辖法律（中国大陆、香港、涉外）
   - 用户身份（律师、法务、个人）

   输出：结构化法律画像（practice profile），供 downstream L3 插件使用。

   参考 claude-for-legal 的 cold-start-interview 模式，
   适配中国法律语境，使用本土法律术语和分类体系。
*/
type ColdStartPlugin struct{}

// ColdStartProfile 冷启动输出的法律画像结构。
type ColdStartProfile struct {
	ContractType    string   `json:"contract_type"`              // 合同类型：买卖合同/服务合同/租赁合同/NDA/其他
	LegalArea       string   `json:"legal_area"`                 // 法律领域：合同法/劳动法/知识产权法/公司法
	PartyRole       string   `json:"party_role"`                 // 当事人角色：甲方/乙方/独立
	Jurisdiction    string   `json:"jurisdiction"`               // 管辖法律：中国大陆/香港/涉外
	UserRole        string   `json:"user_role"`                  // 用户身份：律师/法务/个人/企业
	ContractSummary string   `json:"contract_summary"`           // 合同摘要（2-3句）
	KeyTerms        []string `json:"key_terms,omitempty"`        // 识别出的关键风险领域
	ApplicableLaws  []string `json:"applicable_laws,omitempty"`  // 可能适用的中国法律法规
	RawText         string   `json:"raw_text,omitempty"`         // 原文存档
}

func (p *ColdStartPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "cold_start":
		return p.handleColdStart(msg)
	default:
		return kernel.Message{}, fmt.Errorf("cold_start_plugin: 未知消息类型 %s", msg.Type)
	}
}

func (p *ColdStartPlugin) handleColdStart(msg kernel.Message) (kernel.Message, error) {
	// 解析输入：可能是纯文本合同，也可能是结构化查询
	var input map[string]interface{}
	if err := json.Unmarshal(msg.Payload, &input); err != nil {
		// 无法解析为对象 → 当作原始合同文本处理
		var rawText string
		if err := json.Unmarshal(msg.Payload, &rawText); err != nil {
			return kernel.Message{}, fmt.Errorf("cold_start: payload 必须是 JSON 对象或字符串 (%w)", err)
		}
		input = map[string]interface{}{
			"contract_text": rawText,
		}
	}

	contractText, _ := input["contract_text"].(string)

	// 构建法律画像
	profile := p.buildProfile(contractText, input)

	payload, err := json.Marshal(profile)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("cold_start: 序列化画像失败: %w", err)
	}

	return kernel.Message{
		Type:    "cold_start.result",
		Payload: payload,
	}, nil
}

/* buildProfile 从输入中提取法律画像。

   参照 claude-for-legal 实践画像模板，使用中国法律分类体系：
   - 合同类型：按《民法典》合同编典型合同分类
   - 法律领域：按中国部门法体系
   - 关键风险：中国合同审查实务常见风险点
*/
func (p *ColdStartPlugin) buildProfile(contractText string, input map[string]interface{}) ColdStartProfile {
	// 默认值
	profile := ColdStartProfile{
		ContractType:   inferChineseContractType(contractText, input),
		LegalArea:      "合同法", // 默认为合同法
		PartyRole:      inferPartyRole(input),
		Jurisdiction:   "中国大陆",
		UserRole:       inferUserRole(input),
		ContractSummary: generateChineseSummary(contractText, input),
		KeyTerms:       inferChineseKeyTerms(contractText),
		ApplicableLaws: inferApplicableChineseLaws(contractText, input),
		RawText:        contractText,
	}

	// 如果用户明确指定了字段，优先使用
	if v, ok := input["legal_area"].(string); ok {
		profile.LegalArea = v
	}
	if v, ok := input["jurisdiction"].(string); ok {
		profile.Jurisdiction = v
	}
	if v, ok := input["contract_type"].(string); ok {
		profile.ContractType = v
	}
	if v, ok := input["party_role"].(string); ok {
		profile.PartyRole = v
	}

	return profile
}

/* inferChineseContractType 根据合同文本推断合同类型。

   使用《民法典》合同编典型合同分类：
   - 买卖合同（第595条-第647条）
   - 供用电水气热力合同（第648条-第656条）
   - 赠与合同（第657条-第666条）
   - 借款合同（第667条-第680条）
   - 保证合同（第681条-第702条）
   - 租赁合同（第703条-第734条）
   - 融资租赁合同（第735条-第760条）
   - 保理合同（第761条-第769条）
   - 承揽合同（第770条-第787条）
   - 建设工程合同（第788条-第808条）
   - 运输合同（第809条-第842条）
   - 技术合同（第843条-第887条）
   - 保管合同（第888条-第903条）
   - 仓储合同（第904条-第916条）
   - 委托合同（第919条-第936条）
   - 物业服务合同（第937条-第950条）
   - 行纪合同（第951条-第958条）
   - 中介合同（第959条-第966条）
   - 合伙合同（第967条-第978条）

   实际实现中应使用关键词匹配或 LLM 调用。
   此处为确定性规则引擎，后续可扩展。
*/
func inferChineseContractType(text string, input map[string]interface{}) string {
	if v, ok := input["contract_type"].(string); ok {
		return v
	}
	return "合同审查" // 默认：通用合同审查
}

/* inferPartyRole 推断当事人在合同中的角色。

   中国合同实务中，"甲方/乙方"是最常见的角色分类。
   在部分场景中指代"权利人/义务人"或"委托方/受托方"。
*/
func inferPartyRole(input map[string]interface{}) string {
	if v, ok := input["party_role"].(string); ok {
		return v
	}
	return "待确认" // 需人工确认
}

/* inferUserRole 推断用户身份。

   与 claude-for-legal 的 Role 分类一致：
   律师/法务/个人/企业，影响输出格式和免责声明。
*/
func inferUserRole(input map[string]interface{}) string {
	if v, ok := input["user_role"].(string); ok {
		return v
	}
	return "法律专业人员" // 默认假设
}

/* generateChineseSummary 生成简短的中文摘要。

   使用"2-3句"原则，参考 claude-for-legal 的摘要风格。
*/
func generateChineseSummary(text string, input map[string]interface{}) string {
	if v, ok := input["summary"].(string); ok {
		return v
	}
	if text == "" {
		return "待分析"
	}
	// 截取前100个字符作为默认摘要
	runes := []rune(text)
	if len(runes) > 100 {
		return string(runes[:100]) + "..."
	}
	return text
}

/* inferChineseKeyTerms 识别常见中国合同风险领域。

   参考 claude-for-legal 的 playbook 风险领域分类，
   适配中国合同审查实务常见风险点：
   - 违约责任（违约金、赔偿范围）
   - 管辖权（中国法院 vs 仲裁）
   - 保密义务
   - 知识产权归属
   - 竞业限制
   - 数据合规（《个人信息保护法》）
   - 格式条款（《民法典》第496-498条）
*/
func inferChineseKeyTerms(text string) []string {
	// 基于关键词匹配的初始风险识别
	// 后续可接入 LLM
	return nil // 初始为空，由后续分析步骤填充
}

/* inferApplicableChineseLaws 推断可能适用的中国法律法规。

   返回常见的中国法律体系引用，供 legal_search_plugin 使用。
*/
func inferApplicableChineseLaws(text string, input map[string]interface{}) []string {
	if v, ok := input["applicable_laws"].([]interface{}); ok {
		laws := make([]string, 0, len(v))
		for _, l := range v {
			if s, ok := l.(string); ok {
				laws = append(laws, s)
			}
		}
		if len(laws) > 0 {
			return laws
		}
	}

	// 默认引用清单
	laws := []string{
		"《中华人民共和国民法典》",
		"《中华人民共和国合同法》", // 民法典合同编已吸收，但实务中仍常用
	}
	return laws
}
