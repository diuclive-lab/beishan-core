// Package translate provides translation using a simple API.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const libreURL = "https://libretranslate.com/translate"

type TranslateTool struct{}

func NewTranslateTool() *TranslateTool { return &TranslateTool{} }

func (t *TranslateTool) Name() string { return "translate" }

func (t *TranslateTool) Run(ctx context.Context, args map[string]any) (string, error) {
	text := getText(args)
	if text == "" {
		return "", fmt.Errorf("translate: text is required")
	}
	target := getTarget(args)
	source := getSource(args)

	// Use LibreTranslate (free, no API key)
	data := url.Values{}
	data.Set("q", text)
	data.Set("source", source)
	data.Set("target", target)

	resp, err := http.PostForm(libreURL, data)
	if err != nil {
		// Fallback: return a simple message
		return fmt.Sprintf("翻译: %s → %s", source, text), nil
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		TranslatedText string `json:"translatedText"`
	}
	if json.Unmarshal(body, &result) == nil && result.TranslatedText != "" {
		return result.TranslatedText, nil
	}
	return fmt.Sprintf("[%s] %s", target, text), nil
}

func getText(args map[string]any) string {
	for _, key := range []string{"text", "content", "q", "source_text", "文本"} {
		if v, ok := args[key]; ok {
			s, _ := v.(string)
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func getTarget(args map[string]any) string {
	for _, key := range []string{"target_lang", "target", "to", "lang", "语言"} {
		if v, ok := args[key]; ok {
			s, _ := v.(string)
			if s != "" {
				return normalizeLang(s)
			}
		}
	}
	return "zh"
}

func getSource(args map[string]any) string {
	for _, key := range []string{"source_lang", "source", "from", "原文语言"} {
		if v, ok := args[key]; ok {
			s, _ := v.(string)
			if s != "" {
				return normalizeLang(s)
			}
		}
	}
	return "auto"
}

func normalizeLang(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	m := map[string]string{
		"中文": "zh", "汉语": "zh", "chinese": "zh", "zh-cn": "zh",
		"英文": "en", "英语": "en", "english": "en",
		"日文": "ja", "日语": "ja", "japanese": "ja",
		"韩文": "ko", "韩语": "ko", "korean": "ko",
		"法文": "fr", "法语": "fr", "french": "fr",
		"德文": "de", "德语": "de", "german": "de",
		"俄文": "ru", "俄语": "ru", "russian": "ru",
	}
	if v, ok := m[lang]; ok {
		return v
	}
	return lang
}

func registerTranslateTool() {
}
