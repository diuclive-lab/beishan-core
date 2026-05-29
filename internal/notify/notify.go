package notify

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"beishan/internal/observatory"
)

/* Callback 回调路由：收到 callback:xxx 格式的回程地址，
   解析前缀，分派到对应的发送器。 */
func Callback(raw string, payload json.RawMessage) {
	// panic 兜底：kernel 在 kernel.go:246 以裸 goroutine `go notify.Callback(...)`
	// 调用本函数，且这是唯一非测试调用点（grep 确认）。该 goroutine 内的 panic 不会被
	// kernel 调用方的 recover 捕获，会掀翻整个进程。在此（goroutine 的实际执行体内）兜底，
	// 等价于在 kernel.go:246 行兜底，但无需触碰冻结的 kernel/。SendSlack/SendEmail/
	// SendWeChat 做网络 I/O，理论上可 panic（如底层库 nil deref），故需此防御。
	// 见 DESIGN_PRINCIPLES.md「内核冻结治理」与 docs/MERGE_DECISIONS.md 决策 15。
	defer observatory.Recover("notify.Callback")

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

/* SendViaChannel 结构化通知发送，供 L3 工具直接调用。
   channel: email | slack | wechat
   target: SMTP URL 或 webhook URL
   subject: 仅 email 使用
   payload: 消息内容 */
func SendViaChannel(channel, target, subject string, payload json.RawMessage) error {
	switch channel {
	case "email":
		// email 需要特殊处理：把 subject 嵌入 payload
		if subject != "" {
			wrapped := fmt.Sprintf("Subject: %s\n\n%s", subject, formatPayload(payload))
			payload, _ = json.Marshal(wrapped)
		}
		return SendEmail(target, payload)
	case "slack":
		return SendSlack(target, payload)
	case "wechat":
		return SendWeChat(target, payload)
	default:
		return fmt.Errorf("未知通知渠道: %s", channel)
	}
}
