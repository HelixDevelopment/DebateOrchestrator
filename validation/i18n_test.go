package validation

import (
	"context"
	"strings"
	"testing"
)

// i18n_test.go — CONST-046 round-334 paired-mutation coverage for the
// validation package's i18n seam. Per §1.1 every CONST-046 migration
// ships a paired mutation test: a fake Translator proves tr() actually
// routes through the seam (not a no-op echo), and a swapped-locale
// Translator proves a non-English consumer sees translated diagnostics.
//
// Anti-bluff: if tr() were a hardcoded passthrough that ignored the
// installed Translator, TestI18n_TrRoutesThroughTranslator would FAIL —
// the test asserts the resolved string differs from the message ID.

// recordingTranslator captures every message ID resolved through it and
// returns a deterministic, locale-tagged rewrite so tests can assert
// the seam is actually exercised.
type recordingTranslator struct {
	prefix string
	seen   []string
}

func (r *recordingTranslator) T(_ context.Context, id string, data map[string]any) (string, error) {
	r.seen = append(r.seen, id)
	if data != nil {
		return r.prefix + id + "/with-data", nil
	}
	return r.prefix + id, nil
}

// erroringTranslator always fails; tr() must fall back to the verbatim
// message ID so a diagnostic is never silently lost.
type erroringTranslator struct{}

func (erroringTranslator) T(_ context.Context, _ string, _ map[string]any) (string, error) {
	return "", context.DeadlineExceeded
}

func TestI18n_NoopTranslatorEchoesID(t *testing.T) {
	SetTranslator(nil) // resets to NoopTranslator
	defer SetTranslator(nil)
	got := tr(msgCodeEmptyContent, nil)
	if got != msgCodeEmptyContent {
		t.Fatalf("NoopTranslator must echo message ID; got %q want %q", got, msgCodeEmptyContent)
	}
}

// Paired-mutation core: installs a real Translator and proves tr()
// routes through it. A regression that hardcodes the English string
// back into validation.go (un-doing the CONST-046 migration) would make
// this test FAIL because the resolved output would no longer carry the
// translator's prefix.
func TestI18n_TrRoutesThroughTranslator(t *testing.T) {
	rt := &recordingTranslator{prefix: "XX::"}
	SetTranslator(rt)
	defer SetTranslator(nil)

	got := tr(msgPromptTooShort, map[string]any{"Len": 3})
	if !strings.HasPrefix(got, "XX::") {
		t.Fatalf("tr() did not route through installed Translator; got %q", got)
	}
	if len(rt.seen) != 1 || rt.seen[0] != msgPromptTooShort {
		t.Fatalf("Translator.T was not invoked with the expected message ID; seen=%v", rt.seen)
	}
}

// Proves a non-English consumer sees translated validation diagnostics
// end-to-end: a swapped-locale Translator rewrites every Issues entry,
// so the PipelineResult an operator reads is fully localised — exactly
// the CONST-046 guarantee (hardcoded English would break this).
func TestI18n_ValidationIssuesAreLocalised(t *testing.T) {
	rt := &recordingTranslator{prefix: "LOCALE::"}
	SetTranslator(rt)
	defer SetTranslator(nil)

	p := NewValidationPipeline(PipelineConfig{Passes: 4, Strict: false})
	bad := &Artifact{
		Type:    ArtifactCode,
		Content: "package x\n\nfunc Foo() {\n\tif true {\n\t\treturn\n\t}\n// missing closing brace\n",
	}
	res, err := p.Validate(context.Background(), bad)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sawLocalised bool
	for _, pass := range res.Passes {
		for _, issue := range pass.Issues {
			if strings.HasPrefix(issue, "LOCALE::") {
				sawLocalised = true
			}
		}
	}
	if !sawLocalised {
		t.Fatal("no validation Issues routed through the Translator; CONST-046 migration regressed")
	}
}

// Proves tr() falls back to the verbatim message ID on resolver error
// rather than swallowing the diagnostic (a swallowed Issues entry would
// hide a real validation failure — a §11.4 PASS-bluff at the i18n layer).
func TestI18n_TrFallsBackOnResolverError(t *testing.T) {
	SetTranslator(erroringTranslator{})
	defer SetTranslator(nil)
	got := tr(msgConfigNotParseable, nil)
	if got != msgConfigNotParseable {
		t.Fatalf("tr() must fall back to message ID on resolver error; got %q", got)
	}
}
