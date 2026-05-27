package llmguard

import (
	"fmt"
	"log"
	"time"

	"beishan/internal/llm"
)

// chatFunc 是底层 LLM 调用的间接入口，便于测试时注入桩函数。
// 默认指向 llm.ChatCompletionWithUsage（生产路径）。
// 测试中可临时替换为返回固定结果的函数，避免依赖真实 API。
//
// 这是包内变量而非全局函数指针：仅供 llmguard 自己使用，
// 不允许外部包覆写（保持封装性）。
var chatFunc = llm.ChatCompletionWithUsage

// Chat 是 llmguard 的主入口：受契约约束的 LLM 调用。
//
// 执行流程：
//  1. buildBaseline(c)：根据 Contract 生成需要注入的基线提示词
//  2. injectBaseline()：把基线追加到 messages 的 system 内容（不污染入参）
//  3. chatFunc()：实际调用 LLM
//  4. validateOutput()：校验输出是否符合契约
//  5. 不符合 → 把违规反馈作为新一轮 user message，重试（最多 MaxRetries 次）
//  6. 符合 + Critique → 进入 critique-revise 流程
//  7. 返回最终 output + 累计 usage
//
// 错误语义：
//   - LLM 调用本身失败 → 返回非 nil error（不重试）
//   - 重试用尽仍违反契约 → 返回最后一次 output + 非 nil error
//     调用方可选择忽略错误使用降级输出，或上抛
//
// 时间预算：
//   timeout 是单次 LLM 调用的超时，不是总超时。
//   总耗时上限 = timeout × (MaxRetries+1) [+ timeout×2 if Critique]
//   调用方需自行评估总耗时是否可接受。
func Chat(messages []llm.ChatMessage, c Contract, timeout time.Duration) (string, *llm.Usage, error) {
	// 注入基线（零值 Contract 时 baseline 为空，injectBaseline 直接返回原 messages）
	baseline := buildBaseline(c)
	current := injectBaseline(messages, baseline)

	maxAttempts := c.MaxRetries + 1
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var (
		lastOutput string
		totalUsage = &llm.Usage{}
		lastErr    error
	)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		output, usage, err := chatFunc(current, timeout)
		if err != nil {
			// 真实调用失败（网络/API 错），不在 llmguard 层吞掉
			return "", nil, fmt.Errorf("llmguard.Chat: LLM 调用失败 (attempt %d/%d): %w",
				attempt+1, maxAttempts, err)
		}
		accumulateUsage(totalUsage, usage)
		lastOutput = output

		// 契约校验
		violation := validateOutput(output, c)
		if violation == nil {
			// 校验通过
			if c.Critique && attempt == 0 {
				// 进入 critique-revise（仅第一次成功输出时触发，避免重试链中重复 critique）
				revised, critUsage, critErr := critiqueRevise(messages, output, c, timeout)
				accumulateUsage(totalUsage, critUsage)
				if critErr != nil {
					log.Printf("[llmguard] critique 失败，回退原输出: %v", critErr)
					return output, totalUsage, nil
				}
				return revised, totalUsage, nil
			}
			return output, totalUsage, nil
		}

		// 校验失败，准备下一轮重试
		lastErr = violation
		log.Printf("[llmguard] 契约校验失败 (attempt %d/%d): %v", attempt+1, maxAttempts, violation)

		if attempt+1 < maxAttempts {
			// 把违规反馈拼回 messages（让 LLM 知道哪里错了）
			feedback := fmt.Sprintf("你上一次的输出违反了契约：%v\n请按契约规则重新输出，只输出符合规则的内容。", violation)
			current = append(append([]llm.ChatMessage{}, current...),
				llm.ChatMessage{Role: "assistant", Content: output},
				llm.ChatMessage{Role: "user", Content: feedback})
		}
	}

	// 重试用尽，返回最后一次输出 + 错误标记
	// 调用方可以选择忽略错误使用降级输出
	return lastOutput, totalUsage, fmt.Errorf("llmguard.Chat: 重试 %d 次仍违反契约: %w",
		maxAttempts, lastErr)
}

// accumulateUsage 把 src 的 token 用量累加到 dst。
// 处理 nil src 的情况（某些 provider 不返回 usage）。
func accumulateUsage(dst, src *llm.Usage) {
	if src == nil || dst == nil {
		return
	}
	dst.PromptTokens += src.PromptTokens
	dst.CompletionTokens += src.CompletionTokens
	dst.TotalTokens += src.TotalTokens
	if dst.Model == "" {
		dst.Model = src.Model
	}
}
