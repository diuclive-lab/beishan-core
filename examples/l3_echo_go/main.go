package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

/* L3 Go 插件标准模板。

   所有 Go 语言 L3 插件都从此模板开始。
   编译后由胶水层（L2）spawn 为子进程，通过 stdin/stdout JSON 通信。

   协议流程：
   1. 启动时向 stdout 写 {"type":"register","name":"插件名"}
   2. 从 stdin 读取 dispatch 消息并处理
   3. 处理完成后向 stdout 写 response
   4. 收到 shutdown 消息时退出

   编译：
     cd plugins/l3_echo_go && go build -o l3_echo_go main.go
*/

const pluginName = "l3_echo_go"

/* ProtocolMessage 与 glue/protocol.go 中的定义一致。

   子进程只需要关注 type / id / msg_type / payload 字段。
   trace_id / timestamp / retry_count 由胶水层自动注入。
*/
type ProtocolMessage struct {
	Type    string          `json:"type"`               // register | dispatch | response | shutdown
	Name    string          `json:"name,omitempty"`      // register 时的插件名
	ID      string          `json:"id,omitempty"`        // 请求 ID，响应时必须带回
	MsgType string          `json:"msg_type,omitempty"`  // dispatch 消息类型
	Payload json.RawMessage `json:"payload,omitempty"`   // 业务数据
	Status  string          `json:"status,omitempty"`    // ok / error
	Error   string          `json:"error,omitempty"`
}

func main() {
	// 启动时注册：告诉胶水层本插件的名字
	writeJSON(ProtocolMessage{Type: "register", Name: pluginName})

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg ProtocolMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}

		switch msg.Type {
		case "shutdown":
			fmt.Fprintf(os.Stderr, "[%s] 收到 shutdown，退出\n", pluginName)
			return

		case "dispatch":
			resp := handleDispatch(msg)
			writeJSON(resp)
		}
	}
}

/* handleDispatch 处理 dispatch 消息，返回 response。

   在这里写你的业务逻辑。
   msg.MsgType 是内核消息的 Type 字段（路由依据）。
   msg.Payload 是业务数据的原始 JSON。
*/
func handleDispatch(msg ProtocolMessage) ProtocolMessage {
	fmt.Fprintf(os.Stderr, "[%s] 收到: type=%s payload=%s\n",
		pluginName, msg.MsgType, string(msg.Payload))

	// ─── 业务逻辑（示例：echo） ──────────────────────
	result := map[string]interface{}{
		"echo":   string(msg.Payload),
		"plugin": pluginName,
	}
	payload, _ := json.Marshal(result)

	return ProtocolMessage{
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
