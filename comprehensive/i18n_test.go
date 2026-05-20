package comprehensive

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// i18n_test.go — CONST-046 round-395 paired-mutation coverage for the
// comprehensive package's i18n seam. Per §1.1 every CONST-046 migration
// ships a paired mutation test: a fake Translator proves tr() actually
// routes through the seam (not a no-op echo), and an end-to-end
// StreamDebate run proves a non-English consumer sees translated
// StreamEvent.Content text.
//
// Anti-bluff: if any StreamEvent.Content were re-hardcoded back to an
// English fmt.Sprintf (un-doing the CONST-046 migration), the
// end-to-end test below would FAIL because the resolved Content would
// no longer carry the installed translator's locale prefix.

// recordingTranslator captures every message ID resolved through it and
// returns a deterministic, locale-tagged rewrite so tests can assert
// the seam is actually exercised.
type recordingTranslator struct {
	prefix string
	seen   []string
}

func (r *recordingTranslator) T(_ context.Context, id string, _ map[string]any) (string, error) {
	r.seen = append(r.seen, id)
	return r.prefix + id, nil
}

// erroringTranslator always fails; tr() must fall back to the verbatim
// message ID so a progress event is never silently lost.
type erroringTranslator struct{}

func (erroringTranslator) T(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", context.DeadlineExceeded
}

func TestI18n_NoopTranslatorEchoesID(t *testing.T) {
	SetTranslator(nil) // resets to NoopTranslator
	defer SetTranslator(nil)
	got := tr(msgDebateStarted, map[string]any{"Topic": "x"})
	if got != msgDebateStarted {
		t.Fatalf("NoopTranslator must echo message ID; got %q want %q", got, msgDebateStarted)
	}
}

func TestI18n_ErroringTranslatorFallsBackToID(t *testing.T) {
	SetTranslator(erroringTranslator{})
	defer SetTranslator(nil)
	got := tr(msgPhaseCompleted, map[string]any{"Phase": "p", "Duration": "1s"})
	if got != msgPhaseCompleted {
		t.Fatalf("erroring Translator must fall back to message ID; got %q", got)
	}
}

// Paired-mutation core: installs a real Translator and proves tr()
// routes through it. A regression that hardcodes an English string back
// into manager.go would make this FAIL because the resolved output
// would no longer carry the translator's prefix.
func TestI18n_TrRoutesThroughTranslator(t *testing.T) {
	rt := &recordingTranslator{prefix: "XX::"}
	SetTranslator(rt)
	defer SetTranslator(nil)

	got := tr(msgDebateCompleted, map[string]any{"Rounds": 1, "Participants": 2, "Success": true})
	if !strings.HasPrefix(got, "XX::") {
		t.Fatalf("tr() did not route through installed Translator; got %q", got)
	}
	if len(rt.seen) != 1 || rt.seen[0] != msgDebateCompleted {
		t.Fatalf("Translator.T was not invoked with the expected message ID; seen=%v", rt.seen)
	}
}

// End-to-end: a swapped-locale Translator rewrites every StreamEvent
// Content string, so the event stream an operator reads is fully
// localised — exactly the CONST-046 guarantee. A re-hardcoded Content
// literal would break this.
func TestI18n_StreamEventContentIsLocalised(t *testing.T) {
	rt := &recordingTranslator{prefix: "LOCALE::"}
	SetTranslator(rt)
	defer SetTranslator(nil)

	m, err := NewIntegrationManager(nil, nil)
	if err != nil {
		t.Fatalf("NewIntegrationManager: %v", err)
	}
	if err := m.RegisterAgent(NewAgent(RoleGenerator, "p", "m", 0.8)); err != nil {
		t.Fatalf("RegisterAgent generator: %v", err)
	}
	if err := m.RegisterAgent(NewAgent(RoleCritic, "q", "n", 0.7)); err != nil {
		t.Fatalf("RegisterAgent critic: %v", err)
	}

	var events []*StreamEvent
	handler := func(e *StreamEvent) error {
		events = append(events, e)
		return nil
	}

	_, err = m.StreamDebate(context.Background(), &DebateRequest{
		ID:    "loc-1",
		Topic: "localisation",
	}, handler)
	if err != nil {
		t.Fatalf("StreamDebate: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("StreamDebate emitted no events")
	}
	// Progress/lifecycle events carry CONST-046-migrated Content. The
	// "agent_response" event intentionally carries the raw agent answer
	// (real LLM output / self-labelled stub marker) — that is model
	// content, not operator-facing UI text, so it is NOT routed through
	// the i18n seam per CONST-046's own carve-out.
	localisedTypes := map[string]bool{
		"started": true, "phase_started": true,
		"phase_completed": true, "completed": true,
		"cancelled": true, "failed": true,
	}
	checked := 0
	for _, e := range events {
		if !localisedTypes[e.Type] {
			continue
		}
		checked++
		if !strings.HasPrefix(e.Content, "LOCALE::") {
			t.Fatalf("StreamEvent %q Content not localised: %q", e.Type, e.Content)
		}
	}
	if checked == 0 {
		t.Fatal("no lifecycle StreamEvents were emitted to verify")
	}
	// The "started" event must resolve the debate-started message ID.
	if rt.seen[0] != msgDebateStarted {
		t.Fatalf("first resolved message ID = %q, want %q", rt.seen[0], msgDebateStarted)
	}
}

// Cancelled-before-start path also routes Content through the seam.
func TestI18n_CancelledEventIsLocalised(t *testing.T) {
	rt := &recordingTranslator{prefix: "LC::"}
	SetTranslator(rt)
	defer SetTranslator(nil)

	m, err := NewIntegrationManager(nil, nil)
	if err != nil {
		t.Fatalf("NewIntegrationManager: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before start

	var got *StreamEvent
	_, err = m.StreamDebate(ctx, &DebateRequest{ID: "c-1", Topic: "t"}, func(e *StreamEvent) error {
		got = e
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if got == nil || !strings.HasPrefix(got.Content, "LC::") {
		t.Fatalf("cancelled event Content not localised: %+v", got)
	}
	_ = time.Now
}
