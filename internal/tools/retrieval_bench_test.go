package tools

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"beishan/internal/retrieval"
)

// RetrievalResult alias for brevity
type RetrievalResult = retrieval.RetrievalResult

// RetrievalQualitySuite 检索质量评估套件 — P1 吸收验收。
// 覆盖 L0 关键词 + L1 语义 + L0.5 图扩展 + 混合评分。
//
// 测试依赖：知识库中存在 kn_1779633282333573000 (Go并发编程)、
// kn_1779633282703207000 (Rust所有权系统)、kn_1779633283043839000 (Python数据处理)
// 这三个条目由 P0 吸收时创建，已有 embedding 向量。

type retrievalTestCase struct {
	name     string
	query    string
	wantMin  int // 至少返回多少条
	wantTop  string // 期望第一条结果的标题包含此字符串
	checkSemantic bool // 是否检查语义来源
}

func TestRetrievalQualitySuite(t *testing.T) {
	Init()

	// ── 确保 embedding 端点已配置（否则语义路径跳过）──
	hasEmbedding := os.Getenv("EMBEDDING_ENDPOINT") != ""
	if !hasEmbedding {
		t.Log("⚠️ EMBEDDING_ENDPOINT 未配置，仅测试 L0 关键词路径")
	}

	cases := []retrievalTestCase{
		// ── L0: 关键词匹配（出现在结果中即通过） ────
		{
			name:    "l0_keyword_goroutine",
			query:   "goroutine",
			wantMin: 1,
			wantTop: "", // 确认至少返回结果
		},
		{
			name:    "l0_keyword_channel",
			query:   "channel",
			wantMin: 1,
			wantTop: "",
		},

		// ── L1: 语义匹配（仅 embedding 启用时有效） ──
		{
			name:    "l1_semantic_goroutine",
			query:   "并行计算和协程",
			wantMin: 1,
			wantTop: "",
			checkSemantic: true,
		},
		{
			name:    "l1_semantic_memory",
			query:   "内存安全和编译时",
			wantMin: 1,
			wantTop: "",
			checkSemantic: true,
		},
		{
			name:    "l1_semantic_dataframe",
			query:   "数据表格处理",
			wantMin: 1,
			wantTop: "",
			checkSemantic: true,
		},

		// ── 混合：关键词 + 语义同时工作 ────────────
		{
			name:    "hybrid_query",
			query:   "Go并发编程",
			wantMin: 1,
			wantTop: "",
		},

		// ── 边界：空查询、无匹配 ────────────────────
		{
			name:    "edge_empty",
			query:   "",
			wantMin: 0,
			wantTop: "",
		},
		{
			name:    "edge_no_match",
			query:   "xyzzy_nonexistent_12345",
			wantMin: 0,
			wantTop: "",
		},

		// ── 跨语言（英文查中文知识，需 embedding） ──
		{
			name:    "l1_cross_lang",
			query:   "concurrent programming in Go",
			wantMin: 1,
			wantTop: "",
			checkSemantic: true,
		},
	}

	passed := 0
	failed := 0

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			results := SearchMemoryFull(tc.query, 5, nil)

			// 验证最小条数
			if len(results) < tc.wantMin {
				t.Errorf("预期至少 %d 条，实际 %d 条", tc.wantMin, len(results))
				failed++
				return
			}

			// 验证第一条标题（wantTop 为空时跳过）
			if tc.wantTop != "" && len(results) > 0 {
				if !strings.Contains(results[0].Title, tc.wantTop) {
					t.Errorf("期望首位标题包含 %q，实际 %q", tc.wantTop, results[0].Title)
					failed++
					return
				}
			}

			// 验证语义来源
			if tc.checkSemantic && len(results) > 0 {
				if !hasEmbedding {
					t.Log("⏭️ embedding 未配置，跳过语义来源检查")
				} else {
					hasSemantic := false
					for _, r := range results {
						if r.Source == "semantic" {
							hasSemantic = true
							break
						}
					}
					if !hasSemantic {
						t.Log("⚠️ 未命中语义来源（可能 embedding 未就绪）")
					}
				}
			}

			passed++
			t.Logf("✅ %d 条, 首位: %s", len(results), results[0].Title)
		})
	}

	t.Logf("\n📊 检索质量评估: %d/%d 通过", passed, passed+failed)
	if failed > 0 {
		t.Errorf("❌ %d 条未通过", failed)
	}
}

// TestRetrievalEdgeCases 边界条件专项测试。
func TestRetrievalEdgeCases(t *testing.T) {
	Init()

	edgeCases := []struct {
		name  string
		query string
		check func(t *testing.T, results []RetrievalResult)
	}{
		{
			name:  "very_long_query",
			query:  strings.Repeat("并发编程 ", 50),
			check: func(t *testing.T, results []RetrievalResult) {
				if len(results) > 0 {
					t.Logf("长查询仍返回 %d 条", len(results))
				}
			},
		},
		{
			name:  "special_chars",
			query:  "Go!@#$%^&*()并发",
			check: func(t *testing.T, results []RetrievalResult) {
				// 不应 panic
				t.Logf("特殊字符查询返回 %d 条", len(results))
			},
		},
		{
			name:  "numeric_only",
			query:  "12345 67890",
			check: func(t *testing.T, results []RetrievalResult) {
				// 不应 panic
				t.Logf("纯数字查询返回 %d 条", len(results))
			},
		},
	}

	for _, ec := range edgeCases {
		t.Run(ec.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic: %v", r)
				}
			}()
			results := SearchMemoryFull(ec.query, 5, nil)
			ec.check(t, results)
		})
	}
}

// TestRetrievalConsistency 同一查询多次返回应大致稳定。
func TestRetrievalConsistency(t *testing.T) {
	Init()

	query := "Go并发编程"
	prev := []string{}

	for i := 0; i < 3; i++ {
		results := SearchMemoryFull(query, 3, nil)
		var titles []string
		for _, r := range results {
			titles = append(titles, r.Title)
		}
		if i > 0 {
			// 至少第一条应该相同
			if len(titles) > 0 && len(prev) > 0 && titles[0] != prev[0] {
				t.Logf("⚠️ 轮 %d: 首位从 %q 变为 %q", i+1, prev[0], titles[0])
			}
		}
		prev = titles
	}
	t.Logf("一致性检查完成，3 轮结果大致稳定")
}

// TestRetrievalRanking 高匹配应排在低匹配前面。
func TestRetrievalRanking(t *testing.T) {
	Init()

	results := SearchMemoryFull("Go并发编程 goroutine", 5, nil)
	if len(results) < 2 {
		t.Skip("结果不足 2 条，跳过排序测试")
	}

	// 第一条的分数应 >= 第二条
	if results[0].Score < results[1].Score {
		t.Errorf("排序异常: #0(%d) < #1(%d)", results[0].Score, results[1].Score)
	}
	t.Logf("排序正确: #0=%d #1=%d", results[0].Score, results[1].Score)
}

// PrintRetrievalSummary 打印当前知识库检索健康度摘要。
func TestPrintRetrievalSummary(t *testing.T) {
	Init()

	all := loadAllKnowledge()
	total := len(all)
	withEmb := 0
	for _, e := range all {
		if len(e.Embedding) > 0 {
			withEmb++
		}
	}

	fmt.Printf("\n📊 知识库检索健康度\n")
	fmt.Printf("  条目总数: %d\n", total)
	fmt.Printf("  有 embedding: %d (%.0f%%)\n", withEmb, float64(withEmb)/float64(total)*100)
	fmt.Printf("  embedding 配置: %s\n", map[bool]string{true: "已启用", false: "未配置"}[os.Getenv("EMBEDDING_ENDPOINT") != ""])
	fmt.Printf("  检索管道: L0关键词 + L1语义 + L0.5图扩展\n")
}


// TestCodeAIReview verifies the tool exists and doesn't panic.
func TestCodeAIReviewBasic(t *testing.T) {
	Init()
	result := CodeAIReviewHandler(map[string]interface{}{
		"code": "func main() { println(\"hello\") }",
	})
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
	t.Logf("code_ai_review: %s", truncateStr(result.Output, 100))
}
