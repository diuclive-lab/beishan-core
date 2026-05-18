package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"beishan/internal/tools"
)

/* Meta 描述插件的语义信息，供 DeepSeek 路由决策使用。

   最少字段原则：只放对路由有帮助的信息。
   - Description：一句话描述，直接注入路由 prompt
   - Tags：分类标签，备用（当前路由 prompt 不使用，保留扩展）
*/
type Meta struct {
	Description string
	Tags        []string
}

/* Plugin 是所有插件必须实现的接口。

   OnMessage 处理消息。
   如果返回的 Message 不为空，内核会将其送回给原始发送方（用于请求-响应模式）。
   L4 编排 L3 时使用此模式：L4 Call → L3 处理 → 响应回 L4。
*/
type Plugin interface {
	OnMessage(msg Message) (Message, error)
}

/* Kernel 微内核：注册 + 路由 + 请求-响应。

   冻结规则：
   - 不再新增职责
   - 不再解析 Payload
   - 不加入业务逻辑
*/
type Kernel struct {
	plugins map[string]Plugin
	metas   map[string]Meta // 插件语义描述，与 plugins 同步
	mu      sync.RWMutex
	Router  *Router

	// 请求-响应：CorrelationID → 等待中的响应通道
	pending   map[string]chan Message
	pendingMu sync.Mutex
}

func NewKernel(apiKey string) *Kernel {
	k := &Kernel{
		plugins: make(map[string]Plugin),
		metas:   make(map[string]Meta),
		Router:  NewRouter(apiKey),
		pending: make(map[string]chan Message),
	}
	// 注入收件人验证函数：同时检查工具注册中心和内核插件表
	k.Router.SetRecipientValidator(func(name string) bool {
		k.mu.RLock()
		_, inPlugins := k.plugins[name]
		k.mu.RUnlock()
		if inPlugins {
			return true
		}
		_, inTools := tools.GetToolSchema(name)
		return inTools
	})
	return k
}

/* Register 注册插件。Meta 可选，不传则描述为空。

   注册后自动同步到 Router，无需手动调用 SetPlugins。
*/
func (k *Kernel) Register(name string, p Plugin, meta ...Meta) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.plugins[name] = p

	m := Meta{}
	if len(meta) > 0 {
		m = meta[0]
	}
	k.metas[name] = m

	// 同步到 Router：名字 + 描述一起传，Router 负责拼 prompt
	k.Router.AddKnownPlugin(name, m.Description)

	log.Printf("[Kernel] 插件注册: %s", name)
}

// KnownPlugins 返回所有已注册插件名列表。
func (k *Kernel) KnownPlugins() []string {
	k.mu.RLock()
	defer k.mu.RUnlock()
	names := make([]string, 0, len(k.plugins))
	for name := range k.plugins {
		names = append(names, name)
	}
	return names
}

/* Send 发送消息。

   如果 Recipient 为空 → 强制 DeepSeek 路由。
   插件返回的响应消息会发回给 Sender。
*/
func (k *Kernel) Send(msg Message) error {
	if msg.Recipient == "" {
		decision, err := k.Router.Route(msg)
		if err != nil {
			return err
		}
		msg.Recipient = decision.Recipient
	}

	k.mu.RLock()
	plugin, ok := k.plugins[msg.Recipient]
	k.mu.RUnlock()

	if !ok {
		return errors.New("未知收件人: " + msg.Recipient)
	}

	log.Printf("[Kernel] 转发消息: %s -> %s", msg.Type, msg.Recipient)

	response, err := plugin.OnMessage(msg)

	if err == nil && msg.CorrelationID != "" && response.Sender == "" {
		response.Sender = msg.Recipient
		response.Recipient = msg.Sender
		response.CorrelationID = msg.CorrelationID
		k.deliverResponse(response)
	}

	return err
}

/* Call 同步请求-响应调用。

   L4 编排 L3 时使用：
   resp, err := kernel.Call(Message{Recipient:"search_plugin", Type:"web_search", Payload:...})
*/
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

func newCorrelationID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
