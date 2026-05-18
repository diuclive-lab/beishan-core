package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

/* SendWeChat 发送消息到企业微信机器人 webhook。

   target: 企业微信机器人 Webhook URL
           https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=xxxx

   setup:
     1. 打开企业微信 → 群聊 → 添加群机器人 → 复制 Webhook URL
     2. 在 beishan-core 中以 callback:wechat:URL 使用
     3. 注意：个人微信不支持 API，需使用企业微信

   WeChat Work bot webhook:
     POST https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=KEY
     {"msgtype":"text","text":{"content":"消息内容"}}
*/
func SendWeChat(webhookURL string, payload json.RawMessage) error {
	msg := map[string]interface{}{
		"msgtype": "text",
		"text": map[string]string{
			"content": formatPayload(payload),
		},
	}
	body, _ := json.Marshal(msg)

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("企业微信请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("企业微信返回 %d", resp.StatusCode)
	}

	return nil
}
