package main

import (
	"testing"

	"beishan/internal/tools"
)

func TestDefaultRoutingRules(t *testing.T) {
	rules := tools.DefaultRoutingRules()
	if len(rules) == 0 {
		t.Fatal("expected default routing rules")
	}
}

func TestEvidenceRouterDesktop(t *testing.T) {
	router := tools.NewEvidenceRouter(tools.DefaultRoutingRules())
	// "桌面上都有什么md文件" → should NOT match desktop_actuator
	// "文件" is a negative keyword for desktop_actuator
	result := router.Route("帮我看一下桌面上都有什么md文件")
	if result != nil && result.Tool == "memory_plugin" && result.MsgType == "desktop_actuator" {
		t.Errorf("desktop files should not route to desktop_actuator, got %s", result.Tool)
	}
}

func TestEvidenceRouterDesktopScreen(t *testing.T) {
	router := tools.NewEvidenceRouter(tools.DefaultRoutingRules())
	// "看桌面" → should route to desktop_actuator
	result := router.Route("看桌面上现在有什么")
	if result == nil || result.Tool != "memory_plugin" || result.MsgType != "desktop_actuator" {
		t.Errorf("看桌面 should route to desktop_actuator, got %v", result)
	}
}

func TestPrerouteEvidenceRouter(t *testing.T) {
	rules := tools.DefaultRoutingRules()
	if len(rules) < 5 {
		t.Fatalf("expected at least 5 rules, got %d", len(rules))
	}
}

func TestJsonEscape(t *testing.T) {
	s := jsonEscape(`hello "world"`)
	if len(s) == 0 {
		t.Fatal("expected non-empty")
	}
}

func TestNewSessionID(t *testing.T) {
	id1 := newSessionID()
	id2 := newSessionID()
	if id1 == id2 {
		t.Fatal("expected unique IDs")
	}
	if len(id1) != 16 {
		t.Fatalf("expected 16 chars, got %d", len(id1))
	}
}

func TestBuildWorkflowSummary(t *testing.T) {
	s := buildWorkflowSummary("/nonexistent")
	_ = s
}
