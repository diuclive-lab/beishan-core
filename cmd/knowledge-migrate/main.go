// cmd/knowledge-migrate — 知识库一次性数据迁移工具
//
// 当前包含的迁移：
//   strip-hw-prefix  去除 summary 字段中被错误注入的 【硬件信息】前缀
//                    （bug 来源：knowledge.go 曾在每次写入时调用 HardwareSummary()，
//                     已于 2026-05-28 删除该逻辑）
//
// 用法：
//   go run ./cmd/knowledge-migrate              # dry-run，只打印不修改
//   go run ./cmd/knowledge-migrate --apply      # 实际写入
//   go run ./cmd/knowledge-migrate --apply --verbose
//
// 幂等：可反复执行，无副作用。

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var hwPrefixRe = regexp.MustCompile(`^(【[^】]*】)+`)

func main() {
	apply   := flag.Bool("apply",   false, "实际写入（默认 dry-run）")
	verbose := flag.Bool("verbose", false, "打印每条变更详情")
	flag.Parse()

	kdir := knowledgeDir()
	entries, err := os.ReadDir(kdir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "无法读取知识库目录 %s: %v\n", kdir, err)
		os.Exit(1)
	}

	var total, polluted, fixed, emptyAfterClean int
	var emptyIDs []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || strings.HasSuffix(e.Name(), ".embed.json") {
			continue
		}
		total++
		path := filepath.Join(kdir, e.Name())

		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取失败 %s: %v\n", e.Name(), err)
			continue
		}

		var entry map[string]interface{}
		if err := json.Unmarshal(raw, &entry); err != nil {
			fmt.Fprintf(os.Stderr, "解析失败 %s: %v\n", e.Name(), err)
			continue
		}

		summary, _ := entry["summary"].(string)
		if !hwPrefixRe.MatchString(summary) {
			continue
		}
		polluted++

		clean := strings.TrimSpace(hwPrefixRe.ReplaceAllString(summary, ""))
		if clean == "" {
			emptyAfterClean++
			id, _ := entry["id"].(string)
			emptyIDs = append(emptyIDs, fmt.Sprintf("[%s] %s", id, entry["title"]))
		}
		if *verbose {
			id, _ := entry["id"].(string)
			afterPreview := clean
			if afterPreview == "" {
				afterPreview = "(空 — 需人工补写 summary)"
			}
			fmt.Printf("[%s] %s\n  before: %s\n  after:  %s\n\n",
				id, entry["title"], summary[:min(80, len(summary))], afterPreview[:min(80, len(afterPreview))])
		}

		if !*apply {
			continue
		}

		entry["summary"] = clean
		out, _ := json.MarshalIndent(entry, "", "  ")
		if err := os.WriteFile(path, out, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入失败 %s: %v\n", e.Name(), err)
			continue
		}
		fixed++
	}

	fmt.Printf("\n=== knowledge-migrate: strip-hw-prefix ===\n")
	fmt.Printf("扫描条目: %d\n", total)
	fmt.Printf("污染条目: %d\n", polluted)
	if *apply {
		fmt.Printf("已修复:   %d\n", fixed)
	} else {
		fmt.Printf("（dry-run，未修改。加 --apply 执行）\n")
	}
	if emptyAfterClean > 0 {
		fmt.Printf("\n⚠️  strip 后 summary 为空的条目（%d 条，需人工补写）：\n", emptyAfterClean)
		for _, s := range emptyIDs {
			fmt.Printf("   %s\n", s)
		}
	}
}

func knowledgeDir() string {
	home := os.Getenv("HERMES_HOME")
	if home == "" {
		h, _ := os.UserHomeDir()
		home = filepath.Join(h, ".hermes")
	}
	return filepath.Join(home, "memory", "knowledge")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
