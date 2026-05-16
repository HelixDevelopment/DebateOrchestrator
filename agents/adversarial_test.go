package agents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type recordingLLM struct {
	responses []string
	calls     int
}

func (r *recordingLLM) Complete(_ context.Context, _ string) (string, error) {
	if r.calls < len(r.responses) {
		out := r.responses[r.calls]
		r.calls++
		return out, nil
	}
	r.calls++
	return "", errors.New("no more canned responses")
}

func TestDefaultAdversarialConfigSane(t *testing.T) {
	cfg := DefaultAdversarialConfig()
	if cfg.MaxRounds <= 0 || cfg.MinVulnerabilities <= 0 || cfg.Timeout <= 0 {
		t.Fatalf("DefaultAdversarialConfig: implausible defaults %+v", cfg)
	}
}

func TestAdversarialProtocolFallbackWithoutLLM(t *testing.T) {
	ap := NewAdversarialProtocol(DefaultAdversarialConfig(), nil)
	res, err := ap.Execute(context.Background(),
		"func q(db *sql.DB, id string) error { q := fmt.Sprintf(\"SELECT * FROM x WHERE id='%s'\", id); _, e := db.Query(q); return e }",
		"go")
	if err != nil {
		t.Fatalf("Execute: unexpected error %v", err)
	}
	if res == nil || res.Rounds == 0 {
		t.Fatalf("Execute: expected at least one round, got %+v", res)
	}
	if len(res.AttackReports) == 0 || len(res.AttackReports[0].Vulnerabilities) == 0 {
		t.Fatalf("Fallback attack: expected at least one vulnerability, got %+v", res.AttackReports)
	}
	if res.AttackReports[0].OverallRisk <= 0 {
		t.Fatalf("Fallback attack: expected positive risk, got %v", res.AttackReports[0].OverallRisk)
	}
}

func TestAdversarialProtocolWithLLM_ParsesCanonicalFormat(t *testing.T) {
	llm := &recordingLLM{responses: []string{
		// attack
		"VULNERABILITIES\n" +
			"ID: VULN-1\n" +
			"Category: injection\n" +
			"Severity: critical\n" +
			"Description: SQL injection\n" +
			"---\n" +
			"EDGE_CASES\n---\n" +
			"STRESS_SCENARIOS\n---\n" +
			"OVERALL_RISK: 0.8\n",
		// defense
		"PATCHED_VULNERABILITIES: VULN-1\n" +
			"PATCHES\n" +
			"VULN-1: Use parameterized queries\n" +
			"---\n" +
			"REMAINING_RISKS: NONE\n" +
			"CONFIDENCE: 0.9\n" +
			"PATCHED_CODE\n```go\nfunc q() {}\n```\n",
	}}
	cfg := DefaultAdversarialConfig()
	cfg.MaxRounds = 1
	ap := NewAdversarialProtocol(cfg, llm)
	res, err := ap.Execute(context.Background(), "code", "go")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(res.AttackReports) != 1 || len(res.AttackReports[0].Vulnerabilities) != 1 {
		t.Fatalf("Expected 1 attack with 1 vuln, got %+v", res.AttackReports)
	}
	if res.AttackReports[0].Vulnerabilities[0].ID != "VULN-1" {
		t.Fatalf("Vulnerability ID = %q, want VULN-1", res.AttackReports[0].Vulnerabilities[0].ID)
	}
	if len(res.DefenseReports) != 1 || res.DefenseReports[0].ConfidenceInDefense < 0.85 {
		t.Fatalf("Expected high-confidence defense, got %+v", res.DefenseReports)
	}
	if !strings.Contains(res.DefenseReports[0].PatchedCode, "func q()") {
		t.Fatalf("PatchedCode missing; got %q", res.DefenseReports[0].PatchedCode)
	}
}

func TestAdversarialProtocolCancellation(t *testing.T) {
	ap := NewAdversarialProtocol(DefaultAdversarialConfig(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ap.Execute(ctx, "x", "go")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestAdversarialProtocolTimeoutHonoured(t *testing.T) {
	cfg := DefaultAdversarialConfig()
	cfg.Timeout = 10 * time.Millisecond
	ap := NewAdversarialProtocol(cfg, nil)
	start := time.Now()
	_, err := ap.Execute(context.Background(), "x", "go")
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected nil or DeadlineExceeded, got %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("Execute did not honour timeout; ran for %v", time.Since(start))
	}
}
