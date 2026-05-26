// Package tools provides tool registration, validation, and schema management.
//
// Port of FangLab internal/tools/filesystem/ tools (2026-05-26).
// Provides: file_guess_type, file_preview, archive_extract.
package tools

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
