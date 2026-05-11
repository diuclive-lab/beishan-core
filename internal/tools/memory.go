package tools

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var memoryThreatPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)ignore\s+(all\s+)?(previous|above|prior|foregoing)\s+(instructions?|directions?|prompts?|messages?)`),
	regexp.MustCompile(`(?i)system\s*prompt\s*(override|reset|change|replace|delete)`),
	regexp.MustCompile(`(?i)you\s+are\s+now\s+(a\s+)?different`),
	regexp.MustCompile(`(?i)<\|im_start\|>`),
	regexp.MustCompile(`(?i)<\|im_end\|>`),
}

func scanMemoryThreats(content string) bool {
	for _, p := range memoryThreatPatterns {
		if p.MatchString(content) {
			return true
		}
	}
	return false
}

func MemoryRead() *ToolResult {
	os.MkdirAll(MemoryDir, 0755)
	path := filepath.Join(MemoryDir, "MEMORY.md")
	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		return successResult("[memory] No entries yet.")
	}
	return successResult("[memory]\n" + string(data))
}

func MemoryAdd(content string) *ToolResult {
	if content == "" {
		return errorResult("content is required")
	}
	if scanMemoryThreats(content) {
		return errorResult("content blocked by threat scanner")
	}
	os.MkdirAll(MemoryDir, 0755)
	path := filepath.Join(MemoryDir, "MEMORY.md")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	defer f.Close()
	entry := fmt.Sprintf("\n§ %s\n", content)
	if _, err := f.WriteString(entry); err != nil {
		return errorResult(fmt.Sprintf("write memory: %v", err))
	}
	return successResult(fmt.Sprintf("Saved to memory: %s", truncateStr(content, 200)))
}

func MemorySearch(query string) *ToolResult {
	os.MkdirAll(MemoryDir, 0755)
	path := filepath.Join(MemoryDir, "MEMORY.md")
	data, _ := os.ReadFile(path)
	lines := strings.Split(string(data), "\n")

	var results []string
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), strings.ToLower(query)) {
			results = append(results, line)
		}
	}

	if len(results) == 0 {
		return successResult("No matching memory entries.")
	}
	return successResult(strings.Join(results, "\n"))
}
