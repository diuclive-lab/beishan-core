package tools

import (
	"strings"
	"testing"
)

func TestMarkdownToBlocks_Headings(t *testing.T) {
	md := "# 一级标题\n\n## 二级标题\n\n### 三级标题"
	blocks := MarkdownToBlocks(md)

	if len(blocks) != 3 {
		t.Fatalf("期望 3 个块，实际 %d", len(blocks))
	}
	if blocks[0].Type != BlockHeading {
		t.Errorf("第一个块应为 heading，实际 %s", blocks[0].Type)
	}
	if blocks[1].Type != BlockHeading {
		t.Errorf("第二个块应为 heading，实际 %s", blocks[1].Type)
	}
}

func TestMarkdownToBlocks_Paragraph(t *testing.T) {
	md := "这是一段普通文本。\n\n这是另一段。"
	blocks := MarkdownToBlocks(md)

	if len(blocks) < 2 {
		t.Fatalf("期望至少 2 个块，实际 %d", len(blocks))
	}
	if blocks[0].Type != BlockParagraph {
		t.Errorf("应为 paragraph，实际 %s", blocks[0].Type)
	}
	if !strings.Contains(blocks[0].Content, "普通文本") {
		t.Errorf("内容应包含'普通文本'，实际 %q", blocks[0].Content)
	}
}

func TestMarkdownToBlocks_CodeBlock(t *testing.T) {
	md := "```go\nfunc main() {}\n```"
	blocks := MarkdownToBlocks(md)

	if len(blocks) < 1 {
		t.Fatalf("期望至少 1 个块")
	}
	if blocks[0].Type != BlockCode {
		t.Errorf("应为 code block，实际 %s", blocks[0].Type)
	}
}

func TestMarkdownToBlocks_List(t *testing.T) {
	md := "- 项目一\n- 项目二\n- 项目三"
	blocks := MarkdownToBlocks(md)

	if len(blocks) < 1 {
		t.Fatalf("期望至少 1 个块")
	}
	if blocks[0].Type != BlockList {
		t.Errorf("应为 list，实际 %s", blocks[0].Type)
	}
}

func TestMarkdownToBlocks_Blockquote(t *testing.T) {
	md := "> 这是一段引用"
	blocks := MarkdownToBlocks(md)

	if len(blocks) < 1 {
		t.Fatalf("期望至少 1 个块")
	}
	if blocks[0].Type != BlockQuote {
		t.Errorf("应为 blockquote，实际 %s", blocks[0].Type)
	}
}

func TestMarkdownToBlocks_Table(t *testing.T) {
	md := "| 名称 | 值 |\n|------|----|\n| A   | 1 |"
	blocks := MarkdownToBlocks(md)

	if len(blocks) < 1 {
		t.Fatalf("期望至少 1 个块")
	}
	if blocks[0].Type != BlockTable {
		t.Errorf("应为 table，实际 %s", blocks[0].Type)
	}
}

func TestBlocksToMarkdown_RoundTrip(t *testing.T) {
	md := "# 测试文档\n\n这是一段正文。\n\n- 列表项一\n- 列表项二\n\n```go\npackage main\n```\n\n> 引用文本"
	blocks := MarkdownToBlocks(md)

	if len(blocks) == 0 {
		t.Fatal("MarkdownToBlocks 返回空")
	}

	// 验证每种类型都存在
	types := map[BlockType]int{}
	for _, b := range blocks {
		types[b.Type]++
	}
	if types[BlockHeading] == 0 {
		t.Error("缺少 heading 块")
	}
	if types[BlockParagraph] == 0 {
		t.Error("缺少 paragraph 块")
	}
	if types[BlockList] == 0 {
		t.Error("缺少 list 块")
	}
	if types[BlockCode] == 0 {
		t.Error("缺少 code 块")
	}

	// 验证内容完整
	reconstructed := blocksToMarkdown(blocks)
	if reconstructed == "" {
		t.Fatal("BlocksToMarkdown 返回空")
	}
	if !strings.Contains(reconstructed, "测试文档") {
		t.Errorf("重建 markdown 应包含'测试文档': %s", reconstructed[:min(60, len(reconstructed))])
	}
}
