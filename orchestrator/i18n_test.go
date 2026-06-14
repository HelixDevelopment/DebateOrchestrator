package orchestrator

import (
	"context"
	"strings"
	"testing"
)

// i18n_test.go — CONST-046 round-395 paired-mutation coverage for the
// orchestrator package's i18n seam. Per §1.1 every CONST-046 migration
// ships a paired mutation test: a fake Translator proves tr() actually
// routes through the seam (not a no-op echo), and an end-to-end
// ConductDebate run proves a non-English consumer sees a translated
// ConsensusResponse.
//
// Anti-bluff: if any ConsensusResponse string were re-hardcoded back to
// an English fmt.Sprintf (un-doing the CONST-046 migration), the
// end-to-end test below would FAIL because the resolved Conclusion /
// Reasoning / KeyPoints would no longer carry the locale prefix.

// recordingTranslator captures every message ID resolved through it and
// returns a deterministic, locale-tagged rewrite.
type recordingTranslator struct {
	prefix string
	seen   []string
}

func (r *recordingTranslator) T(_ context.Context, id string, _ map[string]any) (string, error) {
	r.seen = append(r.seen, id)
	return r.prefix + id, nil
}

// erroringTranslator always fails; tr() must fall back to the verbatim
// message ID so an outcome string is never silently lost.
type erroringTranslator struct{}

func (erroringTranslator) T(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", context.DeadlineExceeded
}

func TestI18n_NoopTranslatorEchoesID(t *testing.T) {
	// Reconciled per §11.4.120 (HXC-079): the package DEFAULT translator
	// is now the bundle-backed translator (renders prose out of the box),
	// so SetTranslator(nil) no longer echoes the raw key. NoopTranslator's
	// echo contract is unchanged — assert it by installing NoopTranslator
	// explicitly rather than via the package default.
	SetTranslator(NoopTranslator{})
	defer SetTranslator(nil)
	got := tr(msgConsensusConclusion, map[string]any{"Topic": "x", "Rounds": 1})
	if got != msgConsensusConclusion {
		t.Fatalf("NoopTranslator must echo message ID; got %q want %q", got, msgConsensusConclusion)
	}
}

func TestI18n_ErroringTranslatorFallsBackToID(t *testing.T) {
	SetTranslator(erroringTranslator{})
	defer SetTranslator(nil)
	got := tr(msgConsensusReasoning, map[string]any{"Confidence": "0.5", "Calls": 2})
	if got != msgConsensusReasoning {
		t.Fatalf("erroring Translator must fall back to message ID; got %q", got)
	}
}

// Paired-mutation core: installs a real Translator and proves tr()
// routes through it.
func TestI18n_TrRoutesThroughTranslator(t *testing.T) {
	rt := &recordingTranslator{prefix: "XX::"}
	SetTranslator(rt)
	defer SetTranslator(nil)

	got := tr(msgConsensusKeyTopic, map[string]any{"Topic": "alpha"})
	if !strings.HasPrefix(got, "XX::") {
		t.Fatalf("tr() did not route through installed Translator; got %q", got)
	}
	if len(rt.seen) != 1 || rt.seen[0] != msgConsensusKeyTopic {
		t.Fatalf("Translator.T not invoked with expected message ID; seen=%v", rt.seen)
	}
}

// End-to-end: a swapped-locale Translator rewrites the
// ConsensusResponse so an operator reading ConductDebate's result sees
// fully localised text — the CONST-046 guarantee.
func TestI18n_ConsensusResponseIsLocalised(t *testing.T) {
	rt := &recordingTranslator{prefix: "LOCALE::"}
	SetTranslator(rt)
	defer SetTranslator(nil)

	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig())
	if err := o.RegisterProvider("ollama", "llama3", 0.8); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := o.RegisterProvider("openai", "gpt-4", 0.9); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	resp, err := o.ConductDebate(context.Background(), &DebateRequest{
		ID:        "loc-1",
		Topic:     "localisation",
		MaxRounds: 1,
	})
	if err != nil {
		t.Fatalf("ConductDebate: %v", err)
	}
	if resp.Consensus == nil {
		t.Fatal("ConductDebate returned nil Consensus")
	}
	c := resp.Consensus
	if !strings.HasPrefix(c.Conclusion, "LOCALE::") {
		t.Fatalf("Conclusion not localised: %q", c.Conclusion)
	}
	if !strings.HasPrefix(c.Reasoning, "LOCALE::") {
		t.Fatalf("Reasoning not localised: %q", c.Reasoning)
	}
	if !strings.HasPrefix(c.Summary, "LOCALE::") {
		t.Fatalf("Summary not localised: %q", c.Summary)
	}
	for i, kp := range c.KeyPoints {
		if !strings.HasPrefix(kp, "LOCALE::") {
			t.Fatalf("KeyPoint[%d] not localised: %q", i, kp)
		}
	}
}
