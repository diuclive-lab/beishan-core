package tools

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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
	maxLines := 500
	if m, ok := args["max_lines"].(float64); ok && m > 0 {
		maxLines = int(m)
	}

	if path == "" {
		return errorResult("path 不能为空")
	}

	// 展开 ~/
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, path[2:])
	}

	clean := filepath.Clean(path)

	// 基本安全检查：禁止 /etc/shadow 等敏感路径
	sensitive := []string{"/etc/shadow", "/etc/passwd", "/private/etc"}
	for _, s := range sensitive {
		if strings.HasPrefix(clean, s) {
			return errorResult("禁止读取敏感系统文件")
		}
	}

	// 大小限制：2MB
	info, err := os.Stat(clean)
	if err != nil {
		return errorResult(fmt.Sprintf("文件未找到: %s", path))
	}
	if info.IsDir() {
		return errorResult(fmt.Sprintf("%s 是目录，请用 dir_scan", path))
	}
	if info.Size() > 2*1024*1024 {
		return errorResult("文件超过 2MB 限制")
	}

	data, err := os.ReadFile(clean)
	if err != nil {
		return errorResult(fmt.Sprintf("读取失败: %v", err))
	}

	content := string(data)
	lines := strings.Split(content, "\n")
	truncated := false
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
		content = strings.Join(lines, "\n")
	}

	result := map[string]interface{}{
		"path":      clean,
		"size":      info.Size(),
		"total_lines": len(strings.Split(string(data), "\n")),
		"returned_lines": len(lines),
		"truncated": truncated,
		"content":   content,
	}
	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
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

// ─── go_struct_scan ──────────────────────────────────────────────────────────

type GoType struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind"` // struct, interface, func, type_alias
	Fields  []string `json:"fields,omitempty"`
	Methods []string `json:"methods,omitempty"`
	Doc     string   `json:"doc,omitempty"`
	Line    int      `json:"line"`
}

type GoImport struct {
	Path  string `json:"path"`
	Alias string `json:"alias,omitempty"`
}

type GoScanResult struct {
	File      string     `json:"file"`
	Package   string     `json:"package"`
	Imports   []GoImport `json:"imports"`
	Types     []GoType   `json:"types"`
	Functions []GoType   `json:"functions"`
	Exports   []string   `json:"exports"`
	Lines     int        `json:"lines"`
}

func GoStructScanHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
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
		return errorResult(fmt.Sprintf("文件未找到: %s", path))
	}
	if info.Size() > 2*1024*1024 {
		return errorResult("文件超过 2MB 限制")
	}

	// 用 Go 标准库解析 AST
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, clean, nil, parser.ParseComments)
	if err != nil {
		// AST 解析失败，降级为正则扫描
		return goStructScanRegex(clean)
	}

	result := GoScanResult{
		File:    clean,
		Package: file.Name.Name,
	}

	// 收集 imports
	for _, imp := range file.Imports {
		gi := GoImport{Path: strings.Trim(imp.Path.Value, `"`)}
		if imp.Name != nil {
			gi.Alias = imp.Name.Name
		}
		result.Imports = append(result.Imports, gi)
	}

	// 收集类型和函数
	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			if node.Tok == token.TYPE {
				for _, spec := range node.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					gt := GoType{
						Name: ts.Name.Name,
						Line: fset.Position(ts.Pos()).Line,
					}
					if node.Doc != nil {
						gt.Doc = node.Doc.Text()
					}

					switch ts.Type.(type) {
					case *ast.StructType:
						gt.Kind = "struct"
						st := ts.Type.(*ast.StructType)
						for _, field := range st.Fields.List {
							for _, name := range field.Names {
								ft := ""
								if field.Type != nil {
									ft = types.ExprString(field.Type)
								}
								gt.Fields = append(gt.Fields, fmt.Sprintf("%s %s", name.Name, ft))
							}
						}
					case *ast.InterfaceType:
						gt.Kind = "interface"
						it := ts.Type.(*ast.InterfaceType)
						for _, method := range it.Methods.List {
							for _, name := range method.Names {
								ft := ""
								if method.Type != nil {
									ft = types.ExprString(method.Type)
								}
								gt.Methods = append(gt.Methods, fmt.Sprintf("%s %s", name.Name, ft))
							}
						}
					default:
						gt.Kind = "type_alias"
					}
					result.Types = append(result.Types, gt)
					if ts.Name.IsExported() {
						result.Exports = append(result.Exports, ts.Name.Name)
					}
				}
			}
		case *ast.FuncDecl:
			if node.Recv == nil {
				// 顶层函数
				gt := GoType{
					Name: node.Name.Name,
					Kind: "func",
					Line: fset.Position(node.Pos()).Line,
				}
				if node.Doc != nil {
					gt.Doc = node.Doc.Text()
				}
				// 参数签名
				if node.Type.Params != nil {
					var params []string
					for _, p := range node.Type.Params.List {
						pt := types.ExprString(p.Type)
						if len(p.Names) > 0 {
							for _, name := range p.Names {
								params = append(params, fmt.Sprintf("%s %s", name.Name, pt))
							}
						} else {
							params = append(params, pt)
						}
					}
					gt.Fields = params // 复用 Fields 存参数
				}
				result.Functions = append(result.Functions, gt)
				if node.Name.IsExported() {
					result.Exports = append(result.Exports, node.Name.Name)
				}
			} else {
				// 方法（附加到对应类型的 Methods）
			 recvType := types.ExprString(node.Recv.List[0].Type)
				methodSig := fmt.Sprintf("%s(%s)", node.Name.Name, funcParams(node))
				found := false
				for i, t := range result.Types {
					if t.Name == recvType || "*"+t.Name == recvType {
						result.Types[i].Methods = append(result.Types[i].Methods, methodSig)
						found = true
						break
					}
				}
				if !found {
					// 接收者类型还未出现，单独记录
					result.Types = append(result.Types, GoType{
						Name:    recvType,
						Kind:    "method_owner",
						Methods: []string{methodSig},
						Line:    fset.Position(node.Pos()).Line,
					})
				}
				if node.Name.IsExported() {
					result.Exports = append(result.Exports, node.Name.Name)
				}
			}
		}
		return true
	})

	// 行数
	lineCount := 0
	data, _ := os.ReadFile(clean)
	if data != nil {
		lineCount = len(strings.Split(string(data), "\n"))
	}
	result.Lines = lineCount

	// 排序
	sort.Slice(result.Types, func(i, j int) bool { return result.Types[i].Line < result.Types[j].Line })
	sort.Slice(result.Functions, func(i, j int) bool { return result.Functions[i].Line < result.Functions[j].Line })
	sort.Strings(result.Exports)

	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

// funcParams 提取函数参数签名字符串
func funcParams(node *ast.FuncDecl) string {
	if node.Type.Params == nil {
		return ""
	}
	var params []string
	for _, p := range node.Type.Params.List {
		pt := types.ExprString(p.Type)
		if len(p.Names) > 0 {
			for _, name := range p.Names {
				params = append(params, name.Name+" "+pt)
			}
		} else {
			params = append(params, pt)
		}
	}
	return strings.Join(params, ", ")
}

// goStructScanRegex AST 解析失败时的降级方案：正则提取
func goStructScanRegex(path string) *ToolResult {
	data, err := os.ReadFile(path)
	if err != nil {
		return errorResult(fmt.Sprintf("读取失败: %v", err))
	}
	content := string(data)
	lines := strings.Split(content, "\n")

	result := map[string]interface{}{
		"file":       path,
		"fallback":   true,
		"lines":      len(lines),
		"types":      regexExtract(content, `type\s+(\w+)\s+(struct|interface)`),
		"functions":  regexExtract(content, `func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(`),
		"imports":    regexExtract(content, `"([^"]+)"`),
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func regexExtract(content, pattern string) []string {
	re := regexp.MustCompile(pattern)
	matches := re.FindAllStringSubmatch(content, -1)
	var results []string
	seen := make(map[string]bool)
	for _, m := range matches {
		if len(m) > 1 && !seen[m[1]] {
			seen[m[1]] = true
			results = append(results, m[1])
		}
	}
	return results
}

// ─── 注册 ────────────────────────────────────────────────────────────────────


/* ─── code_tree — 源码文件树扫描 ───────────────── */

type TreeResult struct {
	TotalFiles int            `json:"total_files"`
	ByLang     map[string]int `json:"by_lang"`
	ByDir      map[string]int `json:"by_dir"`
}

func CodeTreeHandler(args map[string]interface{}) *ToolResult {
	root, _ := args["root"].(string)
	if root == "" {
		return errorResult("root 不能为空")
	}
	root = filepath.Clean(root)
	r := TreeResult{ByLang: make(map[string]int), ByDir: make(map[string]int)}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if lang := detectSrcLang(filepath.Ext(path), info.Name()); lang != "" {
			relDir, _ := filepath.Rel(root, filepath.Dir(path))
			r.ByLang[lang]++
			r.ByDir[relDir]++
			r.TotalFiles++
		}
		return nil
	})
	b, _ := json.MarshalIndent(r, "", "  ")
	return successResult(string(b))
}

/* ─── code_stats — 代码量统计 ──────────────────── */

type CodeStatsResult struct {
	TotalFiles  int            `json:"total_files"`
	TotalLines  int            `json:"total_lines"`
	ByLang      map[string]int `json:"by_lang"`
	EntryPoints []string       `json:"entry_points"`
}

func CodeStatsHandler(args map[string]interface{}) *ToolResult {
	root, _ := args["root"].(string)
	if root == "" {
		return errorResult("root 不能为空")
	}
	root = filepath.Clean(root)
	r := CodeStatsResult{ByLang: make(map[string]int)}
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if lang := detectSrcLang(filepath.Ext(path), info.Name()); lang != "" {
			r.TotalFiles++
			r.ByLang[lang]++
			r.TotalLines += countFileLines(path)
			if n := info.Name(); n == "main.go" || n == "cli.py" || n == "main.py" {
				r.EntryPoints = append(r.EntryPoints, path)
			}
		}
		return nil
	})
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

	Register("dir_scan", "扫描目录结构。返回文件树 + 统计信息。支持深度限制和扩展名过滤。",
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
	Register("code_tree", "扫描项目源码文件树。按语言和目录统计。替代 find 命令。",
		map[string]interface{}{
			"type": "object",
			"required": []string{"root"},
			"properties": map[string]interface{}{
				"root": stringParam("项目根目录路径"),
			},
		},
		CodeTreeHandler,
	)

	Register("code_stats", "统计项目代码量：文件数、行数、语言分布、入口点。替代 grep 计数。",
		map[string]interface{}{
			"type": "object",
			"required": []string{"root"},
			"properties": map[string]interface{}{
				"root": stringParam("项目根目录路径"),
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

}
