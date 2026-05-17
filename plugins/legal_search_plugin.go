package plugins

import (
	"encoding/json"
	"fmt"

	"beishan/internal/tools"
	"beishan/kernel"
)

/* LegalSearchPlugin 中国法律检索插件（L3）。

   职责：接收法律画像/检索请求，适配中国法律数据库查询结构：
   - 北大法宝 (pkulaw.com) — 法律法规、司法案例、法学期刊
   - 威科先行 (hk.wkinfo.com.cn) — 法律法规、实务指南
   - 中国裁判文书网 (wenshu.court.gov.cn) — 裁判文书

   检索策略：
   1. 先查法律层级（法律 → 行政法规 → 司法解释 → 部门规章）
   2. 再查相关案例（最高人民法院指导性案例 → 典型案例）
   3. 最后查学术观点（辅助参考）

   输出：结构化法律引用清单，含效力层级标识。
*/
type LegalSearchPlugin struct{}

// LegalSearchQuery 法律检索请求结构。
type LegalSearchQuery struct {
	Keywords      []string `json:"keywords,omitempty"`       // 检索关键词
	Laws          []string `json:"laws,omitempty"`           // 特定法律引用，如 民法典 第584条
	ContractType  string   `json:"contract_type,omitempty"`  // 合同类型（缩小检索范围）
	LegalArea     string   `json:"legal_area,omitempty"`     // 法律领域
	Jurisdiction  string   `json:"jurisdiction,omitempty"`   // 管辖法律
	SearchTarget  string   `json:"search_target,omitempty"`  // 检索目标：laws/cases/commentary
}

// LegalSearchResult 法律检索结果结构。
type LegalSearchResult struct {
	Statutes []LegalReference `json:"statutes"`          // 法律法规
	Cases    []LegalReference `json:"cases,omitempty"`   // 司法案例
	Articles []LegalReference `json:"articles,omitempty"` // 学术文章/实务指南
}

// LegalReference 单一法律引用。
type LegalReference struct {
	Title    string `json:"title"`               // 名称，如"中华人民共和国民法典"
	Citation string `json:"citation"`            // 引用编号，如"中华人民共和国主席令第45号"
	Articles []int  `json:"articles,omitempty"`  // 具体条款
	Level    string `json:"level"`               // 效力层级：宪法/法律/行政法规/司法解释/部门规章
	Source   string `json:"source"`              // 来源：pkulaw/wkinfo/wenshu
	URL      string `json:"url,omitempty"`       // 原文链接
	Summary  string `json:"summary,omitempty"`   // 摘要（检索来源返回时填充）
}

// 中国法律效力层级排序（从高到低）
var chineseLegalHierarchy = []string{
	"宪法",
	"法律",
	"司法解释",
	"行政法规",
	"地方性法规",
	"部门规章",
	"规范性文件",
	"行业惯例",
}

func (p *LegalSearchPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	switch msg.Type {
	case "legal_search":
		return p.handleLegalSearch(msg)
	default:
		return kernel.Message{}, fmt.Errorf("legal_search_plugin: 未知消息类型 %s", msg.Type)
	}
}

func (p *LegalSearchPlugin) handleLegalSearch(msg kernel.Message) (kernel.Message, error) {
	var query LegalSearchQuery
	if err := json.Unmarshal(msg.Payload, &query); err != nil {
		// 纯文本回退：把 payload 当作关键词
		var text string
		if e2 := json.Unmarshal(msg.Payload, &text); e2 != nil {
			return kernel.Message{}, fmt.Errorf("legal_search: payload 解析失败: %w", err)
		}
		query = LegalSearchQuery{Keywords: []string{text}}
	}

	// 执行检索：先查法律条文，再查案例
	result := LegalSearchResult{}

	// 1. 检索法律法规
	statutes, err := p.searchStatutes(query)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("legal_search: 法律检索失败: %w", err)
	}
	result.Statutes = statutes

	// 2. 检索司法案例（如需要）
	if query.SearchTarget == "" || query.SearchTarget == "cases" || query.SearchTarget == "all" {
		cases, err := p.searchCases(query, statutes)
		if err == nil {
			result.Cases = cases
		}
	}

	// 3. 检索学术观点（如需要）
	if query.SearchTarget == "commentary" || query.SearchTarget == "all" {
		articles, err := p.searchCommentary(query)
		if err == nil {
			result.Articles = articles
		}
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("legal_search: 序列化结果失败: %w", err)
	}

	return kernel.Message{
		Type:    "legal_search.result",
		Payload: payload,
	}, nil
}

/* searchStatutes 检索中国法律法规。

   数据源优先级：
   1. 中国司法大数据服务网 (data.court.gov.cn)
   2. 北大法宝 (pkulaw.com)
   3. 通用 web_search (DuckDuckGo) 回退

   检索结果按法律效力层级排序（宪法 > 法律 > 司法解释 > 行政法规 > 部门规章）。
*/
func (p *LegalSearchPlugin) searchStatutes(query LegalSearchQuery) ([]LegalReference, error) {
	searchTerms := buildSearchTerms(query)
	if len(searchTerms) == 0 {
		return nil, fmt.Errorf("检索关键词为空")
	}

	// 1. 司法大数据服务网优先
	if refs, ok := tryJudicialSearch(searchTerms[0], query); ok {
		sortByLegalHierarchy(refs)
		return refs, nil
	}

	// 2. 北大法宝专用检索
	if refs, ok := tryPkulawSearch(searchTerms[0]); ok {
		sortByLegalHierarchy(refs)
		return refs, nil
	}

	// 3. 回退到通用 web_search
	payload, _ := json.Marshal(map[string]interface{}{
		"query": searchTerms[0] + " 中国法律法规",
	})
	result := tools.ValidateAndExecute("web_search", payload)
	if !result.Success {
		return buildDefaultReferences(query), nil
	}

	refs := parseWebSearchResult(result.Output, query)
	sortByLegalHierarchy(refs)
	return refs, nil
}

/* searchCases 检索中国司法案例。

   数据源：
   - 中国裁判文书网 (wenshu.court.gov.cn)
   - 北大法宝司法案例库
   - 最高人民法院指导性案例

   优先检索最高人民法院指导性案例，其次为典型案例。
*/
func (p *LegalSearchPlugin) searchCases(query LegalSearchQuery, statutes []LegalReference) ([]LegalReference, error) {
	searchTerms := buildSearchTerms(query)
	if len(searchTerms) == 0 {
		return nil, nil
	}

	// 1. 司法大数据案例检索优先
	if refs, ok := tryJudicialSearch(searchTerms[0], query); ok {
		return refs, nil
	}

	// 2. 回退到通用 web_search
	payload, _ := json.Marshal(map[string]interface{}{
		"query": searchTerms[0] + " 案例 裁判",
	})
	result := tools.ValidateAndExecute("web_search", payload)
	if !result.Success {
		return nil, nil
	}

	return parseWebSearchResult(result.Output, query), nil
}

/* searchCommentary 检索学术观点和实务指南。

   数据源：北大法宝法学期刊、威科先行实务指南。
*/
func (p *LegalSearchPlugin) searchCommentary(query LegalSearchQuery) ([]LegalReference, error) {
	searchTerms := buildSearchTerms(query)
	if len(searchTerms) == 0 {
		return nil, nil
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"query": searchTerms[0] + " 法律分析 实务",
	})
	result := tools.ValidateAndExecute("web_search", payload)
	if !result.Success {
		return nil, nil
	}

	return parseWebSearchResult(result.Output, query), nil
}

/* buildSearchTerms 从查询构建检索关键词。 */
func buildSearchTerms(query LegalSearchQuery) []string {
	var terms []string

	// 如果有指定的法律引用，优先使用
	if len(query.Laws) > 0 {
		for _, law := range query.Laws {
			terms = append(terms, law)
		}
		return terms
	}

	// 使用关键词
	if len(query.Keywords) > 0 {
		terms = query.Keywords
	} else if query.ContractType != "" {
		terms = append(terms, query.ContractType, "法律")
	} else {
		terms = append(terms, "合同法")
	}

	return terms
}

/* tryJudicialSearch 通过 judicial_search 工具查询司法大数据。

   调用流程：
   1. 构造 payload → ValidateAndExecute("judicial_search", ...)
   2. 解析返回的 JSON 数组为 JudicialSearchResult 结构
   3. 转换为 LegalReference（标准引用格式）

   数据源优先级：司法大数据服务网 → 裁判文书网 → 静默失败
   失败时由调用方决定是否回退到 web_search。
*/
func tryJudicialSearch(term string, query LegalSearchQuery) ([]LegalReference, bool) {
	payload, _ := json.Marshal(map[string]interface{}{
		"query":     term,
		"case_type": query.ContractType,
	})
	result := tools.ValidateAndExecute("judicial_search", payload)
	if !result.Success || result.Output == "[]" {
		return nil, false
	}

	// 解析 judicial_search 返回的 JSON 数组
	var rawResults []map[string]interface{}
	if err := json.Unmarshal([]byte(result.Output), &rawResults); err != nil {
		return nil, false
	}

	var refs []LegalReference
	for _, r := range rawResults {
		title, _ := r["title"].(string)
		source, _ := r["source"].(string)
		url, _ := r["url"].(string)
		summary, _ := r["summary"].(string)

		level := "裁判文书"
		if source == "data_court" {
			level = "司法统计"
		}

		refs = append(refs, LegalReference{
			Title:   title,
			Level:   level,
			Source:  source,
			URL:     url,
			Summary: summary,
		})
	}

	return refs, len(refs) > 0
}

/* tryPkulawSearch 尝试北大法宝专用检索。

   如果将来接入北大法宝 MCP 连接器，在此实现。
   当前返回 false 回退到通用 web_search。
*/
func tryPkulawSearch(term string) ([]LegalReference, bool) {
	// TBD: 接入北大法宝 API / MCP 连接器
	// 参考 claude-for-legal CONNECTORS.md 的 MCP 连接器模式
	return nil, false
}

/* buildDefaultReferences 当检索失败时返回最小法律引用集。

   确保下游 clause_analyzer_plugin 仍然可以运行，
   但标注了来源不可靠的警告。
*/
func buildDefaultReferences(query LegalSearchQuery) []LegalReference {
	refs := []LegalReference{
		{
			Title:    "《中华人民共和国民法典》",
			Citation: "中华人民共和国主席令第45号",
			Level:    "法律",
			Source:   "model_knowledge",
			Summary:  "未经验证——检索失败，使用模型知识填充。使用前请核对原文。",
		},
	}

	// 根据合同类型补充默认引用
	if query.ContractType != "" {
		refs = append(refs, LegalReference{
			Title:   "关于" + query.ContractType + "的司法解释",
			Level:   "司法解释",
			Source:  "model_knowledge",
			Summary: "未经验证——检索失败。请通过北大法宝或威科先行确认最新版本。",
		})
	}

	return refs
}

/* sortByLegalHierarchy 按中国法律效力层级排序。 */
func sortByLegalHierarchy(refs []LegalReference) {
	// 简单插入排序，按层级优先级
	for i := 1; i < len(refs); i++ {
		for j := i; j > 0 && hierarchyRank(refs[j-1].Level) > hierarchyRank(refs[j].Level); j-- {
			refs[j], refs[j-1] = refs[j-1], refs[j]
		}
	}
}

/* hierarchyRank 返回效力层级的排序权重。 */
func hierarchyRank(level string) int {
	for i, h := range chineseLegalHierarchy {
		if h == level {
			return i
		}
	}
	return len(chineseLegalHierarchy) // 未知层级排最后
}

/* parseWebSearchResult 解析 web_search 结果为法律引用。

   当前为简化实现，直接返回空结果等待完整的 web_search 解析逻辑。
   后续可接入 LLM 做结构化提取。
*/
func parseWebSearchResult(output string, query LegalSearchQuery) []LegalReference {
	// TBD: 接入 LLM 解析搜索结果
	return nil
}
