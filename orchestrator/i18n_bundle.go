package orchestrator

// i18n_bundle.go — HXC-079 root-cause fix. The orchestrator package
// shipped its operator-facing template bundle (i18n/active.en.yaml) but
// provided NO loader, so the package default translator (NoopTranslator)
// echoed raw message keys verbatim — the consensus Conclusion/Summary
// printed `debate.orchestrator.consensus_conclusion` instead of the
// resolved prose "Debate on <topic> completed across N round(s)."
//
// This file adds bundleTranslator: a dependency-free, stdlib-only
// Translator that embeds the shipped YAML bundle, parses its flat
// `key: "template"` entries, and renders the {{.Name}} placeholders
// against the templateData map using text/template (the same {{.Name}}
// syntax go-i18n uses, so consumer locale bundles stay compatible).
//
// CONST-051(B) decoupling preserved: the bundle is the submodule's OWN
// shipped English source; it contains no consuming-project context. A
// consuming project may still SetTranslator(...) a richer go-i18n-backed
// Translator with locale overrides — that wired translator always wins
// over this default (see i18n.go currentTranslator). This default exists
// only so the package renders correct prose OUT OF THE BOX (the §11.4
// "feature must work for the end user" guarantee), never echoing a raw
// key to an operator.

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"text/template"
)

//go:embed i18n/active.en.yaml
var enBundleFS embed.FS

// bundleTranslator resolves message IDs against an in-memory map of
// go-i18n-style templates parsed from the embedded English bundle. It is
// safe for concurrent use after construction (templates are immutable;
// the parsed-template cache is guarded by a mutex).
type bundleTranslator struct {
	raw map[string]string // messageID -> raw template string

	mu       sync.Mutex
	compiled map[string]*template.Template // lazily compiled per-ID
}

// newBundleTranslator parses the embedded active.en.yaml into a
// messageID->template map. A parse failure returns an error; callers
// fall back to NoopTranslator only if construction fails (so a broken
// bundle is loud, never silently swallowed — itself a §11.4 PASS-bluff).
func newBundleTranslator() (*bundleTranslator, error) {
	data, err := enBundleFS.ReadFile("i18n/active.en.yaml")
	if err != nil {
		return nil, fmt.Errorf("read embedded i18n bundle: %w", err)
	}
	raw, err := parseFlatYAMLBundle(data)
	if err != nil {
		return nil, fmt.Errorf("parse i18n bundle: %w", err)
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("i18n bundle parsed to zero entries")
	}
	return &bundleTranslator{raw: raw, compiled: map[string]*template.Template{}}, nil
}

// T resolves id against the bundle, rendering {{.Name}} placeholders with
// templateData. An unknown id, or a render error, returns ("", err) so
// the package-level tr() helper falls back to the verbatim message ID
// rather than emitting a half-rendered string.
func (b *bundleTranslator) T(_ context.Context, id string, templateData map[string]any) (string, error) {
	tmplStr, ok := b.raw[id]
	if !ok {
		return "", fmt.Errorf("i18n: unknown message id %q", id)
	}
	tmpl, err := b.templateFor(id, tmplStr)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	// missingkey=error so a placeholder the caller forgot to supply is a
	// loud failure (→ tr() falls back to the key), never a silent
	// "<no value>" leaked to the operator.
	if err := tmpl.Option("missingkey=error").Execute(&buf, templateData); err != nil {
		return "", fmt.Errorf("i18n: render %q: %w", id, err)
	}
	return buf.String(), nil
}

func (b *bundleTranslator) templateFor(id, tmplStr string) (*template.Template, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if t, ok := b.compiled[id]; ok {
		return t, nil
	}
	t, err := template.New(id).Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("i18n: compile template %q: %w", id, err)
	}
	b.compiled[id] = t
	return t, nil
}

// parseFlatYAMLBundle parses the submodule's flat go-i18n English source
// — lines of the form `message.id: "template string"` (or single-quoted,
// or unquoted) — into a map. Comments (`# ...`) and blank lines are
// skipped. This is a deliberately minimal, dependency-free reader for the
// flat key/value shape the bundle uses (no nested maps, no lists); it
// rejects nothing silently — a malformed value line returns an error.
func parseFlatYAMLBundle(data []byte) (map[string]string, error) {
	out := map[string]string{}
	for lineNo, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		colon := strings.IndexByte(trimmed, ':')
		if colon < 0 {
			return nil, fmt.Errorf("i18n bundle line %d: no key separator: %q", lineNo+1, trimmed)
		}
		key := strings.TrimSpace(trimmed[:colon])
		val := strings.TrimSpace(trimmed[colon+1:])
		// Strip a trailing inline comment only when the value is unquoted.
		unq, err := unquoteYAMLScalar(val)
		if err != nil {
			return nil, fmt.Errorf("i18n bundle line %d (%q): %w", lineNo+1, key, err)
		}
		if key == "" {
			return nil, fmt.Errorf("i18n bundle line %d: empty key", lineNo+1)
		}
		out[key] = unq
	}
	return out, nil
}

// unquoteYAMLScalar handles the three scalar forms the bundle uses:
// double-quoted ("..."), single-quoted ('...'), and bare. Double-quoted
// strings are processed via strconv.Unquote so escapes resolve; single-
// quoted strings have YAML's '' → ' un-escaping; bare scalars are taken
// verbatim (trimmed). go-i18n template syntax ({{.Name}}) needs no
// special handling — it survives all three forms unchanged.
func unquoteYAMLScalar(val string) (string, error) {
	if len(val) >= 2 && val[0] == '"' && val[len(val)-1] == '"' {
		s, err := strconv.Unquote(val)
		if err != nil {
			return "", fmt.Errorf("invalid double-quoted scalar: %w", err)
		}
		return s, nil
	}
	if len(val) >= 2 && val[0] == '\'' && val[len(val)-1] == '\'' {
		inner := val[1 : len(val)-1]
		return strings.ReplaceAll(inner, "''", "'"), nil
	}
	return val, nil
}

func init() {
	// Install the bundle-backed translator as the package default so the
	// orchestrator renders prose out of the box. If the embedded bundle
	// ever fails to load, fall back loudly to NoopTranslator (key echo) —
	// a swallowed Conclusion would hide the debate outcome (§11.4).
	if bt, err := newBundleTranslator(); err == nil {
		defaultTranslator = bt
		translatorMu.Lock()
		// Only adopt the default if no consumer has wired one yet.
		if _, isNoop := translator.(NoopTranslator); isNoop {
			translator = bt
		}
		translatorMu.Unlock()
	}
}
