package main

import (
	"errors"
	"strings"
	"testing"
)

type mockRunner struct {
	buildOk bool
	vetOk   bool
	gitOut  string
	hardOk  bool
}

func (m mockRunner) Run(name string, args ...string) error {
	if name == "go" {
		for _, a := range args {
			if a == "build" && !m.buildOk {
				return errors.New("build fail")
			}
			if a == "vet" && !m.vetOk {
				return errors.New("vet fail")
			}
		}
	}
	if name == "bash" && !m.hardOk {
		return errors.New("hard fail")
	}
	return nil
}

func (m mockRunner) Output(name string, args ...string) (string, error) {
	if name == "git" {
		return m.gitOut, nil
	}
	if name == "bash" && len(args) > 0 && strings.Contains(args[0], "check_hardening") {
		return "  [1] ✅\n  [2] ✅\n", nil
	}
	return "", nil
}

func TestBuildHealthReportClean(t *testing.T) {
	r := mockRunner{buildOk: true, vetOk: true, hardOk: true}
	rep := BuildHealthReport(".", r)
	if rep.GitDirty {
		t.Fatal("expected clean git")
	}
	if rep.Status != "pass" {
		t.Fatalf("status = %s", rep.Status)
	}
}

func TestBuildHealthReportBuildFail(t *testing.T) {
	r := mockRunner{buildOk: false, vetOk: true, hardOk: true}
	rep := BuildHealthReport(".", r)
	if rep.GoBuildOk {
		t.Fatal("expected build fail")
	}
	if rep.Status != "fail" {
		t.Fatalf("status = %s", rep.Status)
	}
}

func TestBuildHealthReportGitDirty(t *testing.T) {
	r := mockRunner{buildOk: true, vetOk: true, hardOk: true, gitOut: " M file.go"}
	rep := BuildHealthReport(".", r)
	if !rep.GitDirty {
		t.Fatal("expected dirty git")
	}
	if len(rep.DirtyFiles) != 1 {
		t.Fatalf("dirty files = %d", len(rep.DirtyFiles))
	}
}

func TestBuildHealthReportWarnOnDirty(t *testing.T) {
	r := mockRunner{buildOk: true, vetOk: true, hardOk: true, gitOut: " M file.go"}
	rep := BuildHealthReport(".", r)
	if rep.Status != "warn" {
		t.Fatalf("dirty -> status=%s, want warn", rep.Status)
	}
}
