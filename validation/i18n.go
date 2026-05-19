package validation

// i18n.go declares package validation's hardcoded-content abstraction
// per CONST-046 (round-334 §11.4 anti-bluff sweep, 2026-05-19/20).
//
// CONST-051(B) decoupling: this submodule is project-not-aware. The
// validation package therefore declares its OWN Translator contract
// rather than importing any consuming-project i18n package. A
// consuming binary (HelixCode, HelixAgent, ATMOSphere) wires a real
// Translator via SetTranslator at boot; absent that wiring the
// package-level tr() helper falls back to NoopTranslator, which echoes
// the message ID verbatim (loud, never silently swallowed — a silent
// swallow would itself be a §11.4 PASS-bluff at the i18n layer).
//
// The validation pipeline's PassResult.Issues entries are operator-
// facing diagnostic text (surfaced in CLI / UI / API responses).
// Hardcoded English Issues silently break the product for non-English
// operators (CONST-046 rationale). Every Issues string composed from a
// stable diagnostic class is routed through tr().

import (
	"context"
	"sync"
)

// Translator is the contract package validation uses for every
// CONST-046-migrated operator-facing diagnostic string. It mirrors the
// minimal go-i18n surface used across the Helix codebase so a
// consuming project can satisfy it with a thin adapter.
type Translator interface {
	// T resolves messageID against the active locale. templateData
	// supplies named placeholders for go-i18n-style interpolation; pass
	// nil when the message has no placeholders.
	T(ctx context.Context, messageID string, templateData map[string]any) (string, error)
}

// NoopTranslator returns the messageID verbatim. SAFETY default for
// unit tests within this package + backward-compat for callers who
// have not yet wired a real Translator. Production consumers MUST
// inject a real Translator via SetTranslator at boot.
type NoopTranslator struct{}

// T returns id unchanged (loud echo). Never returns an error.
func (NoopTranslator) T(_ context.Context, id string, _ map[string]any) (string, error) {
	return id, nil
}

var (
	translatorMu sync.RWMutex
	translator   Translator = NoopTranslator{}
)

// SetTranslator installs the process-wide Translator the validation
// package uses for operator-facing diagnostic strings. Passing nil
// resets to NoopTranslator (loud echo) rather than panicking — a nil
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
// On any resolver error it falls back to the verbatim message ID so a
// diagnostic is never lost — a swallowed Issues entry would hide a real
// validation failure from the operator (§11.4 PASS-bluff). templateData
// carries named placeholders; pass nil when there are none.
func tr(messageID string, templateData map[string]any) string {
	out, err := currentTranslator().T(context.Background(), messageID, templateData)
	if err != nil || out == "" {
		return messageID
	}
	return out
}

// Message IDs for package validation's operator-facing diagnostic
// strings. Keeping them as exported-looking constants (unexported here
// to preserve the package's API surface) gives the consuming project's
// i18n bundle a single authoritative key list to translate against.
const (
	msgCodeEmptyContent       = "validation.issue.code_empty_content"
	msgMismatchedCurly        = "validation.issue.mismatched_curly"
	msgMismatchedSquare       = "validation.issue.mismatched_square"
	msgMismatchedParen        = "validation.issue.mismatched_paren"
	msgConfigEmptyContent     = "validation.issue.config_empty_content"
	msgConfigNotParseable     = "validation.issue.config_not_parseable"
	msgDocEmpty               = "validation.issue.doc_empty"
	msgDocNoHeader            = "validation.issue.doc_no_header"
	msgPromptEmpty            = "validation.issue.prompt_empty"
	msgPromptTooShort         = "validation.issue.prompt_too_short"
	msgUnknownArtifactType    = "validation.issue.unknown_artifact_type"
	msgTodoMarkerStrict       = "validation.issue.todo_marker_strict"
	msgTodoMarkerWarning      = "validation.issue.todo_marker_warning"
	msgPanicInNonTest         = "validation.issue.panic_in_non_test"
	msgSecretLeak             = "validation.issue.secret_leak"
	msgUnclosedCodeBlock      = "validation.issue.unclosed_code_block"
	msgPromptUnfilledMustache = "validation.issue.prompt_unfilled_mustache"
	msgPromptPlaceholderTag   = "validation.issue.prompt_placeholder_tag"
	msgOrphanFunction         = "validation.issue.orphan_function"
	msgMetadataVersionEmpty   = "validation.issue.metadata_version_empty"
	msgMetadataVersionType    = "validation.issue.metadata_version_type"
	msgLineTooLong            = "validation.issue.line_too_long"
	msgTrailingWhitespace     = "validation.issue.trailing_whitespace"
	msgDocTooShort            = "validation.issue.doc_too_short"
	msgPromptVeryShort        = "validation.issue.prompt_very_short"
	msgPromptVeryLong         = "validation.issue.prompt_very_long"
)
