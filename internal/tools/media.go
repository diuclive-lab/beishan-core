package tools

import (
	"bytes"
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

/* ─── 图片分析（Vision Analyze）──────────────────

   预留接口。需要外部 Vision API（如 GPT-4V、Claude Vision）驱动。
*/

func registerVisionTool() {
	Register("vision_analyze", "AI 视觉分析，分析图片内容并返回描述。需要配置外部 Vision API。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"image_url"},
			"properties": map[string]interface{}{
				"image_url": stringParam("图片来源 URL（http/https）或本地文件路径"),
				"question":  stringParam("可选：针对图片的具体问题"),
			},
		},
		visionAnalyzeHandler,
	)
}

func visionAnalyzeHandler(args map[string]interface{}) *ToolResult {
	imageURL, _ := args["image_url"].(string)
	question, _ := args["question"].(string)
	if imageURL == "" {
		return errorResult("image_url（图片来源）不能为空")
	}

	var imageData []byte
	var size int64
	format := "unknown"

	if strings.HasPrefix(imageURL, "http://") || strings.HasPrefix(imageURL, "https://") {
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Get(imageURL)
		if err != nil {
			return errorResult(fmt.Sprintf("下载图片失败: %v", err))
		}
		defer resp.Body.Close()
		imageData, _ = io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		size = int64(len(imageData))
	} else {
		info, err := os.Stat(imageURL)
		if err != nil {
			return errorResult(fmt.Sprintf("本地图片未找到: %v", err))
		}
		size = info.Size()
		data, err := os.ReadFile(imageURL)
		if err == nil {
			imageData = data
		}
	}

	if len(imageData) > 4 {
		switch {
		case imageData[0] == 0xFF && imageData[1] == 0xD8:
			format = "JPEG"
		case imageData[0] == 0x89 && imageData[1] == 'P':
			format = "PNG"
		case imageData[0] == 'G' && imageData[1] == 'I':
			format = "GIF"
		}
	}

	return successResult(fmt.Sprintf(
		"图片: %s\n大小: %d bytes\n格式: %s\n问题: %s\n\n"+
			"[预留接口] 视觉分析需要配置外部 Vision API（如 GPT-4V、Claude Sonnet 4），\n"+
			"设置 ANTHROPIC_API_KEY 或 OPENAI_API_KEY 后启用完整分析能力。",
		imageURL, size, format, question,
	))
}

/* ─── 图片生成（Image Generation）───────────────

   预留接口。需要外部生成后端（DALL-E、Stable Diffusion 等）。
*/

func registerImageGenTool() {
	Register("image_generate", "AI 图片生成，根据文字描述生成图片。需要配置外部生成后端。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"prompt"},
			"properties": map[string]interface{}{
				"prompt":       stringParam("描述图片的文字提示词（Prompt）"),
				"aspect_ratio": stringParam("宽高比，如 square（正方形）、16:9、4:3，默认 square"),
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
	ratio, _ := args["aspect_ratio"].(string)
	if ratio == "" {
		ratio = "square"
	}

	outputDir := filepath.Join(HermesHome, "generated_images")
	os.MkdirAll(outputDir, 0755)
	outputPath := filepath.Join(outputDir, fmt.Sprintf("gen_%d.png", time.Now().Unix()))

	return successResult(fmt.Sprintf(
		"图片生成请求已记录:\n提示词: %s\n比例: %s\n输出路径: %s\n\n"+
			"[预留接口] 图片生成需要配置后端：\n"+
			"- DALL-E: 设置 OPENAI_API_KEY\n"+
			"- Stable Diffusion: 设置 SD_API_URL\n"+
			"- Midjourney: 通过配置文件设置",
		prompt, ratio, outputPath,
	))
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
