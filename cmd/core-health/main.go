// core-health — lightweight project health check.
// Usage: go run ./cmd/core-health [--json]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type HealthReport struct {
	GitDirty          bool     `json:"git_dirty"`
	DirtyFiles        []string `json:"dirty_files,omitempty"`
	GoBuildOk         bool     `json:"go_build_ok"`
	GoVetOk           bool     `json:"go_vet_ok"`
	RightFlowers      int      `json:"right_flowers_enabled"`
	HardeningInvariant bool    `json:"hardening_invariant_ok"`
}

func main() {
	jsonFlag := flag.Bool("json", false, "output JSON")
	flag.Parse()

	r := HealthReport{}

	// Git dirty
	gitOut, _ := exec.Command("git", "status", "--short").Output()
	r.GitDirty = len(strings.TrimSpace(string(gitOut))) > 0
	if r.GitDirty {
		for _, line := range strings.Split(string(gitOut), "\n") {
			if strings.TrimSpace(line) != "" {
				r.DirtyFiles = append(r.DirtyFiles, strings.TrimSpace(line))
			}
		}
	}

	// Go build
	r.GoBuildOk = exec.Command("go", "build", "./...").Run() == nil

	// Go vet
	r.GoVetOk = exec.Command("go", "vet", "./...").Run() == nil

	// Count right flowers
	entries, _ := os.ReadDir("./right_flowers")
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".yaml") {
			data, _ := os.ReadFile("./right_flowers/" + e.Name())
			if strings.Contains(string(data), "enabled: true") {
				r.RightFlowers++
			}
		}
	}

	// Hardening invariant
	r.HardeningInvariant = exec.Command("bash", "eval/scripts/check_hardening_invariants.sh").Run() == nil

	if *jsonFlag {
		json.NewEncoder(os.Stdout).Encode(r)
		return
	}
	fmt.Printf("=== Core Health ===\n")
	fmt.Printf("Git dirty: %v\n", r.GitDirty)
	fmt.Printf("Go build:  %v\n", r.GoBuildOk)
	fmt.Printf("Go vet:    %v\n", r.GoVetOk)
	fmt.Printf("Right flowers enabled: %d\n", r.RightFlowers)
	fmt.Printf("Hardening invariants: %v\n", r.HardeningInvariant)
}
