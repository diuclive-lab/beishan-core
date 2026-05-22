package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

/* ─── 知识库导入导出 ────────────────────────────

   knowledge_export：导出所有条目为 JSON 文件（含 BOW 向量文件路径）
   knowledge_import：从 JSON 文件导入（跳过已存在的 ID）

   用于两台机器之间手动同步知识库。
*/

func KnowledgeExportHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	if path == "" {
		path = filepath.Join(HermesHome, "knowledge_export.json")
	}

	all := loadAllKnowledge()
	if len(all) == 0 {
		return errorResult("知识库为空")
	}

	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.MarshalIndent(all, "", "  ")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return errorResult(fmt.Sprintf("写入失败: %v", err))
	}
	return successResult(fmt.Sprintf(`{"path":"%s","count":%d,"message":"已导出 %d 条知识。导入后请运行 knowledge_embed_all 补 BOW 向量。"}`, path, len(all), len(all)))
}

func KnowledgeImportHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult("path（导入文件路径）不能为空")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return errorResult(fmt.Sprintf("读取文件失败: %v", err))
	}
	var entries []*KnowledgeEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return errorResult(fmt.Sprintf("解析 JSON 失败: %v", err))
	}

	imported := 0
	skipped := 0
	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		if existing := loadKnowledge(e.ID); existing != nil {
			skipped++
			continue
		}
		saveKnowledge(e)
		imported++
	}

	return successResult(fmt.Sprintf(
		`{"imported":%d,"skipped":%d,"total":%d,"message":"导入 %d 条，跳过 %d 条"}`,
		imported, skipped, len(entries), imported, skipped))
}

func registerSyncTools() {
	Register("knowledge_export", "导出全部知识条目为 JSON 文件。用于跨机器手动同步。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{
				"path": stringParam("导出路径，默认 ~/.hermes/knowledge_export.json"),
			},
		},
		KnowledgeExportHandler,
	)

	Register("knowledge_import", "从 JSON 文件导入知识条目（跳过已存在的 ID）。用于跨机器手动同步。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path": stringParam("导入文件路径"),
			},
		},
		KnowledgeImportHandler,
	)
}
