package tools

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"beishan/internal/observatory"
)

/* ─── embedding 引擎 ────────────────────────── */

var forcedEmbeddingEndpoint string

// SetEmbeddingEndpoint 程序化设置 embedding API 端点，覆盖环境变量。
// 由 main.go 在启动 embedding sidecar 后调用，使语义搜索可用。
func SetEmbeddingEndpoint(url string) {
	forcedEmbeddingEndpoint = url
}

func embeddingEndpoint() string {
	if forcedEmbeddingEndpoint != "" {
		return forcedEmbeddingEndpoint
	}
	return os.Getenv("EMBEDDING_ENDPOINT")
}
func embeddingModel() string {
	if m := os.Getenv("EMBEDDING_MODEL"); m != "" {
		return m
	}
	return "nomic-embed-text"
}

func embeddingEnabled() bool {
	return embeddingEndpoint() != ""
}

// tryEmbedding 调通用 embedding API 计算文本向量。
// 不绑定任何具体工具（Ollama/llama.cpp/glue 均可），靠环境变量配置端点。
func tryEmbedding(text string) ([]float64, bool) {
	if !embeddingEnabled() {
		return nil, false
	}
	// 截断到 300 字符（nomic-embed 的 embedding 上下文窗口实测 ~512 token）
	runes := []rune(text)
	if len(runes) > 300 {
		text = string(runes[:300])
	}
	body, err := json.Marshal(map[string]interface{}{
		"model": embeddingModel(),
		"input": text,
	})
	if err != nil {
		return nil, false
	}
	req, err := http.NewRequest("POST", embeddingEndpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	req.Header.Set("Content-Type", "application/json")
	if k := os.Getenv("EMBEDDING_API_KEY"); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	} else if k := os.Getenv("LOCAL_API_KEY"); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	var result struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, false
	}
	if len(result.Data) == 0 || len(result.Data[0].Embedding) == 0 {
		return nil, false
	}
	return result.Data[0].Embedding, true
}

// searchByEmbedding 向量相似度检索。
// 无 embedding 的条目异步触发惰性补全。
func searchByEmbedding(queryEmb []float64, limit int, namespace string) []ScoredEntry {
	all := loadAllKnowledge()
	var scored []ScoredEntry
	var pending []*KnowledgeEntry

	for _, entry := range all {
		// 跳过过期和已归档条目
		if entry.Status != "" && entry.Status != "active" {
			continue
		}
		if !matchNamespace(entry, namespace) {
			continue
		}
		if entry.Ephemeral && entry.ExpiresAt > 0 && time.Now().Unix() > entry.ExpiresAt {
			continue
		}
		if len(entry.Embedding) == 0 {
			pending = append(pending, entry)
			continue
		}
		sim := cosineSimilarity(queryEmb, entry.Embedding)
		if sim >= 0.4 {
			scored = append(scored, ScoredEntry{
				ID:         entry.ID,
				Title:      entry.Title,
				Summary:    entry.Summary,
				Tags:       entry.Tags,
				SourceType: entry.SourceType,
				Score:      int(sim * 100),
			})
		}
	}

	if len(pending) > 0 {
		observatory.SafeGo("knowledge.batchFillEmbedding", func() { batchFillEmbedding(pending) })
	}

	sort.Slice(scored, func(i, j int) bool {
		// 同分数时 memory 优先
		if scored[i].Score == scored[j].Score {
			im := scored[i].SourceType == "memory"
			jm := scored[j].SourceType == "memory"
			if im != jm {
				return im
			}
		}
		return scored[i].Score > scored[j].Score
	})
	if len(scored) > limit {
		scored = scored[:limit]
	}
	return scored
}

// batchFillEmbedding 批量补全 embedding，后台调用。
func batchFillEmbedding(entries []*KnowledgeEntry) {
	if !embeddingEnabled() {
		return
	}
	for _, e := range entries {
		if len(e.Embedding) > 0 {
			continue
		}
		text := e.Title + " " + e.Summary
		if emb, ok := tryEmbedding(text); ok {
			e.Embedding = emb
			saveKnowledge(e)
		}
	}
}

/* ─── KnowledgeReindex 批量补全工具 ──────────── */

// embeddingText 为知识条目生成用于计算向量的文本。
// 优先使用 content（有实质内容时），回退到 title+summary。
// 检测 summary 是否为 macOS 系统噪声（形如 "darwin/20.x.x ..."），若是则忽略。
func embeddingText(e *KnowledgeEntry) string {
	if e.Content != "" {
		return e.Title + " " + e.Content
	}
	// 匹配 macOS 内核版本字符串噪声：小写 "darwin/" 后跟版本号
	if strings.Contains(strings.ToLower(e.Summary), "darwin/") && len(e.Summary) > 30 {
		return e.Title
	}
	return e.Title + " " + e.Summary
}

/*
KnowledgeReindex 为所有无 embedding 或维度不匹配的知识条目重新计算语义向量。

	force=true 时强制重算所有条目（忽略已有 embedding）。
*/
func KnowledgeReindex() *ToolResult {
	if !embeddingEnabled() {
		return successResult(`{"message":"EMBEDDING_ENDPOINT 未设置，跳过"}`)
	}
	all := loadAllKnowledge()
	if len(all) == 0 {
		return successResult(`{"message":"知识库为空，跳过","count":0}`)
	}
	var count int
	// 先用一条文本探测当前 API 的向量维度
	probeText := all[0].Title
	probeEmb, ok := tryEmbedding(probeText)
	if !ok || len(probeEmb) == 0 {
		return successResult(`{"message":"embedding API 不可用，跳过"}`)
	}
	apiDim := len(probeEmb)

	for _, e := range all {
		// 跳过已有正确维度的 embedding
		if len(e.Embedding) == apiDim {
			continue
		}
		text := embeddingText(e)
		if emb, ok := tryEmbedding(text); ok && len(emb) == apiDim {
			e.Embedding = emb
			saveKnowledge(e)
			count++
		}
	}
	return successResult(fmt.Sprintf(`{"message":"重算完成","count":%d,"dim":%d}`, count, apiDim))
}

// KnowledgeBackup 将知识库目录备份到带时间戳的子目录。
//
// 备份内容：
//   - knowledgeDir（所有 .json 知识条目）
//   - calibration.jsonl（分类校准数据）
//
// 保留策略：最多保留最近 7 份，自动删除更早的备份。
// 默认目录：~/.hermes/backups/knowledge_YYYYMMDD_HHMMSS
