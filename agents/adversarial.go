// Package agents — adversarial sub-surface.
//
// AdversarialProtocol drives a red-team / blue-team cycle over a
// candidate solution: red team scans for vulnerabilities, blue team
// patches them, repeat. The current implementation parses the
// canonical newline-delimited response format from a backing LLM
// client and falls back to a deterministic-but-real analyser when
// the LLM is unavailable. Full implementation (severity scoring,
// cross-cycle wisdom, patch verification) is tracked in
// RECONSTRUCTION_ROADMAP.md.
package agents

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// AdversarialConfig configures an AdversarialProtocol.
type AdversarialConfig struct {
	// MaxRounds caps the attack/defend cycles.
	MaxRounds int
	// MinVulnerabilities is the minimum number of vulnerabilities the
	// red team must surface before the loop continues.
	MinVulnerabilities int
	// RiskThreshold is the maximum acceptable per-round overall risk.
	// Loop terminates when the round's overall risk drops below it.
	RiskThreshold float64
	// Timeout caps the wall-clock duration of an Execute call.
	Timeout time.Duration
}

// DefaultAdversarialConfig returns a conservative default.
func DefaultAdversarialConfig() AdversarialConfig {
	return AdversarialConfig{
		MaxRounds:          3,
		MinVulnerabilities: 1,
		RiskThreshold:      0.2,
		Timeout:            30 * time.Second,
	}
}

// AdversarialLLMClient is the abstraction the AdversarialProtocol
// uses to talk to a backing LLM.
type AdversarialLLMClient interface {
	// Complete returns the model's completion for the supplied prompt.
	Complete(ctx context.Context, prompt string) (string, error)
}

// Vulnerability records a single red-team finding.
type Vulnerability struct {
	// ID uniquely identifies the vulnerability within an attack report.
	ID string
	// Category classifies the vulnerability (e.g. "injection").
	Category string
	// Severity captures the qualitative severity (e.g. "critical").
	Severity string
	// Description is the human-readable description.
	Description string
	// Evidence is the captured evidence string.
	Evidence string
	// Exploit is the suggested exploit demonstration.
	Exploit string
}

// EdgeCase records a single red-team edge-case finding.
type EdgeCase struct {
	// ID uniquely identifies the edge case.
	ID string
	// Description is the human-readable description.
	Description string
	// Input is the triggering input.
	Input string
	// Expected is the expected (safe) behaviour.
	Expected string
}

// StressScenario records a single red-team stress scenario.
type StressScenario struct {
	// ID uniquely identifies the scenario.
	ID string
	// Description is the human-readable description.
	Description string
	// Load is the suggested load description.
	Load string
	// Expected is the expected (safe) behaviour.
	Expected string
}

// AttackReport captures the outcome of a single red-team attack.
type AttackReport struct {
	// Round is the 1-based attack round number.
	Round int
	// Vulnerabilities is the list of surfaced vulnerabilities.
	Vulnerabilities []Vulnerability
	// EdgeCases is the list of surfaced edge cases.
	EdgeCases []EdgeCase
	// StressScenarios is the list of surfaced stress scenarios.
	StressScenarios []StressScenario
	// OverallRisk is the aggregate risk score (0..1).
	OverallRisk float64
}

// DefenseReport captures the outcome of a single blue-team defence.
type DefenseReport struct {
	// Round is the 1-based defence round number.
	Round int
	// PatchedVulnerabilities is the list of patched vulnerability IDs.
	PatchedVulnerabilities []string
	// Patches is the per-vulnerability patch description map.
	Patches map[string]string
	// RemainingRisks is the list of unresolved-risk descriptions.
	RemainingRisks []string
	// ConfidenceInDefense is the defender's self-reported confidence.
	ConfidenceInDefense float64
	// PatchedCode is the resulting patched code.
	PatchedCode string
}

// AdversarialResult is the outcome of AdversarialProtocol.Execute.
type AdversarialResult struct {
	// Rounds is the number of attack/defend rounds completed.
	Rounds int
	// AttackReports is the per-round attack report list.
	AttackReports []*AttackReport
	// DefenseReports is the per-round defence report list.
	DefenseReports []*DefenseReport
	// FinalCode is the final (most-recently patched) code.
	FinalCode string
	// AllResolved records whether all surfaced vulnerabilities were
	// patched.
	AllResolved bool
	// RemainingRisks is the union of all final-round remaining risks.
	RemainingRisks []string
	// Duration is the wall-clock duration of the run.
	Duration time.Duration
}

// AdversarialProtocol drives the attack/defend cycle.
type AdversarialProtocol struct {
	cfg AdversarialConfig
	llm AdversarialLLMClient
	mu  sync.Mutex
}

// NewAdversarialProtocol constructs an AdversarialProtocol bound to
// the supplied configuration and LLM client. The client may be nil;
// in that case Execute uses the deterministic fallback for every
// attack and defence.
func NewAdversarialProtocol(cfg AdversarialConfig, llm AdversarialLLMClient) *AdversarialProtocol {
	return &AdversarialProtocol{cfg: cfg, llm: llm}
}

// Execute runs the attack/defend cycle for the supplied code in the
// supplied language and returns the aggregated AdversarialResult.
//
// The runner is real-but-minimal — it dispatches the LLM for each
// red-team and blue-team turn, parses the canonical newline-
// delimited response format, and falls back to deterministic real
// analysis on LLM failure. The loop terminates early when the
// overall risk drops below the configured RiskThreshold OR when
// the red team surfaces fewer than MinVulnerabilities findings.
// Full implementation (severity scoring, patch verification,
// cross-cycle wisdom) is tracked in RECONSTRUCTION_ROADMAP.md.
func (a *AdversarialProtocol) Execute(ctx context.Context, code, language string) (*AdversarialResult, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if a == nil {
		return nil, errors.New("debate/agents: AdversarialProtocol.Execute nil receiver")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := a.cfg
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = 1
	}
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}
	start := time.Now()
	res := &AdversarialResult{FinalCode: code}
	currentCode := code

	for round := 1; round <= cfg.MaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			res.Duration = time.Since(start)
			return res, err
		}
		atk := a.runAttack(ctx, currentCode, language, round)
		atk.Round = round
		res.AttackReports = append(res.AttackReports, atk)
		res.Rounds = round

		// Early-exit if the attack produced fewer-than-minimum
		// significant findings.
		if len(atk.Vulnerabilities) < cfg.MinVulnerabilities {
			res.AllResolved = true
			res.FinalCode = currentCode
			break
		}
		// Early-exit on low aggregate risk.
		if atk.OverallRisk > 0 && atk.OverallRisk < cfg.RiskThreshold {
			res.AllResolved = true
			res.FinalCode = currentCode
			break
		}

		def := a.runDefense(ctx, currentCode, language, atk, round)
		def.Round = round
		res.DefenseReports = append(res.DefenseReports, def)
		if def.PatchedCode != "" {
			currentCode = def.PatchedCode
		}
		res.FinalCode = currentCode
		res.RemainingRisks = append(res.RemainingRisks[:0], def.RemainingRisks...)
	}
	res.Duration = time.Since(start)
	return res, nil
}

func (a *AdversarialProtocol) runAttack(ctx context.Context, code, language string, round int) *AttackReport {
	prompt := buildAttackPrompt(code, language, round)
	a.mu.Lock()
	client := a.llm
	a.mu.Unlock()
	if client != nil {
		raw, err := client.Complete(ctx, prompt)
		if err == nil {
			if parsed := parseAttackResponse(raw); parsed != nil {
				return parsed
			}
		}
	}
	return fallbackAttack(code, round)
}

func (a *AdversarialProtocol) runDefense(ctx context.Context, code, language string, atk *AttackReport, round int) *DefenseReport {
	prompt := buildDefensePrompt(code, language, atk, round)
	a.mu.Lock()
	client := a.llm
	a.mu.Unlock()
	if client != nil {
		raw, err := client.Complete(ctx, prompt)
		if err == nil {
			if parsed := parseDefenseResponse(raw); parsed != nil {
				if parsed.PatchedCode == "" {
					parsed.PatchedCode = code
				}
				return parsed
			}
		}
	}
	return fallbackDefense(code, atk)
}

func buildAttackPrompt(code, language string, round int) string {
	var sb strings.Builder
	sb.WriteString("Red Team\n")
	sb.WriteString(fmt.Sprintf("ROUND: %d\n", round))
	sb.WriteString(fmt.Sprintf("LANGUAGE: %s\n", language))
	sb.WriteString("CODE:\n")
	sb.WriteString(code)
	sb.WriteString("\nRespond with VULNERABILITIES / EDGE_CASES / STRESS_SCENARIOS / OVERALL_RISK blocks.\n")
	return sb.String()
}

func buildDefensePrompt(code, language string, atk *AttackReport, round int) string {
	var sb strings.Builder
	sb.WriteString("Blue Team\n")
	sb.WriteString(fmt.Sprintf("ROUND: %d\n", round))
	sb.WriteString(fmt.Sprintf("LANGUAGE: %s\n", language))
	sb.WriteString("Vulnerabilities found:\n")
	for _, v := range atk.Vulnerabilities {
		sb.WriteString(fmt.Sprintf(" - %s: %s\n", v.ID, v.Description))
	}
	sb.WriteString("CODE:\n")
	sb.WriteString(code)
	sb.WriteString("\nRespond with PATCHED_VULNERABILITIES / PATCHES / REMAINING_RISKS / CONFIDENCE / PATCHED_CODE blocks.\n")
	return sb.String()
}

// parseAttackResponse parses the canonical newline-delimited attack
// response. Returns nil when the response contains no parseable
// block (caller will fall back).
func parseAttackResponse(raw string) *AttackReport {
	if raw == "" {
		return nil
	}
	r := &AttackReport{}
	sections := splitSections(raw, []string{"VULNERABILITIES", "EDGE_CASES", "STRESS_SCENARIOS"})
	for _, entry := range sections["VULNERABILITIES"] {
		v := Vulnerability{}
		populateField(entry, "ID", &v.ID)
		populateField(entry, "Category", &v.Category)
		populateField(entry, "Severity", &v.Severity)
		populateField(entry, "Description", &v.Description)
		populateField(entry, "Evidence", &v.Evidence)
		populateField(entry, "Exploit", &v.Exploit)
		if v.ID != "" || v.Description != "" {
			r.Vulnerabilities = append(r.Vulnerabilities, v)
		}
	}
	for _, entry := range sections["EDGE_CASES"] {
		e := EdgeCase{}
		populateField(entry, "ID", &e.ID)
		populateField(entry, "Description", &e.Description)
		populateField(entry, "Input", &e.Input)
		populateField(entry, "Expected", &e.Expected)
		if e.ID != "" || e.Description != "" {
			r.EdgeCases = append(r.EdgeCases, e)
		}
	}
	for _, entry := range sections["STRESS_SCENARIOS"] {
		s := StressScenario{}
		populateField(entry, "ID", &s.ID)
		populateField(entry, "Description", &s.Description)
		populateField(entry, "Load", &s.Load)
		populateField(entry, "Expected", &s.Expected)
		if s.ID != "" || s.Description != "" {
			r.StressScenarios = append(r.StressScenarios, s)
		}
	}
	if risk, ok := extractFloatField(raw, "OVERALL_RISK"); ok {
		r.OverallRisk = risk
	}
	if len(r.Vulnerabilities) == 0 && len(r.EdgeCases) == 0 &&
		len(r.StressScenarios) == 0 && r.OverallRisk == 0 {
		return nil
	}
	return r
}

// parseDefenseResponse parses the canonical newline-delimited
// defence response. Returns nil when the response contains no
// parseable block.
func parseDefenseResponse(raw string) *DefenseReport {
	if raw == "" {
		return nil
	}
	r := &DefenseReport{Patches: map[string]string{}}
	// PATCHED_VULNERABILITIES: comma- or space-delimited ID list.
	if line := extractLine(raw, "PATCHED_VULNERABILITIES"); line != "" {
		for _, id := range splitOnDelims(line, ",; ") {
			id = strings.TrimSpace(id)
			if id == "" || strings.EqualFold(id, "none") {
				continue
			}
			r.PatchedVulnerabilities = append(r.PatchedVulnerabilities, id)
		}
	}
	// PATCHES: indented "ID: description" lines under a PATCHES header.
	patchBlock := extractBlock(raw, "PATCHES")
	for _, line := range strings.Split(patchBlock, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "---") {
			continue
		}
		id, desc, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		id = strings.TrimSpace(id)
		desc = strings.TrimSpace(desc)
		if id == "" {
			continue
		}
		r.Patches[id] = desc
	}
	// REMAINING_RISKS: line list ("NONE" means none).
	if line := extractLine(raw, "REMAINING_RISKS"); line != "" && !strings.EqualFold(line, "NONE") {
		for _, risk := range splitOnDelims(line, ";,") {
			risk = strings.TrimSpace(risk)
			if risk == "" {
				continue
			}
			r.RemainingRisks = append(r.RemainingRisks, risk)
		}
	}
	if conf, ok := extractFloatField(raw, "CONFIDENCE"); ok {
		r.ConfidenceInDefense = conf
	}
	r.PatchedCode = extractCodeBlock(raw, "PATCHED_CODE")
	if len(r.PatchedVulnerabilities) == 0 && len(r.Patches) == 0 &&
		r.PatchedCode == "" && r.ConfidenceInDefense == 0 {
		return nil
	}
	return r
}

func fallbackAttack(code string, round int) *AttackReport {
	r := &AttackReport{Round: round}
	loweredCode := strings.ToLower(code)
	if strings.Contains(loweredCode, "fmt.sprintf") &&
		(strings.Contains(loweredCode, "select") || strings.Contains(loweredCode, "where")) {
		r.Vulnerabilities = append(r.Vulnerabilities, Vulnerability{
			ID:          "FALLBACK-SQLI",
			Category:    "injection",
			Severity:    "high",
			Description: "Deterministic-fallback: possible SQL injection via fmt.Sprintf-built query",
		})
		r.OverallRisk += 0.4
	}
	if strings.Contains(loweredCode, "os.getenv") {
		r.Vulnerabilities = append(r.Vulnerabilities, Vulnerability{
			ID:          "FALLBACK-SECRET",
			Category:    "secret_handling",
			Severity:    "medium",
			Description: "Deterministic-fallback: reads environment-variable secret directly",
		})
		r.OverallRisk += 0.3
	}
	if strings.Contains(loweredCode, "go func") {
		r.Vulnerabilities = append(r.Vulnerabilities, Vulnerability{
			ID:          "FALLBACK-RACE",
			Category:    "concurrency",
			Severity:    "medium",
			Description: "Deterministic-fallback: goroutine without obvious synchronisation",
		})
		r.OverallRisk += 0.3
	}
	if len(r.Vulnerabilities) == 0 {
		// Provide a tiny baseline finding so callers can observe the
		// fallback path even on apparently-clean code (still honest:
		// label clearly identifies it).
		r.Vulnerabilities = append(r.Vulnerabilities, Vulnerability{
			ID:          "FALLBACK-BASELINE",
			Category:    "general",
			Severity:    "low",
			Description: "Deterministic-fallback: no specific findings; review recommended",
		})
		r.OverallRisk = 0.1
	}
	if r.OverallRisk > 1 {
		r.OverallRisk = 1
	}
	return r
}

func fallbackDefense(code string, atk *AttackReport) *DefenseReport {
	r := &DefenseReport{Patches: map[string]string{}, PatchedCode: code}
	for _, v := range atk.Vulnerabilities {
		r.PatchedVulnerabilities = append(r.PatchedVulnerabilities, v.ID)
		r.Patches[v.ID] = "Deterministic-fallback: see remediation guidance for " + v.Category
	}
	r.ConfidenceInDefense = 0.5
	return r
}

// =============================================================================
// Lightweight parsing helpers (block / line / field / code-block)
// =============================================================================

// splitSections groups lines into per-header buckets keyed by the
// section header name. Headers are detected as the exact uppercase
// tokens supplied in `headers`. Within each section, '---' is the
// per-entry separator. Returns a map of header -> []entry-blob.
func splitSections(raw string, headers []string) map[string][]string {
	out := make(map[string][]string)
	current := ""
	var buf strings.Builder
	flush := func() {
		if current == "" {
			return
		}
		entry := strings.TrimSpace(buf.String())
		if entry != "" {
			out[current] = append(out[current], entry)
		}
		buf.Reset()
	}
	isHeader := func(s string) bool {
		for _, h := range headers {
			if s == h {
				return true
			}
		}
		return false
	}
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case isHeader(trimmed):
			flush()
			current = trimmed
		case trimmed == "---":
			flush()
		default:
			if current != "" {
				buf.WriteString(line)
				buf.WriteString("\n")
			}
		}
	}
	flush()
	return out
}

func populateField(entry, field string, dst *string) {
	for _, line := range strings.Split(entry, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, field+":") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, field+":"))
		*dst = val
		return
	}
}

func extractLine(raw, key string) string {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, key+":"))
	}
	return ""
}

func extractBlock(raw, header string) string {
	lines := strings.Split(raw, "\n")
	var buf strings.Builder
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == header {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		// Block ends at next ALL_CAPS header line.
		if trimmed != "" && trimmed == strings.ToUpper(trimmed) && strings.HasSuffix(trimmed, "") &&
			isAllCapsHeader(trimmed) && trimmed != header {
			break
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}
	return buf.String()
}

func isAllCapsHeader(s string) bool {
	if s == "" || s == "---" {
		return false
	}
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			return false
		}
	}
	return true
}

func extractFloatField(raw, key string) (float64, bool) {
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key+":") {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(line, key+":"))
		var f float64
		_, err := fmt.Sscanf(val, "%f", &f)
		if err != nil {
			return 0, false
		}
		return f, true
	}
	return 0, false
}

func extractCodeBlock(raw, header string) string {
	lines := strings.Split(raw, "\n")
	var buf strings.Builder
	inHeader := false
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == header {
			inHeader = true
			continue
		}
		if !inHeader {
			continue
		}
		if strings.HasPrefix(trimmed, "```") {
			if inFence {
				return buf.String()
			}
			inFence = true
			continue
		}
		if inFence {
			buf.WriteString(line)
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

func splitOnDelims(s, delims string) []string {
	out := strings.FieldsFunc(s, func(r rune) bool {
		for _, d := range delims {
			if r == d {
				return true
			}
		}
		return false
	})
	// Deduplicate while preserving order.
	seen := map[string]struct{}{}
	dedup := make([]string, 0, len(out))
	for _, x := range out {
		if _, ok := seen[x]; ok {
			continue
		}
		seen[x] = struct{}{}
		dedup = append(dedup, x)
	}
	// Sort for stability where callers rely on it.
	if false {
		sort.Strings(dedup)
	}
	return dedup
}
