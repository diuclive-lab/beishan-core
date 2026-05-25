package tools

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
)

/* ─── CSV 分析工具（纯 Go，零依赖）─────────────

   两个工具：
   - csv_profile:   CSV 文件概览（列数/行数/列名/类型/样本值）
   - csv_sample:    CSV 数据预览（前 N 行）
*/

func registerCSVTools() {
	Register("csv_profile", "Analyze a CSV file: column names, types (number/text), row count, sample values.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("CSV file path"),
			},
		},
		csvProfileHandler,
	)

	Register("csv_sample", "Preview first N rows of a CSV file.",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("CSV file path"),
				"rows": intParam("Number of rows to show (default 5)"),
			},
		},
		csvSampleHandler,
	)
}

func csvProfileHandler(args map[string]interface{}) *ToolResult {
	path := strArg(args, "path")
	if path == "" {
		return errorResult("path is required")
	}

	f, err := os.Open(path)
	if err != nil {
		return errorResult(fmt.Sprintf("cannot open file: %v", err))
	}
	defer f.Close()

	r := csv.NewReader(f)
	headers, err := r.Read()
	if err != nil {
		return errorResult(fmt.Sprintf("cannot read CSV: %v", err))
	}

	colTypes := make([]string, len(headers))
	colSamples := make([]string, len(headers))
	rows := 0

	for rows < 100 {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		rows++
		for i, v := range record {
			if i >= len(headers) {
				continue
			}
			if colTypes[i] == "" {
				if _, err := strconv.ParseFloat(v, 64); err == nil {
					colTypes[i] = "number"
				} else if v != "" {
					colTypes[i] = "text"
				}
			}
			if colSamples[i] == "" && v != "" {
				colSamples[i] = v
			}
		}
	}

	result := fmt.Sprintf("=== CSV Profile: %s ===\n", path)
	result += fmt.Sprintf("Columns: %d  Rows: %d+\n\n", len(headers), rows)
	for i, h := range headers {
		t := colTypes[i]
		if t == "" {
			t = "empty"
		}
		s := colSamples[i]
		if s == "" {
			s = "-"
		}
		if len(s) > 40 {
			s = s[:40] + "..."
		}
		result += fmt.Sprintf("  [%d] %s (%s) e.g. \"%s\"\n", i, h, t, s)
	}
	return successResult(result)
}

func csvSampleHandler(args map[string]interface{}) *ToolResult {
	path := strArg(args, "path")
	if path == "" {
		return errorResult("path is required")
	}

	n := 5
	if v, ok := args["rows"]; ok {
		if vi, ok := v.(float64); ok && int(vi) > 0 {
			n = int(vi)
		}
	}

	f, err := os.Open(path)
	if err != nil {
		return errorResult(fmt.Sprintf("cannot open file: %v", err))
	}
	defer f.Close()

	r := csv.NewReader(f)
	headers, err := r.Read()
	if err != nil {
		return errorResult(fmt.Sprintf("cannot read CSV: %v", err))
	}

	result := fmt.Sprintf("=== CSV Sample (top %d): %s ===\n\n", n, path)
	result += fmt.Sprintf("%v\n", headers)

	count := 0
	for count < n {
		record, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		result += fmt.Sprintf("%v\n", record)
		count++
	}
	result += fmt.Sprintf("\n%d rows shown", count)
	return successResult(result)
}
