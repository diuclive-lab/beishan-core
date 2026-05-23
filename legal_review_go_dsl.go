package main

import (
	"time"

	"beishan/internal/workflow"
	"beishan/kernel"
)

// legalReviewGoWorkflow 是 legal_review 的 Go-DSL 声明式版本，
// 等价于 workflows/legal_review.yaml，但拥有编译时类型安全。
func registerLegalReviewGoDSL(k *kernel.Kernel, toolHost map[string]string, ensureTool func(string) bool) {
	lrWorkflow := workflow.GoWorkflow{
		Name:       "legal_review",
		ResultStep: "write_report",
		Steps: []workflow.GoStep{
			{
				ID:   "cold_start",
				Type: workflow.GoStepPlugin,
				Recipient: "cold_start_plugin",
				MsgType:   "cold_start",
				PluginTimeout: 30 * time.Second,
				Input: &workflow.GoStepInput{
					RawInputKeys: map[string]string{"contract_text": "input"},
				},
				OutputVar: "cold_start",
				OnError:   workflow.ErrorFailWorkflow,
			},
			{
				ID:   "legal_search",
				Type: workflow.GoStepPlugin,
				Recipient: "legal_search_plugin",
				MsgType:   "legal_search",
				PluginTimeout: 30 * time.Second,
				RetryDelay:    time.Second,
				MaxRetries:    1,
				Input: &workflow.GoStepInput{
					RawInputKeys: map[string]string{"query": "cold_start"},
				},
				OutputVar: "legal_search",
				OnError:   workflow.ErrorFailWorkflow,
			},
			{
				ID:   "clause_analysis",
				Type: workflow.GoStepPlugin,
				Recipient: "clause_analyzer_plugin",
				MsgType:   "clause_analysis",
				PluginTimeout: 30 * time.Second,
				Input: &workflow.GoStepInput{
					Merge: []workflow.GoInputSource{
						{Key: "contract", Value: "${input}"},
						{Key: "profile", Step: "cold_start", Field: "output"},
						{Key: "laws", Step: "legal_search", Field: "output"},
					},
				},
				OutputVar: "clause_analysis",
				OnError:   workflow.ErrorFailWorkflow,
			},
			{
				ID:   "write_report",
				Type: workflow.GoStepPlugin,
				Recipient: "legal_write_plugin",
				MsgType:   "legal_write",
				PluginTimeout: 30 * time.Second,
				Input: &workflow.GoStepInput{
					Merge: []workflow.GoInputSource{
						{Key: "contract", Value: "${input}"},
						{Key: "profile", Step: "cold_start", Field: "output"},
						{Key: "analysis", Step: "clause_analysis", Field: "output"},
					},
				},
				OutputVar: "write_report",
			},
		},
	}

	plugin := workflow.NewGoWorkflowPlugin(k, toolHost, ensureTool, map[string]workflow.GoWorkflow{
		"legal_review": lrWorkflow,
	})
	k.Register("legal_review_v2_plugin", plugin, kernel.Meta{
		Description: "法律审查编排(Go-DSL版)，四步：冷启动→检索→分析→报告",
		Tags:        []string{"legal", "workflow"},
		Types:       []string{"legal_review"},
	})
}
