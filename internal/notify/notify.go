package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

/* Callback 回调路由：收到 callback:xxx 格式的回程地址，
   解析前缀，分派到对应的发送器。 */
func Callback(raw string, payload json.RawMessage) {
	// 格式: "callback:slack:https://hooks.slack.com/..."
	//        "callback:email:smtp://user@host/to@addr"
	//        "callback:wechat:https://qyapi.weixin.qq.com/..."
	parts := strings.SplitN(raw, ":", 3)
	if len(parts) < 3 {
		log.Printf("[notify] 回调地址格式错误: %s", raw)
		return
	}
	platform := parts[1] // slack / email / wechat
	target := parts[2]   // URL 或配置

	switch platform {
	case "slack":
		if err := SendSlack(target, payload); err != nil {
			log.Printf("[notify] Slack 推送失败: %v", err)
		}
	case "email":
		if err := SendEmail(target, payload); err != nil {
			log.Printf("[notify] 邮件发送失败: %v", err)
		}
	case "wechat":
		if err := SendWeChat(target, payload); err != nil {
			log.Printf("[notify] 企业微信推送失败: %v", err)
		}
	default:
		log.Printf("[notify] 未知回调平台: %s", platform)
	}
}

// formatPayload 将 payload 转为可读文本
func formatPayload(payload json.RawMessage) string {
	var s string
	if err := json.Unmarshal(payload, &s); err == nil {
		return s
	}
	return string(payload)
}

var _ = fmt.Sprintf // suppress unused
