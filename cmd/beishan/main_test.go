package main

import (
	"strings"
	"testing"

	"beishan/internal/observatory"
)

// TestHealthResponseJSON_OKThenDegraded 证明 /health 的降级上报：无降级→"ok"，
// 登记降级后→"degraded" 且 body 含降级详情；且始终保留 version 字段
// （deploy.sh / daemon_drift.sh 依赖它解析现网版本）。
//
// 依赖「测试进程起始无降级」：cmd/beishan 的测试二进制独立运行，main() 不被调用，
// 只有本测试触碰 observatory 降级态，故起始为空，无需跨包重置。
func TestHealthResponseJSON_OKThenDegraded(t *testing.T) {
	if s := string(healthResponseJSON()); !strings.Contains(s, `"status":"ok"`) {
		t.Fatalf("无降级应报 ok，实得 %s", s)
	}

	observatory.RecordDegradation("test:health", "forced for test")

	s := string(healthResponseJSON())
	if !strings.Contains(s, `"status":"degraded"`) {
		t.Fatalf("有降级应报 degraded，实得 %s", s)
	}
	if !strings.Contains(s, "test:health") {
		t.Fatalf("degraded body 应含降级组件名，实得 %s", s)
	}
	if !strings.Contains(s, `"version"`) {
		t.Fatalf("body 应始终保留 version 字段（deploy.sh 依赖），实得 %s", s)
	}
}
