package tools

import (
	"fmt"
	"strings"
	"time"

	"beishan/internal/llm"
)

func registerUsageTools() {
	Register("usage_report", "查看 LLM 调用成本统计。按日期汇总 token 消耗，支持按调用方和模型分类。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{
				"date": stringParam("日期，格式 2006-01-02，默认今天"),
				"days": intParam("查看最近 N 天汇总，默认 1"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			date, _ := args["date"].(string)
			days := 1
			if d, ok := args["days"].(float64); ok && d > 0 {
				days = int(d)
			}

			if date == "" && days == 1 {
				date = time.Now().Format("2006-01-02")
			}

			var summaries []llm.UsageSummary
			if days > 1 {
				for i := 0; i < days; i++ {
					d := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
					s := llm.SummarizeUsage(d)
					if s.Records > 0 {
						summaries = append(summaries, s)
					}
				}
			} else {
				s := llm.SummarizeUsage(date)
				summaries = append(summaries, s)
			}

			if len(summaries) == 0 {
				return successResult("无使用记录。")
			}

			var sb strings.Builder
			totalCalls := 0
			totalTokens := 0
			callerAgg := make(map[string]int)
			modelAgg := make(map[string]int)

			for _, s := range summaries {
				totalCalls += s.TotalCalls
				totalTokens += s.TotalTokens
				for k, v := range s.ByCaller {
					callerAgg[k] += v
				}
				for k, v := range s.ByModel {
					modelAgg[k] += v
				}
			}

			sb.WriteString(fmt.Sprintf("## LLM 使用统计（最近 %d 天）\n\n", len(summaries)))
			sb.WriteString(fmt.Sprintf("- 总调用次数: %d\n", totalCalls))
			sb.WriteString(fmt.Sprintf("- 总 token 消耗: %s\n", formatTokenCount(totalTokens)))

			if len(callerAgg) > 0 {
				sb.WriteString("\n### 按调用方\n")
				for caller, tokens := range callerAgg {
					sb.WriteString(fmt.Sprintf("- %s: %s\n", caller, formatTokenCount(tokens)))
				}
			}

			if len(modelAgg) > 0 {
				sb.WriteString("\n### 按模型\n")
				for model, tokens := range modelAgg {
					sb.WriteString(fmt.Sprintf("- %s: %s\n", model, formatTokenCount(tokens)))
				}
			}

			if len(summaries) > 1 {
				sb.WriteString("\n### 每日明细\n")
				for _, s := range summaries {
					sb.WriteString(fmt.Sprintf("- %s: %d 次, %s tokens\n", s.Date, s.TotalCalls, formatTokenCount(s.TotalTokens)))
				}
			}

			return successResult(sb.String())
		},
	)
}

func formatTokenCount(n int) string {
	if n >= 1000000 {
		return fmt.Sprintf("%.1fM", float64(n)/1000000)
	}
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
