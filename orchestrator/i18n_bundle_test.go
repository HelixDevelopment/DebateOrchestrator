package orchestrator

import (
	"context"
	"os"
	"strings"
	"testing"
)

// i18n_bundle_test.go — HXC-079 RED-on-broken-artifact + GREEN regression
// guard per §11.4.115 (polarity switch). The defect: a live /debate e2e
// showed the consensus Conclusion/Summary printing the LITERAL i18n
// message-key `debate.orchestrator.consensus_conclusion` instead of
// resolved prose, because the package shipped the template bundle
// (i18n/active.en.yaml) but NO loader — the default translator
// (NoopTranslator) echoed the raw key verbatim.
//
// RED_MODE polarity (§11.4.115):
//   RED_MODE=1 (default) → reproduce-and-assert the defect is PRESENT on
//     the pre-fix artifact: the default-translator consensus Conclusion
//     CONTAINS the literal key `debate.orchestrator.consensus_conclusion`.
//   RED_MODE=0 → standing GREEN regression-guard: the default-translator
//     consensus Conclusion is resolved PROSE and does NOT contain the key.
//
// One source, two roles: the bug-catcher IS the regression guard.

func redMode() bool {
	// §11.4.115 polarity switch. On the FIXED artifact the standing role
	// is the GREEN regression-guard (default), so the suite stays green in
	// CI. Set RED_MODE=1 to reproduce the historical HXC-079 defect on a
	// PRE-FIX artifact (the bug-catcher role) — captured RED evidence is
	// in the HXC-079 fix record.
	return os.Getenv("RED_MODE") == "1"
}

// conductDefaultDebate runs ConductDebate with NO translator wired by the
// consumer — i.e. exactly the out-of-the-box default that the live e2e
// exercised. It resets the package translator to the package default
// first so the test reflects a fresh process.
func conductDefaultDebate(t *testing.T) *ConsensusResponse {
	t.Helper()
	// Reset to the package default translator (what a fresh consumer
	// process has before any SetTranslator call).
	ResetTranslatorToDefault()
	t.Cleanup(func() { SetTranslator(nil) })

	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig())
	if err := o.RegisterProvider("ollama", "llama3", 0.8); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := o.RegisterProvider("openai", "gpt-4", 0.9); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	resp, err := o.ConductDebate(context.Background(), &DebateRequest{
		ID:        "hxc079-1",
		Topic:     "renewable energy",
		MaxRounds: 2,
	})
	if err != nil {
		t.Fatalf("ConductDebate: %v", err)
	}
	if resp.Consensus == nil {
		t.Fatal("ConductDebate returned nil Consensus")
	}
	return resp.Consensus
}

func TestHXC079_ConsensusConclusionRendersProse(t *testing.T) {
	c := conductDefaultDebate(t)
	const rawKey = "debate.orchestrator.consensus_conclusion"

	if redMode() {
		// RED: reproduce the defect on the current artifact. The raw key
		// surfaces in the Conclusion (and Summary mirrors it).
		if !strings.Contains(c.Conclusion, rawKey) {
			t.Fatalf("RED_MODE=1 expected defect PRESENT: Conclusion should contain literal key %q, got %q", rawKey, c.Conclusion)
		}
		if !strings.Contains(c.Summary, rawKey) {
			t.Fatalf("RED_MODE=1 expected defect PRESENT: Summary should contain literal key %q, got %q", rawKey, c.Summary)
		}
		t.Logf("RED reproduced (defect present): Conclusion=%q", c.Conclusion)
		return
	}

	// GREEN regression-guard: the key must be RESOLVED to prose with the
	// real Topic + Rounds substituted, and must NOT contain the raw key.
	if strings.Contains(c.Conclusion, rawKey) {
		t.Fatalf("GREEN guard: Conclusion still contains raw key %q (not resolved): %q", rawKey, c.Conclusion)
	}
	if strings.Contains(c.Summary, rawKey) {
		t.Fatalf("GREEN guard: Summary still contains raw key %q (not resolved): %q", rawKey, c.Summary)
	}
	// Real Topic + Rounds must be present in the prose.
	if !strings.Contains(c.Conclusion, "renewable energy") {
		t.Fatalf("GREEN guard: Conclusion missing real Topic substitution: %q", c.Conclusion)
	}
	if !strings.Contains(c.Conclusion, "2") {
		t.Fatalf("GREEN guard: Conclusion missing real Rounds substitution: %q", c.Conclusion)
	}
	t.Logf("GREEN prose: Conclusion=%q Summary=%q", c.Conclusion, c.Summary)
}

// TestHXC079_DefaultBundleResolvesAllKeys proves the default translator
// resolves EVERY consensus key to prose (no raw key leaks anywhere in the
// ConsensusResponse) and that locale overrides remain possible (a wired
// translator still wins).
func TestHXC079_DefaultBundleResolvesAllKeys(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK HXC-079: GREEN-only assertion; run with RED_MODE=0")
	}
	c := conductDefaultDebate(t)
	keys := []string{
		"debate.orchestrator.consensus_conclusion",
		"debate.orchestrator.consensus_reasoning",
		"debate.orchestrator.consensus_key_topic",
		"debate.orchestrator.consensus_key_rounds",
		"debate.orchestrator.consensus_key_participants",
	}
	blob := c.Conclusion + "\n" + c.Reasoning + "\n" + c.Summary + "\n" + strings.Join(c.KeyPoints, "\n")
	for _, k := range keys {
		if strings.Contains(blob, k) {
			t.Fatalf("raw key %q leaked into ConsensusResponse: %s", k, blob)
		}
	}
	// Wired translator must still win (decoupling: consumer locale override).
	rt := &recordingTranslator{prefix: "SR::"}
	SetTranslator(rt)
	got := tr(msgConsensusConclusion, map[string]any{"Topic": "x", "Rounds": 1})
	if !strings.HasPrefix(got, "SR::") {
		t.Fatalf("wired translator must win over default bundle; got %q", got)
	}
}
