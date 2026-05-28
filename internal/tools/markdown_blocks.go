package tools

import (
	"github.com/88250/lute"
	"github.com/88250/lute/ast"
)

var luteEngine = lute.New()

func init() {
	luteEngine.SetAutoSpace(true)
	luteEngine.SetChineseParagraphBeginningSpace(true)
	luteEngine.SetGFMTable(true)
	luteEngine.SetGFMTaskListItem(true)
	luteEngine.SetGFMStrikethrough(true)
	luteEngine.SetGFMAutoLink(true)
	luteEngine.SetCodeSyntaxHighlight(true)
	luteEngine.SetEmoji(true)
	luteEngine.SetFootnotes(true)
	luteEngine.SetToC(true)
}

// MarkdownToBlocks 将 markdown 文本解析为 Block 列表。
func MarkdownToBlocks(markdown string) []*Block {
	if markdown == "" {
		return nil
	}

	_, tree := luteEngine.Md2BlockDOMTree(markdown, false)
	if tree == nil || tree.Root == nil {
		return nil
	}

	var blocks []*Block
	for c := tree.Root.FirstChild; c != nil; c = c.Next {
		block := luteNodeToBlock(c)
		if block != nil {
			blocks = append(blocks, block)
		}
	}
	return blocks
}

// luteNodeToBlock 将 Lute AST 节点转换为 Block。
func luteNodeToBlock(node *ast.Node) *Block {
	if node == nil {
		return nil
	}

	b := &Block{
		ID:      node.ID,
		Content: node.Text(),
		Markdown: node.TokensStr(),
	}

	switch node.Type {
	case ast.NodeDocument:
		b.Type = BlockDocument
	case ast.NodeHeading:
		b.Type = BlockHeading
	case ast.NodeParagraph:
		b.Type = BlockParagraph
	case ast.NodeList:
		b.Type = BlockList
	case ast.NodeListItem:
		b.Type = BlockListItem
	case ast.NodeCodeBlock:
		b.Type = BlockCode
	case ast.NodeBlockquote:
		b.Type = BlockQuote
	case ast.NodeTable:
		b.Type = BlockTable
	case ast.NodeImage:
		b.Type = BlockImage
	default:
		b.Type = BlockParagraph
	}

	return b
}

// BlocksToMarkdown 将 Block 列表渲染为 markdown 文本。
// 将 block 内容拼接为 markdown，用 Lute 格式化输出。
func blocksToMarkdown(blocks []*Block) string {
	if len(blocks) == 0 {
		return ""
	}

	// 简单的块→文本拼接，然后用 Lute 格式化
	var raw string
	for _, b := range blocks {
		switch b.Type {
		case BlockHeading:
			prefix := "# "
			if b.Depth > 1 {
				prefix = string(make([]byte, b.Depth))
				for i := range prefix {
					prefix = prefix[:i] + "#"
				}
				prefix += " "
			}
			raw += prefix + b.Content + "\n\n"
		case BlockCode:
			raw += "```\n" + b.Content + "\n```\n\n"
		case BlockQuote:
			raw += "> " + b.Content + "\n\n"
		case BlockList, BlockListItem:
			raw += "- " + b.Content + "\n"
		default:
			raw += b.Content + "\n\n"
		}
	}

	formatted := luteEngine.FormatStr("block", raw)
	return formatted
}

// luteNodeToBlockType 映射 Lute 节点类型到 BlockType。
func luteNodeToBlockType(nodeType ast.NodeType) BlockType {
	switch nodeType {
	case ast.NodeHeading:
		return BlockHeading
	case ast.NodeParagraph:
		return BlockParagraph
	case ast.NodeList:
		return BlockList
	case ast.NodeListItem:
		return BlockListItem
	case ast.NodeCodeBlock:
		return BlockCode
	case ast.NodeBlockquote:
		return BlockQuote
	case ast.NodeTable:
		return BlockTable
	default:
		return BlockParagraph
	}
}

// RenderBlocksToHTML 使用 Lute 将 blocks 渲染为 HTML。
func renderBlocksToHTML(blocks []*Block) string {
	md := blocksToMarkdown(blocks)
	if md == "" {
		return ""
	}
	return luteEngine.MarkdownStr("render", md)
}
