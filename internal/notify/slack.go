package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

/* SendSlack 发送消息到 Slack webhook。

   target: Slack Incoming Webhook URL
           https://hooks.slack.com/services/Txxx/Bxxx/xxxx

   setup:
     1. 打开 https://api.slack.com/apps
     2. 创建 App → Incoming Webhooks → 激活 → 复制 Webhook URL
     3. 在 beishan-core 中以 callback:slack:URL 使用
*/
func SendSlack(webhookURL string, payload json.RawMessage) error {
	msg := map[string]interface{}{
		"text": formatPayload(payload),
	}
	body, _ := json.Marshal(msg)

	resp, err := http.Post(webhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("Slack 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("Slack 返回 %d", resp.StatusCode)
	}

	logPrefix := "[Slack]"
	_ = time.Second // suppress unused
	_ = fmt.Sprintf
	_ = logPrefix
	return nil
}
