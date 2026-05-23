package plugins

import (
	"beishan/kernel"
	"encoding/json"
	"fmt"
	"time"
)

/* L4 标准模板（Go 语言）

   L4 的职责：编排 L3 插件完成多步任务。

   核心模式：
   1. 收到一个"复杂任务"消息
   2. 拆分成子步骤
   3. 每个子步骤通过 kernel.Call() 调用 L3 插件
   4. 拿到 L3 结果后，判断下一步做什么
   5. 最终返回结果

   与 L3 的区别：
   - L3 自己干活（搜索、写文件）
   - L4 调 L3 干活（编排）

   与 L3 的共同点：
   - 都实现 kernel.Plugin 接口
   - 都通过内核通信
*/

// ResearchPlugin 演示：做研究的 L4 插件
type ResearchPlugin struct {
	kernel *kernel.Kernel
}

func NewResearch(k *kernel.Kernel) *ResearchPlugin {
	return &ResearchPlugin{kernel: k}
}

// OnMessage 收到研究任务，开始编排
func (p *ResearchPlugin) OnMessage(msg kernel.Message) (kernel.Message, error) {
	topic := string(msg.Payload)

	fmt.Printf("[研究] 开始研究: %s\n", topic)

	// ─── 步骤 1：搜索 ─────────────────────────
	// 调 L3 search_plugin
	result, err := p.kernel.Call(kernel.Message{
		Recipient: "search_plugin",
		Sender:    "research_plugin",
		Type:      "web_search",
		Payload:   []byte(topic),
	}, 30*time.Second)

	if err != nil {
		return kernel.Message{}, fmt.Errorf("研究失败: 搜索出错: %w", err)
	}

	// 解析搜索结果
	var searchResponse struct {
		Payload json.RawMessage `json:"payload"`
	}
	json.Unmarshal(result.Payload, &searchResponse)
	_ = searchResponse // 实际场景中这里解析搜索结果

	fmt.Printf("[研究] 搜索完成\n")

	// ─── 步骤 2：根据结果判断是否补充搜索 ────────
	// 这里可以再加一次 Call
	_ = result

	// ─── 步骤 3：保存结果 ─────────────────────
	_, err = p.kernel.Call(kernel.Message{
		Recipient: "write_plugin",
		Sender:    "research_plugin",
		Type:      "write_file",
		Payload:   []byte(fmt.Sprintf(`研究结果：%s`, topic)),
	}, 10*time.Second)

	if err != nil {
		return kernel.Message{}, fmt.Errorf("研究失败: 保存出错: %w", err)
	}

	fmt.Printf("[研究] 研究完成: %s\n", topic)

	return kernel.Message{
		Sender:  "research_plugin",
		Type:    "research.result",
		Payload: []byte(fmt.Sprintf(`{"topic":"%s","status":"done"}`, topic)),
	}, nil
}
