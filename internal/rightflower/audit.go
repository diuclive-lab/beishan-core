package rightflower

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type AuditRecord struct {
	Timestamp string `json:"timestamp"`
	Flower    string `json:"flower"`
	Method    string `json:"method"`
	Status    string `json:"status"`
	LatencyMs int64  `json:"latency_ms"`
}

var auditDisabled bool

func init() {
	if os.Getenv("RIGHTFLOWER_AUDIT_DISABLE") == "1" {
		auditDisabled = true
	}
}

func WriteAudit(record AuditRecord) {
	if auditDisabled {
		return
	}
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".hermes", "runtime", "rightflower",
		time.Now().UTC().Format("2006-01-02")+".jsonl")
	os.MkdirAll(filepath.Dir(path), 0755)
	data, _ := json.Marshal(record)
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if f != nil {
		f.Write(data)
		f.Write([]byte("\n"))
		f.Close()
	}
}
