package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

/* ─── code_read L3 工具 ─────────────────────────

   受控文件读取。硬化层：
   - 路径规范化（filepath.Clean）
   - 路径穿越检测（../）
   - 范围限制（必须在项目根目录内）
   - 纯内容返回，无 AI 解释
*/

var projectRoot string

func getProjectRoot() string {
	if projectRoot != "" {
		return projectRoot
	}
	// 尝试获取项目根目录（从 HermesHome 向上找或当前目录）
	wd, _ := os.Getwd()
	if wd != "" {
		projectRoot = wd
	}
	return projectRoot
}

func CodeReadHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult("path 不能为空")
	}

	root := getProjectRoot()
	if root == "" {
		return errorResult("无法确定项目根目录")
	}

	// 展开 ~/
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}

	// 规范化路径
	clean := filepath.Clean(path)
	if clean == "." || clean == "/" {
		return errorResult("无效的路径")
	}

	// 路径穿越检测
	if strings.Contains(clean, "..") {
		return errorResult("路径包含 ..，已拒绝")
	}

	// 如果是相对路径，拼接项目根目录
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(root, clean)
	}

	// 范围检测：必须在项目根目录下
	rel, err := filepath.Rel(root, clean)
	if err != nil || strings.HasPrefix(rel, "..") {
		return errorResult(fmt.Sprintf("路径超出项目范围: %s", path))
	}

	// 检查文件存在性
	info, err := os.Stat(clean)
	if err != nil {
		return errorResult(fmt.Sprintf("文件未找到: %s", path))
	}
	if info.IsDir() {
		return errorResult(fmt.Sprintf("%s 是目录，不是文件", path))
	}

	// 大小限制：最大 1MB
	if info.Size() > 1024*1024 {
		return errorResult("文件超过 1MB 限制，请指定具体文件")
	}

	// 读取内容
	data, err := os.ReadFile(clean)
	if err != nil {
		return errorResult(fmt.Sprintf("读取失败: %v", err))
	}

	content := string(data)
	result := map[string]interface{}{
		"path":    clean,
		"size":    len(content),
		"lines":   len(strings.Split(content, "\n")),
		"content": content,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func CodeDiffHandler(args map[string]interface{}) *ToolResult {
	pathA, _ := args["path_a"].(string)
	pathB, _ := args["path_b"].(string)
	if pathA == "" || pathB == "" {
		return errorResult("path_a 和 path_b 都需要指定")
	}

	// 读文件 A
	dataA, errA := readCheckedFile(pathA)
	if errA != nil {
		return errorResult(fmt.Sprintf("读取 path_a 失败: %v", errA))
	}

	// 读文件 B
	dataB, errB := readCheckedFile(pathB)
	if errB != nil {
		return errorResult(fmt.Sprintf("读取 path_b 失败: %v", errB))
	}

	linesA := strings.Split(string(dataA), "\n")
	linesB := strings.Split(string(dataB), "\n")

	var added, removed, unchanged int
	maxLen := len(linesA)
	if len(linesB) > maxLen {
		maxLen = len(linesB)
	}

	// 简单逐行对比
	minLen := len(linesA)
	if len(linesB) < minLen {
		minLen = len(linesB)
	}
	for i := 0; i < minLen; i++ {
		if linesA[i] == linesB[i] {
			unchanged++
		} else {
			removed++
			added++
		}
	}
	// 多出的行
	if len(linesB) > len(linesA) {
		added += len(linesB) - len(linesA)
	} else if len(linesA) > len(linesB) {
		removed += len(linesA) - len(linesB)
	}

	result := map[string]interface{}{
		"file_a":   pathA,
		"file_b":   pathB,
		"added":    added,
		"removed":  removed,
		"unchanged": unchanged,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

// readCheckedFile 带安全检查读取文件（复用 code_read 的硬化逻辑）
func readCheckedFile(path string) ([]byte, error) {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}
	clean := filepath.Clean(path)
	if strings.Contains(clean, "..") {
		return nil, fmt.Errorf("路径包含 ..，已拒绝")
	}
	root := getProjectRoot()
	if root != "" {
		if !filepath.IsAbs(clean) {
			clean = filepath.Join(root, clean)
		}
		rel, err := filepath.Rel(root, clean)
		if err != nil || strings.HasPrefix(rel, "..") {
			return nil, fmt.Errorf("路径超出项目范围")
		}
	}
	return os.ReadFile(clean)
}

func registerCodeReadTools() {
	Register("code_read", "受控文件读取。路径规范化 + 穿越检测 + 范围限制。返回纯文件内容。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("文件路径（相对项目根目录或绝对路径）"),
			},
		},
		CodeReadHandler,
	)

	Register("code_diff", "对比两个文件的变更。简单位对比：返回新增/删除/未变行数。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path_a", "path_b"},
			"properties": map[string]interface{}{
				"path_a": stringParam("原始文件路径"),
				"path_b": stringParam("新文件路径"),
			},
		},
		CodeDiffHandler,
	)
}
