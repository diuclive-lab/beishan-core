package plugins

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"beishan/internal/llm"
	"beishan/internal/tools"
	"beishan/kernel"
)

/* ─── Typed Mode Constant ──────────────────────── */

// ThinkMode 插件执行模式（L4 语义，不污染 L1）
type ThinkMode string

const (
	ModeChat          ThinkMode = "chat"
	ModeReviewExtract ThinkMode = "review_extract"
)

// extractMode 从 Payload 提取 mode（L4 语义，whitelist）
func extractMode(payload []byte) ThinkMode {
	var obj struct {
		Mode string `json:"mode"`
	}

	if json.Unmarshal(payload, &obj) != nil {
		return ModeChat
	}

	switch ThinkMode(obj.Mode) {
	case ModeChat, ModeReviewExtract:
		return ThinkMode(obj.Mode)
	default:
		return ModeChat
	}
}

/* ─── Schema Contract ──────────────────────────── */

// KnowledgeCandidate 知识候选条目
type KnowledgeCandidate struct {
	Title   string  `json:"title"`
	Summary string  `json:"summary"`
	Score   float64 `json:"score"`
	Reason  string  `json:"reason"`
}

// ReviewResult 审查结果（LLM output contract）
type ReviewResult struct {
	Candidates []KnowledgeCandidate `json:"candidates"`
}

// validateReview 验证审查结果（typed validation gate）
func validateReview(r ReviewResult) error {
	if len(r.Candidates) > 10 {
		return fmt.Errorf("too many candidates: %d (max 10)", len(r.Candidates))
	}
	for i, c := range r.Candidates {
		if c.Title == "" {
			return fmt.Errorf("candidate %d: missing title", i+1)
		}
		if len(c.Title) > 100 {
			return fmt.Errorf("candidate %d: title too long (>100 chars)", i+1)
		}
		if c.Summary == "" {
			return fmt.Errorf("candidate %d: missing summary", i+1)
		}
		if len(c.Summary) > 500 {
			return fmt.Errorf("candidate %d: summary too long (>500 chars)", i+1)
		}
		if c.Score < 0 || c.Score > 1 {
			return fmt.Errorf("candidate %d: invalid score %.2f (must be 0-1)", i+1, c.Score)
		}
		if c.Reason == "" {
			return fmt.Errorf("candidate %d: missing reason", i+1)
		}
	}
	return nil
}

// strengthenPrompt 强化 prompt（基于 basePrompt，避免 snowball）
func strengthenPrompt(basePrompt string, parseErr, validateErr error) string {
	if parseErr != nil {
		return basePrompt + "\n\n请严格输出JSON格式，不要包含任何其他文本。"
	}
	if validateErr != nil {
		return basePrompt + fmt.Sprintf("\n\n请确保JSON格式正确：每个candidate必须有title、summary、score(0-1)、reason字段。错误：%v", validateErr)
	}
	return basePrompt
}

// callStructuredLLM 调用 LLM 并强制输出结构化 JSON（retry loop 收敛）
func callStructuredLLM(prompt string) (ReviewResult, error) {
	const maxRetry = 1
	basePrompt := prompt
	var lastErr error

	for attempt := 0; attempt <= maxRetry; attempt++ {
		raw, err := llm.ChatCompletion(
			"你是知识提取助手。严格按用户要求输出JSON，不要输出任何其他文本。",
			prompt,
			120*time.Second,
		)
		if err != nil {
			lastErr = fmt.Errorf("LLM call failed: %w", err)
			continue
		}

		var result ReviewResult
		parseErr := json.Unmarshal([]byte(raw), &result)

		// 先检查解析错误，再校验 schema
		if parseErr != nil {
			lastErr = fmt.Errorf("contract violation: invalid JSON: %w", parseErr)
			prompt = strengthenPrompt(basePrompt, parseErr, nil)
			continue
		}

		validateErr := validateReview(result)
		if validateErr == nil {
			return result, nil
		}

		// 强化 prompt（基于 basePrompt，避免 snowball）
		prompt = strengthenPrompt(basePrompt, nil, validateErr)
		lastErr = fmt.Errorf("contract violation: schema mismatch: %w", validateErr)
	}

	return ReviewResult{}, fmt.Errorf("contract violation after %d retries: %w", maxRetry, lastErr)
}

// renderReviewReport 渲染审查报告（deterministic）
func renderReviewReport(result ReviewResult) string {
	var sb strings.Builder

	sb.WriteString("[Knowledge Review Report]\n")
	sb.WriteString(fmt.Sprintf("候选知识: %d\n\n", len(result.Candidates)))

	for i, c := range result.Candidates {
		sb.WriteString(fmt.Sprintf("Candidate %d\n", i+1))
		sb.WriteString(fmt.Sprintf("  score: %.2f\n", c.Score))
		sb.WriteString(fmt.Sprintf("  title: %q\n", c.Title))
		sb.WriteString(fmt.Sprintf("  summary: %q\n", c.Summary))
		sb.WriteString(fmt.Sprintf("  reason: %s\n", c.Reason))
		sb.WriteString("\n")
	}

	if len(result.Candidates) > 0 {
		sb.WriteString("是否确认入库？回复「确认 1,2」或「跳过」\n")
	} else {
		sb.WriteString("未发现值得记录的知识。\n")
	}

	return sb.String()
}

/* ─── Suggest-to-Remember ──────────────────────── */

// PendingRemember 待确认的记忆建议
type PendingRemember struct {
	ID        string
	Title     string
	Summary   string
	Tags      []string
	ExpiresAt int64
}

// PendingReview 待确认的批量审查候选（Stage 3: Knowledge Review）
type PendingReview struct {
	Candidates []KnowledgeCandidate
	ExpiresAt  int64
	CreatedAt  int64
}

// ReviewFile 审查报告文件结构（持久化存储）
type ReviewFile struct {
	ID         string             `json:"id"`
	CreatedAt  int64              `json:"created_at"`
	Candidates []KnowledgeCandidate `json:"candidates"`
}

var (
	// session-scoped pending remember
	pendingRemembers   = make(map[string]*PendingRemember)
	pendingRemembersMu sync.Mutex

	// session-scoped pending review（内存缓存，加速访问）
	pendingReviews   = make(map[string]*PendingReview)
	pendingReviewsMu sync.Mutex
)

// getReviewDir 获取审查报告存储目录
func getReviewDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hermes", "reviews")
}

// saveReviewToFile 将审查报告写入文件
func saveReviewToFile(reviewID string, candidates []KnowledgeCandidate) error {
	dir := getReviewDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建审查目录失败: %w", err)
	}

	rf := ReviewFile{
		ID:         reviewID,
		CreatedAt:  time.Now().Unix(),
		Candidates: candidates,
	}

	data, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化审查报告失败: %w", err)
	}

	path := filepath.Join(dir, reviewID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("写入审查报告失败: %w", err)
	}

	fmt.Printf("[review] 报告已保存: %s\n", path)
	return nil
}

// loadReviewFromFile 从文件读取审查报告
func loadReviewFromFile(reviewID string) (*ReviewFile, error) {
	path := filepath.Join(getReviewDir(), reviewID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取审查报告失败: %w", err)
	}

	var rf ReviewFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return nil, fmt.Errorf("解析审查报告失败: %w", err)
	}

	return &rf, nil
}

// deleteReviewFile 删除审查报告文件
func deleteReviewFile(reviewID string) error {
	path := filepath.Join(getReviewDir(), reviewID+".json")
	return os.Remove(path)
}

// listReviewFiles 列出所有待确认的审查报告
func listReviewFiles() ([]ReviewFile, error) {
	dir := getReviewDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var reviews []ReviewFile
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		reviewID := strings.TrimSuffix(e.Name(), ".json")
		rf, err := loadReviewFromFile(reviewID)
		if err != nil {
			continue
		}
		reviews = append(reviews, *rf)
	}

	return reviews, nil
}

// rememberTriggers 保守的触发关键词
var rememberTriggers = []string{
	"已放弃", "最终方案", "确认结论", "架构决定",
	"经验教训", "踩坑记录", "最终决定",
}

// shouldSuggestRemember 检测是否值得记住
func shouldSuggestRemember(userText, reply string) bool {
	combined := userText + " " + reply
	for _, trigger := range rememberTriggers {
		if strings.Contains(combined, trigger) {
			return true
		}
	}
	return false
}

// extractRememberCandidate 从对话中提取记忆候选
func extractRememberCandidate(userText, reply string) (title, summary string) {
	// 简单提取：用触发词所在句子作为摘要
	combined := userText + " " + reply
	for _, trigger := range rememberTriggers {
		if idx := strings.Index(combined, trigger); idx >= 0 {
			// 提取触发词前后各 50 字符
			start := idx - 50
			if start < 0 {
				start = 0
			}
			end := idx + len(trigger) + 50
			if end > len(combined) {
				end = len(combined)
			}
			summary = combined[start:end]
			title = trigger
			return
		}
	}
	return
}

// createPendingRemember 创建待确认记忆（session-scoped，single-active）
func createPendingRemember(sessionID, title, summary string) *PendingRemember {
	id := "pr_" + newID()
	pr := &PendingRemember{
		ID:        id,
		Title:     title,
		Summary:   summary,
		ExpiresAt: time.Now().Add(10 * time.Minute).Unix(),
	}
	pendingRemembersMu.Lock()
	pendingRemembers[sessionID] = pr // replace previous
	pendingRemembersMu.Unlock()
	return pr
}

// confirmPendingRemember 确认待确认记忆
func confirmPendingRemember(sessionID string) *PendingRemember {
	pendingRemembersMu.Lock()
	defer pendingRemembersMu.Unlock()
	pr, ok := pendingRemembers[sessionID]
	if !ok {
		return nil
	}
	if time.Now().Unix() > pr.ExpiresAt {
		delete(pendingRemembers, sessionID)
		return nil
	}
	delete(pendingRemembers, sessionID)
	return pr
}

// isConfirmReply 检测是否是确认回复
func isConfirmReply(sessionID, text string) bool {
	t := strings.TrimSpace(text)
	if t == "确认" || t == "是" || t == "yes" {
		pendingRemembersMu.Lock()
		defer pendingRemembersMu.Unlock()
		pr, ok := pendingRemembers[sessionID]
		return ok && time.Now().Unix() <= pr.ExpiresAt
	}
	return false
}

// cleanupExpiredPending 清理过期的 pending remember
func cleanupExpiredPending() {
	pendingRemembersMu.Lock()
	defer pendingRemembersMu.Unlock()
	now := time.Now().Unix()
	for id, pr := range pendingRemembers {
		if now > pr.ExpiresAt {
			delete(pendingRemembers, id)
		}
	}
}

// isBatchConfirm 检测是否是批量确认回复（如 "确认 1,2" 或 "确认 reviewID 1,2"）
func isBatchConfirm(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasPrefix(t, "确认 ") && len(t) > len("确认 ")
}

// parseBatchConfirmWithID 解析批量确认回复，返回 reviewID 和选中的候选索引
// 支持格式：确认 1,2（使用默认 reviewID）或 确认 reviewID 1,2
func parseBatchConfirmWithID(text string) (string, []int, error) {
	t := strings.TrimSpace(text)
	t = strings.TrimPrefix(t, "确认 ")

	// 尝试解析 "reviewID 1,2" 格式
	parts := strings.SplitN(t, " ", 2)
	if len(parts) == 2 && strings.HasPrefix(parts[0], "review_") {
		reviewID := parts[0]
		indices, err := parseIndices(parts[1])
		if err != nil {
			return "", nil, err
		}
		return reviewID, indices, nil
	}

	// 传统格式 "1,2"（无 reviewID）
	indices, err := parseIndices(t)
	if err != nil {
		return "", nil, err
	}
	return "", indices, nil
}

// parseIndices 解析逗号分隔的索引列表
func parseIndices(text string) ([]int, error) {
	parts := strings.Split(text, ",")
	var indices []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid index: %q", p)
		}
		indices = append(indices, n)
	}
	if len(indices) == 0 {
		return nil, errors.New("no valid indices")
	}
	return indices, nil
}

// storePendingReview 存储待确认的审查候选
func storePendingReview(sessionID string, candidates []KnowledgeCandidate) {
	pendingReviewsMu.Lock()
	defer pendingReviewsMu.Unlock()

	// overwrite policy: 覆盖之前的 review
	pendingReviews[sessionID] = &PendingReview{
		Candidates: candidates,
		ExpiresAt:  time.Now().Add(30 * time.Minute).Unix(),
	}
}

// getPendingReview 获取待确认的审查候选（懒清理）
func getPendingReview(sessionID string) *PendingReview {
	pendingReviewsMu.Lock()
	defer pendingReviewsMu.Unlock()

	pr, ok := pendingReviews[sessionID]
	if !ok {
		return nil
	}
	if time.Now().Unix() > pr.ExpiresAt {
		delete(pendingReviews, sessionID)
		return nil
	}
	return pr
}

// clearPendingReview 清除待确认的审查候选
func clearPendingReview(sessionID string) {
	pendingReviewsMu.Lock()
	defer pendingReviewsMu.Unlock()
	delete(pendingReviews, sessionID)
}

// ─── 自然语言入口（Stage 3）────────────────────────

// reviewTriggers 审查触发关键词
var reviewTriggers = []string{
	"审查一下", "知识审查", "审查对话", "审查最近",
	"review", "knowledge review",
}

// listReviewTriggers 查看报告触发关键词
var listReviewTriggers = []string{
	"待审查报告", "审查队列", "review queue",
}

// confirmAllTriggers 全部确认触发关键词
var confirmAllTriggers = []string{
	"确认全部", "全部入库", "confirm all",
}

// skipTriggers 跳过/清理触发关键词
var skipTriggers = []string{
	"跳过", "清理审查", "skip",
}

// isReviewTrigger 检测是否触发知识审查
func isReviewTrigger(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, trigger := range reviewTriggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

// isListReviews 检测是否查看审查报告
func isListReviews(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, trigger := range listReviewTriggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

// isConfirmAll 检测是否全部确认
func isConfirmAll(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, trigger := range confirmAllTriggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

// isSkipAll 检测是否跳过/清理
func isSkipAll(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	for _, trigger := range skipTriggers {
		if strings.Contains(lower, trigger) {
			return true
		}
	}
	return false
}

// triggerReviewWorkflow 触发知识审查工作流
func (p *ThinkPlugin) triggerReviewWorkflow() (kernel.Message, error) {
	// 通过 kernel 触发 workflow_plugin
	resp, err := p.Kernel.Call(kernel.Message{
		Recipient: "workflow_plugin",
		Type:      "workflow_run",
		Payload:   []byte(`{"workflow":"knowledge_review_scheduler"}`),
	}, 180*time.Second)
	if err != nil {
		reply := fmt.Sprintf("知识审查失败: %v", err)
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	// 返回审查结果
	return resp, nil
}

// handleListReviews 展示所有待确认的审查报告
func (p *ThinkPlugin) handleListReviews() (kernel.Message, error) {
	reviews, err := listReviewFiles()
	if err != nil {
		reply := fmt.Sprintf("读取审查报告失败: %v", err)
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	if len(reviews) == 0 {
		reply := "没有待确认的审查报告。回复「审查一下」可触发新的知识审查。"
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("待确认的审查报告 (%d 个):\n\n", len(reviews)))
	for i, r := range reviews {
		ts := time.Unix(r.CreatedAt, 0).Format("01-02 15:04")
		sb.WriteString(fmt.Sprintf("%d. [%s] %s (%d 个候选)\n", i+1, ts, r.ID, len(r.Candidates)))
		for j, c := range r.Candidates {
			sb.WriteString(fmt.Sprintf("   %d. %s (score: %.2f)\n", j+1, c.Title, c.Score))
		}
		sb.WriteString(fmt.Sprintf("   回复「确认 %s 1,2」即可入库\n\n", r.ID))
	}

	reply := sb.String()
	replyJSON, _ := json.Marshal(reply)
	return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
}

// handleConfirmAll 全部确认入库（最新报告）
func (p *ThinkPlugin) handleConfirmAll() (kernel.Message, error) {
	reviews, err := listReviewFiles()
	if err != nil {
		reply := fmt.Sprintf("读取审查报告失败: %v", err)
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	if len(reviews) == 0 {
		reply := "没有待确认的审查报告。回复「审查一下」可触发新的知识审查。"
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	// 取最新的报告
	latest := reviews[len(reviews)-1]
	var recorded []string
	for i, c := range latest.Candidates {
		result := tools.KnowledgeRemember(c.Title, c.Summary, nil, 0)
		recorded = append(recorded, fmt.Sprintf("%d. %s: %s", i+1, c.Title, result.Output))
	}

	// 清理文件
	deleteReviewFile(latest.ID)

	reply := fmt.Sprintf("已入库 %d 条知识（报告 %s）：\n%s", len(recorded), latest.ID, strings.Join(recorded, "\n"))
	replyJSON, _ := json.Marshal(reply)
	return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
}

// handleSkipAll 跳过/清理所有审查报告
func (p *ThinkPlugin) handleSkipAll() (kernel.Message, error) {
	reviews, err := listReviewFiles()
	if err != nil {
		reply := fmt.Sprintf("读取审查报告失败: %v", err)
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	if len(reviews) == 0 {
		reply := "没有待清理的审查报告。"
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	// 删除所有报告文件
	count := 0
	for _, r := range reviews {
		if deleteReviewFile(r.ID) == nil {
			count++
		}
	}

	reply := fmt.Sprintf("已清理 %d 个审查报告。", count)
	replyJSON, _ := json.Marshal(reply)
	return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
}

// newID 生成随机 ID
func newID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}

/* ThinkPlugin （思考插件）

   处理一般对话、问答、闲聊、创意写作。
   收到 chat 消息后调 DeepSeek 生成回答。

   当用户直接说"你好"、"帮我写一首诗"等通用请求时，
   DeepSeek 路由到本插件，由本插件再调 DeepSeek 生成回答。
*/
type ThinkPlugin struct {
	Kernel *kernel.Kernel
}

func (p *ThinkPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	if msg.Type != "chat" {
		return kernel.Message{}, fmt.Errorf("think_plugin: 未知类型 %s", msg.Type)
	}

	userText := extractPrompt(msg.Payload)
	sessionID := extractSessionID(msg.Payload)
	mode := extractMode(msg.Payload)

	// 清理过期的 pending remember（懒清理 review）
	cleanupExpiredPending()

	// 批量确认检测（L4 语义）
	if isBatchConfirm(userText) {
		reviewID, indices, err := parseBatchConfirmWithID(userText)
		if err != nil {
			reply := fmt.Sprintf("格式错误，请使用：确认 1,2 或 确认 reviewID 1,2")
			replyJSON, _ := json.Marshal(reply)
			return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
		}

		// 获取审查报告（优先从内存缓存，其次从文件）
		var candidates []KnowledgeCandidate
		if reviewID != "" {
			// 从文件加载
			rf, loadErr := loadReviewFromFile(reviewID)
			if loadErr != nil {
				reply := fmt.Sprintf("找不到审查报告 %s", reviewID)
				replyJSON, _ := json.Marshal(reply)
				return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
			}
			candidates = rf.Candidates
		} else {
			// 从内存缓存加载（兼容旧格式）
			pr := getPendingReview(sessionID)
			if pr == nil {
				reply := fmt.Sprintf("没有待确认的审查报告，请先运行知识审查工作流")
				replyJSON, _ := json.Marshal(reply)
				return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
			}
			candidates = pr.Candidates
			reviewID = sessionID
		}

		// 执行入库
		var recorded []string
		for _, idx := range indices {
			if idx > 0 && idx <= len(candidates) {
				candidate := candidates[idx-1]
				result := tools.KnowledgeRemember(candidate.Title, candidate.Summary, nil, 0)
				recorded = append(recorded, fmt.Sprintf("%d. %s: %s", idx, candidate.Title, result.Output))
			}
		}

		// 清理（内存缓存 + 文件）
		clearPendingReview(reviewID)
		deleteReviewFile(reviewID)

		reply := fmt.Sprintf("已记录 %d 条知识：\n%s", len(recorded), strings.Join(recorded, "\n"))
		replyJSON, _ := json.Marshal(reply)
		return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
	}

	// 单条确认检测（Stage 2）
	if isConfirmReply(sessionID, userText) {
		pr := confirmPendingRemember(sessionID)
		if pr != nil {
			result := tools.KnowledgeRemember(pr.Title, pr.Summary, pr.Tags, 0)
			reply := fmt.Sprintf("已记录：%s\n%s", pr.Title, result.Output)
			replyJSON, _ := json.Marshal(reply)
			return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
		}
	}

	// 自然语言入口：知识审查触发
	if isReviewTrigger(userText) {
		return p.triggerReviewWorkflow()
	}

	// 自然语言入口：查看审查报告
	if isListReviews(userText) {
		return p.handleListReviews()
	}

	// 自然语言入口：全部确认
	if isConfirmAll(userText) {
		return p.handleConfirmAll()
	}

	// 自然语言入口：跳过/清理
	if isSkipAll(userText) {
		return p.handleSkipAll()
	}

	// Mode dispatch（L4 语义，不污染 L1）
	switch mode {
	case ModeReviewExtract:
		return p.handleReviewExtract(userText, sessionID)
	default:
		return p.handleChat(userText, sessionID)
	}
}

// handleChat 处理普通聊天
func (p *ThinkPlugin) handleChat(userText, sessionID string) (kernel.Message, error) {
	background := ""
	trace := tools.NewRetrievalTrace(userText)

	// Round 1: 确定性检索
	results := tools.SearchMemoryFull(userText, 3, trace)

	// Multi-hop decision（纯代码，零 LLM）
	if shouldHop, reason := tools.NeedsSecondHop(results); shouldHop {
		secondQuery := tools.DeriveSecondQuery(results, reason)
		if secondQuery != "" {
			moreResults := tools.SearchMemoryFull(secondQuery, 2, trace)
			results = tools.MergeResults(results, moreResults)
			trace.Add(tools.RetrievalStage{
				Stage:  "multi_hop",
				Method: "code_decision",
				Input:  reason,
				Output: map[string]any{"query": secondQuery, "merged": len(results)},
				Reason: fmt.Sprintf("trigger=%s", reason),
			})
			fmt.Printf("[思考] 多跳检索: reason=%s query=%q → 合并后 %d 条\n", reason, secondQuery, len(results))
		}
	}

	if len(results) > 0 {
		background = tools.FormatForPromptFull(results)
		fmt.Printf("[思考] 检索到 %d 条相关记忆\n", len(results))
	}
	trace.Log()

	reply, err := callDeepSeek(userText, background)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("think_plugin: %w", err)
	}

	// Suggest-to-Remember
	if shouldSuggestRemember(userText, reply) {
		title, summary := extractRememberCandidate(userText, reply)
		if title != "" {
			pr := createPendingRemember(sessionID, title, summary)
			reply += fmt.Sprintf("\n\n---\n是否将此结论加入知识库？回复「确认」即可。")
			fmt.Printf("[思考] 检测到值得记住的结论: %s (session=%s, pending=%s)\n", title, sessionID, pr.ID)
		}
	}

	replyJSON, _ := json.Marshal(reply)
	fmt.Printf("[思考] %s\n", truncate(reply, 120))
	return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
}

// handleReviewExtract 处理知识审查提取（Stage 3 Hardened）
func (p *ThinkPlugin) handleReviewExtract(userText, sessionID string) (kernel.Message, error) {
	// 调用 LLM 并强制输出结构化 JSON（schema contract + validation gate）
	result, err := callStructuredLLM(userText)
	if err != nil {
		return kernel.Message{}, fmt.Errorf("review extract failed: %w", err)
	}

	// 生成审查报告 ID
	reviewID := fmt.Sprintf("review_%d", time.Now().UnixMilli())

	// 写入文件持久化（方案 A：Workflow 内闭环）
	if len(result.Candidates) > 0 {
		if err := saveReviewToFile(reviewID, result.Candidates); err != nil {
			fmt.Printf("[review] 保存报告失败: %v\n", err)
		} else {
			// 同时存入内存缓存（加速后续确认）
			storePendingReview(reviewID, result.Candidates)
			fmt.Printf("[review] 存储 %d 个审查候选 (reviewID=%s)\n", len(result.Candidates), reviewID)
		}

		// 异步发送通知（方向 B：通知闭环）
		go p.sendReviewNotification(reviewID, len(result.Candidates))
	}

	// 渲染报告（deterministic renderer）
	report := renderReviewReport(result)
	if len(result.Candidates) > 0 {
		report += fmt.Sprintf("\n\n报告 ID: %s\n回复「确认 %s 1,2」即可入库", reviewID, reviewID)
	}

	replyJSON, _ := json.Marshal(report)
	return kernel.Message{Type: "chat.response", Payload: replyJSON}, nil
}

// sendReviewNotification 发送审查完成通知（方向 B）
func (p *ThinkPlugin) sendReviewNotification(reviewID string, candidateCount int) {
	// 从环境变量获取通知配置
	notifyChannel := os.Getenv("REVIEW_NOTIFY_CHANNEL")
	notifyTarget := os.Getenv("REVIEW_NOTIFY_TARGET")

	// 未配置通知渠道则跳过
	if notifyChannel == "" || notifyTarget == "" {
		return
	}

	message := fmt.Sprintf("[知识审查报告] 发现 %d 条值得记住的知识\n报告 ID: %s\n回复「审查队列」查看详情",
		candidateCount, reviewID)

	notifyPayload, _ := json.Marshal(map[string]string{
		"channel": notifyChannel,
		"target":  notifyTarget,
		"message": message,
		"subject": "知识审查完成",
	})

	// 通过 kernel 发送通知
	p.Kernel.Send(kernel.Message{
		Recipient: "notify_plugin",
		Type:      "notify_send",
		Payload:   notifyPayload,
	})

	fmt.Printf("[review] 已发送通知: %s (%d 条候选)\n", reviewID, candidateCount)
}

var systemPrompt = `你是 beishan-core 智能助手。你所在的系统提供以下能力：

- 搜索网络（search_plugin）
- 读写文件（write_plugin）
- 执行终端命令（terminal_plugin）
- 浏览器导航与网页提取（browser_plugin）
- 记忆存储与检索（memory_plugin）
- 待办管理（todo_plugin）
- 文本转语音（tts_plugin）
- 图片生成（image_gen_plugin）
- 法律审查工作流（workflow_plugin）

当用户需要上述能力时，请明确告知用户可以发送指定请求。
例如："你可以说'帮我搜索XX'来完成搜索"。

对于你能直接回答的问题（闲聊、知识、创作等），直接回答。
不需要说明自己是AI助手。`

// callDeepSeek 调 LLM API 生成回答。
// 使用共享 llm.ChatCompletion（不经过 think_plugin 自身路径，避免递归）。
// background 为检索到的知识上下文，为空时不注入。
func callDeepSeek(prompt string, background string) (string, error) {
	userMsg := prompt
	if background != "" {
		userMsg = "请参考以下背景知识回答：\n" + background + "\n\n用户问题：\n" + prompt
	}
	return llm.ChatCompletion(systemPrompt, userMsg, 60*time.Second)
}

// extractPrompt 从 Payload 中提取用户提示文本。
// 支持两种格式：
//   - JSON 对象：提取 message 字段（workflow 场景）
//   - JSON 字符串：直接返回（普通场景）
func extractPrompt(payload []byte) string {
	s := string(payload)
	// 尝试解析为 JSON 对象，提取 message 字段
	var obj map[string]interface{}
	if json.Unmarshal(payload, &obj) == nil {
		if msg, ok := obj["message"].(string); ok {
			return msg
		}
	}
	// 回退：去掉外层引号
	return strings.TrimFunc(s, func(r rune) bool { return r == '"' })
}

// extractSessionID 从 Payload 中提取 session_id。
// session_id 是 runtime concept，存储在 Payload 中（L4 层负责），
// 不依赖 L1 kernel.Message。
func extractSessionID(payload []byte) string {
	var obj map[string]interface{}
	if json.Unmarshal(payload, &obj) == nil {
		if sid, ok := obj["session_id"].(string); ok {
			return sid
		}
	}
	return ""
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
