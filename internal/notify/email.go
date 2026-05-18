package notify

import (
	"encoding/json"
	"fmt"
	"net/smtp"
	"strings"
)

/* SendEmail 通过 SMTP 发送邮件。

   target 格式: smtp://user:pass@smtp.example.com:587/to@example.com

   setup:
     1. 使用你的邮箱的 SMTP 服务（QQ/163/Gmail 等）
     2. 获取授权码（非登录密码）
     3. 在 beishan-core 中以 callback:email:smtp://格式使用

   以 QQ 邮箱为例：
     SMTP 服务器: smtp.qq.com:587
     用户名: your@qq.com
     密码: 授权码（在 QQ 邮箱设置 → 账户 → 生成授权码）
     target: smtp://your@qq.com:授权码@smtp.qq.com:587/to@example.com
*/
func SendEmail(target string, payload json.RawMessage) error {
	// 解析 smtp://user:pass@host:port/to@addr
	rest := strings.TrimPrefix(target, "smtp://")
	parts := strings.SplitN(rest, "@", 2)
	if len(parts) != 2 {
		return fmt.Errorf("邮件地址格式错误: 缺少 @")
	}
	userPass := parts[0] // user:pass
	hostPort := strings.SplitN(parts[1], "/", 2)
	host := ""
	to := ""
	if len(hostPort) == 2 {
		host = hostPort[0]
		to = strings.TrimPrefix(hostPort[1], "to=")
	} else {
		return fmt.Errorf("邮件地址格式错误: 缺少收件人")
	}

	user := ""
	pass := ""
	if up := strings.SplitN(userPass, ":", 2); len(up) == 2 {
		user = up[0]
		pass = up[1]
	}

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: beishan-core 通知\r\n\r\n%s",
		user, to, formatPayload(payload))

	auth := smtp.PlainAuth("", user, pass, strings.Split(host, ":")[0])
	if err := smtp.SendMail(host, auth, user, []string{to}, []byte(msg)); err != nil {
		return fmt.Errorf("SMTP 发送失败: %w", err)
	}

	logPrefix := "[邮件]"
	_ = json.Marshal
	_ = logPrefix
	return nil
}
