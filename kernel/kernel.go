package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"beishan/internal/notify"
)

/* Meta 描述插件的语义信息，供 DeepSeek 路由决策使用。

   最少字段原则：只放对路由有帮助的信息。
   - Description：一句话描述，直接注入路由 prompt
   - Tags：分类标签，备用
   - Types：该插件支持的消息类型列表，供 skill_factory 生成和校验用
   - Example：示例调用，注入 Router prompt 帮 DeepSeek 理解精确的 type 名和参数
*/
type Meta struct {
	Description string
	Tags        []string
	Types       []string
	Example     string
}

/* Plugin 是所有插件必须实现的接口。 */
type Plugin interface {
	OnMessage(msg Message) (Message, error)
}

/* Kernel 微内核：注册 + 路由 + 请求-响应 + 回程路由。

   冻结规则：
   - 不再新增职责
   - 不再解析 Payload
   - 不加入业务逻辑
*/
type Kernel struct {
	plugins map[string]Plugin
	metas   map[string]Meta
	mu      sync.RWMutex
	Router  *Router

	pending   map[string]chan Message
	pendingMu sync.Mutex

	// SessionHandler 由外部注入（如 HTTP 层），
	// 收到 session: 前缀的 ReplyTo 时回调，内核不持有 session 状态。
	SessionHandler func(sessionID string, msg Message)
}

func NewKernel(apiKey string) *Kernel {
	k := &Kernel{
		plugins: make(map[string]Plugin),
		metas:   make(map[string]Meta),
		Router:  NewRouter(apiKey),
		pending: make(map[string]chan Message),
	}
	return k
}

/* Register 注册插件。Meta 可选，不传则描述为空。 */
func (k *Kernel) Register(name string, p Plugin, meta ...Meta) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.plugins[name] = p

	m := Meta{}
	if len(meta) > 0 {
		m = meta[0]
	}
	k.metas[name] = m

	k.Router.AddKnownPlugin(name, m.Description, m.Example)

	log.Printf("[Kernel] 插件注册: %s", name)
}

// RegisterUnlisted 注册插件但不注入 Router 提示词，不参与首轮 AI 路由。
// 用于右花等外部工具，仅支持通过 Recipient 显式调用。
func (k *Kernel) RegisterUnlisted(name string, p Plugin, meta ...Meta) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.plugins[name] = p
	m := Meta{}
	if len(meta) > 0 {
		m = meta[0]
	}
	k.metas[name] = m
	log.Printf("[Kernel] 插件注册(不路由): %s", name)
}

func (k *Kernel) KnownPlugins() []string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	names := make([]string, 0, len(k.plugins))
	for name := range k.plugins {
		names = append(names, name)
	}
	return names
}


/* KnownPluginsMeta 返回所有已注册插件的名 → Meta 映射，含 Description 和 Tags。
   供 skill_factory_plugin 构建含描述的插件列表，提升 DeepSeek 生成质量。 */
func (k *Kernel) KnownPluginsMeta() map[string]Meta {
	k.mu.RLock()
	defer k.mu.RUnlock()
	result := make(map[string]Meta, len(k.metas))
	for name, m := range k.metas {
		result[name] = m
	}
	return result
}

/* Send 发送消息。

   如果 Recipient 为空 → 强制 DeepSeek 路由。
   完成后检查 ReplyTo，调 deliverReply 处理回程路由。
*/
func (k *Kernel) Send(msg Message) error {
	if msg.Recipient == "" {
		decision, err := k.Router.Route(msg)
		if err != nil {
		return err
		}
		msg.Recipient = decision.Recipient
		if decision.MsgType != "" {
		msg.Type = decision.MsgType
		}
		if len(decision.Payload) > 0 {
			// 解包 LLM 输出的字符串 payload（如 "{\"keyword\":\"...\"}" → {"keyword":"..."})
			payload := decision.Payload
			var str string
			if err := json.Unmarshal(payload, &str); err == nil && len(str) > 0 && str[0] == '{' {
				if json.Valid([]byte(str)) {
					payload = json.RawMessage(str)
				}
			}
			// 如果路由器返回空对象 {}，保留原始 payload（避免覆盖用户消息）
			if string(payload) == "{}" && len(msg.Payload) > 0 && string(msg.Payload) != "{}" {
				// 保留原始 payload
			} else {
				msg.Payload = payload
			}
		}
	}

	k.mu.RLock()
	plugin, ok := k.plugins[msg.Recipient]
	k.mu.RUnlock()

	if !ok {
		return errors.New("未知收件人: " + msg.Recipient)
	}

	log.Printf("[Kernel] 转发消息: %s -> %s", msg.Type, msg.Recipient)

	response, err := plugin.OnMessage(msg)

	if err == nil && msg.CorrelationID != "" {
		response.Sender = msg.Recipient
		response.Recipient = msg.Sender
		response.CorrelationID = msg.CorrelationID
		k.deliverResponse(response)
	}

	if err == nil && msg.ReplyTo != "" {
		response.ReplyTo = msg.ReplyTo
		k.deliverReply(response)
	}

	return err
}

/* Call 同步请求-响应。 */
func (k *Kernel) Call(msg Message, timeout time.Duration) (Message, error) {
	msg.CorrelationID = newCorrelationID()

	ch := make(chan Message, 1)
	k.pendingMu.Lock()
	k.pending[msg.CorrelationID] = ch
	k.pendingMu.Unlock()

	defer func() {
		k.pendingMu.Lock()
		delete(k.pending, msg.CorrelationID)
		k.pendingMu.Unlock()
	}()

	if err := k.Send(msg); err != nil {
		return Message{}, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		return Message{}, fmt.Errorf("Call 超时: %s -> %s", msg.Type, msg.Recipient)
	}
}

func (k *Kernel) deliverResponse(msg Message) {
	k.pendingMu.Lock()
	ch, ok := k.pending[msg.CorrelationID]
	k.pendingMu.Unlock()

	if ok {
		select {
		case ch <- msg:
		default:
		}
	}
}

/* deliverReply 根据 ReplyTo 前缀分派回程路由。 */
func (k *Kernel) deliverReply(msg Message) {
	switch {
	case strings.HasPrefix(msg.ReplyTo, "plugin:"):
		target := strings.TrimPrefix(msg.ReplyTo, "plugin:")
		if err := k.Send(Message{
		Sender:    msg.Recipient,
		Recipient: target,
		Type:      msg.Type,
		Payload:   msg.Payload,
		}); err != nil {
		log.Printf("[Kernel] deliverReply 失败: %v", err)
		}

	case strings.HasPrefix(msg.ReplyTo, "session:"):
		sessionID := strings.TrimPrefix(msg.ReplyTo, "session:")
		if k.SessionHandler != nil {
		k.SessionHandler(sessionID, msg)
		} else {
		log.Printf("[Kernel] SessionHandler 未设置，丢弃 session 回程: %s", sessionID)
		}

	case strings.HasPrefix(msg.ReplyTo, "callback:"):
		go notify.Callback(msg.ReplyTo, msg.Payload)

	default:
		log.Printf("[Kernel] 未知 ReplyTo 格式: %s", msg.ReplyTo)
	}
}

func newCorrelationID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
