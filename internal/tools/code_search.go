package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

/* ─── L3 工具：code_grep ───────────────────────── */

// GrepMatch 单条 grep 匹配结果
type GrepMatch struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Content string `json:"content"`
}

// CodeGrep 执行 ripgrep 搜索，返回结构化结果
// L3 工具：确定性、schema 校验、不调 LLM
func CodeGrep(query, searchPath string, includes []string, limit int) ([]GrepMatch, error) {
	if limit <= 0 {
		limit = 20
	}

	args := []string{
		"--json",           // JSON 输出
		"--max-count", "3", // 每文件最多 3 条
		"--no-heading",
		"-i", // 大小写不敏感
	}
	for _, inc := range includes {
		args = append(args, "--glob", inc)
	}
	args = append(args, query, searchPath)

	cmd := exec.Command("rg", args...)
	output, err := cmd.Output()
	if err != nil {
		// rg 没找到结果返回 exit code 1，不是错误
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil, nil
		}
		return nil, fmt.Errorf("rg 执行失败: %w", err)
	}

	// 解析 rg --json 输出
	var matches []GrepMatch
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}
		var msg struct {
			Type string `json:"type"`
			Data struct {
				Path struct {
					Text string `json:"text"`
				} `json:"path"`
				LineNumber int `json:"line_number"`
				Lines      struct {
					Text string `json:"text"`
				} `json:"lines"`
			} `json:"data"`
		}
		if json.Unmarshal([]byte(line), &msg) == nil && msg.Type == "match" {
			matches = append(matches, GrepMatch{
				File:    msg.Data.Path.Text,
				Line:    msg.Data.LineNumber,
				Content: strings.TrimSpace(msg.Data.Lines.Text),
			})
			if len(matches) >= limit {
				break
			}
		}
	}
	return matches, nil
}

/* ─── L3 工具：code_read ───────────────────────── */

// CodeReadResult 文件读取结果
type CodeReadResult struct {
	Path  string   `json:"path"`
	Lines []string `json:"lines"`
	Start int      `json:"start"`
	End   int      `json:"end"`
}

// CodeRead 读取文件指定行范围
func CodeRead(filePath string, startLine, endLine int) (*CodeReadResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	allLines := strings.Split(string(data), "\n")
	if startLine < 1 {
		startLine = 1
	}
	if endLine > len(allLines) {
		endLine = len(allLines)
	}
	if endLine < startLine {
		endLine = startLine
	}

	// 上下文窗口上限 50 行（防止注入过多）
	if endLine-startLine > 50 {
		endLine = startLine + 50
	}

	return &CodeReadResult{
		Path:  filePath,
		Lines: allLines[startLine-1 : endLine],
		Start: startLine,
		End:   endLine,
	}, nil
}

/* ─── L3 工具：code_symbols ────────────────────── */

// CodeSymbol 代码符号
type CodeSymbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // func|type|const|var|interface|method
	File      string `json:"file"`
	Line      int    `json:"line"`
	Signature string `json:"signature"`
}

// CodeSymbols 提取文件中的符号（用 grep 正则模拟 AST）
func CodeSymbols(filePath string) ([]CodeSymbol, error) {
	// 用 rg 提取 func/type/const/var 定义行
	patterns := []struct {
		kind    string
		pattern string
	}{
		{"func", `^func \w+`},
		{"method", `^func \(.+\) \w+`},
		{"type", `^type \w+`},
	}

	var symbols []CodeSymbol
	for _, p := range patterns {
		matches, _ := CodeGrep(p.pattern, filePath, nil, 50)
		for _, m := range matches {
			name := extractSymbolName(m.Content, p.kind)
			if name != "" {
				symbols = append(symbols, CodeSymbol{
					Name:      name,
					Kind:      p.kind,
					File:      m.File,
					Line:      m.Line,
					Signature: strings.TrimSpace(m.Content),
				})
			}
		}
	}
	return symbols, nil
}

func extractSymbolName(line, kind string) string {
	// 简单提取：func Xxx( → Xxx
	// type Xxx struct → Xxx
	// 后续可换 go/ast
	fields := strings.Fields(line)
	switch kind {
	case "func", "method":
		if len(fields) >= 2 {
			name := strings.TrimSuffix(fields[1], "(")
			return name
		}
	case "type":
		if len(fields) >= 2 {
			return fields[1]
		}
	}
	return ""
}

/* ─── 辅助函数 ─────────────────────────────────── */

// FileExists 检查文件是否存在
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// DirExists 检查目录是否存在
func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// FindProjectRoot 查找项目根目录（向上查找 go.mod 或 .git）
func FindProjectRoot(startPath string) string {
	dir := startPath
	for {
		if FileExists(filepath.Join(dir, "go.mod")) || DirExists(filepath.Join(dir, ".git")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return startPath
}
