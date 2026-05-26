// UNIMPLEMENTED: channels 包是余量设计，当前未被任何地方 import。
// 创建日期: 2026-05-25 | 实现前提: 需要明确多通道接入的具体场景
//
// Package channels 定义多通道消息抽象层。
//
// 设计原则：
// - Channel 接口是余量设计，当前无实现。
// - 后续通过 plugins/channel_plugin 注册具体通道。
// - 不依赖 kernel，纯接口 + 注册表。
package channels

import (
	"fmt"
	"sync"
)

// ChannelMessage 统一的消息结构，所有通道共享。
type ChannelMessage struct {
	Channel   string `json:"channel"`   // 通道名: telegram / slack / email / wechat
	Recipient string `json:"recipient"` // 收件人 ID / 群组 ID / 邮箱地址
	Subject   string `json:"subject,omitempty"`
	Body      string `json:"body"`
	HTML      bool   `json:"html,omitempty"` // HTML 格式（仅部分通道支持）
}

// Channel 通道接口。每个具体通道（telegram/slack/email）实现此接口。
type Channel interface {
	// Name 返回通道名称。
	Name() string

	// Send 发送一条消息。
	Send(msg ChannelMessage) error

	// Connect 建立连接。对于 HTTP 通道（如 Telegram Bot API）可无操作。
	Connect() error

	// Close 关闭连接。
	Close() error
}

// Manager 管理所有已注册的通道。
type Manager struct {
	mu       sync.RWMutex
	channels map[string]Channel
}

// GlobalManager 全局通道管理器。
var GlobalManager = &Manager{channels: make(map[string]Channel)}

// Register 注册一个通道。
func (m *Manager) Register(ch Channel) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	name := ch.Name()
	if _, ok := m.channels[name]; ok {
		return fmt.Errorf("通道 %s 已注册", name)
	}
	m.channels[name] = ch
	return nil
}

// Send 向指定通道发送消息。通道未注册时返回错误。
func (m *Manager) Send(msg ChannelMessage) error {
	m.mu.RLock()
	ch, ok := m.channels[msg.Channel]
	m.mu.RUnlock()
	if !ok {
		return fmt.Errorf("通道 %s 未注册", msg.Channel)
	}
	return ch.Send(msg)
}

// SendAll 向所有已注册通道广播。
func (m *Manager) SendAll(msg ChannelMessage) []error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var errs []error
	for _, ch := range m.channels {
		if err := ch.Send(msg); err != nil {
			errs = append(errs, err)
		}
	}
	return errs
}

// List 返回所有已注册通道的名称。
func (m *Manager) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.channels))
	for name := range m.channels {
		names = append(names, name)
	}
	return names
}

// CloseAll 关闭所有通道。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ch := range m.channels {
		ch.Close()
	}
}
