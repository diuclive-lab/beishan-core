package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

/* ─── github_readme L3 工具 ─────────────────────

   输入 GitHub 仓库 URL，返回 raw README 内容 + 项目元数据。
   不需要 API key，直接请求 raw.githubusercontent.com。
*/

type GitHubRepoInfo struct {
	Owner     string `json:"owner"`
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	ReadmeURL string `json:"readme_url"`
}

type GitHubReadmeResult struct {
	Title   string `json:"title"`             // 从 README 提取的项目名
	Content string `json:"content"`            // README 原文（Markdown）
	Size    int    `json:"size"`               // 内容大小
	Repo    GitHubRepoInfo `json:"repo"`       // 仓库信息
	Error   string `json:"error,omitempty"`
}

// githubRepoRE 匹配 GitHub 仓库 URL：https://github.com/owner/repo
var githubRepoRE = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/#?]+)`)

// extractGitHubRepo 从 URL 中提取 owner/repo
func extractGitHubRepo(rawURL string) (*GitHubRepoInfo, bool) {
	m := githubRepoRE.FindStringSubmatch(rawURL)
	if len(m) < 3 {
		return nil, false
	}
	return &GitHubRepoInfo{
		Owner:  m[1],
		Repo:   strings.TrimSuffix(m[2], ".git"),
		Branch: "main",
	}, true
}

// fetchGitHubReadme 获取 GitHub 仓库的 README 内容
func fetchGitHubReadme(repo *GitHubRepoInfo) *GitHubReadmeResult {
	// 先试 main 分支，失败试 master
	branches := []string{repo.Branch, "master"}
	for _, branch := range branches {
		rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/README.md",
			repo.Owner, repo.Repo, branch)
		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(rawURL)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			continue
		}

		data, _ := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
		content := string(data)

		// 提取标题：第一个 # 开头的行
		title := repo.Repo
		for _, line := range strings.Split(content, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "# ") {
				title = strings.TrimPrefix(trimmed, "# ")
				title = strings.TrimSpace(title)
				break
			}
		}

		return &GitHubReadmeResult{
			Title:   title,
			Content: content,
			Size:    len(content),
			Repo: GitHubRepoInfo{
				Owner:     repo.Owner,
				Repo:      repo.Repo,
				Branch:    branch,
				ReadmeURL: rawURL,
			},
		}
	}

	return &GitHubReadmeResult{
		Repo:  *repo,
		Error: "无法获取 README（main 和 master 分支均失败）",
	}
}

func GitHubReadmeHandler(args map[string]interface{}) *ToolResult {
	rawURL, _ := args["url"].(string)
	if rawURL == "" {
		return errorResult("url（GitHub 仓库地址）不能为空")
	}

	repo, ok := extractGitHubRepo(rawURL)
	if !ok {
		return errorResult("不是有效的 GitHub 仓库 URL。格式: https://github.com/owner/repo")
	}

	result := fetchGitHubReadme(repo)
	if result.Error != "" {
		b, _ := json.Marshal(result)
		// 返回成功但含 error 字段，让调用方决定是否降级
		return successResult(string(b))
	}

	b, _ := json.MarshalIndent(result, "", "  ")
	return successResult(string(b))
}

func registerGitHubTools() {
	Register("github_readme", "获取 GitHub 仓库的 README 内容。不需要 API key。输入仓库 URL，返回完整 README Markdown。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"url"},
			"properties": map[string]interface{}{
				"url": stringParam("GitHub 仓库 URL，如 https://github.com/tinyhumansai/openhuman"),
			},
		},
		GitHubReadmeHandler,
	)
}
