package tools

import (
	"encoding/json"
	"fmt"
	"os"

	"beishan/internal/notify"
)

/* ─── NotifySend 通知发送 ───────────────────────── */

func NotifySend(channel, target, subject, message string) *ToolResult {
	if channel == "" {
		channel = "email"
	}
	if target == "" {
		target = os.Getenv("NOTIFY_TARGET")
	}
	if target == "" {
		return errorResult("target 不能为空（可设置 NOTIFY_TARGET 环境变量或传入 target 参数）")
	}
	if message == "" {
		return errorResult("message 不能为空")
	}

	payload, _ := json.Marshal(message)
	err := notify.SendViaChannel(channel, target, subject, payload)
	if err != nil {
		return errorResult(fmt.Sprintf("通知发送失败: %v", err))
	}
	return successResult(fmt.Sprintf("通知已通过 %s 发送: %s", channel, truncateStr(message, 100)))
}

/* ─── Tool 注册 ─────────────────────────────────── */

func registerNotifyTools() {
	Register("notify_send", "发送通知（支持 email/slack/wechat），适用于 workflow 执行完成后推送报告。",
		map[string]interface{}{
			"type":     "object",
			"required": []string{"channel", "target", "message"},
			"properties": map[string]interface{}{
				"channel": stringParam("通知渠道: email | slack | wechat"),
				"target":  stringParam("目标地址: SMTP URL 或 webhook URL"),
				"subject": stringParam("主题（仅 email 使用）"),
				"message": stringParam("通知正文内容"),
			},
		},
		func(args map[string]interface{}) *ToolResult {
			return NotifySend(
				strArg(args, "channel"),
				strArg(args, "target"),
				strArg(args, "subject"),
				strArg(args, "message"),
			)
		},
	)
}
