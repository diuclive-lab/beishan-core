// Port of FangLab internal/tools/filesystem/ tools (2026-05-26).
// Provides: file_guess_type, file_preview, archive_extract, csv_select_columns,
// json_extract, file_stat, multi_file_compare, text_extract_fields.
package tools

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func init() {
	registerFileSystemTools()
}

func registerFileSystemTools() {
	Register("file_guess_type", "Guess a file's type, mime, and likely handling path before choosing a heavier tool.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "File path."},
			},
			"required": []string{"path"},
		},
		func(args map[string]interface{}) *ToolResult {
			path, ok := args["path"].(string)
			if !ok || strings.TrimSpace(path) == "" {
				return ErrorResult("file_guess_type requires a non-empty path")
			}
			info, err := os.Stat(path)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot access path: %v", err))
			}
			if info.IsDir() {
				return SuccessResult(fmt.Sprintf(`{"kind":"directory","path":"%s"}`, path))
			}
			buf := make([]byte, 512)
			f, err := os.Open(path)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot open file: %v", err))
			}
			defer f.Close()
			n, _ := f.Read(buf)
			mime := http.DetectContentType(buf[:n])
			ext := strings.ToLower(filepath.Ext(path))
			kind := inferFileKind(ext, mime)
			return SuccessResult(fmt.Sprintf(`{"kind":"%s","mime":"%s","ext":"%s","path":"%s"}`, kind, mime, ext, path))
		},
	)

	Register("file_preview", "Preview the beginning of a local text file without reading the entire file.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":      map[string]interface{}{"type": "string", "description": "File path."},
				"max_lines": map[string]interface{}{"type": "integer", "description": "Max lines to preview (default 40)."},
				"max_bytes": map[string]interface{}{"type": "integer", "description": "Max bytes to read (default 4096)."},
			},
			"required": []string{"path"},
		},
		func(args map[string]interface{}) *ToolResult {
			path, ok := args["path"].(string)
			if !ok || strings.TrimSpace(path) == "" {
				return ErrorResult("file_preview requires a non-empty path")
			}
			maxLines := 40
			if v, ok := asInt(args["max_lines"]); ok && v > 0 {
				maxLines = v
			}
			maxBytes := 4096
			if v, ok := asInt(args["max_bytes"]); ok && v > 0 {
				maxBytes = v
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot read file: %v", err))
			}

			byteTruncated := false
			if len(content) > maxBytes {
				content = content[:maxBytes]
				byteTruncated = true
			}

			preview, lineCount, lineTruncated := previewLines(string(content), maxLines)
			result := fmt.Sprintf(`{"path":"%s","preview":%q,"line_count":%d,"byte_truncated":%v,"line_truncated":%v}`,
				path, preview, lineCount, byteTruncated, lineTruncated)
			return SuccessResult(result)
		},
	)

	Register("archive_extract", "Inspect archive contents and optionally extract them.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":             map[string]interface{}{"type": "string", "description": "Archive path."},
				"destination_dir":  map[string]interface{}{"type": "string", "description": "Optional extraction directory. If omitted, only previews contents."},
				"max_entries":      map[string]interface{}{"type": "integer", "description": "Maximum entries to preview."},
			},
			"required": []string{"path"},
		},
		func(args map[string]interface{}) *ToolResult {
			path, ok := args["path"].(string)
			if !ok || strings.TrimSpace(path) == "" {
				return ErrorResult("archive_extract requires a non-empty path")
			}
			maxEntries := 50
			if v, ok := asInt(args["max_entries"]); ok && v > 0 {
				maxEntries = v
			}

			entries, err := listArchiveEntries(path, maxEntries)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot list archive: %v", err))
			}

			var dest string
			if raw, ok := args["destination_dir"].(string); ok && strings.TrimSpace(raw) != "" {
				dest = raw
				if err := os.MkdirAll(dest, 0o755); err != nil {
					return ErrorResult(fmt.Sprintf("cannot create destination: %v", err))
				}
				if err := extractArchive(path, dest); err != nil {
					return ErrorResult(fmt.Sprintf("extraction failed: %v", err))
				}
			}

			result := fmt.Sprintf(`{"path":"%s","entries":%d,"destination":"%s"}`, path, len(entries), dest)
			return SuccessResult(result)
		},
	)

	Register("csv_select_columns", "Select specific columns from a CSV file and preview their values.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "CSV file path."},
				"columns": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Column names to select."},
				"rows":    map[string]interface{}{"type": "integer", "description": "Max rows to return (default 10)."},
			},
			"required": []string{"path", "columns"},
		},
		func(args map[string]interface{}) *ToolResult {
			path, _ := args["path"].(string)
			if path == "" {
				return ErrorResult("csv_select_columns requires a path")
			}
			columns := toStringSlice(args["columns"])
			if len(columns) == 0 {
				return ErrorResult("csv_select_columns requires at least one column")
			}
			maxRows := 10
			if v, ok := asInt(args["rows"]); ok && v > 0 {
				maxRows = v
			}
			f, err := os.Open(path)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot open file: %v", err))
			}
			defer f.Close()
			r := csv.NewReader(f)
			records, err := r.ReadAll()
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot read CSV: %v", err))
			}
			if len(records) < 1 {
				return ErrorResult("CSV file has no header row")
			}
			headers := records[0]
			idxMap := make(map[string]int, len(headers))
			for i, h := range headers {
				idxMap[h] = i
			}
			var selected []int
			var missing []string
			for _, col := range columns {
				if idx, ok := idxMap[col]; ok {
					selected = append(selected, idx)
				} else {
					missing = append(missing, col)
				}
			}
			if len(selected) == 0 {
				return ErrorResult(fmt.Sprintf("no requested columns found in CSV headers: %v", headers))
			}
			var rows []map[string]string
			for _, rec := range records[1:] {
				row := make(map[string]string)
				for _, idx := range selected {
					if idx < len(rec) {
						row[records[0][idx]] = rec[idx]
					}
				}
				rows = append(rows, row)
				if len(rows) >= maxRows {
					break
				}
			}
			out, _ := json.Marshal(map[string]interface{}{
				"path":    path,
				"columns": columns,
				"missing": missing,
				"rows":    rows,
			})
			return SuccessResult(string(out))
		},
	)

	Register("json_extract", "Extract specific fields from a JSON file using dot-separated paths.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":   map[string]interface{}{"type": "string", "description": "JSON file path."},
				"fields": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Dot-separated field paths (e.g. 'user.name'). Empty = summarize root."},
			},
			"required": []string{"path"},
		},
		func(args map[string]interface{}) *ToolResult {
			path, _ := args["path"].(string)
			if path == "" {
				return ErrorResult("json_extract requires a path")
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot read file: %v", err))
			}
			var payload interface{}
			if err := json.Unmarshal(content, &payload); err != nil {
				return ErrorResult(fmt.Sprintf("invalid JSON: %v", err))
			}
			fields := toStringSlice(args["fields"])
			selection := make(map[string]interface{})
			if len(fields) == 0 {
				switch v := payload.(type) {
				case map[string]interface{}:
					keys := make([]string, 0, len(v))
					for k := range v {
						keys = append(keys, k)
					}
					selection["top_level_keys"] = keys
				case []interface{}:
					selection["array_length"] = len(v)
				default:
					selection["value"] = v
				}
			} else {
				for _, field := range fields {
					if val, ok := resolveJSONPath(payload, field); ok {
						selection[field] = val
					}
				}
			}
			out, _ := json.Marshal(map[string]interface{}{
				"path":      path,
				"fields":    fields,
				"selection": selection,
			})
			return SuccessResult(string(out))
		},
	)

	Register("file_stat", "Inspect file or directory metadata (size, type, mod time, permissions).",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string", "description": "File or directory path."},
			},
			"required": []string{"path"},
		},
		func(args map[string]interface{}) *ToolResult {
			path, _ := args["path"].(string)
			if path == "" {
				return ErrorResult("file_stat requires a path")
			}
			info, err := os.Stat(path)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot stat path: %v", err))
			}
			kind := "file"
			if info.IsDir() {
				kind = "directory"
			}
			ext := ""
			if !info.IsDir() {
				ext = strings.ToLower(filepath.Ext(path))
			}
			textExts := map[string]bool{".txt": true, ".md": true, ".json": true, ".yaml": true, ".yml": true, ".csv": true, ".go": true, ".py": true, ".js": true, ".ts": true, ".rs": true, ".sh": true}
			out, _ := json.Marshal(map[string]interface{}{
				"path":                 path,
				"name":                 info.Name(),
				"kind":                 kind,
				"size":                 info.Size(),
				"extension":            ext,
				"mod_time":             info.ModTime().UTC().Format("2006-01-02T15:04:05Z"),
				"is_dir":               info.IsDir(),
				"likely_readable_text": textExts[ext],
			})
			return SuccessResult(string(out))
		},
	)

	Register("multi_file_compare", "Compare two text files and show their relationship.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path_a":    map[string]interface{}{"type": "string", "description": "First file path."},
				"path_b":    map[string]interface{}{"type": "string", "description": "Second file path."},
				"max_bytes": map[string]interface{}{"type": "integer", "description": "Max bytes per file (default 4096)."},
			},
			"required": []string{"path_a", "path_b"},
		},
		func(args map[string]interface{}) *ToolResult {
			pathA, _ := args["path_a"].(string)
			pathB, _ := args["path_b"].(string)
			if pathA == "" || pathB == "" {
				return ErrorResult("multi_file_compare requires path_a and path_b")
			}
			maxBytes := 4096
			if v, ok := asInt(args["max_bytes"]); ok && v > 0 {
				maxBytes = v
			}
			contentA, err := os.ReadFile(pathA)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot read path_a: %v", err))
			}
			contentB, err := os.ReadFile(pathB)
			if err != nil {
				return ErrorResult(fmt.Sprintf("cannot read path_b: %v", err))
			}
			truncA := false
			if len(contentA) > maxBytes {
				contentA = contentA[:maxBytes]
				truncA = true
			}
			truncB := false
			if len(contentB) > maxBytes {
				contentB = contentB[:maxBytes]
				truncB = true
			}
			same := string(contentA) == string(contentB)
			rel := "different"
			if same {
				rel = "identical"
			}
			out, _ := json.Marshal(map[string]interface{}{
				"path_a":    pathA,
				"path_b":    pathB,
				"same":      same,
				"relation":  rel,
				"bytes_a":   len(contentA),
				"bytes_b":   len(contentB),
				"truncated": truncA || truncB,
			})
			return SuccessResult(string(out))
		},
	)

	Register("text_extract_fields", "Extract labeled fields from text content or a file.",
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text":   map[string]interface{}{"type": "string", "description": "Inline text content (provide this or path)."},
				"path":   map[string]interface{}{"type": "string", "description": "File path to read text from (provide this or text)."},
				"fields": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "Field labels to extract (e.g. 'name', 'version')."},
			},
			"required": []string{"fields"},
		},
		func(args map[string]interface{}) *ToolResult {
			fields := toStringSlice(args["fields"])
			if len(fields) == 0 {
				return ErrorResult("text_extract_fields requires at least one field")
			}
			var text string
			if t, ok := args["text"].(string); ok && strings.TrimSpace(t) != "" {
				text = t
			} else if p, ok := args["path"].(string); ok && strings.TrimSpace(p) != "" {
				data, err := os.ReadFile(p)
				if err != nil {
					return ErrorResult(fmt.Sprintf("cannot read file: %v", err))
				}
				text = string(data)
			} else {
				return ErrorResult("text_extract_fields requires either text or path")
			}
			lines := strings.Split(text, "\n")
			selections := make(map[string]string)
			for _, field := range fields {
				fieldLower := strings.ToLower(strings.TrimSpace(field))
				for _, line := range lines {
					trimmed := strings.TrimSpace(line)
					if strings.HasPrefix(strings.ToLower(trimmed), fieldLower+":") {
						selections[field] = strings.TrimSpace(trimmed[len(fieldLower)+1:])
						break
					}
					if strings.HasPrefix(strings.ToLower(trimmed), fieldLower+"=") {
						selections[field] = strings.TrimSpace(trimmed[len(fieldLower)+1:])
						break
					}
				}
			}
			out, _ := json.Marshal(map[string]interface{}{
				"fields":    fields,
				"selection": selections,
			})
			return SuccessResult(string(out))
		},
	)
}

// inferFileKind guesses the file type from extension and MIME.
func inferFileKind(ext, mime string) string {
	switch ext {
	case ".txt", ".md", ".rst", ".asciidoc":
		return "document"
	case ".json":
		return "json"
	case ".csv", ".tsv":
		return "tabular"
	case ".xml", ".html", ".htm":
		return "markup"
	case ".yaml", ".yml":
		return "yaml"
	case ".pdf":
		return "pdf"
	case ".zip", ".tar", ".gz", ".tgz":
		return "archive"
	case ".jpg", ".jpeg", ".png", ".gif", ".svg", ".webp":
		return "image"
	case ".mp3", ".wav", ".flac", ".aac", ".ogg":
		return "audio"
	case ".mp4", ".avi", ".mkv", ".mov":
		return "video"
	case ".go", ".py", ".js", ".ts", ".rs", ".java", ".c", ".cpp", ".h", ".hpp":
		return "source_code"
	case ".so", ".dylib", ".dll", ".exe":
		return "binary"
	}
	if strings.HasPrefix(mime, "text/") {
		return "text"
	}
	if strings.HasPrefix(mime, "image/") {
		return "image"
	}
	return "unknown"
}

// previewLines returns the first N lines of content.
func previewLines(content string, maxLines int) (preview string, lineCount int, truncated bool) {
	lines := strings.SplitN(content, "\n", maxLines+1)
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	return strings.Join(lines, "\n"), len(lines), truncated
}

// listArchiveEntries lists entries in a zip/tar/tar.gz archive.
func listArchiveEntries(path string, maxEntries int) ([]map[string]interface{}, error) {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return listZipEntries(path, maxEntries)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar"):
		return listTarEntries(path, maxEntries)
	default:
		return nil, fmt.Errorf("unsupported archive format: %s", path)
	}
}

func listZipEntries(path string, max int) ([]map[string]interface{}, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var entries []map[string]interface{}
	for _, f := range r.File {
		if len(entries) >= max {
			break
		}
		info := f.FileInfo()
		entries = append(entries, map[string]interface{}{
			"name":   f.Name,
			"size":   info.Size(),
			"mode":   info.Mode().String(),
			"is_dir": info.IsDir(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i]["name"].(string) < entries[j]["name"].(string)
	})
	return entries, nil
}

func listTarEntries(path string, max int) ([]map[string]interface{}, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(strings.ToLower(path), ".tar.gz") || strings.HasSuffix(strings.ToLower(path), ".tgz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		reader = gz
	}

	tr := tar.NewReader(reader)
	var entries []map[string]interface{}
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(entries) >= max {
			break
		}
		entries = append(entries, map[string]interface{}{
			"name":   header.Name,
			"size":   header.Size,
			"mode":   header.FileInfo().Mode().String(),
			"is_dir": header.FileInfo().IsDir(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i]["name"].(string) < entries[j]["name"].(string)
	})
	return entries, nil
}

// extractArchive extracts a zip/tar/tar.gz archive to destination.
func extractArchive(path, dest string) error {
	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(path, dest)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"), strings.HasSuffix(lower, ".tar"):
		return extractTar(path, dest)
	default:
		return fmt.Errorf("unsupported archive format: %s", path)
	}
}

func extractZip(path, dest string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		destPath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(destPath, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(destPath), 0o755)
		src, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			src.Close()
			return err
		}
		io.Copy(out, src)
		out.Close()
		src.Close()
	}
	return nil
}

func extractTar(path, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var reader io.Reader = f
	if strings.HasSuffix(strings.ToLower(path), ".tar.gz") || strings.HasSuffix(strings.ToLower(path), ".tgz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		reader = gz
	}

	tr := tar.NewReader(reader)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		destPath := filepath.Join(dest, header.Name)
		if header.FileInfo().IsDir() {
			os.MkdirAll(destPath, 0o755)
			continue
		}
		os.MkdirAll(filepath.Dir(destPath), 0o755)
		out, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, header.FileInfo().Mode())
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, tr); err != nil {
			out.Close()
			return err
		}
		out.Close()
	}
	return nil
}

// toStringSlice extracts a []string from a map value (supports []interface{} from JSON).
func toStringSlice(v interface{}) []string {
	raw, ok := v.([]interface{})
	if !ok {
		return nil
	}
	var out []string
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// resolveJSONPath extracts a value from nested JSON using dot-separated paths (e.g. "user.address.city").
func resolveJSONPath(data interface{}, path string) (interface{}, bool) {
	parts := strings.Split(path, ".")
	cur := data
	var ok bool
	for _, part := range parts {
		switch node := cur.(type) {
		case map[string]interface{}:
			cur, ok = node[part]
			if !ok {
				return nil, false
			}
		case []interface{}:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			cur = node[idx]
		default:
			return nil, false
		}
	}
	return cur, true
}

// asInt extracts an int from a map value (supports float64 from JSON, int, and json.Number).
func asInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
