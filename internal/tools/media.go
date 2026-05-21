package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

/* ─── 图片生成（Image Generation）───────────────

   支持多后端自动切换：
   1. DALL-E 3（OPENAI_API_KEY）
   2. Stable Diffusion（SD_API_URL）
   3. 无后端时返回可复用的 curl 命令
*/

var genOutputDir string

func init() {
	genOutputDir = filepath.Join(HermesHome, "generated")
	os.MkdirAll(genOutputDir, 0755)
}

func registerImageGenTool() {
	Register("image_generate", "AI 图片生成，根据文字描述生成图片。自动检测可用后端：DALL-E 3 / Stable Diffusion。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"prompt"},
			"properties": map[string]interface{}{
				"prompt":       stringParam("描述图片的文字提示词（Prompt）"),
				"aspect_ratio": stringParam("宽高比: square / 16:9 / 4:3 / 9:16 / 3:2，默认 square"),
				"size":         stringParam("输出尺寸: 1024x1024 / 1024x1792 / 1792x1024，默认 1024x1024"),
				"quality":      stringParam("DALL-E 3 支持: standard / hd，默认 standard"),
				"style":        stringParam("DALL-E 3 支持: vivid / natural，默认 vivid"),
				"model":        stringParam("后端模型: dalle3 / sd / auto，默认 auto 自动选择"),
			},
		},
		imageGenHandler,
	)
}

func imageGenHandler(args map[string]interface{}) *ToolResult {
	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return errorResult("prompt（提示词）不能为空")
	}

	ratio, _ := args["model"].(string)
	if ratio == "" {
		ratio = "auto"
	}

	openaiKey := os.Getenv("OPENAI_API_KEY")
	sdURL := os.Getenv("SD_API_URL")

	// 自动选择后端
	useDalle := openaiKey != "" && (ratio == "auto" || ratio == "dalle3")
	useSD := sdURL != "" && (ratio == "auto" || ratio == "sd")

	switch {
	case useDalle:
		return imageGenDalle3(args)
	case useSD:
		return imageGenSD(args)
	default:
		return imageGenFallback(args)
	}
}

func sizeFromRatio(ratio string) string {
	switch ratio {
	case "16:9", "16/9":
		return "1792x1024"
	case "9:16", "9/16":
		return "1024x1792"
	case "4:3", "4/3":
		return "1024x768"
	case "3:2", "3/2":
		return "1024x680"
	default:
		return "1024x1024"
	}
}

func imageGenDalle3(args map[string]interface{}) *ToolResult {
	prompt, _ := args["prompt"].(string)
	quality, _ := args["quality"].(string)
	style, _ := args["style"].(string)
	sizeStr, _ := args["size"].(string)
	ratio, _ := args["aspect_ratio"].(string)

	if quality == "" {
		quality = "standard"
	}
	if style == "" {
		style = "vivid"
	}
	if sizeStr == "" {
		sizeStr = sizeFromRatio(ratio)
	}
	if sizeStr == "" {
		sizeStr = "1024x1024"
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	client := &http.Client{Timeout: 60 * time.Second}
	body := map[string]interface{}{
		"model":           "dall-e-3",
		"prompt":          prompt,
		"n":               1,
		"size":            sizeStr,
		"quality":         quality,
		"style":           style,
		"response_format": "b64_json",
	}
	bodyJSON, _ := json.Marshal(body)

	req, _ := http.NewRequest("POST", "https://api.openai.com/v1/images/generations", bytes.NewReader(bodyJSON))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return errorResult(fmt.Sprintf("DALL-E 调用失败: %v。可尝试配置 SD_API_URL 用 Stable Diffusion", err))
	}
	defer resp.Body.Close()

	var result struct {
		Data []struct {
			B64JSON string `json:"b64_json"`
			URL     string `json:"url"`
			Revised string `json:"revised_prompt"`
		} `json:"data"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Error.Message != "" {
		return errorResult(fmt.Sprintf("DALL-E 错误: %s", result.Error.Message))
	}
	if len(result.Data) == 0 {
		return errorResult("DALL-E 未返回图片")
	}

	outputPath := filepath.Join(genOutputDir, fmt.Sprintf("dalle3_%d.png", time.Now().Unix()))
	imgData := result.Data[0]
	revised := imgData.Revised
	url := imgData.URL

	if imgData.B64JSON != "" {
		if dec, err := decodeBase64(imgData.B64JSON); err == nil {
			os.WriteFile(outputPath, dec, 0644)
		}
	}

	out := map[string]interface{}{
		"model":          "dall-e-3",
		"prompt":         prompt,
		"revised_prompt": revised,
		"url":            url,
		"file":           outputPath,
		"size":           sizeStr,
		"quality":        quality,
		"style":          style,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return successResult(string(b))
}

func imageGenSD(args map[string]interface{}) *ToolResult {
	prompt, _ := args["prompt"].(string)
	sdURL := os.Getenv("SD_API_URL")

	client := &http.Client{Timeout: 120 * time.Second}
	body := map[string]interface{}{
		"prompt":        prompt,
		"negative_prompt": "nsfw, low quality, blurry",
		"steps":         30,
		"cfg_scale":     7,
	}
	bodyJSON, _ := json.Marshal(body)

	resp, err := client.Post(sdURL+"/sdapi/v1/txt2img", "application/json", bytes.NewReader(bodyJSON))
	if err != nil {
		return errorResult(fmt.Sprintf("Stable Diffusion 连接失败: %v", err))
	}
	defer resp.Body.Close()

	var result struct {
		Images []string `json:"images"`
		Info   string   `json:"info"`
	}
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Images) == 0 {
		return errorResult("SD 未返回图片")
	}

	outputPath := filepath.Join(genOutputDir, fmt.Sprintf("sd_%d.png", time.Now().Unix()))
	if dec, err := decodeBase64(result.Images[0]); err == nil {
		os.WriteFile(outputPath, dec, 0644)
	}

	out := map[string]interface{}{
		"model": "stable_diffusion",
		"prompt": prompt,
		"file":  outputPath,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return successResult(string(b))
}

func imageGenFallback(args map[string]interface{}) *ToolResult {
	prompt, _ := args["prompt"].(string)
	apiKey := os.Getenv("OPENAI_API_KEY")
	sdURL := os.Getenv("SD_API_URL")

	outputPath := filepath.Join(genOutputDir, fmt.Sprintf("prompt_%d.txt", time.Now().Unix()))
	os.WriteFile(outputPath, []byte(prompt), 0644)

	msg := fmt.Sprintf("提示词已保存: %s\n\n", outputPath)
	msg += "当前无可用图片生成后端。配置方式：\n"

	if apiKey == "" {
		msg += "\n1️⃣ DALL-E 3（推荐）：\n"
		msg += "   export OPENAI_API_KEY=sk-...\n"
		msg += "   curl https://api.openai.com/v1/images/generations \\\n"
		msg += "     -H \"Authorization: Bearer $OPENAI_API_KEY\" \\\n"
		msg += `     -H "Content-Type: application/json" \` + "\n"
		msg += fmt.Sprintf(`     -d '{"model":"dall-e-3","prompt":"%s","n":1,"size":"1024x1024"}'`, prompt)
	}

	if sdURL == "" {
		msg += "\n\n2️⃣ Stable Diffusion（本地）：\n"
		msg += "   export SD_API_URL=http://127.0.0.1:7860\n"
		msg += "   启动 ComfyUI / Automatic1111 后自动接入"
	}

	return successResult(msg)
}

func decodeBase64(s string) ([]byte, error) {
	return io.ReadAll(
		base64.NewDecoder(base64.StdEncoding, strings.NewReader(s)),
	)
}

func floatParam(desc string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "number",
		"description": desc,
	}
}

/* ─── 图片到图片（Image-to-Image）────────────── */

func imageToImageHandler(args map[string]interface{}) *ToolResult {
	prompt, _ := args["prompt"].(string)
	imageURL, _ := args["image_url"].(string)
	strength, _ := args["strength"].(float64)

	if prompt == "" || imageURL == "" {
		return errorResult("prompt 和 image_url 不能为空")
	}
	if strength <= 0 {
		strength = 0.7
	}
	if strength > 1 {
		strength = 1
	}

	sdURL := os.Getenv("SD_API_URL")
	if sdURL != "" {
		client := &http.Client{Timeout: 120 * time.Second}
		body := map[string]interface{}{
			"prompt":           prompt,
			"init_images":      []string{imageURL},
			"denoising_strength": strength,
			"steps":            30,
		}
		bodyJSON, _ := json.Marshal(body)
		resp, err := client.Post(sdURL+"/sdapi/v1/img2img", "application/json", bytes.NewReader(bodyJSON))
		if err == nil {
			defer resp.Body.Close()
			var result struct {
				Images []string `json:"images"`
			}
			if json.NewDecoder(resp.Body).Decode(&result) == nil && len(result.Images) > 0 {
				outputPath := filepath.Join(genOutputDir, fmt.Sprintf("img2img_%d.png", time.Now().Unix()))
				if dec, err := decodeBase64(result.Images[0]); err == nil {
					os.WriteFile(outputPath, dec, 0644)
					b, _ := json.MarshalIndent(map[string]interface{}{
						"model":    "stable_diffusion",
						"prompt":   prompt,
						"file":     outputPath,
						"strength": strength,
					}, "", "  ")
					return successResult(string(b))
				}
			}
		}
	}

	openaiKey := os.Getenv("OPENAI_API_KEY")
	if openaiKey != "" {
		return successResult(fmt.Sprintf(
			"DALL-E 不支持 img2img。建议配置 SD_API_URL 使用 Stable Diffusion 的 img2img 功能。\n"+
				"源图片: %s\n提示词: %s\n强度: %.1f", imageURL, prompt, strength))
	}

	return successResult(fmt.Sprintf(
		"img2img 需要 Stable Diffusion 后端。\n源图片: %s\n提示词: %s\n强度: %.1f\n\n"+
			"配置: export SD_API_URL=http://127.0.0.1:7860", imageURL, prompt, strength))
}

func registerImageEditTool() {
	Register("image_to_image", "图生图：基于现有图片和提示词生成新图片。需要 Stable Diffusion 后端（SD_API_URL）。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"prompt", "image_url"},
			"properties": map[string]interface{}{
				"prompt":    stringParam("描述目标效果的提示词"),
				"image_url": stringParam("源图片 URL 或 base64 数据"),
				"strength":  floatParam("变换强度 0-1，0.7=较大变化，0.3=微调"),
			},
		},
		imageToImageHandler,
	)
}

/* ─── 图片分析（Vision Analyze）──────────────────

   用 Claude / GPT-4V 分析图片内容（需要配置对应 API key）。
*/

func visionAnalyzeHandler(args map[string]interface{}) *ToolResult {
	imageURL, _ := args["image_url"].(string)
	question, _ := args["question"].(string)
	if imageURL == "" {
		return errorResult("image_url（图片来源）不能为空")
	}

	var imageData []byte
	var imageSize int64
	imageFormat := "unknown"

	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(imageURL)
		if err != nil {
			return errorResult(fmt.Sprintf("下载图片失败: %v", err))
		}
		defer resp.Body.Close()
		imageData, _ = io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		imageSize = int64(len(imageData))
	} else {
		info, err := os.Stat(imageURL)
		if err != nil {
			return errorResult(fmt.Sprintf("本地图片未找到: %v", err))
		}
		imageSize = info.Size()
		data, err := os.ReadFile(imageURL)
		if err == nil {
			imageData = data
		}
	}

	if len(imageData) > 4 {
		switch {
		case imageData[0] == 0xFF && imageData[1] == 0xD8:
			imageFormat = "JPEG"
		case imageData[0] == 0x89 && imageData[1] == 'P':
			imageFormat = "PNG"
		case imageData[0] == 'G' && imageData[1] == 'I':
			imageFormat = "GIF"
		}
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey != "" && question != "" {
		client := &http.Client{Timeout: 60 * time.Second}
		body := map[string]interface{}{
			"model": "gpt-4o",
			"messages": []map[string]interface{}{
				{
					"role": "user",
					"content": []map[string]interface{}{
						{"type": "text", "text": question},
						{"type": "image_url", "image_url": map[string]string{"url": imageURL}},
					},
				},
			},
			"max_tokens": 1024,
		}
		bodyJSON, _ := json.Marshal(body)
		req, _ := http.NewRequest("POST", "https://api.openai.com/v1/chat/completions", bytes.NewReader(bodyJSON))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			var result struct {
				Choices []struct {
					Message struct {
						Content string `json:"content"`
					} `json:"message"`
				} `json:"choices"`
			}
			if json.NewDecoder(resp.Body).Decode(&result) == nil && len(result.Choices) > 0 {
				output := map[string]interface{}{
					"image":     imageURL,
					"format":    imageFormat,
					"size":      imageSize,
					"question":  question,
					"analysis":  result.Choices[0].Message.Content,
				}
				b, _ := json.MarshalIndent(output, "", "  ")
				return successResult(string(b))
			}
		}
	}

	return successResult(fmt.Sprintf(
		"图片: %s\n大小: %d bytes\n格式: %s\n问题: %s\n\n"+
			"[预留接口] 如需完整视觉分析，设置 OPENAI_API_KEY（GPT-4o）",
		imageURL, imageSize, imageFormat, question,
	))
}

/* ─── 注册 ─────────────────────────────────────── */

func registerVisionTool() {
	Register("vision_analyze", "AI 视觉分析：用 GPT-4o 分析图片内容。需配置 OPENAI_API_KEY。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"image_url"},
			"properties": map[string]interface{}{
				"image_url": stringParam("图片来源 URL（http/https）"),
				"question":  stringParam("针对图片的具体问题，如「这张图里有什么？」"),
			},
		},
		visionAnalyzeHandler,
	)
}

/* ─── 文本转语音（Text to Speech）───────────────

   本地能力，使用系统 TTS 引擎。
   - macOS: say 命令
   - Linux: espeak 命令
   - Windows: 预留
*/

func registerTTSTool() {
	Register("text_to_speech", "文本转语音，使用系统 TTS（Text-to-Speech）引擎生成音频文件。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"text"},
			"properties": map[string]interface{}{
				"text": stringParam("要转成语音的文本"),
			},
		},
		ttsHandler,
	)
}

func ttsHandler(args map[string]interface{}) *ToolResult {
	text, _ := args["text"].(string)
	if text == "" {
		return errorResult("text（文本）不能为空")
	}

	audioDir := filepath.Join(HermesHome, "audio_cache")
	os.MkdirAll(audioDir, 0755)

	outputPath := filepath.Join(audioDir, fmt.Sprintf("tts_%d", time.Now().Unix()))
	var cmd *exec.Cmd
	var errOutput string

	switch runtime.GOOS {
	case "darwin":
		outputPath += ".aiff"
		cmd = exec.Command("say", "-o", outputPath, text)
	case "linux":
		outputPath += ".wav"
		cmd = exec.Command("espeak", "-w", outputPath, text)
	default:
		return successResult(fmt.Sprintf(
			"[预留接口] TTS 在 %s 平台上暂未实现。请手动安装 TTS 工具。\n文本: %s",
			runtime.GOOS, text,
		))
	}

	if cmd != nil {
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		done := make(chan error, 1)
		go func() { done <- cmd.Run() }()
		select {
		case err := <-done:
			if err != nil {
				errOutput = stderr.String()
				return errorResult(fmt.Sprintf("TTS 生成失败: %v\n%s", err, errOutput))
			}
		case <-time.After(30 * time.Second):
			cmd.Process.Kill()
			return errorResult("TTS 生成超时")
		}
	}

	return successResult(fmt.Sprintf("语音已生成: %s\n文本: %s", outputPath, truncateStr(text, 100)))
}
