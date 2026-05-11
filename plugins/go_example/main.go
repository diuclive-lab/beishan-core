package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

/* Go 插件标准模板。

   所有 Go 插件都从这个模板开始。
   编译后由胶水层 spawn 为子进程，通过 stdin/stdout 通信。

   编译：
   cd plugins/go_example && go build -o go_example main.go
*/

const pluginName = "go_example"

type Message struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Sender  string          `json:"sender,omitempty"`
	MsgType string          `json:"msg_type,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Status  string          `json:"status,omitempty"`
	Error   string          `json:"error,omitempty"`
}

func main() {
	// 启动时注册
	writeJSON(Message{Type: "register", Name: pluginName})

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "shutdown":
			return

		case "dispatch":
			resp := handleDispatch(msg)
			writeJSON(resp)
		}
	}
}

/* handleDispatch 处理 dispatch 消息，返回 response。

   在这里写你的业务逻辑。
*/
func handleDispatch(msg Message) Message {
	fmt.Fprintf(os.Stderr, "[%s] 收到消息: type=%s  payload=%s\n",
		pluginName, msg.MsgType, string(msg.Payload))

	// ─── 业务逻辑 ──────────────────────────────
	result := map[string]interface{}{
		"echo": string(msg.Payload),
	}
	payload, _ := json.Marshal(result)
	// ──────────────────────────────────────────

	return Message{
		Type:    "response",
		ID:      msg.ID,
		Status:  "ok",
		Payload: payload,
	}
}

func writeJSON(v interface{}) {
	data, _ := json.Marshal(v)
	os.Stdout.Write(data)
	os.Stdout.Write([]byte("\n"))
}
