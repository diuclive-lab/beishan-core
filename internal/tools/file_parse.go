package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

/* ─── FileParse 文件解析 ───────────────────────── */

func FileParse(path string) *ToolResult {
	if path == "" {
		return errorResult("path 不能为空")
	}

	// 安全检查：不允许路径遍历
	clean := filepath.Clean(expandPath(path))
	if hasParentPathComponent(clean) {
		return errorResult("路径包含非法字符")
	}

	// 检查文件是否存在
	info, err := os.Stat(clean)
	if err != nil {
		return errorResult(fmt.Sprintf("文件不存在: %v", err))
	}
	if info.IsDir() {
		return errorResult("路径是目录，不是文件")
	}
	if info.Size() > 50*1024*1024 {
		return errorResult("文件超过 50MB 限制")
	}

	ext := strings.ToLower(filepath.Ext(clean))
	content, err := parseFile(clean, ext)
	if err != nil {
		return errorResult(fmt.Sprintf("解析失败: %v", err))
	}

	result := map[string]interface{}{
		"path":     clean,
		"type":     ext,
		"size":     info.Size(),
		"filename": info.Name(),
		"content":  content,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func parseFile(path, ext string) (string, error) {
	switch ext {
	case ".txt", ".md", ".markdown":
		return parseTextFile(path)
	case ".pdf":
		return parsePDF(path)
	default:
		return "", fmt.Errorf("不支持的文件类型: %s（支持: .txt, .md, .pdf）", ext)
	}
}

func parseTextFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func parsePDF(path string) (string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return "", fmt.Errorf("打开 PDF 失败: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	totalPage := r.NumPage()
	for pageNum := 1; pageNum <= totalPage; pageNum++ {
		p := r.Page(pageNum)
		if p.V.IsNull() {
			continue
		}

		rows, err := p.GetTextByRow()
		if err != nil {
			continue
		}

		for _, row := range rows {
			for _, text := range row.Content {
				sb.WriteString(text.S)
			}
			sb.WriteString("\n")
		}
		if pageNum < totalPage {
			sb.WriteString("\n--- 分页 ---\n\n")
		}
	}

	result := strings.TrimSpace(sb.String())
	if result == "" {
		return "", fmt.Errorf("PDF 未提取到文本内容（可能是扫描件或图片型 PDF）")
	}
	return result, nil
}

func hasParentPathComponent(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

/* ─── Tool 注册 ─────────────────────────────────── */

func registerFileParseTools() {
	Register("file_parse", "解析文件内容（支持 .txt, .md, .pdf），提取文本供后续处理。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("文件路径（绝对路径或相对路径）"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return FileParse(strArg(args, "path"))
		},
	)
}
