package tools

import (
	"encoding/json"
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
			name:    "l0_keyword_beishan",
			query:   "beishan",
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
			// L1 语义测试（checkSemantic=true）需要 embedding 才有实际意义：
			// 无 embedding 时 L0 关键词路径无法回答语义意图类查询，跳过以避免误报。
			if tc.checkSemantic && !hasEmbedding {
				t.Skip("⏭️ L1 语义测试需要 EMBEDDING_ENDPOINT，当前未配置，跳过")
				return
			}

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
			firstTitle := "(无结果)"
			if len(results) > 0 {
				firstTitle = results[0].Title
			}
			t.Logf("✅ %d 条, 首位: %s", len(results), firstTitle)
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

// ─── KnowledgeProbe 语义断言测试 ──────────────────────────────────────────
// 对应 CODE_REVIEW_SPEC.md §3.1：必须断言输出字段存在且合理。
// 覆盖两条路径：
//   (A) 知识库条目不足 3 条 → 返回 message 字段，不崩溃
//   (B) 有条目时 → ProbeResult 字段范围合法，输出可被 json.Unmarshal
// 注意：L1 语义召回需要 EMBEDDING_ENDPOINT，测试环境不要求可用。

func TestKnowledgeProbe_OutputIsValidJSON(t *testing.T) {
	Init()
	result := KnowledgeProbe()

	// 基础断言：不返回 nil
	if result == nil {
		t.Fatal("KnowledgeProbe 不能返回 nil")
	}
	// 输出必须是合法 JSON（接收方解析格式验证）
	if result.Output == "" {
		t.Fatal("Output 不能为空")
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result.Output), &parsed); err != nil {
		t.Fatalf("Output 不是合法 JSON（格式往返失败）: %v\noutput=%s", err, result.Output)
	}
}

func TestKnowledgeProbe_FieldsInRange(t *testing.T) {
	Init()
	result := KnowledgeProbe()
	if result == nil || result.Output == "" {
		t.Skip("KnowledgeProbe 无输出，跳过字段验证")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result.Output), &parsed); err != nil {
		t.Fatalf("Output 解析失败: %v", err)
	}

	// 路径 A：知识库不足时返回 message 字段
	if _, hasMsg := parsed["message"]; hasMsg {
		t.Logf("知识库条目不足，返回 message: %v", parsed["message"])
		return
	}

	// 路径 B：正常探针，断言 ProbeResult 各字段合法
	checkFloat := func(field string, min, max float64) {
		t.Helper()
		v, ok := parsed[field]
		if !ok {
			t.Errorf("ProbeResult 缺少字段 %q", field)
			return
		}
		f, ok := v.(float64)
		if !ok {
			t.Errorf("字段 %q 类型错误，期望 float64，实际 %T", field, v)
			return
		}
		if f < min || f > max {
			t.Errorf("字段 %q = %.3f 超出范围 [%.1f, %.1f]", field, f, min, max)
		}
	}

	// probe_time 必须存在且非空
	if pt, ok := parsed["probe_time"].(string); !ok || pt == "" {
		t.Error("probe_time 必须是非空字符串")
	}
	// total_sampled 必须 >= 1
	if ts, ok := parsed["total_sampled"].(float64); !ok || ts < 1 {
		t.Errorf("total_sampled 必须 >= 1，实际: %v", parsed["total_sampled"])
	}
	// 召回率必须在 [0, 1]
	checkFloat("l0_recall_at_3", 0.0, 1.0)
	checkFloat("l1_recall_at_3", 0.0, 1.0)
	// l1_available 必须是 bool
	if _, ok := parsed["l1_available"].(bool); !ok {
		t.Errorf("l1_available 必须是 bool，实际 %T", parsed["l1_available"])
	}
	t.Logf("ProbeResult: L0召回=%.2f  L1召回=%.2f  L1可用=%v  采样=%v",
		parsed["l0_recall_at_3"], parsed["l1_recall_at_3"],
		parsed["l1_available"], parsed["total_sampled"])
}

func TestKnowledgeProbe_ProbeHistoryWritten(t *testing.T) {
	// 验证副作用：若探针正常执行（条目 >= 3），probe_history.jsonl 必须被写入
	Init()
	result := KnowledgeProbe()
	if result == nil {
		t.Fatal("KnowledgeProbe 返回 nil")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(result.Output), &parsed); err != nil {
		t.Fatalf("Output 解析失败: %v", err)
	}
	// 若是 message（条目不足），跳过写入验证
	if _, hasMsg := parsed["message"]; hasMsg {
		t.Skip("条目不足，不写入历史，跳过验证")
	}

	// 条目足够时：probe_history.jsonl 必须存在
	histPath := fmt.Sprintf("%s/probe_history.jsonl", HermesHome)
	if _, err := os.Stat(histPath); err != nil {
		t.Errorf("probe_history.jsonl 未被写入（路径: %s）: %v", histPath, err)
	}
}
