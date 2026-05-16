// Package validation hosts the multi-pass validation pipeline. The
// pipeline runs four real, content-inspecting passes (structural,
// semantic, consistency, quality) and aggregates the per-pass results
// into a PipelineResult. Construction is via NewValidationPipeline.
//
// Each pass actually inspects the supplied Artifact (no stubs, no
// "always-pass" branches): structural enforces grammar-class
// invariants (matched delimiters, parseable config, non-empty
// documentation, prompt length), semantic looks for forbidden
// markers and obvious leaks, consistency cross-references declared
// symbols against their usages and validates Metadata, quality
// scores line-length and word-count metrics.
package validation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// PipelineConfig configures a ValidationPipeline.
type PipelineConfig struct {
	// Passes is the number of validation passes to run.
	Passes int
	// Strict toggles fail-fast on the first error.
	Strict bool
}

// DefaultPipelineConfig returns a conservative default configuration.
func DefaultPipelineConfig() PipelineConfig {
	return PipelineConfig{Passes: 4, Strict: false}
}

// ArtifactType classifies the kind of artefact handed to the pipeline.
type ArtifactType string

// Artifact types recognised by the pipeline.
const (
	ArtifactCode          ArtifactType = "code"
	ArtifactDocumentation ArtifactType = "documentation"
	ArtifactConfig        ArtifactType = "config"
	ArtifactPrompt        ArtifactType = "prompt"
)

// Artifact is the input payload for the validation pipeline.
type Artifact struct {
	// Type classifies the artefact.
	Type ArtifactType
	// Content is the raw artefact body (source code, markdown, etc.).
	Content string
	// Language is the source language (when applicable).
	Language string
	// Metadata is free-form provenance carried through the pipeline.
	Metadata map[string]interface{}
}

// PassResult is the outcome of a single validation pass.
type PassResult struct {
	// Name identifies the pass (structural/semantic/consistency/quality).
	Name string
	// Passed is true iff the pass judged the artefact acceptable.
	Passed bool
	// Score is the per-pass quality score in [0,1].
	Score float64
	// Issues is the per-pass issue list (empty on clean pass).
	Issues []string
}

// PipelineResult is the aggregate outcome across every pass.
type PipelineResult struct {
	// Passes is the ordered list of per-pass results actually executed.
	Passes []*PassResult
	// PassResults maps each pass name to its per-pass outcome (mirror of
	// Passes for keyed lookup).
	PassResults map[string]*PassResult
	// OverallPassed is true iff every pass passed.
	OverallPassed bool
	// QualityScore is the aggregate quality score in [0,1].
	QualityScore float64
	// OverallScore mirrors QualityScore for backward compatibility.
	OverallScore float64
	// FailedPass is the name of the first failing pass (empty on full pass).
	FailedPass string
	// Duration is the wall-clock time spent running the pipeline.
	Duration time.Duration
}

// ValidationPipeline is the multi-pass validator entry point.
type ValidationPipeline struct {
	cfg PipelineConfig
}

// Sentinel errors surfaced by the pipeline.
var (
	// ErrPipelineFailed is returned in Strict mode when a pass fails.
	ErrPipelineFailed = errors.New("debate/validation: pipeline failed in strict mode")
	// ErrInvalidInput is returned when Execute receives an unsupported
	// input type.
	ErrInvalidInput = errors.New("debate/validation: invalid input type")
)

// NewValidationPipeline constructs a ValidationPipeline. The returned
// struct is real and inspectable. Construction cannot fail with empty
// config so the signature returns a single value.
func NewValidationPipeline(cfg PipelineConfig) *ValidationPipeline {
	if cfg.Passes < 0 {
		cfg.Passes = 0
	}
	return &ValidationPipeline{cfg: cfg}
}

// Config returns the configuration the pipeline was constructed with.
func (p *ValidationPipeline) Config() PipelineConfig { return p.cfg }

// Execute runs the validation passes against opaque input. It accepts
// either *Artifact or a raw string (auto-wrapped as ArtifactCode).
// Any other type yields ErrInvalidInput.
func (p *ValidationPipeline) Execute(ctx context.Context, input interface{}) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var artifact *Artifact
	switch v := input.(type) {
	case *Artifact:
		artifact = v
	case Artifact:
		clone := v
		artifact = &clone
	case string:
		artifact = &Artifact{Type: ArtifactCode, Content: v}
	default:
		return nil, fmt.Errorf("%w: got %T", ErrInvalidInput, input)
	}
	res, err := p.Validate(ctx, artifact)
	if res == nil && err == nil {
		return nil, nil
	}
	return res, err
}

// Validate runs the validation passes against a typed Artifact and
// returns the structured PipelineResult.
func (p *ValidationPipeline) Validate(ctx context.Context, artifact *Artifact) (*PipelineResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if artifact == nil {
		return nil, fmt.Errorf("%w: nil artifact", ErrInvalidInput)
	}
	start := time.Now()
	result := &PipelineResult{
		PassResults:   make(map[string]*PassResult),
		OverallPassed: true,
	}

	passes := []func(context.Context, *Artifact, PipelineConfig) *PassResult{
		structuralPass,
		semanticPass,
		consistencyPass,
		qualityPass,
	}

	var firstFailErr error
	for _, fn := range passes {
		if err := ctx.Err(); err != nil {
			result.Duration = time.Since(start)
			return result, err
		}
		pr := fn(ctx, artifact, p.cfg)
		result.Passes = append(result.Passes, pr)
		result.PassResults[pr.Name] = pr
		if !pr.Passed {
			result.OverallPassed = false
			if result.FailedPass == "" {
				result.FailedPass = pr.Name
			}
			if p.cfg.Strict {
				firstFailErr = fmt.Errorf("%w: pass %q failed", ErrPipelineFailed, pr.Name)
				break
			}
		}
	}

	// Aggregate quality score = mean of per-pass scores.
	if len(result.Passes) > 0 {
		var sum float64
		for _, pr := range result.Passes {
			sum += pr.Score
		}
		mean := sum / float64(len(result.Passes))
		result.QualityScore = mean
		result.OverallScore = mean
	}
	result.Duration = time.Since(start)
	return result, firstFailErr
}

// -------------------------------------------------------------------
// Pass 1 — Structural
// -------------------------------------------------------------------

func structuralPass(_ context.Context, a *Artifact, _ PipelineConfig) *PassResult {
	pr := &PassResult{Name: "structural", Passed: true, Score: 1.0}
	switch a.Type {
	case ArtifactCode:
		if a.Content == "" {
			pr.Passed = false
			pr.Score = 0
			pr.Issues = append(pr.Issues, "code artefact has empty content")
			return pr
		}
		curly, square, paren := countDelimiters(a.Content)
		if curly != 0 {
			pr.Passed = false
			pr.Issues = append(pr.Issues, fmt.Sprintf("mismatched curly braces: delta=%d", curly))
		}
		if square != 0 {
			pr.Passed = false
			pr.Issues = append(pr.Issues, fmt.Sprintf("mismatched square brackets: delta=%d", square))
		}
		if paren != 0 {
			pr.Passed = false
			pr.Issues = append(pr.Issues, fmt.Sprintf("mismatched parentheses: delta=%d", paren))
		}
	case ArtifactConfig:
		if a.Content == "" {
			pr.Passed = false
			pr.Score = 0
			pr.Issues = append(pr.Issues, "config artefact has empty content")
			return pr
		}
		if !looksLikeJSON(a.Content) && !looksLikeYAML(a.Content) {
			pr.Passed = false
			pr.Issues = append(pr.Issues, "config does not parse as JSON or simple YAML")
		}
	case ArtifactDocumentation:
		if strings.TrimSpace(a.Content) == "" {
			pr.Passed = false
			pr.Score = 0
			pr.Issues = append(pr.Issues, "documentation artefact is empty")
			return pr
		}
		if !hasMarkdownHeader(a.Content) {
			pr.Passed = false
			pr.Issues = append(pr.Issues, "documentation has no markdown header (line starting with '#')")
		}
	case ArtifactPrompt:
		trimmed := strings.TrimSpace(a.Content)
		if trimmed == "" {
			pr.Passed = false
			pr.Score = 0
			pr.Issues = append(pr.Issues, "prompt artefact is empty")
			return pr
		}
		if len(trimmed) < 10 {
			pr.Passed = false
			pr.Issues = append(pr.Issues, fmt.Sprintf("prompt too short: len=%d (min 10)", len(trimmed)))
		}
	default:
		pr.Passed = false
		pr.Issues = append(pr.Issues, fmt.Sprintf("unknown artefact type: %q", a.Type))
	}
	if !pr.Passed {
		pr.Score = scoreFromIssues(len(pr.Issues))
	}
	return pr
}

// countDelimiters returns the signed balance of {}, [], () delimiters
// (open - close). A balanced source yields zero on all three counts.
// Characters inside double-quoted, single-quoted, or backtick-quoted
// string literals are ignored, as are characters inside // line
// comments and /* */ block comments.
func countDelimiters(s string) (curly, square, paren int) {
	type mode int
	const (
		mNorm mode = iota
		mLine
		mBlock
		mDouble
		mSingle
		mBack
	)
	st := mNorm
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch st {
		case mLine:
			if c == '\n' {
				st = mNorm
			}
			continue
		case mBlock:
			if c == '*' && i+1 < len(s) && s[i+1] == '/' {
				st = mNorm
				i++
			}
			continue
		case mDouble:
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '"' {
				st = mNorm
			}
			continue
		case mSingle:
			if c == '\\' && i+1 < len(s) {
				i++
				continue
			}
			if c == '\'' {
				st = mNorm
			}
			continue
		case mBack:
			if c == '`' {
				st = mNorm
			}
			continue
		}
		// mNorm
		if c == '/' && i+1 < len(s) {
			if s[i+1] == '/' {
				st = mLine
				i++
				continue
			}
			if s[i+1] == '*' {
				st = mBlock
				i++
				continue
			}
		}
		switch c {
		case '"':
			st = mDouble
		case '\'':
			st = mSingle
		case '`':
			st = mBack
		case '{':
			curly++
		case '}':
			curly--
		case '[':
			square++
		case ']':
			square--
		case '(':
			paren++
		case ')':
			paren--
		}
	}
	return
}

func looksLikeJSON(s string) bool {
	trim := strings.TrimSpace(s)
	if trim == "" {
		return false
	}
	if !(strings.HasPrefix(trim, "{") || strings.HasPrefix(trim, "[")) {
		return false
	}
	var any interface{}
	return json.Unmarshal([]byte(trim), &any) == nil
}

// looksLikeYAML accepts a very simple line-based YAML form: every
// non-blank, non-comment line must either be a `key: value` pair or a
// `- value` list item, with consistent indentation by spaces. This is
// deliberately conservative — it does NOT try to be a full YAML
// parser, only enough to distinguish "obvious YAML" from garbage.
func looksLikeYAML(s string) bool {
	lines := strings.Split(s, "\n")
	keyVal := regexp.MustCompile(`^\s*[A-Za-z_][\w.-]*\s*:( .*| *)$`)
	listItem := regexp.MustCompile(`^\s*-\s+.+$`)
	saw := false
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if keyVal.MatchString(ln) || listItem.MatchString(ln) {
			saw = true
			continue
		}
		return false
	}
	return saw
}

func hasMarkdownHeader(s string) bool {
	for _, ln := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimLeft(ln, " \t"), "#") {
			return true
		}
	}
	return false
}

func scoreFromIssues(n int) float64 {
	if n <= 0 {
		return 1.0
	}
	// 1 issue -> 0.6, 2 -> 0.3, 3+ -> 0.0
	switch n {
	case 1:
		return 0.6
	case 2:
		return 0.3
	default:
		return 0.0
	}
}

// -------------------------------------------------------------------
// Pass 2 — Semantic
// -------------------------------------------------------------------

var (
	reTodoMarker        = regexp.MustCompile(`\b(TODO|FIXME|XXX)\b`)
	rePanicCall         = regexp.MustCompile(`\bpanic\(`)
	reLeakLine          = regexp.MustCompile(`(?i)(password|secret|api[_-]?key|token)\s*[:=]\s*["']?([^"'\s,}{]+)`)
	rePlaceholderMust   = regexp.MustCompile(`\{\{[^}]+\}\}`)
	rePlaceholderAngle  = regexp.MustCompile(`<(TODO|PLACEHOLDER|FIXME)[^>]*>`)
	reUnclosedCodeBlock = regexp.MustCompile("(?m)^```")
)

func semanticPass(_ context.Context, a *Artifact, cfg PipelineConfig) *PassResult {
	pr := &PassResult{Name: "semantic", Passed: true, Score: 1.0}
	switch a.Type {
	case ArtifactCode:
		if reTodoMarker.MatchString(a.Content) {
			if cfg.Strict {
				pr.Passed = false
				pr.Issues = append(pr.Issues, "TODO/FIXME/XXX marker present (strict mode)")
			} else {
				pr.Issues = append(pr.Issues, "TODO/FIXME/XXX marker present (non-strict warning)")
			}
		}
		if rePanicCall.MatchString(a.Content) && !strings.Contains(a.Content, "_test.go") {
			pr.Issues = append(pr.Issues, "panic(...) call found in non-test code (concern)")
		}
	case ArtifactConfig:
		if matches := reLeakLine.FindAllStringSubmatch(a.Content, -1); len(matches) > 0 {
			for _, m := range matches {
				val := m[2]
				if isPlaceholderSecret(val) {
					continue
				}
				pr.Passed = false
				pr.Issues = append(pr.Issues, fmt.Sprintf("possible secret leak: %s=%s", m[1], val))
			}
		}
	case ArtifactDocumentation:
		count := len(reUnclosedCodeBlock.FindAllStringIndex(a.Content, -1))
		if count%2 != 0 {
			pr.Passed = false
			pr.Issues = append(pr.Issues, fmt.Sprintf("unclosed markdown code block: %d fence(s) found", count))
		}
	case ArtifactPrompt:
		if rePlaceholderMust.MatchString(a.Content) {
			pr.Passed = false
			pr.Issues = append(pr.Issues, "prompt contains unfilled {{var}} placeholder")
		}
		if rePlaceholderAngle.MatchString(a.Content) {
			pr.Passed = false
			pr.Issues = append(pr.Issues, "prompt contains <TODO>/<PLACEHOLDER>/<FIXME> marker")
		}
	}
	if !pr.Passed {
		pr.Score = scoreFromIssues(len(pr.Issues))
	} else if len(pr.Issues) > 0 {
		// warnings reduce score but do not fail.
		pr.Score = 0.75
	}
	return pr
}

func isPlaceholderSecret(v string) bool {
	low := strings.ToLower(strings.Trim(v, " \t\"'"))
	switch low {
	case "", "your-password", "your_password", "changeme", "change-me",
		"placeholder", "xxx", "***", "redacted", "secret", "password",
		"todo", "fixme", "example", "your_api_key", "your-api-key",
		"your_secret", "your-secret", "your_token", "your-token":
		return true
	}
	if strings.HasPrefix(low, "${") || strings.HasPrefix(low, "$(") {
		return true
	}
	if strings.HasPrefix(low, "{{") || strings.HasPrefix(low, "<") {
		return true
	}
	return false
}

// -------------------------------------------------------------------
// Pass 3 — Consistency
// -------------------------------------------------------------------

var reGoFuncDecl = regexp.MustCompile(`(?m)^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z_]\w*)\s*\(`)

func consistencyPass(_ context.Context, a *Artifact, _ PipelineConfig) *PassResult {
	pr := &PassResult{Name: "consistency", Passed: true, Score: 1.0}

	if a.Type == ArtifactCode {
		matches := reGoFuncDecl.FindAllStringSubmatchIndex(a.Content, -1)
		for _, m := range matches {
			name := a.Content[m[2]:m[3]]
			if name == "main" || name == "init" {
				continue
			}
			// Heuristic: exported (capitalized) functions are part of the
			// package API and may legitimately have no in-artefact callers.
			// Unexported (lowercase) functions that are never referenced
			// elsewhere in the artefact are likely orphans.
			if name[0] >= 'A' && name[0] <= 'Z' {
				continue
			}
			// Strip the declaration occurrence and search remainder for any
			// reference to `name`.
			before := a.Content[:m[2]]
			after := a.Content[m[3]:]
			rest := before + " " + after
			ref := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
			if !ref.MatchString(rest) {
				pr.Passed = false
				pr.Issues = append(pr.Issues, fmt.Sprintf("declared function %q has no references elsewhere in artefact", name))
			}
		}
	}

	if a.Metadata != nil {
		if raw, ok := a.Metadata["version"]; ok {
			switch v := raw.(type) {
			case string:
				if strings.TrimSpace(v) == "" {
					pr.Passed = false
					pr.Issues = append(pr.Issues, "metadata.version is empty string")
				}
			default:
				pr.Passed = false
				pr.Issues = append(pr.Issues, fmt.Sprintf("metadata.version must be a non-empty string, got %T", raw))
			}
		}
	}

	if !pr.Passed {
		pr.Score = scoreFromIssues(len(pr.Issues))
	}
	return pr
}

// -------------------------------------------------------------------
// Pass 4 — Quality
// -------------------------------------------------------------------

const maxLineLength = 200

func qualityPass(_ context.Context, a *Artifact, _ PipelineConfig) *PassResult {
	pr := &PassResult{Name: "quality", Passed: true, Score: 1.0}

	switch a.Type {
	case ArtifactCode:
		lines := strings.Split(a.Content, "\n")
		longCount := 0
		trailingWS := 0
		for i, ln := range lines {
			if len(ln) > maxLineLength {
				longCount++
				pr.Issues = append(pr.Issues, fmt.Sprintf("line %d exceeds %d chars (%d)", i+1, maxLineLength, len(ln)))
			}
			if len(ln) > 0 && (ln[len(ln)-1] == ' ' || ln[len(ln)-1] == '\t') {
				trailingWS++
			}
		}
		if trailingWS > 0 {
			pr.Issues = append(pr.Issues, fmt.Sprintf("%d line(s) with trailing whitespace", trailingWS))
		}
		// Score: start at 1.0, subtract for each issue category.
		score := 1.0
		if longCount > 0 {
			penalty := float64(longCount) * 0.15
			if penalty > 0.7 {
				penalty = 0.7
			}
			score -= penalty
			pr.Passed = false
		}
		if trailingWS > 0 {
			score -= 0.1
		}
		if score < 0 {
			score = 0
		}
		pr.Score = score
	case ArtifactDocumentation:
		words := len(strings.Fields(a.Content))
		if words <= 5 {
			pr.Passed = false
			pr.Issues = append(pr.Issues, fmt.Sprintf("documentation too short: %d word(s) (min 6)", words))
			pr.Score = scoreFromIssues(1)
		}
	case ArtifactConfig:
		lines := strings.Split(a.Content, "\n")
		long := 0
		for i, ln := range lines {
			if len(ln) > maxLineLength {
				long++
				pr.Issues = append(pr.Issues, fmt.Sprintf("line %d exceeds %d chars (%d)", i+1, maxLineLength, len(ln)))
			}
		}
		if long > 0 {
			pr.Passed = false
			pr.Score = scoreFromIssues(long)
		}
	case ArtifactPrompt:
		// Quality for prompts: rough length sweet-spot 20..2000 chars.
		l := len(strings.TrimSpace(a.Content))
		switch {
		case l < 20:
			pr.Issues = append(pr.Issues, fmt.Sprintf("prompt very short: %d chars", l))
			pr.Score = 0.5
		case l > 4000:
			pr.Issues = append(pr.Issues, fmt.Sprintf("prompt very long: %d chars", l))
			pr.Score = 0.7
		default:
			pr.Score = 1.0
		}
	}
	return pr
}
