package browser

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// SessionID 是浏览器会话的唯一标识。
type SessionID string

// SessionManager 管理 Chrome 浏览器引擎实例池。
// 参考 OWL StoragePartition + Chromium NORTHSTAR 阶段 3 设计。
//
// Incognito=true 的会话使用独立 temp profile，Release 时自动清理。
// Incognito=false 的会话复用持久 profile，可被多次 Acquire。
type SessionManager struct {
	mu       sync.RWMutex
	sessions map[SessionID]*managedSession
	nextID   int64
}

// managedSession 包装一个引擎实例及其元数据。
type managedSession struct {
	Engine    Engine
	Config    ChromeConfig
	DataDir   string      // temp dir（incognito 用，结束后清理）
	CreatedAt time.Time
	RefCount  int         // 引用计数（持久会话可复用）
	closed    bool
}

// NewSessionManager 创建会话管理器。
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[SessionID]*managedSession),
	}
}

// Acquire 获取一个浏览器引擎会话。
//   - Incognito=true：总是创建新临时会话（temp profile，隔离）
//   - Incognito=false：复用持久会话（同名 profile 共享引擎）
//
// 返回 SessionID 用于 Release。
func (sm *SessionManager) Acquire(cfg ChromeConfig) (SessionID, Engine, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if cfg.Incognito {
		return sm.createSession(cfg)
	}

	// 持久会话：查找现有空闲会话
	for id, s := range sm.sessions {
		if s.closed {
			continue
		}
		if !s.Config.Incognito && s.Config.UserDataDir == cfg.UserDataDir {
			s.RefCount++
			return id, s.Engine, nil
		}
	}

	// 没有可复用的，创建新持久会话
	return sm.createSession(cfg)
}

// Release 归还会话。
//   - Incognito：销毁引擎实例 + 清理 temp data dir
//   - 持久：递减引用计数（ref count 到 0 时不销毁，供后续复用）
func (sm *SessionManager) Release(id SessionID) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	s, ok := sm.sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found", id)
	}
	if s.closed {
		return fmt.Errorf("session %s already closed", id)
	}

	if s.Config.Incognito {
		// Incognito：关闭引擎 + 清理 temp 目录
		s.Engine.Close()
		s.closed = true
		if s.DataDir != "" {
			os.RemoveAll(s.DataDir)
		}
		delete(sm.sessions, id)
		return nil
	}

	// 持久会话：递减引用
	s.RefCount--
	return nil
}

// CloseAll 关闭并清理所有会话。
func (sm *SessionManager) CloseAll() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for id, s := range sm.sessions {
		if s.closed {
			continue
		}
		s.Engine.Close()
		s.closed = true
		if s.DataDir != "" {
			os.RemoveAll(s.DataDir)
		}
		delete(sm.sessions, id)
	}
}

// Stats 返回会话管理器统计信息（调试/监控用）。
func (sm *SessionManager) Stats() map[string]interface{} {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var persistent, incognito int
	for _, s := range sm.sessions {
		if s.closed {
			continue
		}
		if s.Config.Incognito {
			incognito++
		} else {
			persistent++
		}
	}
	return map[string]interface{}{
		"persistent": persistent,
		"incognito":  incognito,
		"total":      persistent + incognito,
	}
}

func (sm *SessionManager) createSession(cfg ChromeConfig) (SessionID, Engine, error) {
	sm.nextID++
	id := SessionID(fmt.Sprintf("sess_%d", sm.nextID))

	// 处理 incognito data dir
	if cfg.Incognito && cfg.UserDataDir == "" {
		tmpDir, err := os.MkdirTemp("", "beishan-chrome-*")
		if err != nil {
			return "", nil, fmt.Errorf("create temp dir: %w", err)
		}
		cfg.UserDataDir = tmpDir
	}

	eng, err := NewChromeWithConfig(cfg)
	if err != nil {
		return "", nil, err
	}

	dataDir := ""
	if cfg.Incognito {
		dataDir = cfg.UserDataDir
	}

	sm.sessions[id] = &managedSession{
		Engine:    eng,
		Config:    cfg,
		DataDir:   dataDir,
		CreatedAt: time.Now(),
		RefCount:  1,
	}
	log.Printf("[browser] session %s created (incognito=%v, dir=%s)", id, cfg.Incognito, cfg.UserDataDir)
	return id, eng, nil
}

// GlobalSessionManager 是全局会话管理器实例。
var GlobalSessionManager = NewSessionManager()

// AcquireBrowser 是外部使用的便利函数。
//  agent=true  → incognito session
//  agent=false → persistent session（复用）
func AcquireBrowser(agent bool) (SessionID, Engine, error) {
	cfg := ChromeConfig{
		Headless:  true,
		Incognito: agent,
	}
	if !agent {
		cfg.UserDataDir = DefaultProfileDir()
	}
	return GlobalSessionManager.Acquire(cfg)
}

// ReleaseBrowser 释放通过 AcquireBrowser 获取的会话。
func ReleaseBrowser(id SessionID) error {
	return GlobalSessionManager.Release(id)
}
