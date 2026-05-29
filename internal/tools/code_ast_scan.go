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
	root, _ := args["root"].(string)
	importLimit := 40
	if l, ok := args["import_limit"].(float64); ok && l > 0 {
		importLimit = int(l)
	}

	// root mode: batch scan entire directory
	if root != "" {
		return goStructScanBatch(root, importLimit)
	}

	if path == "" {
		return errorResult("需要 path（单文件）或 root（批量目录）参数")
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
		"file":      path,
		"fallback":  true,
		"lines":     len(lines),
		"types":     regexExtract(content, `type\s+(\w+)\s+(struct|interface)`),
		"functions": regexExtract(content, `func\s+(?:\([^)]+\)\s+)?(\w+)\s*\(`),
		"imports":   regexExtract(content, `"([^"]+)"`),
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

/* ─── goStructScanBatch — go_struct_scan 的批量模式 ── */
type ImportFreq struct {
	ImportPath string `json:"import_path"`
	Count      int    `json:"count"`
}

type BatchScanResult struct {
	Root      string       `json:"root"`
	Files     int          `json:"files"`
	Types     []GoType     `json:"types"`
	Functions []GoType     `json:"functions"`
	Exports   []string     `json:"exports"`
	Imports   []ImportFreq `json:"imports_frequency,omitempty"`
}

func goStructScanBatch(root string, importLimit int) *ToolResult {
	r := BatchScanResult{Root: root}
	exportSet := make(map[string]bool)
	importCounts := make(map[string]int)

	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".go") || strings.HasSuffix(info.Name(), "_test.go") ||
			strings.Contains(path, "/vendor/") || strings.Contains(path, "/.git/") {
			return nil
		}
		r.Files++
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil
		}
		// Collect imports for frequency analysis
		for _, imp := range file.Imports {
			path := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(path, "/") {
				importCounts[path]++
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.GenDecl:
				if node.Tok == token.TYPE {
					for _, spec := range node.Specs {
						ts, ok := spec.(*ast.TypeSpec)
						if !ok {
							continue
						}
						gt := GoType{Name: ts.Name.Name, Kind: "type_alias", Line: fset.Position(ts.Pos()).Line}
						switch ts.Type.(type) {
						case *ast.StructType:
							gt.Kind = "struct"
						case *ast.InterfaceType:
							gt.Kind = "interface"
						}
						r.Types = append(r.Types, gt)
						if ts.Name.IsExported() {
							exportSet[ts.Name.Name] = true
						}
					}
				}
			case *ast.FuncDecl:
				if node.Recv == nil {
					r.Functions = append(r.Functions, GoType{
						Name: node.Name.Name, Kind: "func",
						Line: fset.Position(node.Pos()).Line,
					})
					if node.Name.IsExported() {
						exportSet[node.Name.Name] = true
					}
				}
			}
			return true
		})
		return nil
	})

	for e := range exportSet {
		r.Exports = append(r.Exports, e)
	}
	sort.Strings(r.Exports)

	if importLimit > 0 {
		var freq []ImportFreq
		for path, count := range importCounts {
			freq = append(freq, ImportFreq{ImportPath: path, Count: count})
		}
		sort.Slice(freq, func(i, j int) bool { return freq[i].Count > freq[j].Count })
		if len(freq) > importLimit {
			freq = freq[:importLimit]
		}
		r.Imports = freq
	}

	b, _ := json.MarshalIndent(r, "", "  ")
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
