package plugins

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"beishan/internal/tools"
	"beishan/kernel"
)

/* ─── Typed Mode Constant ──────────────────────── */

// ThinkMode 插件执行模式（L4 语义，不污染 L1）
type ThinkMode string

const (
	ModeChat          ThinkMode = "chat"
	ModeReviewExtract ThinkMode = "review_extract"
	ModeNoRetrieval   ThinkMode = "no_retrieval" // workflow 步骤：跳过检索，直接调 LLM
	ModeTrace         ThinkMode = "trace"        // 检索后返回结构化 trace
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
	case ModeChat, ModeReviewExtract, ModeNoRetrieval, ModeTrace:
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

// callStructuredLLM 调用 LLM 并强制输出结构化 JSON（retry loop 收敛）。
// 通过 handleChatNoRetrieval 走 think_plugin 的 LLM 路径，不直接调 llm 包。
func (p *ThinkPlugin) callStructuredLLM(prompt string) (ReviewResult, error) {
	const maxRetry = 1
	basePrompt := prompt
	var lastErr error

	for attempt := 0; attempt <= maxRetry; attempt++ {
		payload, _ := json.Marshal(map[string]string{
			"message": prompt,
			"system":  "你是知识提取助手。严格按用户要求输出JSON，不要输出任何其他文本。",
		})
		msg, err := p.handleChatNoRetrieval(payload, "")
		if err != nil {
			lastErr = fmt.Errorf("LLM call failed: %w", err)
			continue
		}

		var raw string
		if err := json.Unmarshal(msg.Payload, &raw); err != nil {
			raw = string(msg.Payload)
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

// isForceSaveReply 检测是否是强制记录回复（跳过事实核查）
func isForceSaveReply(sessionID, text string) bool {
	t := strings.TrimSpace(text)
	if t == "强制记录" || t == "是的，强制记录" || t == "强制入库" {
		pendingRemembersMu.Lock()
		defer pendingRemembersMu.Unlock()
		pr, ok := pendingRemembers[sessionID]
		return ok && time.Now().Unix() <= pr.ExpiresAt
	}
	return false
}

// isMergeReply 检测是否是合并回复（与已有条目合并）
func isMergeReply(sessionID, text string) bool {
	t := strings.TrimSpace(text)
	if t == "确认合并" || t == "合并" {
		pendingRemembersMu.Lock()
		defer pendingRemembersMu.Unlock()
		pr, ok := pendingRemembers[sessionID]
		return ok && time.Now().Unix() <= pr.ExpiresAt
	}
	return false
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
	"knowledge review",
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

// handleReviewExtract 处理知识审查提取（Stage 3 Hardened）
func (p *ThinkPlugin) handleReviewExtract(userText, sessionID string) (kernel.Message, error) {
	// 调用 LLM 并强制输出结构化 JSON（schema contract + validation gate）
	result, err := p.callStructuredLLM(userText)
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
