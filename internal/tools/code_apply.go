package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

/* ─── code_apply L3 工具 ────────────────────────

   受控写入文件。硬化层：
   - 路径规范化 + 穿越检测 + 范围限制（同 code_read）
   - 自动备份原文件（.bak.时间戳）
   - 前置检查：必须经过 code_security_check
   - 回滚支持
   - 自修改防护
*/

func CodeApplyHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)
	skipCheck, _ := args["skip_security_check"].(bool)

	if path == "" || content == "" {
		return errorResult("path 和 content 不能为空")
	}

	root := getProjectRoot()
	if root == "" {
		return errorResult("无法确定项目根目录")
	}

	// 路径规范化
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	clean := filepath.Clean(path)

	// 穿越检测
	if strings.Contains(clean, "..") {
		return errorResult("路径包含 ..，已拒绝")
	}

	// 相对路径拼接项目根目录
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(root, clean)
	}

	// 范围检测
	rel, err := filepath.Rel(root, clean)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errorResult("路径超出项目范围")
	}

	// 自修改防护：不能写入 beishan-core 关键源码
	selfModPaths := []string{
		"internal/tools/knowledge.go", "internal/tools/tools.go",
		"internal/tools/validate.go", "internal/tools/schema_registry.go",
		"kernel/kernel.go", "kernel/router.go", "kernel/msg.go",
	}
	for _, smp := range selfModPaths {
		if strings.HasSuffix(clean, smp) {
			return errorResult(fmt.Sprintf("自修改防护：禁止写入 %s", smp))
		}
	}

	// 安全检查（默认开启）
	if !skipCheck {
		checkArgs := map[string]interface{}{
			"diff": content,
		}
		checkResult := CodeSecurityCheckHandler(checkArgs)
		if !checkResult.Success {
			return errorResult(fmt.Sprintf("安全检查失败: %s", checkResult.Output))
		}
		var check SecurityCheckResult
		if err := json.Unmarshal([]byte(checkResult.Output), &check); err == nil {
			if !check.Safe {
				return errorResult(fmt.Sprintf("❌ 安全检查未通过：%d 个阻止性问题, %d 个警告",
					len(check.Blocked), len(check.Warnings)))
			}
		}
	}

	// 自动备份
	if _, err := os.Stat(clean); err == nil {
		backupDir := filepath.Join(root, ".backup")
		os.MkdirAll(backupDir, 0755)
		relPath := strings.ReplaceAll(rel, "/", "_")
		backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%d.bak", relPath, time.Now().Unix()))
		if origData, err := os.ReadFile(clean); err == nil {
			os.WriteFile(backupPath, origData, 0644)
		}
	}

	// 写入
	os.MkdirAll(filepath.Dir(clean), 0755)
	if err := os.WriteFile(clean, []byte(content), 0644); err != nil {
		return errorResult(fmt.Sprintf("写入失败: %v", err))
	}

	result := map[string]interface{}{
		"path":    clean,
		"size":    len(content),
		"message": fmt.Sprintf("已写入 %s（%d bytes）", clean, len(content)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func CodeRollbackHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult("path 不能为空")
	}

	root := getProjectRoot()
	if root == "" {
		return errorResult("无法确定项目根目录")
	}

	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return errorResult("路径包含 ..，已拒绝")
	}

	if !filepath.IsAbs(clean) {
		clean = filepath.Join(root, clean)
	}

	rel, err := filepath.Rel(root, clean)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errorResult("路径超出项目范围")
	}

	backupDir := filepath.Join(root, ".backup")
	relPath := strings.ReplaceAll(rel, "/", "_")

	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return errorResult("没有找到备份文件")
	}

	// 找到最新的备份
	var latest string
	var latestTime int64
	for _, e := range entries {
		var ts int64
		if _, err := fmt.Sscanf(e.Name(), relPath+".%d.bak", &ts); err == nil && ts > latestTime {
			latestTime = ts
			latest = e.Name()
		}
	}

	if latest == "" {
		return errorResult("没有找到备份文件")
	}

	backupData, err := os.ReadFile(filepath.Join(backupDir, latest))
	if err != nil {
		return errorResult(fmt.Sprintf("读取备份失败: %v", err))
	}

	if err := os.WriteFile(clean, backupData, 0644); err != nil {
		return errorResult(fmt.Sprintf("回滚写入失败: %v", err))
	}

	return successResult(fmt.Sprintf(`{"path":"%s","backup":"%s","message":"已回滚到备份 %s"}`, clean, latest, latest))
}

func registerCodeApplyTools() {
	Register("code_apply", "受控写入文件。自动安全检查 + 备份 + 自修改防护。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path", "content"},
			"properties": map[string]interface{}{
				"path":               stringParam("文件路径（相对项目根目录或绝对路径）"),
				"content":            stringParam("要写入的内容"),
				"skip_security_check": boolParam("跳过安全检查（默认 false）"),
			},
		},
		CodeApplyHandler,
	)

	Register("code_rollback", "回滚文件到最近的备份版本。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("要回滚的文件路径"),
			},
		},
		CodeRollbackHandler,
	)
}
