// UNIMPLEMENTED: legacy 包目前未被任何地方 import（含具体实现但无调用点）。
// 创建日期: 2026-05-25 | 实现前提: 需要有旧方法名需要解析的接入场景
//
// Package legacy provides backward-compatible method name resolution.
// Inspired by OpenHuman core/legacy_aliases.rs.
package legacy

import "strings"

var aliases = map[string]string{
	// Right flower method aliases (old → new)
	"memory.search":   "openhuman.memory_recall_memories",
	"memory.store":    "openhuman.memory_doc_put",
	"context.retrieve": "openhuman.memory_context_query",
	"code.review":     "openhuman.agent_chat",

	// Kernel method aliases
	"ping": "core.ping",
}

// Resolve rewrites legacy method names to current canonical form.
// Returns the original name if no alias exists.
func Resolve(method string) string {
	if canonical, ok := aliases[method]; ok {
		return canonical
	}
	// Also try case-insensitive match
	lower := strings.ToLower(method)
	if canonical, ok := aliases[lower]; ok {
		return canonical
	}
	return method
}

// Register adds a method alias at runtime.
func Register(legacy, canonical string) {
	aliases[legacy] = canonical
}
