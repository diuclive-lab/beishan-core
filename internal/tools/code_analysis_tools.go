package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"beishan/internal/mcp"
)

/* ─── 代码分析工具集 ──────────────────────

   为三项目融合（beishan-core + TwinFlower + 66）提供代码分析能力。
   - code_read_external: 读取外部项目文件（无项目根目录限制）
   - go_struct_scan: 扫描 Go 源码结构（types/interfaces/functions/imports）
   - dir_scan: 扫描目录结构（文件树 + 统计信息）
*/

// ─── code_read_external ──────────────────────────────────────────────────────

func CodeReadExternalHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	pathsRaw, _ := args["paths"].([]interface{})
	maxLines := 500
	if m, ok := args["max_lines"].(float64); ok && m > 0 {
		maxLines = int(m)
	}

	// Batch mode: read multiple files
	if len(pathsRaw) > 0 {
		var results []map[string]interface{}
		for _, p := range pathsRaw {
			if pStr, ok := p.(string); ok {
				r := readSingleFile(pStr, maxLines)
				results = append(results, r)
			}
		}
		b, _ := json.MarshalIndent(map[string]interface{}{"files": results}, "", "  ")
		return successResult(string(b))
	}

	// Single file mode (original)
	if path == "" {
		return errorResult("需要 path（单文件）或 paths（批量读取）参数")
	}
	r := readSingleFile(path, maxLines)
	b, _ := json.MarshalIndent(r, "", "  ")
	return successResult(string(b))
}

func readSingleFile(path string, maxLines int) map[string]interface{} {
	// 展开 ~/
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}

	clean := filepath.Clean(path)

	// 基本安全检查
	sensitive := []string{"/etc/shadow", "/etc/passwd", "/private/etc"}
	for _, s := range sensitive {
		if strings.HasPrefix(clean, s) {
			return map[string]interface{}{"path": path, "error": "禁止读取敏感系统文件"}
		}
	}

	// 大小限制：2MB
	info, err := os.Stat(clean)
	if err != nil {
		return map[string]interface{}{"path": path, "error": "文件未找到"}
	}
	if info.IsDir() {
		return map[string]interface{}{"path": path, "error": "是目录，请用 dir_scan"}
	}
	if info.Size() > 2*1024*1024 {
		return map[string]interface{}{"path": path, "error": "文件超过 2MB 限制"}
	}

	data, err := os.ReadFile(clean)
	if err != nil {
		return map[string]interface{}{"path": path, "error": fmt.Sprintf("读取失败: %v", err)}
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
		content = strings.Join(lines, "\n")
	}

	return map[string]interface{}{
		"path":           clean,
		"size":           info.Size(),
		"total_lines":    len(strings.Split(string(data), "\n")),
		"returned_lines": len(lines),
		"truncated":      truncated,
		"content":        content,
	}
}

// ─── dir_scan ────────────────────────────────────────────────────────────────

type DirEntry struct {
	Path  string `json:"path"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	Depth int    `json:"depth"`
}

func DirScanHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	maxDepth := 3
	if d, ok := args["max_depth"].(float64); ok && d > 0 {
		maxDepth = int(d)
	}
	extensions := ""
	if e, ok := args["extensions"].(string); ok {
		extensions = e
	}
	maxEntries := 200
	if m, ok := args["max_entries"].(float64); ok && m > 0 {
		maxEntries = int(m)
	}

	if path == "" {
		return errorResult("path 不能为空")
	}

	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}

	clean := filepath.Clean(path)
	info, err := os.Stat(clean)
	if err != nil {
		return errorResult(fmt.Sprintf("路径不存在: %s", path))
	}
	if !info.IsDir() {
		return errorResult(fmt.Sprintf("%s 不是目录", path))
	}

	// 解析扩展名过滤
	var extSet map[string]bool
	if extensions != "" {
		extSet = make(map[string]bool)
		for _, ext := range strings.Split(extensions, ",") {
			ext = strings.TrimSpace(ext)
			if !strings.HasPrefix(ext, ".") {
				ext = "." + ext
			}
			extSet[strings.ToLower(ext)] = true
		}
	}

	var entries []DirEntry
	var walkDir func(dir string, depth int)
	walkDir = func(dir string, depth int) {
		if depth > maxDepth || len(entries) >= maxEntries {
			return
		}
		// 跳过隐藏目录和 vendor
		base := filepath.Base(dir)
		if strings.HasPrefix(base, ".") || base == "vendor" || base == "node_modules" {
			return
		}

		items, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, item := range items {
			if len(entries) >= maxEntries {
				break
			}
			fullPath := filepath.Join(dir, item.Name())
			if item.IsDir() {
				entries = append(entries, DirEntry{Path: fullPath, IsDir: true, Depth: depth + 1})
				walkDir(fullPath, depth+1)
			} else {
				// 扩展名过滤
				if extSet != nil {
					ext := strings.ToLower(filepath.Ext(item.Name()))
					if !extSet[ext] {
						continue
					}
				}
				fi, _ := item.Info()
				size := int64(0)
				if fi != nil {
					size = fi.Size()
				}
				entries = append(entries, DirEntry{Path: fullPath, Size: size, Depth: depth + 1})
			}
		}
	}
	walkDir(clean, 0)

	// 统计
	var totalSize int64
	fileCount := 0
	dirCount := 0
	for _, e := range entries {
		if e.IsDir {
			dirCount++
		} else {
			fileCount++
			totalSize += e.Size
		}
	}

	result := map[string]interface{}{
		"root":        clean,
		"entries":     entries,
		"file_count":  fileCount,
		"dir_count":   dirCount,
		"total_size":  totalSize,
		"truncated":   len(entries) >= maxEntries,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

// ─── 注册 ────────────────────────────────────────────────────────────────────


/* ─── code_tree — 源码文件树扫描 ───────────────── */

type FileEntry struct {
	Path  string `json:"path"`
	Lang  string `json:"lang"`
	Lines int    `json:"lines,omitempty"`
}

type TreeResult struct {
	TotalFiles int            `json:"total_files"`
	ByLang     map[string]int `json:"by_lang"`
	ByDir      map[string]int `json:"by_dir"`
	Files      []FileEntry    `json:"files,omitempty"`
}

func CodeTreeHandler(args map[string]interface{}) *ToolResult {
	root, _ := args["root"].(string)
	if root == "" {
		return errorResult("root 不能为空")
	}
	listMode, _ := args["list_files"].(bool)
	langFilter, _ := args["lang"].(string) // comma-separated: "go,py"

	root = filepath.Clean(root)
	r := TreeResult{
		ByLang: make(map[string]int),
		ByDir:  make(map[string]int),
	}
	if listMode {
		r.Files = make([]FileEntry, 0)
	}

	// Parse language filter
	var langSet map[string]bool
	if langFilter != "" {
		langSet = make(map[string]bool)
		for _, l := range strings.Split(langFilter, ",") {
			langSet[strings.TrimSpace(strings.ToLower(l))] = true
		}
	}

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		lang := detectSrcLang(filepath.Ext(path), info.Name())
		if lang == "" {
			return nil
		}
		if langSet != nil && !langSet[strings.ToLower(lang)] {
			return nil
		}
		// Skip vendor, test, generated, hidden dirs
		if strings.Contains(path, "/vendor/") || strings.Contains(path, "/.git/") ||
			strings.Contains(path, "/__pycache__/") || strings.Contains(path, "/node_modules/") ||
			strings.Contains(path, "/generated/") {
			return nil
		}
		if strings.HasSuffix(info.Name(), "_test.go") || strings.HasPrefix(info.Name(), "test_") {
			return nil
		}

		relDir, _ := filepath.Rel(root, filepath.Dir(path))
		r.ByLang[lang]++
		r.ByDir[relDir]++
		r.TotalFiles++

		if listMode {
			r.Files = append(r.Files, FileEntry{
				Path:  path,
				Lang:  lang,
				Lines: countFileLines(path),
			})
		}
		return nil
	})

	// Sort files by path for deterministic output
	if listMode {
		sort.Slice(r.Files, func(i, j int) bool { return r.Files[i].Path < r.Files[j].Path })
	}

	b, _ := json.MarshalIndent(r, "", "  ")
	return successResult(string(b))
}

/* ─── code_stats — 代码量统计 ──────────────────── */

type FileLineEntry struct {
	Path  string `json:"path"`
	Lang  string `json:"lang"`
	Lines int    `json:"lines"`
}

type CodeStatsResult struct {
	TotalFiles  int             `json:"total_files"`
	TotalLines  int             `json:"total_lines"`
	ByLang      map[string]int  `json:"by_lang"`
	EntryPoints []string        `json:"entry_points"`
	TopFiles    []FileLineEntry `json:"top_files,omitempty"`
}

func CodeStatsHandler(args map[string]interface{}) *ToolResult {
	root, _ := args["root"].(string)
	listFiles, _ := args["list_files"].(bool)
	limit := 30
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	if root == "" {
		return errorResult("root 不能为空")
	}
	root = filepath.Clean(root)
	r := CodeStatsResult{ByLang: make(map[string]int)}

	var files []FileLineEntry
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if lang := detectSrcLang(filepath.Ext(path), info.Name()); lang != "" {
			r.TotalFiles++
			r.ByLang[lang]++
			lines := countFileLines(path)
			r.TotalLines += lines
			if n := info.Name(); n == "main.go" || n == "cli.py" || n == "main.py" {
				r.EntryPoints = append(r.EntryPoints, path)
			}
			if listFiles {
				files = append(files, FileLineEntry{Path: path, Lang: lang, Lines: lines})
			}
		}
		return nil
	})
	if listFiles {
		sort.Slice(files, func(i, j int) bool { return files[i].Lines > files[j].Lines })
		if len(files) > limit {
			files = files[:limit]
		}
		r.TopFiles = files
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return successResult(string(b))
}

/* ─── code_lang_detect — 项目语言检测 ──────────── */

type LangDetectResult struct {
	PrimaryLang string         `json:"primary_lang"`
	Languages   map[string]int `json:"languages"`
	HasGoMod    bool           `json:"has_go_mod"`
	HasSetupPy  bool           `json:"has_setup_py"`
	HasCargo    bool           `json:"has_cargo"`
	HasPackage  bool           `json:"has_package_json"`
	EntryPoint  string         `json:"entry_point"`
}

func CodeLangDetectHandler(args map[string]interface{}) *ToolResult {
	root, _ := args["root"].(string)
	if root == "" {
		return errorResult("root 不能为空")
	}
	root = filepath.Clean(root)
	r := LangDetectResult{Languages: make(map[string]int)}
	for _, f := range []struct{n string; d *bool}{
		{"go.mod", &r.HasGoMod}, {"setup.py", &r.HasSetupPy},
		{"Cargo.toml", &r.HasCargo}, {"package.json", &r.HasPackage},
	} {
		if _, e := os.Stat(filepath.Join(root, f.n)); e == nil { *f.d = true }
	}
	for _, ep := range []string{"main.go", "cli.py", "lib.rs", "index.ts"} {
		if _, e := os.Stat(filepath.Join(root, ep)); e == nil { r.EntryPoint = ep; break }
	}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() { return nil }
		if lang := detectSrcLang(filepath.Ext(path), info.Name()); lang != "" {
			r.Languages[lang]++
		}
		return nil
	})
	maxC := 0
	for l, c := range r.Languages {
		if c > maxC { maxC, r.PrimaryLang = c, l }
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return successResult(string(b))
}

/* ─── 辅助函数 ────────────────────────────────── */

func detectSrcLang(ext, name string) string {
	switch ext {
	case ".go": return "Go"
	case ".py": return "Python"
	case ".rs": return "Rust"
	case ".ts", ".tsx": return "TypeScript"
	case ".js", ".jsx": return "JavaScript"
	case ".java": return "Java"
	case ".rb": return "Ruby"
	case ".c", ".h": return "C"
	case ".cpp", ".hpp": return "C++"
	case ".swift": return "Swift"
	}
	return ""
}

func countFileLines(path string) int {
	d, e := os.ReadFile(path)
	if e != nil { return 0 }
	return len(strings.Split(string(d), "\n"))
}

/* ─── base_capability_inventory — 底座能力资产清单 ─── */

type CapInventory struct {
	Tools        int      `json:"tools"`
	ToolNames    []string `json:"tool_names"`
	MCPSkills    int      `json:"mcp_skills"`
	Workflows    int      `json:"workflows"`
	RightFlowers int      `json:"right_flowers"`
	InternalPkgs []string `json:"internal_packages"`
}

func BaseCapabilityInventoryHandler(args map[string]interface{}) *ToolResult {
	r := CapInventory{}
	r.Tools = len(Registry)
	for name := range Registry {
		r.ToolNames = append(r.ToolNames, name)
	}
	sort.Strings(r.ToolNames)
	if mcpRunner != nil {
		r.MCPSkills = len(mcp.List())
	}
	if dirs, err := filepath.Glob("internal/*/"); err == nil {
		for _, d := range dirs {
			r.InternalPkgs = append(r.InternalPkgs, d)
		}
	}
	if wfs, err := filepath.Glob("workflows/*.yaml"); err == nil {
		r.Workflows = len(wfs)
	}
	if rfs, err := filepath.Glob("right_flowers/*.yaml"); err == nil {
		r.RightFlowers = len(rfs)
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	return successResult(string(b))
}

func registerCodeAnalysisTools() {
	Register("code_read_external", "读取外部项目文件（无项目根目录限制）。支持 ~/ 展开，最大 2MB。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path":      stringParam("文件绝对路径（支持 ~/ 展开）"),
				"max_lines": intParam("最大返回行数（默认 500）"),
			},
		},
		CodeReadExternalHandler,
	)

	RegisterDeprecated("dir_scan", "扫描目录结构。返回文件树 + 统计信息。支持深度限制和扩展名过滤。",
		"已废弃：请使用 code_tree（更强的语言感知和统计能力）。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path":        stringParam("目录路径"),
				"max_depth":   intParam("最大递归深度（默认 3）"),
				"extensions":  stringParam("扩展名过滤，逗号分隔（如 go,yaml,json）"),
				"max_entries": intParam("最大条目数（默认 200）"),
			},
		},
		DirScanHandler,
	)

	Register("go_struct_scan", "扫描 Go 源码结构。提取 types/interfaces/functions/imports/exports。AST 解析，失败时降级正则。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("Go 源码文件路径"),
			},
		},
		GoStructScanHandler,
	)
	Register("code_tree", "扫描项目源码文件树。list_files=true 时返回文件列表（含行数）。lang 过滤（如 go,py）。替代 find 命令。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"root"},
			"properties": map[string]interface{}{
				"root":       stringParam("项目根目录路径"),
				"list_files": boolParam("设为 true 返回文件路径列表（含行数）"),
				"lang":       stringParam("语言过滤，逗号分隔（如 go,py），不传则全语言"),
			},
		},
		CodeTreeHandler,
	)

	Register("code_stats", "统计项目代码量：文件数、行数、语言分布、入口点。list_files=true 返回 top-N 文件行数排行。替代 wc 和 find 计数。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"root"},
			"properties": map[string]interface{}{
				"root":       stringParam("项目根目录路径"),
				"list_files": boolParam("设为 true 返回 top-N 文件行数排行"),
				"limit":      intParam("返回条数（默认 30）"),
			},
		},
		CodeStatsHandler,
	)

	Register("code_lang_detect", "检测项目主语言和技术栈。替代 ls/cat 命令。",
		map[string]interface{}{
			"type": "object",
			"required": []string{"root"},
			"properties": map[string]interface{}{
				"root": stringParam("项目根目录路径"),
			},
		},
		CodeLangDetectHandler,
	)

	Register("base_capability_inventory", "返回底座自身的能力资产清单：工具数/分类、MCP技能、工作流、右花、内部包。替代 grep+ls 拼凑。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{},
			"properties": map[string]interface{}{},
		},
		BaseCapabilityInventoryHandler,
	)

}
