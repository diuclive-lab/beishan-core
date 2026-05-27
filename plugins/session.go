package plugins

import (
	"sync"
	"time"
)

/* ─── Session state machine ──────────────────────────────────────────

   think_plugin 的交互状态机。用户聊天时可能触发知识建议→确认流程：

   StateIdle → LLM 建议记住 → StateRememberPending → 用户确认 → StateIdle
                                                     → 用户拒绝 → StateIdle
                                                     → 超时     → StateIdle

   StateIdle → 用户触发审查 → StateReviewPending → 批量确认 → StateIdle
                                                  → 超时     → StateIdle

   状态靠 sessionID + 内存 map 维护，重启即丢失（符合设计原则：不把
   短暂的对话状态写入持久存储）。
*/

// SessionState 用户会话的交互阶段
type SessionState int

const (
	StateIdle            SessionState = iota // 普通聊天，无待处理状态
	StateRememberPending                     // LLM 建议了知识入库，等用户确认
	StateReviewPending                       // 知识审查报告待用户处理
)

// Session 用户会话状态
type Session struct {
	State   SessionState
	Pending *PendingRemember // 待确认的记忆（StateRememberPending 时非空）
}

// SessionManager 管理所有活跃会话的状态
type SessionManager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

// NewSessionManager 创建会话管理器
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

// Get 获取或创建会话
func (sm *SessionManager) Get(sessionID string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s, ok := sm.sessions[sessionID]
	if !ok {
		s = &Session{State: StateIdle}
		sm.sessions[sessionID] = s
	}
	return s
}

// HasValidPending 检查会话是否有未过期的待确认记忆
func (sm *SessionManager) HasValidPending(sessionID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s, ok := sm.sessions[sessionID]
	if !ok || s.State != StateRememberPending || s.Pending == nil {
		return false
	}
	if time.Now().Unix() > s.Pending.ExpiresAt {
		delete(sm.sessions, sessionID)
		return false
	}
	return true
}

// SetRememberPending 将会话置为"待确认记忆"状态
func (sm *SessionManager) SetRememberPending(sessionID string, pr *PendingRemember) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[sessionID] = &Session{
		State:   StateRememberPending,
		Pending: pr,
	}
}

// Confirm 处理确认操作：返回待确认的记忆并重置状态
func (sm *SessionManager) Confirm(sessionID string) *PendingRemember {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s, ok := sm.sessions[sessionID]
	if !ok || s.State != StateRememberPending || s.Pending == nil {
		return nil
	}
	if time.Now().Unix() > s.Pending.ExpiresAt {
		delete(sm.sessions, sessionID)
		return nil
	}
	pr := s.Pending
	sm.sessions[sessionID] = &Session{State: StateIdle}
	return pr
}

// Reset 重置会话状态
func (sm *SessionManager) Reset(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.sessions[sessionID] = &Session{State: StateIdle}
}

// Cleanup 清理所有过期会话
// Cleanup 清理过期 pending，返回被清理的条目（供调用方写事件）
func (sm *SessionManager) CollectExpiredPendings() []*PendingRemember {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	now := time.Now().Unix()
	var expired []*PendingRemember
	for id, s := range sm.sessions {
		if s.Pending != nil && now > s.Pending.ExpiresAt {
			expired = append(expired, s.Pending)
			delete(sm.sessions, id)
		}
	}
	return expired
}

// ClearPending 清除指定 session 的 pending，返回被清除的条目
func (sm *SessionManager) ClearPending(sessionID string) *PendingRemember {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	s, ok := sm.sessions[sessionID]
	if !ok || s.Pending == nil {
		return nil
	}
	pr := s.Pending
	s.Pending = nil
	s.State = StateIdle
	return pr
}
