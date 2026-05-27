package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"beishan/internal/llm"
)

/* ─── UserProfile 用户画像 ──────────────────────

   记录用户的偏好、技术水平和表达习惯。
   注入 think_plugin 的 system prompt，让 LLM 调整回答风格。
*/

type UserProfile struct {
	Name          string   `json:"name,omitempty"`           // 用户称呼
	PreferredLang string   `json:"preferred_lang,omitempty"` // zh / en
	Expertise     string   `json:"expertise,omitempty"`      // beginner / intermediate / expert
	Interests     []string `json:"interests,omitempty"`      // 关注领域列表
	ResponseStyle string   `json:"response_style,omitempty"` // concise / detailed / code_first
	UpdatedAt     int64    `json:"updated_at,omitempty"`
}

var (
	profileMu  sync.RWMutex
	profileDir string
)

func profilePath() string {
	if profileDir == "" {
		profileDir = HermesHome
	}
	return filepath.Join(profileDir, "user_profile.json")
}

func LoadProfile() *UserProfile {
	profileMu.RLock()
	defer profileMu.RUnlock()

	data, err := os.ReadFile(profilePath())
	if err != nil {
		return defaultProfile()
	}

	var p UserProfile
	if err := json.Unmarshal(data, &p); err != nil {
		return defaultProfile()
	}

	if p.Expertise == "" {
		p.Expertise = "intermediate"
	}
	if p.PreferredLang == "" {
		p.PreferredLang = "zh"
	}
	return &p
}

func SaveProfile(p *UserProfile) error {
	profileMu.Lock()
	defer profileMu.Unlock()

	os.MkdirAll(HermesHome, 0755)
	data, _ := json.MarshalIndent(p, "", "  ")
	return os.WriteFile(profilePath(), data, 0644)
}

func defaultProfile() *UserProfile {
	return &UserProfile{
		PreferredLang: "zh",
		Expertise:     "intermediate",
		Interests:     []string{},
		ResponseStyle: "concise",
	}
}

// ProfileToPrompt 把用户画像格式化为 system prompt 后缀。
// 返回空字符串表示无画像（使用默认 system prompt）。
func ProfileToPrompt() string {
	p := LoadProfile()

	var parts []string
	if p.Name != "" {
		parts = append(parts, fmt.Sprintf("用户称呼: %s", p.Name))
	}

	if p.Expertise != "" {
		label := "中级"
		switch p.Expertise {
		case "beginner":
			label = "初级"
		case "intermediate":
			label = "中级"
		case "expert":
			label = "高级"
		}
		parts = append(parts, fmt.Sprintf("技术水平: %s", label))
	}

	if p.PreferredLang == "en" {
		parts = append(parts, "偏好语言: 英语")
	}

	if len(p.Interests) > 0 {
		parts = append(parts, fmt.Sprintf("关注领域: %s", strings.Join(p.Interests, ", ")))
	}

	if p.ResponseStyle == "code_first" {
		parts = append(parts, "回答偏好: 优先给代码实现，再解释原理")
	} else if p.ResponseStyle == "detailed" {
		parts = append(parts, "回答偏好: 详细解释，附带示例和背景")
	} else {
		parts = append(parts, "回答偏好: 简洁直接，重点突出")
	}

	if len(parts) == 0 {
		return ""
	}

	now := time.Now()
	return fmt.Sprintf("\n\n【用户画像】%s\n当前时间: %s",
		strings.Join(parts, " | "),
		now.Format("2006-01-02 15:04"))
}

func ProfileShowHandler(args map[string]interface{}) *ToolResult {
	p := LoadProfile()
	b, _ := json.MarshalIndent(p, "", "  ")
	return successResult(string(b))
}

func ProfileUpdateHandler(args map[string]interface{}) *ToolResult {
	p := LoadProfile()

	if v, ok := args["name"].(string); ok && v != "" {
		p.Name = v
	}
	if v, ok := args["preferred_lang"].(string); ok {
		if v == "zh" || v == "en" {
			p.PreferredLang = v
		}
	}
	if v, ok := args["expertise"].(string); ok {
		if v == "beginner" || v == "intermediate" || v == "expert" {
			p.Expertise = v
		}
	}
	if v, ok := args["response_style"].(string); ok {
		if v == "concise" || v == "detailed" || v == "code_first" {
			p.ResponseStyle = v
		}
	}
	if v, ok := args["interests"].([]interface{}); ok && len(v) > 0 {
		var list []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				list = append(list, s)
			}
		}
		if len(list) > 0 {
			p.Interests = list
		}
	}

	p.UpdatedAt = time.Now().Unix()
	if err := SaveProfile(p); err != nil {
		return errorResult(fmt.Sprintf("保存失败: %v", err))
	}

	b, _ := json.MarshalIndent(p, "", "  ")
	return successResult(string(b))
}

// ─── 习惯学习 (Habit Inference) ──────────────────────────────

// loadRecentSummariesForInference 读取最近 n 条会话摘要，按 UpdatedAt 倒序。
func loadRecentSummariesForInference(n int) []SessionSummary {
	sessionDir := filepath.Join(MemoryDir, "sessions")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return nil
	}

	var sums []SessionSummary
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".summary.json") {
			continue
		}
		sid := strings.TrimSuffix(e.Name(), ".summary.json")
		s := LoadSessionSummary(sid)
		if s != nil {
			sums = append(sums, *s)
		}
	}

	sort.Slice(sums, func(i, j int) bool {
		return sums[i].UpdatedAt > sums[j].UpdatedAt
	})
	if len(sums) > n {
		sums = sums[:n]
	}
	return sums
}

// topHitKnowledgeSummary 按命中次数排序，返回 top-n 知识条目的简要描述。
func topHitKnowledgeSummary(n int) string {
	all := loadAllKnowledge()
	if len(all) == 0 {
		return ""
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].HitCount > all[j].HitCount
	})
	if len(all) > n {
		all = all[:n]
	}
	var lines []string
	for _, e := range all {
		if e.HitCount == 0 {
			break
		}
		lines = append(lines, fmt.Sprintf("- [%s] %s (命中%d次)", e.ContentType, e.Title, e.HitCount))
	}
	return strings.Join(lines, "\n")
}

// InferAndUpdateProfile 根据近期会话摘要和高频知识，通过 LLM 推断并更新用户画像。
// 最少需要 3 条摘要才触发推断；只更新有信号支撑的字段，不覆盖 Name。
func InferAndUpdateProfile() {
	sums := loadRecentSummariesForInference(20)
	if len(sums) < 3 {
		return
	}

	var ctx strings.Builder
	ctx.WriteString("最近会话摘要：\n")
	for _, s := range sums {
		ctx.WriteString(fmt.Sprintf("- %s（主题: %s）\n", s.Summary, strings.Join(s.Topics, ", ")))
	}
	if top := topHitKnowledgeSummary(10); top != "" {
		ctx.WriteString("\n高频访问知识：\n")
		ctx.WriteString(top)
	}

	system := `你是用户画像分析助手。根据用户的近期对话和高频访问知识，推断其偏好。
只输出一个 JSON 对象，不加任何解释：
{"preferred_lang":"zh或en","expertise":"beginner/intermediate/expert","interests":["领域1"],"response_style":"concise/detailed/code_first"}
规则：interests 最多 5 项，每项不超过 8 个字；无法判断的字段仍然输出原值；不推断 name 字段。`

	reply, err := llm.ChatCompletion(system, ctx.String(), 15*time.Second)
	if err != nil || reply == "" {
		return
	}

	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return
	}

	var inferred struct {
		PreferredLang string   `json:"preferred_lang"`
		Expertise     string   `json:"expertise"`
		Interests     []string `json:"interests"`
		ResponseStyle string   `json:"response_style"`
	}
	if err := json.Unmarshal([]byte(reply[start:end+1]), &inferred); err != nil {
		return
	}

	p := LoadProfile()
	updated := false

	if inferred.PreferredLang == "zh" || inferred.PreferredLang == "en" {
		if p.PreferredLang != inferred.PreferredLang {
			p.PreferredLang = inferred.PreferredLang
			updated = true
		}
	}
	if inferred.Expertise == "beginner" || inferred.Expertise == "intermediate" || inferred.Expertise == "expert" {
		if p.Expertise != inferred.Expertise {
			p.Expertise = inferred.Expertise
			updated = true
		}
	}
	if len(inferred.Interests) > 0 {
		p.Interests = inferred.Interests
		updated = true
	}
	if inferred.ResponseStyle == "concise" || inferred.ResponseStyle == "detailed" || inferred.ResponseStyle == "code_first" {
		if p.ResponseStyle != inferred.ResponseStyle {
			p.ResponseStyle = inferred.ResponseStyle
			updated = true
		}
	}

	if updated {
		p.UpdatedAt = time.Now().Unix()
		SaveProfile(p) //nolint — profile.go 内部方法，profileMu 已在 SaveProfile 内加锁
	}
}

func registerProfileTools() {
	Register("profile_show", "查看当前用户画像配置。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		ProfileShowHandler,
	)

	Register("profile_update", "更新用户画像：称呼、语言偏好、技术水平、回答风格、关注领域。",
		map[string]interface{}{
			"type":                 "object",
			"properties": map[string]interface{}{
				"name":           stringParam("用户称呼，如「小张」「老板」"),
				"preferred_lang": stringParam("偏好语言: zh（中文）/ en（英语），默认 zh"),
				"expertise":      stringParam("技术水平: beginner / intermediate / expert"),
				"response_style": stringParam("回答风格: concise（简洁）/ detailed（详细）/ code_first（代码优先）"),
				"interests": map[string]interface{}{
					"type":        "array",
					"description": "关注领域列表",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
		},
		ProfileUpdateHandler,
	)
}
