// Package tools — file operations: read_file, write_file, patch, search_files.
// Port of tools/file_*.py from Python Hermes Agent.
package tools

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Dangerous path patterns that should be blocked.
var dangerousPaths = []string{
	"/etc/passwd", "/etc/shadow", "/etc/sudoers",
	"~/.ssh/", "/root/.ssh/",
	".env", "credentials.json",
}

// dangerousExtensions are file extensions that should not be overwritten.
var dangerousExtensions = []string{".pem", ".key", ".token", ".secret"}

func isSafePath(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("cannot resolve path: %w", err)
	}

	// Check dangerous paths
	lowerPath := strings.ToLower(absPath)
	for _, dp := range dangerousPaths {
		if strings.Contains(lowerPath, strings.ToLower(dp)) {
			return fmt.Errorf("path contains protected location: %s", dp)
		}
	}

	// Check dangerous extensions (only for write operations)
	for _, ext := range dangerousExtensions {
		if strings.HasSuffix(lowerPath, ext) {
			return fmt.Errorf("refusing to write to sensitive file type: %s", ext)
		}
	}

	return nil
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, path[2:])
	}
	return path
}

func registerFileTools() {
	Register("read_file", "Read a text file with line numbers and pagination. Use instead of cat/head/tail.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":   stringParam("Path to the file (absolute, relative, or ~/path)"),
				"offset": intParam("Line number to start from (1-indexed, default: 1)"),
				"limit":  intParam("Max lines (default: 500, max: 2000)"),
			},
			"required": []string{"path"},
		},
		readFileHandler,
	)

	Register("write_file", "Write content to a file, completely replacing existing content. Creates parent directories.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    stringParam("Path to the file to write"),
				"content": stringParam("Complete content to write to the file"),
			},
			"required": []string{"path", "content"},
		},
		writeFileHandler,
	)

	Register("patch", "Targeted find-and-replace edits in files. Uses fuzzy matching. Returns unified diff.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":         stringParam("File path to edit"),
				"old_string":   stringParam("Text to find (must be unique unless replace_all=true)"),
				"new_string":   stringParam("Replacement text"),
				"replace_all":  boolParam("Replace all occurrences instead of requiring unique match"),
			},
			"required": []string{"path", "old_string", "new_string"},
		},
		patchHandler,
	)

	Register("search_files", "Search file contents (grep mode) or find files by name. Use instead of grep/rg/find/ls.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern":   stringParam("Regex pattern (content search) or glob pattern (file search)"),
				"target":    stringParam("'content' searches inside files, 'files' searches by name (default: 'content')"),
				"path":      stringParam("Directory to search in (default: current directory)"),
				"file_glob": stringParam("Filter by filename pattern (e.g., '*.py')"),
				"limit":     intParam("Max results (default: 50)"),
			},
			"required": []string{"pattern"},
		},
		searchFilesHandler,
	)
}

func readFileHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	offset := 1
	limit := 500

	if o, ok := args["offset"].(float64); ok {
		offset = int(o)
	}
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
		if limit > 2000 {
			limit = 2000
		}
	}

	// 硬化层：通过 code_read 做路径安全校验
	if result := CodeReadHandler(map[string]interface{}{
		"filepath": path,
	}); result.Error != "" && !strings.Contains(result.Error, "not found") {
		return errorResult(fmt.Sprintf("path rejected by hardening: %s", result.Error))
	}

	// Expand ~
	path = expandPath(path)

	data, err := os.ReadFile(path)
	if err != nil {
		return errorResult(fmt.Sprintf("read file: %v", err))
	}

	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)

	if offset < 1 {
		offset = 1
	}
	if offset > totalLines {
		offset = totalLines
	}
	end := offset + limit
	if end > totalLines {
		end = totalLines
	}

	var sb strings.Builder
	for i := offset - 1; i < end; i++ {
		sb.WriteString(fmt.Sprintf("%d|%s\n", i+1, lines[i]))
	}

	result := fmt.Sprintf("Total lines: %d\n%s", totalLines, sb.String())
	return successResult(result)
}

func writeFileHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)

	if path == "" {
		return errorResult("path is required")
	}

	path = expandPath(path)

	// Safety check
	if err := isSafePath(path); err != nil {
		return errorResult(fmt.Sprintf("safety check: %v", err))
	}

	// Create parent dirs
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errorResult(fmt.Sprintf("create dirs: %v", err))
	}

	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return errorResult(fmt.Sprintf("write file: %v", err))
	}

	return successResult(fmt.Sprintf("Written %d bytes to %s", len(content), path))
}

func patchHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)
	replaceAll, _ := args["replace_all"].(bool)

	if path == "" || oldStr == "" {
		return errorResult("path and old_string are required")
	}

	path = expandPath(path)
	if err := isSafePath(path); err != nil {
		return errorResult(fmt.Sprintf("safety check: %v", err))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return errorResult(fmt.Sprintf("read file: %v", err))
	}

	content := string(data)
	var newContent string

	if replaceAll {
		newContent = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		idx := strings.Index(content, oldStr)
		if idx < 0 {
			return errorResult("old_string not found in file")
		}
		newContent = content[:idx] + newStr + content[idx+len(oldStr):]
	}

	if err := os.WriteFile(path, []byte(newContent), 0644); err != nil {
		return errorResult(fmt.Sprintf("write file: %v", err))
	}

	return successResult(fmt.Sprintf("Patched %s", path))
}

func searchFilesHandler(args map[string]interface{}) *ToolResult {
	pattern, _ := args["pattern"].(string)
	target, _ := args["target"].(string)
	searchPath, _ := args["path"].(string)
	fileGlob, _ := args["file_glob"].(string)
	limit := 50

	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}

	if pattern == "" {
		return errorResult("pattern is required")
	}

	if searchPath == "" {
		searchPath = "."
	}

	if target == "files" || target == "" {
		target = "files"
	}

	if target == "files" {
		var results []string
		filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				// Skip hidden dirs and common non-project dirs
				if strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
					return filepath.SkipDir
				}
				if info.Name() == "node_modules" || info.Name() == "venv" {
					return filepath.SkipDir
				}
				return nil
			}

			matched, _ := filepath.Match(pattern, info.Name())
			if matched {
				results = append(results, path)
				if len(results) >= limit {
					return fmt.Errorf("limit reached")
				}
			}
			return nil
		})

		if len(results) == 0 {
			return successResult("No files found matching: " + pattern)
		}
		return successResult(strings.Join(results, "\n"))
	}

	// Content search (grep mode)
	var results []string
	filepath.Walk(searchPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if strings.HasPrefix(info.Name(), ".") && info.Name() != "." {
				return filepath.SkipDir
			}
			if info.Name() == "node_modules" || info.Name() == "venv" || info.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		// Apply file_glob filter
		if fileGlob != "" {
			matched, _ := filepath.Match(fileGlob, info.Name())
			if !matched {
				return nil
			}
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := bytes.Split(data, []byte("\n"))
		for i, line := range lines {
			if bytes.Contains(bytes.ToLower(line), []byte(strings.ToLower(pattern))) {
				results = append(results, fmt.Sprintf("%s:%d: %s", path, i+1, string(line)))
				if len(results) >= limit {
					return fmt.Errorf("limit reached")
				}
			}
		}
		return nil
	})

	if len(results) == 0 {
		return successResult("No matches found for: " + pattern)
	}
	return successResult(strings.Join(results, "\n"))
}
