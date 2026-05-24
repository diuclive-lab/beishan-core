package observatory

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ── Event types ─────────────────────────────────────────────────────

const (
	EventAgentSpawn    = "agent.spawn"
	EventAgentProgress = "agent.progress"
	EventAgentComplete = "agent.complete"
	EventAgentFailed   = "agent.failed"

	EventSearchEmbedding   = "search.embedding"
	EventFailoverSwitch    = "failover.switch"
	EventFailoverRecovery  = "failover.recovery"
)

// ── Event types ─────────────────────────────────────────────────────

// Event is a structured system event with type, data, and metadata.
// Events serve dual purpose: real-time notification (via subscribers)
// and audit trail (via JSONL persistence).
type Event struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`      // dot-notation, e.g. "agent.spawn"
	Timestamp time.Time   `json:"timestamp"`
	SessionID string      `json:"session_id,omitempty"`
	Data      interface{} `json:"data,omitempty"`
}

// ── Agent event data types ──────────────────────────────────────────

// AgentSpawnData is published with EventAgentSpawn.
type AgentSpawnData struct {
	AgentID    string `json:"agent_id"`
	TaskPrompt string `json:"task_prompt,omitempty"`
	ParentID   string `json:"parent_id,omitempty"`
}

// AgentCompleteData is published with EventAgentComplete.
// Carries full conversation history for persistence.
type AgentCompleteData struct {
	AgentID    string            `json:"agent_id"`
	Iterations int               `json:"iterations"`
	ElapsedMs  int64             `json:"elapsed_ms"`
	Output     string            `json:"output"`
	Messages   []json.RawMessage `json:"messages,omitempty"` // full conversation snapshot
}

// AgentFailedData is published with EventAgentFailed.
type AgentFailedData struct {
	AgentID string `json:"agent_id"`
	Error   string `json:"error"`
}

// ── Subscriber ──────────────────────────────────────────────────────

// EventHandler receives events asynchronously.
// MUST return quickly — do not block in the handler.
type EventHandler func(Event)

type subscription struct {
	pattern string   // event type prefix, e.g. "agent." matches all agent events
	handler EventHandler
}

// ── Event bus state ─────────────────────────────────────────────────

var (
	eventsLogDir  string
	eventsMu      sync.RWMutex
	subscribers   []subscription
	eventsFile    *os.File
	eventsFileMu  sync.Mutex
)

// InitEvents initializes the event system. Sets up JSONL log file.
func InitEvents(logDir string) {
	eventsMu.Lock()
	defer eventsMu.Unlock()

	eventsLogDir = logDir
	os.MkdirAll(logDir, 0755)

	// Open daily log file
	logPath := filepath.Join(logDir, fmt.Sprintf("events_%s.jsonl", time.Now().Format("20060102")))
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("[events] failed to open log: %v", err)
		return
	}
	eventsFile = f
	log.Printf("[events] initialized at %s", logPath)
}

// Subscribe registers a handler for events matching the given type prefix.
// Pattern "agent." matches agent.spawn, agent.progress, agent.complete, etc.
// Pattern "*" matches all events.
// Handler MUST return quickly — spin up a goroutine if you need to do work.
func Subscribe(pattern string, handler EventHandler) {
	eventsMu.Lock()
	defer eventsMu.Unlock()
	subscribers = append(subscribers, subscription{pattern: pattern, handler: handler})
	log.Printf("[events] subscriber registered: %q", pattern)
}

// PublishEvent publishes an event: writes to JSONL and notifies all matching subscribers.
// This is synchronous — callers can rely on the event being visible
// after PublishEvent returns. Subscribers run in the caller's goroutine
// and MUST return quickly.
func PublishEvent(evt Event) {
	evt.Timestamp = time.Now().UTC()

	// 1. Persist to JSONL
	eventsFileMu.Lock()
	if eventsFile != nil {
		data, _ := json.Marshal(evt)
		eventsFile.Write(data)
		eventsFile.Write([]byte{'\n'})
	}
	eventsFileMu.Unlock()

	// 2. Notify matching subscribers
	eventsMu.RLock()
	for _, sub := range subscribers {
		if sub.pattern == "*" || strings.HasPrefix(evt.Type, sub.pattern) {
			sub.handler(evt)
		}
	}
	eventsMu.RUnlock()
}

// CloseEvents flushes and closes the event log.
func CloseEvents() {
	eventsFileMu.Lock()
	defer eventsFileMu.Unlock()
	if eventsFile != nil {
		eventsFile.Close()
		eventsFile = nil
	}
}
