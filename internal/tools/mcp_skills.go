package tools

import (
	"fmt"
	"strings"

	"beishan/internal/mcp"
)

var mcpRunner *mcp.SkillRunner

// SetMCPRunner sets the global MCP skill runner. Called from main.go.
func SetMCPRunner(runner *mcp.SkillRunner) {
	mcpRunner = runner
}

func RegisterMCPSkills() {
	for _, s := range mcp.List() {
		name := s.ID
		desc := s.Description
		// Agent-only: MCP skills are NOT exposed to think_plugin or right flowers.
		// Only internal/agent/ can call these via ExecuteAgentTool / HasAgentTool.
		RegisterAgentOnly("skill_"+name, desc,
			map[string]interface{}{
				"type":     "object",
				"required": []string{"prompt"},
				"properties": map[string]interface{}{
					"prompt": stringParam("The task or question for this skill"),
				},
			},
			mcpSkillHandler(name),
		)
	}
}

func mcpSkillHandler(skillID string) ToolHandler {
	return func(args map[string]interface{}) *ToolResult {
		prompt := strArg(args, "prompt")
		if prompt == "" {
			return errorResult("prompt is required")
		}
		if mcpRunner == nil {
			return errorResult("MCP not initialized")
		}

		// Call the skill's primary tool with the prompt
		result, err := mcpRunner.Call(skillID, "market_research", map[string]interface{}{
			"topic": prompt,
		})
		if err != nil {
			// Fall back to a simpler call
			errMsg := err.Error()
			if strings.Contains(errMsg, "market_research") {
				// Try quant_report instead
				result2, err2 := mcpRunner.Call(skillID, "quant_report", map[string]interface{}{
					"symbol": prompt,
				})
				if err2 != nil {
					return errorResult(fmt.Sprintf("skill error: %v / %v", err, err2))
				}
				return successResult(result2)
			}
			return errorResult(fmt.Sprintf("skill error: %v", err))
		}
		return successResult(result)
	}
}
