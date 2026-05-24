package plugins

import (
	"encoding/json"
	"testing"

	"beishan/kernel"
)

func TestThinkPluginRejectsNonChat(t *testing.T) {
	p := &ThinkPlugin{Kernel: nil}
	_, err := p.OnMessage(kernel.Message{Type: "non_chat", Payload: json.RawMessage(`"test"`)})
	if err == nil { t.Fatal("expected error for non-chat type") }
}

func TestSearchPluginImplementsPlugin(t *testing.T) {
	var p interface{} = &SearchPlugin{}
	_, ok := p.(kernel.Plugin)
	if !ok { t.Fatal("SearchPlugin should implement kernel.Plugin") }
}

func TestWritePluginImplementsPlugin(t *testing.T) {
	var p interface{} = &WritePlugin{}
	_, ok := p.(kernel.Plugin)
	if !ok { t.Fatal("WritePlugin should implement kernel.Plugin") }
}

func TestMemoryPluginImplementsPlugin(t *testing.T) {
	var p interface{} = &MemoryPlugin{}
	_, ok := p.(kernel.Plugin)
	if !ok { t.Fatal("MemoryPlugin should implement kernel.Plugin") }
}

func TestLegalWritePluginImplementsPlugin(t *testing.T) {
	var p interface{} = &LegalWritePlugin{}
	_, ok := p.(kernel.Plugin)
	if !ok { t.Fatal("LegalWritePlugin should implement kernel.Plugin") }
}
