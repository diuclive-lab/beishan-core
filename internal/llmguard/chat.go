package llmguard

import (
	"fmt"
	"log"
	"time"

	"beishan/internal/llm"
)

// chatFunction 抽象 LLM 调用：(messages, timeout) → (output, usage, error)。
// 不直接暴露 provider，因为 provider 选择已经在闭包里固定下来。
// 这层抽象同时支持测试桩函数和生产 provider override。
type chatFunction func(messages []llm.ChatMessage, timeout time.Duration) (string, *llm.Usage, error)

// defaultChatFunc 是 Chat() 使用的默认 LLM 入口（走全局 provider）。
// 测试中可临时替换，包外不可见以保持封装性。
var defaultChatFunc chatFunction = llm.ChatCompletionWithUsage

// Chat 受契约约束的 LLM 调用（默认 provider）。
//
// 详见 chatCore 的执行流程说明。
// 用于不需要 per-step provider 切换的常规场景。
func Chat(messages []llm.ChatMessage, c Contract, timeout time.Duration) (string, *llm.Usage, error) {
	return chatCore(messages, c, timeout, defaultChatFunc)
}

// ChatWithProvider 受契约约束的 LLM 调用，指定 provider。
//
// 用于 workflow per-step provider override 场景，例如：
//   - 路由用 DeepSeek（快）
//   - 体力活用本地 Qwen（省钱）
//   - 报告生成用 GPT-4o（质量高）
//
// 内部用闭包把 provider 固定到 chatFunction，
// 后续与 Chat() 共享同一套校验+重试+critique 逻辑。
func ChatWithProvider(provider string, messages []llm.ChatMessage, c Contract, timeout time.Duration) (string, *llm.Usage, error) {
	fn := func(msgs []llm.ChatMessage, t time.Duration) (string, *llm.Usage, error) {
		return llm.ChatCompletionWithProvider(provider, msgs, t)
	}
	return chatCore(messages, c, timeout, fn)
}

// chatCore 是 Chat/ChatWithProvider 共享的契约执行核心。
//
// 执行流程：
//  1. buildBaseline(c)：根据 Contract 生成基线提示词
//  2. injectBaseline()：把基线追加到 messages 的 system 内容（不污染入参）
//  3. fn()：实际调用 LLM（默认 provider 或指定 provider）
//  4. validateOutput()：校验输出是否符合契约
//  5. 不符合 → 把违规反馈作为新 user message，重试（最多 MaxRetries 次）
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
func chatCore(messages []llm.ChatMessage, c Contract, timeout time.Duration, fn chatFunction) (string, *llm.Usage, error) {
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
		output, usage, err := fn(current, timeout)
		if err != nil {
			return "", nil, fmt.Errorf("llmguard.Chat: LLM 调用失败 (attempt %d/%d): %w",
				attempt+1, maxAttempts, err)
		}
		accumulateUsage(totalUsage, usage)
		lastOutput = output

		violation := validateOutput(output, c)
		if violation == nil {
			if c.Critique && attempt == 0 {
				revised, critUsage, critErr := critiqueRevise(messages, output, c, timeout, fn)
				accumulateUsage(totalUsage, critUsage)
				if critErr != nil {
					log.Printf("[llmguard] critique 失败，回退原输出: %v", critErr)
					return output, totalUsage, nil
				}
				return revised, totalUsage, nil
			}
			return output, totalUsage, nil
		}

		lastErr = violation
		log.Printf("[llmguard] 契约校验失败 (attempt %d/%d): %v", attempt+1, maxAttempts, violation)

		if attempt+1 < maxAttempts {
			feedback := fmt.Sprintf("你上一次的输出违反了契约：%v\n请按契约规则重新输出，只输出符合规则的内容。", violation)
			current = append(append([]llm.ChatMessage{}, current...),
				llm.ChatMessage{Role: "assistant", Content: output},
				llm.ChatMessage{Role: "user", Content: feedback})
		}
	}

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
