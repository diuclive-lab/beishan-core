package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

func main() {
	api := "http://127.0.0.1:8013"
	if len(os.Args) > 1 {
		api = os.Args[1]
	}

	// 检查服务是否启动
	resp, err := http.Get(api + "/health")
	if err != nil {
		fmt.Printf("无法连接 %s，请先启动服务：cd /Users/dc/Desktop/0 && go run main.go\n", api)
		os.Exit(1)
	}
	resp.Body.Close()

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Printf("beishan-core REPL — 连接到 %s\n输入消息直接聊天，输入 /help 查看命令\n\n", api)

	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}

		switch {
		case input == "/quit" || input == "/exit":
			return
		case input == "/help":
			fmt.Println("命令:")
			fmt.Println("  /help             显示帮助")
			fmt.Println("  /quit, /exit      退出")
			fmt.Println("  /plugins          列出可用插件")
			fmt.Println("  /workflows        列出工作流")
			fmt.Println("  直接输入          聊天")
			continue
		case input == "/plugins":
			chat(api, "workflow_plugin", "session_list", `{}`)
			continue
		case input == "/workflows":
			fmt.Println("可用工作流:")
			fmt.Println("  legal_review     法律审查（4步链）")
			fmt.Println("  更多工作流放在 workflows/ 目录下")
			continue
		}

		// 检查是否是 /recipient:type text 格式
		recipient := ""
		msgType := "chat"
		text := input
		if strings.HasPrefix(input, "/") {
			parts := strings.SplitN(input[1:], " ", 2)
			if len(parts) == 2 {
				sub := strings.SplitN(parts[0], ":", 2)
				recipient = sub[0]
				if len(sub) > 1 {
					msgType = sub[1]
				}
				text = parts[1]
			}
		}

		if recipient == "" {
			// 普通聊天，交给 think_plugin
			resp, err := http.Post(api+"/api/chat", "application/json",
				strings.NewReader(fmt.Sprintf(`{"message":"%s"}`, jsonEscape(text))))
			if err != nil {
				fmt.Printf("请求失败: %v\n", err)
				continue
			}
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var result map[string]interface{}
			json.Unmarshal(body, &result)
			if note, ok := result["note"]; ok {
				fmt.Printf("  %v\n", note)
			} else if payload, ok := result["payload"]; ok {
				fmt.Printf("  %v\n", payload)
				// try harder to parse payload
				bs, _ := json.MarshalIndent(result, "  ", "  ")
				fmt.Println(string(bs))
			} else {
				bs, _ := json.MarshalIndent(result, "  ", "  ")
				fmt.Println(string(bs))
			}
		} else {
			// 指定插件: /workflow_plugin:workflow_run ...
			var payload string
			if msgType == "workflow_run" {
				payload = fmt.Sprintf(`{"workflow":"%s","input":"%s"}`, text, text)
			} else {
				payload = fmt.Sprintf(`"%s"`, jsonEscape(text))
			}
			chat(api, recipient, msgType, payload)
		}
	}
}

func chat(api, recipient, msgType, payload string) {
	body := fmt.Sprintf(`{"recipient":"%s","type":"%s","payload":%s}`,
		recipient, msgType, payload)
	resp, err := http.Post(api+"/api/chat", "application/json",
		strings.NewReader(body))
	if err != nil {
		fmt.Printf("请求失败: %v\n", err)
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]interface{}
	json.Unmarshal(data, &result)
	bs, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(bs))
}

func jsonEscape(s string) string {
	b, _ := json.Marshal(s)
	return string(b[1 : len(b)-1])
}
