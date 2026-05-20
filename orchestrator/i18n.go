package orchestrator

// i18n.go declares package orchestrator's hardcoded-content abstraction
// per CONST-046 (round-395 §11.4 anti-bluff sweep, 2026-05-19/20).
//
// CONST-051(B) decoupling: this submodule is project-not-aware. The
// orchestrator package declares its OWN Translator contract rather than
// importing any consuming-project i18n package. A consuming binary
// (HelixCode, HelixAgent, ATMOSphere) wires a real Translator via
// SetTranslator at boot; absent that wiring the package-level tr()
// helper falls back to NoopTranslator, which echoes the resolved
// message verbatim (loud, never silently swallowed — a silent swallow
// would itself be a §11.4 PASS-bluff at the i18n layer).
//
// ConductDebate composes a ConsensusResponse whose Conclusion,
// Reasoning, Summary, and KeyPoints fields are operator-facing debate-
// outcome text surfaced in CLI / UI / API responses. Hardcoded English
// here silently breaks the product for non-English operators
// (CONST-046 rationale). Every such string composed from a stable
// outcome class is routed through tr() with run-time values (topic,
// rounds, participants, confidence) passed as go-i18n placeholders.
//
// LLM-system prompts (buildAgentPrompt) and self-labelled stub markers
// (synthesiseContent, invoker-error) are NOT operator-facing content
// and remain outside the i18n channel per CONST-046's own carve-out.

import (
	"context"
	"sync"
)

// Translator is the contract package orchestrator uses for every
// CONST-046-migrated operator-facing string. It mirrors the minimal
// go-i18n surface used across the Helix codebase (and the sibling
// comprehensive / validation packages' Translator) so a consuming
// project can satisfy it with a single thin shared adapter.
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

// SetTranslator installs the process-wide Translator the orchestrator
// package uses for operator-facing ConsensusResponse strings. Passing
// nil resets to NoopTranslator (loud echo) rather than panicking — a
// nil translator must never silently swallow message resolution. Safe
// for concurrent use; consuming binaries call it once at boot.
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
// message ID so an outcome string is never lost — a swallowed
// Conclusion would hide the debate outcome from the operator (§11.4
// PASS-bluff). templateData carries named placeholders; pass nil when
// there are none.
func tr(messageID string, templateData map[string]any) string {
	out, err := currentTranslator().T(context.Background(), messageID, templateData)
	if err != nil || out == "" {
		return messageID
	}
	return out
}

// Message IDs for package orchestrator's operator-facing
// ConsensusResponse strings. Kept as unexported constants to preserve
// the package's API surface while giving the consuming project's i18n
// bundle a single authoritative key list to translate against.
const (
	msgConsensusConclusion   = "debate.orchestrator.consensus_conclusion"
	msgConsensusReasoning    = "debate.orchestrator.consensus_reasoning"
	msgConsensusKeyTopic     = "debate.orchestrator.consensus_key_topic"
	msgConsensusKeyRounds    = "debate.orchestrator.consensus_key_rounds"
	msgConsensusKeyParticip  = "debate.orchestrator.consensus_key_participants"
)
