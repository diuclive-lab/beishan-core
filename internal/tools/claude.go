package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

/* ─── 数据结构 ─────────────────────────────────── */

type ClaudeMemoryFile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Path        string `json:"path"`
	Body        string `json:"body,omitempty"`
}

/* ─── 路径 ─────────────────────────────────────── */

func claudeMemoryDir() string {
	if d := os.Getenv("CLAUDE_MEMORY_DIR"); d != "" {
		return d
	}
	return filepath.Join(os.Getenv("HOME"), ".claude", "projects", "-Users-dc", "memory")
}

/* ─── List — 列出可用记忆文件 ──────────────────── */

func ClaudeMemoryList() *ToolResult {
	memoryDir := claudeMemoryDir()
	idxPath := filepath.Join(memoryDir, "MEMORY.md")

	idxData, err := os.ReadFile(idxPath)
	if err != nil {
		return errorResult(fmt.Sprintf("读取 MEMORY.md 失败: %v", err))
	}

	type entry struct {
		Title       string
		Filename    string
		Description string
	}

	// Parse MEMORY.md: `- [Title](file.md) — Description`
	re := regexp.MustCompile(`- \[([^\]]+)\]\(([^)]+\.md)\)\s*—\s*(.*)`)
	var entries []entry
	for _, line := range strings.Split(string(idxData), "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		entries = append(entries, entry{Title: m[1], Filename: m[2], Description: m[3]})
	}

	var files []ClaudeMemoryFile
	for _, e := range entries {
		files = append(files, ClaudeMemoryFile{
			Name:        e.Title,
			Description: e.Description,
			Path:        filepath.Join(memoryDir, e.Filename),
		})
	}

	// 也扫描目录中不在索引里的 .md 文件
	existing := make(map[string]bool)
	for _, e := range entries {
		existing[e.Filename] = true
	}
	dirEntries, _ := os.ReadDir(memoryDir)
	for _, de := range dirEntries {
		if de.IsDir() || de.Name() == "MEMORY.md" || !strings.HasSuffix(de.Name(), ".md") {
			continue
		}
		if existing[de.Name()] {
			continue
		}
		files = append(files, ClaudeMemoryFile{
			Name: strings.TrimSuffix(de.Name(), ".md"),
			Path: filepath.Join(memoryDir, de.Name()),
		})
	}

	if len(files) == 0 {
		return successResult(`{"files":[],"count":0,"message":"无记忆文件"}`)
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})

	result := map[string]interface{}{
		"files":   files,
		"count":   len(files),
		"message": fmt.Sprintf("找到 %d 个记忆文件", len(files)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── Import — 导入指定或全部记忆文件 ──────────── */

func ClaudeMemoryImport(name string) *ToolResult {
	memoryDir := claudeMemoryDir()
	idxPath := filepath.Join(memoryDir, "MEMORY.md")

	// 读取索引获取文件名映射
	nameToFile := make(map[string]string)
	re := regexp.MustCompile(`- \[([^\]]+)\]\(([^)]+\.md)\)`)
	if data, err := os.ReadFile(idxPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			m := re.FindStringSubmatch(line)
			if m != nil {
				nameToFile[m[1]] = m[2]
			}
		}
	}

	// 确定要导入的文件
	var targets []string
	if name == "" || name == "all" {
		// 导入所有
		dirEntries, _ := os.ReadDir(memoryDir)
		for _, de := range dirEntries {
			if de.IsDir() || de.Name() == "MEMORY.md" || !strings.HasSuffix(de.Name(), ".md") {
				continue
			}
			targets = append(targets, de.Name())
		}
	} else {
		// 按名称查找
		if fn, ok := nameToFile[name]; ok {
			targets = append(targets, fn)
		} else {
			// 尝试直接作为文件名
			candidate := name
			if !strings.HasSuffix(candidate, ".md") {
				candidate += ".md"
			}
			if _, err := os.Stat(filepath.Join(memoryDir, candidate)); err == nil {
				targets = append(targets, candidate)
			} else {
				return errorResult(fmt.Sprintf("未找到记忆文件: %s", name))
			}
		}
	}

	if len(targets) == 0 {
		return successResult(`{"imported":[],"count":0,"message":"无文件可导入"}`)
	}

	var imported []map[string]interface{}

	for _, fn := range targets {
		fpath := filepath.Join(memoryDir, fn)
		data, err := os.ReadFile(fpath)
		if err != nil {
			continue
		}

		content := string(data)
		mf := parseClaudeFrontmatter(content)

		title := mf.Name
		if title == "" {
			title = strings.TrimSuffix(fn, ".md")
		}
		desc := mf.Description
		if desc == "" {
			desc = title
		}
		mtype := mf.Type
		if mtype == "" {
			mtype = "claude_memory"
		}
		body := mf.Body

		// 生成 tags
		tags := []string{"claude_memory", mtype}
		if mtype == "user" {
			tags = append(tags, "用户画像")
		} else if mtype == "feedback" {
			tags = append(tags, "反馈记录")
		} else if mtype == "project" {
			tags = append(tags, "项目背景")
		} else if mtype == "reference" {
			tags = append(tags, "参考信息")
		}

		// 直接调 knowledge_add 保存
		r := KnowledgeAdd("claude_memory", "", title, desc,
			tags, nil, nil, nil, fpath, body, 0, "")
		if r.Success {
			var idObj struct{ ID string }
			json.Unmarshal([]byte(r.Output), &idObj)
			imported = append(imported, map[string]interface{}{
				"id":    idObj.ID,
				"title": title,
				"file":  fn,
			})
		}
	}

	if len(imported) == 0 {
		return errorResult("导入失败：未能成功写入任何条目")
	}

	result := map[string]interface{}{
		"imported": imported,
		"count":    len(imported),
		"message":  fmt.Sprintf("成功导入 %d 个记忆条目", len(imported)),
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── Frontmatter 解析 ─────────────────────────── */

var fmRe = regexp.MustCompile(`(?s)^---\n(.+?)\n---\n?(.*)`)

func parseClaudeFrontmatter(content string) ClaudeMemoryFile {
	var mf ClaudeMemoryFile
	m := fmRe.FindStringSubmatch(content)
	if m == nil {
		mf.Body = content
		return mf
	}

	fmRaw := m[1]
	mf.Body = strings.TrimSpace(m[2])

	for _, line := range strings.Split(fmRaw, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Parse key: value pairs
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		val = strings.Trim(val, `"'`)

		switch key {
		case "name":
			mf.Name = val
		case "description":
			mf.Description = val
		case "type":
			mf.Type = val
		}
	}
	return mf
}

/* ─── Tool 注册 ─────────────────────────────────── */

func registerClaudeTools() {
	Register("claude_memory_list", "列出 Claude 记忆文件。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return ClaudeMemoryList()
		},
	)

	Register("claude_memory_import", "导入 Claude 记忆到知识库（指定名称或 all 全部）。",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": stringParam("记忆名称（留空或 all 导入全部）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return ClaudeMemoryImport(strArg(args, "name"))
		},
	)
}
