package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type HealthReport struct {
	GitDirty          bool     `json:"git_dirty"`
	DirtyFiles        []string `json:"dirty_files,omitempty"`
	GoBuildOk         bool     `json:"go_build_ok"`
	GoVetOk           bool     `json:"go_vet_ok"`
	RightFlowers      int      `json:"right_flowers_enabled"`
	RightFlowerCtlOk   bool     `json:"rightflower_ctl_ok"`
	ManifestValidateOk bool     `json:"manifest_validate_ok"`
	EvalOk             bool     `json:"eval_ok"`
	HardeningScore     string   `json:"hardening_score"`
	EvidenceTraceCount int      `json:"evidence_trace_count"`
	RightFlowerAuditOk bool     `json:"rightflower_audit_ok"`
	HardeningInvariant bool    `json:"hardening_invariant_ok"`
	Status            string   `json:"status"` // pass / warn / fail
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

type runner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) (string, error)
}

type osRunner struct{}

func (osRunner) Run(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
func (osRunner) Output(name string, args ...string) (string, error) {
	out, err := exec.Command(name, args...).Output()
	return string(out), err
}

func BuildHealthReport(root string, r runner) HealthReport {
	if root == "" {
		root, _ = findProjectRoot()
	}
	rep := HealthReport{Status: "pass"}

	// Git dirty
	out, _ := r.Output("git", "-C", root, "status", "--short")
	rep.GitDirty = strings.TrimSpace(out) != ""
	if rep.GitDirty {
		for _, line := range strings.Split(string(out), "\n") {
			if s := strings.TrimSpace(line); s != "" {
				rep.DirtyFiles = append(rep.DirtyFiles, s)
			}
		}
		rep.Status = "warn"
	}

	// Go build
	rep.GoBuildOk = r.Run("go", "build", "./...") == nil
	if !rep.GoBuildOk {
		rep.Status = "fail"
	}

	// Go vet
	rep.GoVetOk = r.Run("go", "vet", "./...") == nil
	if !rep.GoVetOk {
		rep.Status = "fail"
	}

	// Right flower count (YAML parse, not string contains)
	entries, _ := os.ReadDir(filepath.Join(root, "right_flowers"))
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(root, "right_flowers", e.Name()))
		var m struct {
			Enabled bool `yaml:"enabled"`
		}
		if yaml.Unmarshal(data, &m) == nil && m.Enabled {
			rep.RightFlowers++
		}
	}

	// Hardening invariant
	rep.HardeningInvariant = r.Run("bash", filepath.Join(root, "eval/scripts/check_hardening_invariants.sh")) == nil
	if !rep.HardeningInvariant {
		rep.Status = "fail"
	}

	rep.RightFlowerCtlOk = r.Run("go", "build", "./cmd/rightflowerctl/...") == nil
	if !rep.RightFlowerCtlOk {
		rep.Status = "fail"
	}
	rep.ManifestValidateOk = r.Run("go", "run", "./cmd/rightflowerctl", "validate") == nil
	if !rep.ManifestValidateOk {
		rep.Status = "warn"
	}
	// RightFlower audit
	rep.RightFlowerAuditOk = r.Run("test", "-f", "internal/rightflower/audit.go") == nil
	if !rep.RightFlowerAuditOk {
		rep.Status = "warn"
	}
	// Eval suites
	rep.EvalOk = r.Run("go", "run", "./cmd/core-eval/", "--suite", "smoke") == nil
	if !rep.EvalOk {
		rep.Status = "warn"
	}
	// Hardening score
	out2, _ := r.Output("bash", "./eval/scripts/check_hardening_invariants.sh")
	passCount := 0
	for _, line := range strings.Split(out2, "\n") {
		if strings.Contains(line, "✅") {
			passCount++
		}
	}
	if passCount > 0 {
		rep.HardeningScore = fmt.Sprintf("%d/%d", passCount, passCount+1)
	} else {
		rep.HardeningScore = "0/?"
		rep.Status = "warn"
	}
	return rep
}

func (h HealthReport) String() string {
	s := fmt.Sprintf("Status: %s\n", h.Status)
	s += fmt.Sprintf("Git dirty: %v (%d files)\n", h.GitDirty, len(h.DirtyFiles))
	s += fmt.Sprintf("Go build: %v\n", h.GoBuildOk)
	s += fmt.Sprintf("Go vet: %v\n", h.GoVetOk)
	s += fmt.Sprintf("Right flowers enabled: %d\n", h.RightFlowers)
	s += fmt.Sprintf("Hardening invariants: %v\n", h.HardeningInvariant)
	return s
}

func (h HealthReport) JSON() string {
	b, _ := json.MarshalIndent(h, "", "  ")
	return string(b)
}
