package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

/* ─── Prompt 工程工具 ────────────────────────────

   prompt_engineer：把粗糙想法优化为不同平台的优质提示词。
   prompt_template：保存/加载常用提示词模板。

   纯代码逻辑，零外部 API 调用。
*/

type PlatformStyle struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	FormatTips  string `json:"format_tips"`
}

var platforms = []PlatformStyle{
	{
		Name: "midjourney",
		Description: "Midjourney 擅长艺术风格、概念设计、角色设计",
		FormatTips: "短句为主，用 --ar 指定比例，--style raw 去掉默认美化，--v 6 指定版本。关键词用逗号分隔，权重用 :: 标记",
	},
	{
		Name: "dalle3",
		Description: "DALL-E 3 擅长写实照片、商业设计、精准还原描述",
		FormatTips: "自然语言段落，描述主体、环境、光线、构图、风格。不要用负面提示词，不支持权重语法",
	},
	{
		Name: "stable_diffusion",
		Description: "Stable Diffusion 擅长精细控制、特定画风、Inpainting",
		FormatTips: "英文关键词为主，正面提示词用 () 加权，负面提示词用 negative prompt。推荐加画质词(masterpiece,best quality)",
	},
	{
		Name: "flux",
		Description: "Flux Pro 擅长写实照片、产品渲染、建筑可视化",
		FormatTips: "自然语言描述，越详细越好。适合产品广告、室内设计、人物肖像",
	},
}

func PromptEngineerHandler(args map[string]interface{}) *ToolResult {
	idea, _ := args["idea"].(string)
	target, _ := args["target_platform"].(string)
	if idea == "" {
		return errorResult("idea（创意描述）不能为空")
	}

	platforms_raw, ok := args["platforms"].([]interface{})
	if !ok || len(platforms_raw) == 0 {
		if target != "" {
			platforms_raw = []interface{}{target}
		} else {
			platforms_raw = []interface{}{"all"}
		}
	}
	var targets []string
	for _, p := range platforms_raw {
		if s, ok := p.(string); ok {
			targets = append(targets, s)
		}
	}
	if len(targets) == 0 {
		targets = []string{"all"}
	}

	type resultItem struct {
		Platform   PlatformStyle `json:"platform"`
		Prompt     string        `json:"prompt"`
		Negative   string        `json:"negative_prompt,omitempty"`
		Parameters map[string]string `json:"parameters,omitempty"`
		Tips       string        `json:"tips"`
	}

	all := targets[0] == "all"
	var results []resultItem
	for _, p := range platforms {
		if !all && !containsTarget(targets, p.Name) {
			continue
		}

		prompt := optimizePrompt(idea, p.Name)
		item := resultItem{
			Platform: p,
			Prompt:   prompt,
			Tips:     p.FormatTips,
		}

		switch p.Name {
		case "midjourney":
			item.Parameters = map[string]string{
				"aspect":  "--ar 16:9",
				"version": "--v 6",
				"style":   "--style raw",
			}
		case "stable_diffusion":
			item.Negative = "nsfw, low quality, blurry, deformed, disfigured, bad anatomy"
			item.Parameters = map[string]string{
				"steps":  "30",
				"cfg":    "7",
				"sampler": "DPM++ 2M Karras",
			}
		case "flux":
			item.Parameters = map[string]string{
				"aspect_ratio": "16:9",
			}
		}

		results = append(results, item)
	}

	output := map[string]interface{}{
		"idea":        idea,
		"results":     results,
		"count":       len(results),
	}
	b, _ := json.MarshalIndent(output, "", "  ")
	return successResult(string(b))
}

func PromptExplainHandler(args map[string]interface{}) *ToolResult {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return errorResult("prompt（提示词）不能为空")
	}

	analysis := analyzePrompt(prompt)
	b, _ := json.MarshalIndent(analysis, "", "  ")
	return successResult(string(b))
}

type PromptAnalysis struct {
	Length     int               `json:"length"`
	Words      int               `json:"word_count"`
	HasNeg     bool              `json:"has_negative_prompt"`
	HasWeight  bool              `json:"has_weight_syntax"`
	HasAspect  bool              `json:"has_aspect_ratio"`
	Style      string            `json:"detected_style"`
	Quality    int               `json:"quality_score"` // 0-10
	Suggestions []string         `json:"suggestions"`
}

func analyzePrompt(prompt string) PromptAnalysis {
	analysis := PromptAnalysis{
		Length:      len(prompt),
		Words:       len(strings.Fields(prompt)),
		Suggestions: []string{},
	}

	lower := strings.ToLower(prompt)

	if strings.Contains(lower, "negative") || strings.Contains(lower, "nsfw") {
		analysis.HasNeg = true
	}
	if strings.Contains(prompt, "(") && strings.Contains(prompt, ")") {
		analysis.HasWeight = true
	}
	if strings.Contains(lower, "--ar") || strings.Contains(lower, "aspect") {
		analysis.HasAspect = true
	}

	// 检测风格
	styleKeywords := map[string][]string{
		"写实":    {"photorealistic", "photograph", "8k", "realistic", "写实", "真实"},
		"动漫":    {"anime", "manga", "动漫", "二次元"},
		"概念艺术": {"concept art", "fantasy", "cinematic", "概念"},
		"油画":    {"oil painting", "impasto", "油画"},
		"像素":    {"pixel art", "8-bit", "像素"},
		"水彩":    {"watercolor", "水彩"},
		"3D渲染":  {"3d render", "c4d", "blender", "octane"},
	}
	for style, kws := range styleKeywords {
		for _, kw := range kws {
			if strings.Contains(lower, kw) {
				analysis.Style = style
				break
			}
		}
		if analysis.Style != "" {
			break
		}
	}
	if analysis.Style == "" {
		analysis.Style = "未检测到明确风格"
	}

	// 质量评分
	score := 5
	if analysis.Length > 20 {
		score++
	}
	if analysis.Length > 80 {
		score++
	}
	if analysis.HasAspect {
		score++
	}
	if analysis.Style != "未检测到明确风格" {
		score++
	}
	if analysis.Words > 10 {
		score++
	}
	if analysis.Words > 30 {
		score++
	}
	if score > 10 {
		score = 10
	}
	analysis.Quality = score

	// 建议
	if analysis.Length < 30 {
		analysis.Suggestions = append(analysis.Suggestions, "提示词太短，建议详细描述主体、环境、光线、构图")
	}
	if !analysis.HasAspect {
		analysis.Suggestions = append(analysis.Suggestions, "未指定宽高比，建议加 --ar 16:9 或 --ar 1:1")
	}
	if analysis.Style == "未检测到明确风格" {
		analysis.Suggestions = append(analysis.Suggestions, "未指定风格，建议添加如 photorealistic / anime / oil painting 等风格词")
	}
	if analysis.Words < 8 {
		analysis.Suggestions = append(analysis.Suggestions, "关键词太少，建议增加细节描述：材质、颜色、光照、视角")
	}

	return analysis
}

func optimizePrompt(idea, platform string) string {
	switch platform {
	case "midjourney":
		return fmt.Sprintf("%s --ar 16:9 --v 6 --style raw", idea)
	case "dalle3":
		return fmt.Sprintf("请生成一张高质量的图片：%s。照片级写实风格，柔和自然光线，精细细节", idea)
	case "stable_diffusion":
		return fmt.Sprintf("(masterpiece, best quality:1.2), %s, (intricate details:1.1), (sharp focus:1.0)", idea)
	case "flux":
		return fmt.Sprintf("Professional photograph of %s, soft lighting, detailed texture, 8K, architectural photography style", idea)
	default:
		return idea
	}
}

func containsTarget(targets []string, name string) bool {
	for _, t := range targets {
		if t == name {
			return true
		}
	}
	return false
}

func registerPromptTools() {
	Register("prompt_engineer", "提示词工程：把粗糙创意优化为不同平台的优质提示词。支持 Midjourney / DALL-E 3 / Stable Diffusion / Flux，含参数推荐。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"idea"},
			"properties": map[string]interface{}{
				"idea": stringParam("你的创意描述，如「一只在月光下奔跑的银色机械狼」"),
				"target_platform": stringParam("目标平台: midjourney / dalle3 / stable_diffusion / flux。不指定则输出全部"),
				"platforms": map[string]interface{}{
					"type":        "array",
					"description": "目标平台列表，如 [\"midjourney\",\"dalle3\"]",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
		},
		PromptEngineerHandler,
	)

	Register("prompt_analyze", "分析提示词质量：检测长度、风格、权重语法、宽高比，给出优化建议。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"prompt"},
			"properties": map[string]interface{}{
				"prompt": stringParam("要分析的提示词文本"),
			},
		},
		PromptExplainHandler,
	)

	Register("prompt_style_list", "列出支持的提示词风格和平台信息。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			b, _ := json.MarshalIndent(map[string]interface{}{
				"platforms": platforms,
				"count":     len(platforms),
			}, "", "  ")
			return successResult(string(b))
		},
	)
}
