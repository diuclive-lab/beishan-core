package plugins

import (
	"encoding/json"
	"fmt"
	"time"

	"beishan/kernel"
)

/* LegalReviewPlugin 是 L4 法律审查编排插件。

   职责：串联"访谈→检索→分析→生成"四步流程，不执行具体逻辑。
   每个步骤通过 kernel.Call() 调用 L3 插件，Type 明确告知 Router 转发目标。

   使用方式：
     kernel.Register("legal_review_plugin", &LegalReviewPlugin{kernel: k})
*/
type LegalReviewPlugin struct {
	Kernel *kernel.Kernel
}

func (p *LegalReviewPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	if msg.Type != "legal_review" {
		return kernel.Message{}, fmt.Errorf("legal_review: 未知消息类型 %s", msg.Type)
	}

	// ─── 步骤 1：冷启动访谈 ─────────────────────────
	// 调 L3 cold_start_plugin，收集用户需求
	profile, err := p.Kernel.Call(kernel.Message{
		Recipient: "cold_start_plugin",
		Sender:    "legal_review_plugin",
		Type:      "cold_start",
		Payload:   msg.Payload,
	}, 60*time.Second)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("访谈失败: %w", err)
	}

	// ─── 步骤 2：法律检索 ─────────────────────────
	// 调 L3 legal_search_plugin，查询相关法条
	laws, err := p.Kernel.Call(kernel.Message{
		Recipient: "legal_search_plugin",
		Sender:    "legal_review_plugin",
		Type:      "legal_search",
		Payload:   profile.Payload,
	}, 30*time.Second)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("法律检索失败: %w", err)
	}

	// ─── 步骤 3：条款分析 ─────────────────────────
	// 携带合同原文、访谈画像、相关法条，调 L3 clause_analyzer_plugin
	analysisPayload, _ := json.Marshal(map[string]interface{}{
		"contract": string(msg.Payload),
		"profile":  string(profile.Payload),
		"laws":     string(laws.Payload),
	})

	analysis, err := p.Kernel.Call(kernel.Message{
		Recipient: "clause_analyzer_plugin",
		Sender:    "legal_review_plugin",
		Type:      "clause_analysis",
		Payload:   analysisPayload,
	}, 30*time.Second)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("条款分析失败: %w", err)
	}

	// ─── 步骤 4：生成审查报告 ─────────────────────
	// 调 L3 legal_write_plugin，渲染中文法律文书
	_, err = p.Kernel.Call(kernel.Message{
		Recipient: "legal_write_plugin",
		Sender:    "legal_review_plugin",
		Type:      "legal_generate_report",
		Payload:   analysis.Payload,
	}, 30*time.Second)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("报告生成失败: %w", err)
	}

	// ─── 返回最终结果 ────────────────────────────
	return kernel.Message{
		Sender:  "legal_review_plugin",
		Type:    "legal_review.result",
		Payload: analysis.Payload,
	}, nil
}
