package observatory

import (
	"fmt"
	"strings"
)

// ── Severity ──────────────────────────────────────────────────────────────

const (
	SeverityNone    = "none"
	SeverityInfo    = "info"
	SeverityWarning = "warning"
	SeverityError   = "error"
)

var severityRank = map[string]int{SeverityNone: 0, SeverityInfo: 1, SeverityWarning: 2, SeverityError: 3}

// ── Failure Flags ─────────────────────────────────────────────────────────

const (
	// Error-level
	FlagSidecarError         = "sidecar_error"
	FlagEvidencePersistFailed = "evidence_persist_failed"

	// Warning-level
	FlagCitationUnmatched     = "citation_unmatched"
	FlagCitationMatchLow      = "citation_match_low"
	FlagEvidenceMissing       = "evidence_missing"
	FlagTrajectoryOnly        = "trajectory_only"
	FlagDoomLoopDetected      = "doom_loop_detected"
	FlagToolIdenticalRepetition = "tool_identical_repetition"
	FlagToolSequenceLoop      = "tool_sequence_loop"
	FlagRightFlowerFailed     = "rightflower_failed"

	// Info-level
	FlagPossiblePollingLoop  = "possible_polling_loop"
	FlagNoCitationObserved   = "no_citation_observed"
)

// ── Taxonomy ──────────────────────────────────────────────────────────────

// FailureTaxonomy classifies system observations into structured failures.
// Pure function pattern from 66's SidecarFailureTaxonomy.
type FailureTaxonomy struct {
	Flags    []string          `json:"flags"`
	Severity string            `json:"severity"`
	Reasons  []string          `json:"reasons,omitempty"`
	Metrics  map[string]float64 `json:"metrics,omitempty"`
}

// Classify converts metrics into a structured failure taxonomy.
func Classify(metrics map[string]float64) FailureTaxonomy {
	if metrics == nil {
		return FailureTaxonomy{Severity: SeverityNone}
	}
	var flags, reasons, sevs []string

	errFn := func(flag, reason string) { flags = append(flags, flag); reasons = append(reasons, reason); sevs = append(sevs, SeverityError) }
	warnFn := func(flag, reason string) { flags = append(flags, flag); reasons = append(reasons, reason); sevs = append(sevs, SeverityWarning) }
	infoFn := func(flag, reason string) { flags = append(flags, flag); reasons = append(reasons, reason); sevs = append(sevs, SeverityInfo) }

	// Error: infrastructure
	if sf := safe(metrics, "sidecar_error_count"); sf > 0 {
		errFn(FlagSidecarError, "sidecar encountered internal errors")
	}
	if sf := safe(metrics, "evidence_persist_error"); sf > 0 {
		errFn(FlagEvidencePersistFailed, "evidence persistence failed")
	}
	if sf := safe(metrics, "rightflower_error_count"); sf > 0 {
		errFn(FlagRightFlowerFailed, "right flower call failed")
	}

	// Warning: quality
	citeURL := safe(metrics, "citation_url_count")
	citeUnmatched := safe(metrics, "citation_unmatched_count")
	citeMatchRate := safe(metrics, "citation_match_rate")
	if citeURL > 0 && citeUnmatched > 0 {
		warnFn(FlagCitationUnmatched, "unmatched citation URLs")
	}
	if citeURL > 0 && citeMatchRate > 0 && citeMatchRate < 0.5 {
		warnFn(FlagCitationMatchLow, "citation match rate below 0.5")
	}
	if safe(metrics, "loop_detected") > 0 {
		warnFn(FlagDoomLoopDetected, "repetitive tool call pattern")
	}
	if safe(metrics, "tool_identical_repetition") > 0 {
		warnFn(FlagToolIdenticalRepetition, "identical tool calls")
	}
	if safe(metrics, "tool_sequence_loop") > 0 {
		warnFn(FlagToolSequenceLoop, "cyclic tool call pattern")
	}

	// Info: observations
	if safe(metrics, "loop_possible_polling") > 0 && safe(metrics, "loop_detected") == 0 {
		infoFn(FlagPossiblePollingLoop, "possible polling pattern")
	}

	return FailureTaxonomy{
		Flags: flags, Severity: highestSeverity(sevs),
		Reasons: reasons, Metrics: metrics,
	}
}

func safe(m map[string]float64, k string) float64 { if m == nil { return 0 }; return m[k] }

func highestSeverity(sevs []string) string {
	best, bestRank := SeverityNone, 0
	for _, s := range sevs {
		if r := severityRank[s]; r > bestRank { best, bestRank = s, r }
	}
	return best
}

func (ft FailureTaxonomy) HasFlag(flag string) bool {
	for _, f := range ft.Flags { if f == flag { return true } }
	return false
}

func (ft FailureTaxonomy) Markdown() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Severity: **%s**\n\n", ft.Severity))
	for i, flag := range ft.Flags {
		reason := ""
		if i < len(ft.Reasons) { reason = ft.Reasons[i] }
		b.WriteString(fmt.Sprintf("- %s: %s\n", flag, reason))
	}
	return b.String()
}
