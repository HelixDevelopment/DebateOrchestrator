package comprehensive

// i18n.go declares package comprehensive's hardcoded-content abstraction
// per CONST-046 (round-395 §11.4 anti-bluff sweep, 2026-05-19/20).
//
// CONST-051(B) decoupling: this submodule is project-not-aware. The
// comprehensive package therefore declares its OWN Translator contract
// rather than importing any consuming-project i18n package. A
// consuming binary (HelixCode, HelixAgent, ATMOSphere) wires a real
// Translator via SetTranslator at boot; absent that wiring the
// package-level tr() helper falls back to NoopTranslator, which echoes
// the resolved message verbatim (loud, never silently swallowed — a
// silent swallow would itself be a §11.4 PASS-bluff at the i18n layer).
//
// StreamDebate emits StreamEvent values whose Content field is operator-
// facing progress text surfaced through the caller's StreamHandler into
// a CLI, UI, or API response. Hardcoded English Content silently breaks
// the product for non-English operators (CONST-046 rationale). Every
// Content string composed from a stable event class is routed through
// tr() with the run-time values (topic, phase, round, counts) passed as
// go-i18n template placeholders.

import (
	"context"
	"sync"
)

// Translator is the contract package comprehensive uses for every
// CONST-046-migrated operator-facing string. It mirrors the minimal
// go-i18n surface used across the Helix codebase (and the sibling
// validation package's Translator) so a consuming project can satisfy
// it with a single thin adapter shared by both packages.
type Translator interface {
	// T resolves messageID against the active locale. templateData
	// supplies named placeholders for go-i18n-style interpolation; pass
	// nil when the message has no placeholders.
	T(ctx context.Context, messageID string, templateData map[string]any) (string, error)
}

// NoopTranslator returns messageID verbatim. SAFETY default for unit
// tests within this package + backward-compat for callers who have not
// yet wired a real Translator. Production consumers MUST inject a real
// Translator via SetTranslator at boot.
type NoopTranslator struct{}

// T returns id unchanged (loud echo). Never returns an error.
func (NoopTranslator) T(_ context.Context, id string, _ map[string]any) (string, error) {
	return id, nil
}

var (
	translatorMu sync.RWMutex
	translator   Translator = NoopTranslator{}
)

// SetTranslator installs the process-wide Translator the comprehensive
// package uses for operator-facing StreamEvent.Content strings. Passing
// nil resets to NoopTranslator (loud echo) rather than panicking — a nil
// translator must never silently swallow message resolution. Safe for
// concurrent use; consuming binaries call it once at boot.
func SetTranslator(t Translator) {
	translatorMu.Lock()
	defer translatorMu.Unlock()
	if t == nil {
		translator = NoopTranslator{}
		return
	}
	translator = t
}

// currentTranslator returns the installed Translator under a read lock.
func currentTranslator() Translator {
	translatorMu.RLock()
	defer translatorMu.RUnlock()
	return translator
}

// tr resolves a CONST-046 message ID against the installed Translator.
// On any resolver error (or empty result) it falls back to the verbatim
// message ID so a progress event is never lost — a swallowed Content
// string would hide debate progress from the operator (§11.4 PASS-
// bluff). templateData carries named placeholders; pass nil when there
// are none.
func tr(messageID string, templateData map[string]any) string {
	out, err := currentTranslator().T(context.Background(), messageID, templateData)
	if err != nil || out == "" {
		return messageID
	}
	return out
}

// Message IDs for package comprehensive's operator-facing StreamEvent
// Content strings. Kept as unexported constants to preserve the
// package's API surface while giving the consuming project's i18n
// bundle a single authoritative key list to translate against.
const (
	msgCtxCancelledBeforeStart = "debate.comprehensive.ctx_cancelled_before_start"
	msgDebateStarted           = "debate.comprehensive.debate_started"
	msgDebateFailed            = "debate.comprehensive.debate_failed"
	msgDebateCancelled         = "debate.comprehensive.debate_cancelled"
	msgCtxCancelledMidStream   = "debate.comprehensive.ctx_cancelled_mid_stream"
	msgPhaseStarted            = "debate.comprehensive.phase_started"
	msgCtxCancelledMidPhase    = "debate.comprehensive.ctx_cancelled_mid_phase"
	msgPhaseCompleted          = "debate.comprehensive.phase_completed"
	msgDebateCompleted         = "debate.comprehensive.debate_completed"
)
