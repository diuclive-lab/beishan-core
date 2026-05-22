package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

/* ─── code_security_check L3 工具 ──────────────

   扫描 diff / 代码变更中的危险模式。
   纯正则匹配，不依赖 LLM。

   硬化边界：
   - 危险命令（rm -rf、强制删除）
   - 路径穿越（../）
   - 敏感路径写入（/etc/、/dev/ 等）
   - 命令注入（exec.Command + 变量拼接）
   - 不安全权限（chmod 777）
   - 自修改防护（写入 beishan-core 自身关键文件）
*/

type SecurityIssue struct {
	Line        int    `json:"line,omitempty"`     // 行号（如有）
	Severity    string `json:"severity"`           // block / warning / info
	Rule        string `json:"rule"`               // 规则名
	Description string `json:"description"`        // 问题描述
	Snippet     string `json:"snippet,omitempty"`  // 匹配到的代码片段
}

type SecurityCheckResult struct {
	Safe    bool             `json:"safe"`    // true=通过, false=有阻止性问题
	Blocked []SecurityIssue  `json:"blocked"` // 阻止性问题
	Warnings []SecurityIssue `json:"warnings"` // 警告性问题
	Infos   []SecurityIssue  `json:"infos"`   // 提示性问题
}

// securityRule 一条安全检查规则
type securityRule struct {
	Name        string          // 规则名
	Severity    string          // block / warning / info
	Description string          // 描述
	Pattern     *regexp.Regexp  // 匹配模式
	Exclude     *regexp.Regexp  // 排除模式（命中此模式则跳过）
	MatchAll    bool            // 是否要求全文匹配（而非行匹配）
}

var securityRules = []securityRule{
	// ── 阻止性规则（block）──────────────────────
	{
		Name:        "dangerous_rm_rf",
		Severity:    "block",
		Description: "危险删除命令: rm -rf 与变量路径拼接可能导致意外删除",
		Pattern:     regexp.MustCompile(`rm\s+-[^'"\n]*rf|os\.RemoveAll\s*\(\s*[^"'\d]`),
		Exclude:     regexp.MustCompile(`rm\s+-rf\s+["']/tmp/|os\.RemoveAll\s*\(\s*["']/tmp/`),
	},
	{
		Name:        "path_traversal",
		Severity:    "block",
		Description: "路径穿越: 检测到 ../ 路径可能导致越权访问",
		Pattern:     regexp.MustCompile(`filepath\.Join\([^)]*\.\./|\.\./|\.\.\\`),
		Exclude:     regexp.MustCompile(`test|_test\.go`),
	},
	{
		Name:        "sensitive_path_write",
		Severity:    "block",
		Description: "检测到写入系统敏感路径: /etc/ /dev/ /proc/ /usr/",
		Pattern:     regexp.MustCompile(`"/etc/|"/dev/|"/proc/|"/usr/|"/sys/|"/boot/|WriteFile\([^)]*"/etc/|WriteFile\([^)]*"/dev/`),
	},
	{
		Name:        "dangerous_permissions",
		Severity:    "block",
		Description: "不安全的文件权限: chmod 777/0777 或 os.FileMode(0777)",
		Pattern:     regexp.MustCompile(`chmod\s+0?777|FileMode\(0?777\)|0?777`),
		Exclude:     regexp.MustCompile(`example|test|_test\.go`),
	},
	{
		Name:        "self_modification",
		Severity:    "block",
		Description: "自修改防护: 检测到写入 beishan-core 关键源代码文件",
		Pattern:     regexp.MustCompile(`internal/tools/(knowledge|tools|validate|schema).go|kernel/kernel\.go|kernel/router\.go`),
		MatchAll:    true,
	},

	// ── 警告性规则（warning）────────────────────
	{
		Name:        "command_injection",
		Severity:    "warning",
		Description: "命令注入风险: exec.Command 参数包含变量拼接",
		Pattern:     regexp.MustCompile(`exec\.Command\([^)]*\+\s*[a-zA-Z]|exec\.Command\([^)]*fmt\.Sprintf`),
	},
	{
		Name:        "unsafe_write_path",
		Severity:    "warning",
		Description: "写入路径使用了变量拼接，可能存在路径穿越风险",
		Pattern:     regexp.MustCompile(`WriteFile\([^)]*\+|os\.Create\([^)]*\+`),
	},

	// ── 提示性规则（info）──────────────────────
	{
		Name:        "hardcoded_secret",
		Severity:    "info",
		Description: "检测到可能的硬编码密钥或 Token",
		Pattern:     regexp.MustCompile(`api_key\s*=\s*["'][A-Za-z0-9]{32,}|apiKey\s*[:=]\s*["'][A-Za-z0-9]{32,}`),
		Exclude:     regexp.MustCompile(`_test\.go|example|\.env`),
	},
}

func CodeSecurityCheckHandler(args map[string]interface{}) *ToolResult {
	diff, _ := args["diff"].(string)
	filepath_, _ := args["filepath"].(string)

	if diff == "" && filepath_ == "" {
		return errorResult("diff 或 filepath 至少需要一个")
	}

	content := diff
	if content == "" && filepath_ != "" {
		// 从文件读取内容
		data, err := readFileContents(filepath_)
		if err != nil {
			return errorResult(fmt.Sprintf("读取文件失败: %v", err))
		}
		content = data
	}

	if content == "" {
		return errorResult("检测内容为空")
	}

	result := SecurityCheckResult{
		Safe:     true,
		Blocked:  []SecurityIssue{},
		Warnings: []SecurityIssue{},
		Infos:    []SecurityIssue{},
	}

	lines := strings.Split(content, "\n")

	for _, rule := range securityRules {
		if rule.MatchAll {
			// 全文匹配
			if rule.Pattern.MatchString(content) {
				if rule.Exclude != nil && rule.Exclude.MatchString(content) {
					continue
				}
				issue := SecurityIssue{
					Severity:    rule.Severity,
					Rule:       rule.Name,
					Description: rule.Description,
					Snippet:    truncateStr(rule.Pattern.FindString(content), 80),
				}
				addIssue(&result, issue)
			}
			continue
		}

		// 逐行匹配
		for i, line := range lines {
			if !rule.Pattern.MatchString(line) {
				continue
			}
			if rule.Exclude != nil && rule.Exclude.MatchString(line) {
				continue
			}

			issue := SecurityIssue{
				Line:        i + 1,
				Severity:    rule.Severity,
				Rule:       rule.Name,
				Description: rule.Description,
				Snippet:    truncateStr(strings.TrimSpace(line), 80),
			}
			addIssue(&result, issue)
		}
	}

	// 有 block 级别问题时标记不安全
	if len(result.Blocked) > 0 {
		result.Safe = false
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func addIssue(result *SecurityCheckResult, issue SecurityIssue) {
	switch issue.Severity {
	case "block":
		result.Blocked = append(result.Blocked, issue)
	case "warning":
		result.Warnings = append(result.Warnings, issue)
	default:
		result.Infos = append(result.Infos, issue)
	}
}

// readFileContents 读取文件内容，带路径安全性检查
func readFileContents(path string) (string, error) {
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return "", fmt.Errorf("路径包含 ..，已拒绝")
	}
	data, err := os.ReadFile(clean)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func registerCodeSecurityTools() {
	Register("code_security_check", "扫描代码变更/diff 中的安全风险。纯正则匹配，不依赖 LLM。支持 block/warning/info 三级。",
		map[string]interface{}{
			"type":     "object",
			"properties": map[string]interface{}{
				"diff":     stringParam("要扫描的 diff 或代码内容"),
				"filepath": stringParam("要扫描的文件路径（与 diff 二选一）"),
			},
		},
		CodeSecurityCheckHandler,
	)
}
