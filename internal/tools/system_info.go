package tools

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

/* ─── SystemInfo 系统环境快照 ────────────────── */

type SystemInfo struct {
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	CPUModel        string `json:"cpu_model,omitempty"`
	CPUCores        int    `json:"cpu_cores"`
	MemoryGB        int    `json:"memory_gb"`
	HasMetalSupport bool   `json:"has_metal_support,omitempty"`
	Summary         string `json:"summary"` // 一句话硬件摘要
}

// GetSystemInfo 采集当前系统环境信息。
// 确定性代码，不调 LLM。
func GetSystemInfo() *ToolResult {
	info := collectSystemInfo()
	b, _ := json.MarshalIndent(info, "", "  ")
	return successResult(string(b))
}

// collectSystemInfo 收集系统信息。
func collectSystemInfo() SystemInfo {
	info := SystemInfo{
		OS:    runtime.GOOS,
		Arch:  runtime.GOARCH,
		CPUCores: runtime.NumCPU(),
	}

	// CPU 型号（macOS）
	if cpu := execSysctl("machdep.cpu.brand_string"); cpu != "" {
		info.CPUModel = strings.TrimSpace(cpu)
	}

	// 内存（macOS：以 GB 为单位）
	if mem := execSysctl("hw.memsize"); mem != "" {
		var bytes int64
		if _, err := fmt.Sscanf(mem, "%d", &bytes); err == nil {
			info.MemoryGB = int(bytes / 1024 / 1024 / 1024)
		}
	}

	// Metal 支持（macOS）
	if out, err := execCmd("metal", "version"); err == nil && len(out) > 0 {
		info.HasMetalSupport = true
	}

	// 硬件摘要
	info.Summary = fmt.Sprintf("%s/%s %s %d核 %dGB",
		info.OS, info.Arch, info.CPUModel, info.CPUCores, info.MemoryGB)

	return info
}

// HardwareSummary 返回一行硬件摘要（用于知识条目中附带）。
func HardwareSummary() string {
	info := collectSystemInfo()
	return info.Summary
}

func execSysctl(key string) string {
	out, err := execCmd("sysctl", "-n", key)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func execCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func registerSystemInfoTools() {
	Register("system_info", "获取当前系统硬件信息（CPU/内存/GPU/OS），用于知识条目的环境感知。",
		map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
		func(args map[string]interface{}) *ToolResult {
			return GetSystemInfo()
		},
	)
}
