package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

/* ─── 知识库导入导出 ────────────────────────────

   knowledge_export：导出所有条目为 JSON 文件（含 BOW 向量文件路径）
   knowledge_import：从 JSON 文件导入（冲突时返回详情，force=true 覆盖）

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

type importConflict struct {
	ID        string `json:"id"`
	OldTitle  string `json:"old_title"`
	NewTitle  string `json:"new_title"`
	OldStatus string `json:"old_status,omitempty"`
}

func KnowledgeImportHandler(args map[string]interface{}) *ToolResult {
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult("path（导入文件路径）不能为空")
	}
	force, _ := args["force"].(bool)

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
	var conflicts []importConflict

	for _, e := range entries {
		if e.ID == "" {
			continue
		}
		existing := loadKnowledge(e.ID)
		if existing != nil {
			if force {
				// 覆盖前保存版本快照
				saveVersionSnapshot(e.ID, existing)
				saveKnowledge(e)
				imported++
				fmt.Printf("[sync] 覆盖条目 %s: %q → %q\n", e.ID, existing.Title, e.Title)
			} else {
				conflicts = append(conflicts, importConflict{
					ID:        e.ID,
					OldTitle:  existing.Title,
					NewTitle:  e.Title,
					OldStatus: existing.Status,
				})
				skipped++
			}
			continue
		}
		saveKnowledge(e)
		imported++
	}

	result := map[string]interface{}{
		"imported":  imported,
		"skipped":   skipped,
		"conflicts": conflicts,
		"total":     len(entries),
	}
	if len(conflicts) > 0 {
		result["message"] = fmt.Sprintf("导入 %d 条，跳过 %d 条冲突。使用 force:true 可覆盖（旧版本自动备份到 history/）", imported, len(conflicts))
	} else {
		result["message"] = fmt.Sprintf("导入 %d 条，跳过 %d 条", imported, skipped)
	}
	b, _ := json.Marshal(result)
	return successResult(string(b))
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

	Register("knowledge_import", "从 JSON 文件导入知识条目。冲突时返回详情，force=true 可覆盖（旧版本自动备份）。用于跨机器手动同步。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"path"},
			"properties": map[string]interface{}{
				"path":  stringParam("导入文件路径"),
				"force": boolParam("冲突时覆盖已有条目（旧版本自动备份到 history/）"),
			},
		},
		KnowledgeImportHandler,
	)
}
