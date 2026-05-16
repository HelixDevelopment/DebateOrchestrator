// Package testing hosts the LLM-driven test-case generator, sandboxed
// executor, contrastive analyser, and test-case validator.
//
// Production surfaces (real, deterministic):
//   - SandboxedTestExecutor.Execute spawns real child processes via
//     os/exec, captures stdout/stderr/exit-code/wall-time, and (on
//     Linux) enforces RLIMIT_AS + RLIMIT_CPU via withRlimits.
//   - DifferentialContrastiveAnalyzer.Analyze iterates result pairs
//     and produces a structured ContrastiveReport (no LLM calls).
//   - BasicTestCaseValidator.Validate inspects TestCase fields against
//     the rules documented on the method (no LLM calls).
//   - LLMTestCaseGenerator.Generate / GenerateBatch drive a real
//     LLMAdapter callback, parse the JSON-shaped LLM response
//     (tolerating ```json / ``` markdown fences), map the parsed
//     payload onto a TestCase, and run BasicTestCaseValidator.Validate
//     on the result before returning. GenerateBatch fans out up to
//     batchConcurrency Generate calls in parallel and returns either
//     the full slice or a partial slice + ErrPartialBatch wrapping
//     the first observed failure.
//
// No honest-stub surfaces remain in this package.
//
// Note: package name "testing" intentionally shadows stdlib testing —
// inside this package use stdtesting alias when needed; consumers
// import as digital.vasic.debate/testing.
package testing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Sentinel errors surfaced by the sandboxed executor.
var (
	// ErrUnsupportedLanguage is returned when Solution.Language is not
	// one of the languages the SandboxedTestExecutor knows how to
	// invoke (currently: go, bash/shell/sh, python).
	ErrUnsupportedLanguage = errors.New("debate/testing: unsupported language")
	// ErrSandboxSetup wraps tempdir / file-write / exec-lookup errors
	// surfaced during pre-spawn preparation. Callers should errors.Is
	// against this sentinel to distinguish setup failures from
	// child-process exit failures.
	ErrSandboxSetup = errors.New("debate/testing: sandbox setup failed")
	// ErrInsufficientResults is returned by
	// DifferentialContrastiveAnalyzer.Analyze when fewer than two
	// non-nil TestExecutionResults are supplied. Contrastive analysis
	// requires at least one pair to compare.
	ErrInsufficientResults = errors.New("debate/testing: insufficient results for contrastive analysis")
	// ErrInvalidName is returned by BasicTestCaseValidator.Validate
	// when the TestCase Description (treated as the case "name") is
	// empty after trim.
	ErrInvalidName = errors.New("debate/testing: invalid test case name")
	// ErrNoTestPayload is returned by BasicTestCaseValidator.Validate
	// when both Input and ExpectedOutput are nil.
	ErrNoTestPayload = errors.New("debate/testing: test case has no input or expected output")
	// ErrInvalidTimeout is returned by BasicTestCaseValidator.Validate
	// when TestCase.Timeout is set to a non-positive value or exceeds
	// one hour.
	ErrInvalidTimeout = errors.New("debate/testing: invalid test case timeout")
	// ErrInvalidCategory is returned by BasicTestCaseValidator.Validate
	// when TestCase.Category is set to a value outside the known
	// constants (CategoryFunctional, CategoryEdgeCase,
	// CategoryPerformance, CategorySecurity).
	ErrInvalidCategory = errors.New("debate/testing: invalid test case category")
	// ErrInvalidGenerateRequest is returned by LLMTestCaseGenerator.Generate
	// (and surfaced from GenerateBatch) when the supplied *GenerateRequest
	// is nil or carries an empty Topic — the generator cannot prompt the
	// LLM without a non-empty subject.
	ErrInvalidGenerateRequest = errors.New("debate/testing: invalid generate request")
	// ErrLLMOutputInvalid is returned when the LLMAdapter returned a
	// response that could not be parsed as the expected JSON shape, or
	// that parsed cleanly but failed BasicTestCaseValidator.Validate.
	// Callers should errors.Is against this sentinel to distinguish
	// "the model gave us junk" from transport / setup failures.
	ErrLLMOutputInvalid = errors.New("debate/testing: invalid LLM output")
	// ErrPartialBatch is returned by LLMTestCaseGenerator.GenerateBatch
	// when at least one — but not all — of the per-item Generate calls
	// failed. The returned slice contains the successful TestCases (so
	// callers can salvage partial work); the wrapped error identifies
	// the first observed failure.
	ErrPartialBatch = errors.New("debate/testing: partial batch failure")
	// ErrAdapterNotConfigured is returned by LLMTestCaseGenerator
	// methods when the receiver was constructed with a nil adapter (or
	// with a non-nil adapter whose Ask callback is nil). Without an
	// adapter the generator cannot contact an LLM and would otherwise
	// segfault — fail fast and loud instead.
	ErrAdapterNotConfigured = errors.New("debate/testing: LLM adapter not configured")
)

// maxTestCaseTimeout caps the per-case timeout the validator accepts.
// Anything greater is rejected as ErrInvalidTimeout — runaway timeouts
// indicate either a misconfiguration or an attempt to bypass the
// sandbox watchdog.
const maxTestCaseTimeout = time.Hour

// batchConcurrency caps the number of in-flight Generate calls a
// single GenerateBatch invocation will fan out. Empirical sweet spot
// for typical LLM endpoints — high enough to amortise round-trip
// latency, low enough to avoid tripping per-key rate limits.
const batchConcurrency = 4

// LLMAdapter is the canonical "ask an LLM" surface used by the
// test-case generator. It is a small struct wrapping a callback so
// that future enhancements (rate limits, retries, telemetry) can be
// added without breaking caller signatures.
type LLMAdapter struct {
	// Ask is the underlying callback. Supply a closure that performs
	// the real LLM HTTP call.
	Ask func(ctx context.Context, prompt string) (string, error)
}

// NewProviderAdapter constructs an LLMAdapter from a bare callback.
// Returned by value so callers can stack-allocate or take its address
// as needed.
func NewProviderAdapter(ask func(ctx context.Context, prompt string) (string, error)) LLMAdapter {
	return LLMAdapter{Ask: ask}
}

// Difficulty classifies the synthetic test cases the generator emits.
type Difficulty string

// Difficulty levels recognised by the generator.
const (
	DifficultyBasic        Difficulty = "basic"
	DifficultyIntermediate Difficulty = "intermediate"
	DifficultyAdvanced     Difficulty = "advanced"
)

// TestCategory classifies the kind of behaviour a test case exercises.
type TestCategory string

// Test categories recognised by the generator.
const (
	CategoryFunctional  TestCategory = "functional"
	CategoryEdgeCase    TestCategory = "edge_case"
	CategoryPerformance TestCategory = "performance"
	CategorySecurity    TestCategory = "security"
)

// GenerateRequest is the input payload for batch test-case synthesis.
type GenerateRequest struct {
	// AgentID identifies the requesting agent for audit/log purposes.
	AgentID string
	// TargetSolution is the code (or other artefact) under test.
	TargetSolution string
	// Language is the source language of TargetSolution.
	Language string
	// Context is free-form context (topic, debate prompt, etc.).
	Context string
	// Difficulty controls case complexity.
	Difficulty Difficulty
	// Categories selects which test categories to synthesise (used by
	// future batch-by-category flows).
	Categories []TestCategory
	// Topic is the short subject the LLM is asked to generate a case
	// for (e.g. "string reversal", "JSON parsing edge cases"). Required
	// by Generate / GenerateBatch — empty Topic yields
	// ErrInvalidGenerateRequest.
	Topic string
	// Category is the single category Generate stamps onto the
	// produced TestCase. Independent of Categories (the slice) so a
	// caller can drive single-case generation without populating both.
	Category TestCategory
	// ContextHints carries optional key/value tags the prompt builder
	// flattens into the prompt as "ContextHints: k1=v1, k2=v2". nil or
	// empty maps are omitted from the prompt.
	ContextHints map[string]string
}

// TestCase is a single synthetic test case produced by the generator.
type TestCase struct {
	// ID uniquely identifies the case within a batch.
	ID string
	// Name is the short LLM-supplied case name (e.g. "reverse-empty").
	// LLMTestCaseGenerator.Generate populates this from the parsed
	// LLM response; the validator mirrors it into Description when
	// Description is empty so downstream validation succeeds.
	Name string
	// Description is the human-readable purpose of the test. Used by
	// BasicTestCaseValidator as the "name" field (legacy: this struct
	// pre-dates the Name field above).
	Description string
	// Category is the kind of behaviour exercised.
	Category TestCategory
	// Difficulty mirrors GenerateRequest.Difficulty so consumers can
	// stratify a returned batch without re-correlating with the
	// originating request.
	Difficulty Difficulty
	// Input is the input payload supplied to the executor.
	Input interface{}
	// ExpectedOutput is the canonical reference output (if any).
	ExpectedOutput interface{}
	// Notes is the optional free-form reasoning the LLM supplied
	// alongside the case (the "notes" field of the parsed JSON
	// response). Empty when the model did not include it.
	Notes string
	// Timeout, if non-zero, caps the per-case wall-clock budget the
	// executor should honour for this case. Validated by
	// BasicTestCaseValidator: must be > 0 and <= maxTestCaseTimeout
	// (1 hour) when set.
	Timeout time.Duration
	// Metadata carries free-form tags propagated by the generator.
	Metadata map[string]interface{}
}

// TestExecutionResult is the outcome of executing one TestCase.
type TestExecutionResult struct {
	// TestCaseID echoes the originating TestCase.ID.
	TestCaseID string
	// Passed is true iff the executor judged the test case a pass.
	// "Pass" means: child exit code == 0 AND (if ExpectedOutput is set)
	// captured Output equals the expected output after trim.
	Passed bool
	// Output is the captured stdout (combined with stderr when the
	// child fails so the operator can see what went wrong).
	Output string
	// Stderr is the captured stderr stream, kept separate for callers
	// that want to surface it independently of stdout.
	Stderr string
	// Error is the human-readable failure reason (empty on pass).
	Error string
	// ExitCode is the child process exit status. -1 indicates the
	// child was terminated by signal before producing an exit code
	// (timeout, ctx cancel, OOM kill, CPU rlimit kill, …).
	ExitCode int
	// Duration is the wall-clock time the case consumed.
	Duration time.Duration
	// LimitsEnforced reports whether kernel-level RLIMIT_AS / RLIMIT_CPU
	// caps were actually installed for this run. True on Linux; false
	// on non-Linux dev builds where the executor still spawns the
	// child honestly but without kernel isolation.
	LimitsEnforced bool
	// TimedOut is true if the run was terminated by the executor's
	// timeout / ctx-cancel watchdog rather than exiting on its own.
	TimedOut bool
}

// Solution is the code-under-test handed to the executor alongside
// each TestCase.
type Solution struct {
	// ID identifies the solution (e.g. the debate-result ID).
	ID string
	// AgentID identifies the agent that produced the solution.
	AgentID string
	// Language is the source language of Code.
	Language string
	// Code is the source under test.
	Code string
	// Description is the human-readable summary of the solution.
	Description string
}

// Option configures the executor at construction time.
type Option func(*executorOptions)

type executorOptions struct {
	timeout     time.Duration
	memoryLimit int64
	cpuLimit    float64
}

// WithTimeout caps per-test wall-clock time.
func WithTimeout(d time.Duration) Option {
	return func(o *executorOptions) { o.timeout = d }
}

// WithMemoryLimit caps per-test RSS in bytes.
func WithMemoryLimit(b int64) Option {
	return func(o *executorOptions) { o.memoryLimit = b }
}

// WithCPULimit caps per-test CPU usage as a fractional core count
// (e.g. 2.0 == two full cores; 0.5 == half a core).
func WithCPULimit(cores float64) Option {
	return func(o *executorOptions) { o.cpuLimit = cores }
}

// LLMTestCaseGenerator synthesises test cases by calling out to a real
// LLMAdapter, parsing the returned JSON, and validating the produced
// case with a BasicTestCaseValidator. The adapter pointer may be nil
// at construction time, but every later Generate / GenerateBatch call
// will then short-circuit with ErrAdapterNotConfigured.
type LLMTestCaseGenerator struct {
	adapter   *LLMAdapter
	validator *BasicTestCaseValidator
}

// NewLLMTestCaseGenerator constructs a generator bound to the adapter
// pointer and validator. The validator may be nil; callers that pass
// nil opt out of post-synthesis sanity checks (Generate still maps
// LLM output onto a TestCase but does not reject malformed cases).
// The adapter may also be nil at construction time, but every later
// Generate / GenerateBatch call will then return
// ErrAdapterNotConfigured.
func NewLLMTestCaseGenerator(adapter *LLMAdapter, validator *BasicTestCaseValidator) *LLMTestCaseGenerator {
	return &LLMTestCaseGenerator{adapter: adapter, validator: validator}
}

// llmTestCaseResponse is the JSON shape the generator expects the LLM
// to return. Field tags use snake_case so the model can match common
// JSON conventions; the parser is tolerant of extra fields.
type llmTestCaseResponse struct {
	Name           string `json:"name"`
	Input          string `json:"input"`
	ExpectedOutput string `json:"expected_output"`
	Notes          string `json:"notes"`
}

// Generate produces a single test case by prompting the configured
// LLMAdapter with a structured request, parsing the JSON response,
// mapping it onto a TestCase, and (if a validator is configured)
// validating the result before returning it.
//
// Failure modes (each wraps a documented sentinel):
//   - nil req OR empty req.Topic → ErrInvalidGenerateRequest.
//   - nil adapter OR adapter.Ask == nil → ErrAdapterNotConfigured.
//   - adapter.Ask returns error → that error is returned wrapped with
//     %w so callers can errors.Is the underlying adapter error.
//   - response not parseable as JSON (after fence-stripping) →
//     ErrLLMOutputInvalid.
//   - validator.Validate fails → ErrLLMOutputInvalid wrapping the
//     validator's diagnostic.
//   - ctx cancelled at any point → ctx.Err() propagated promptly.
func (g *LLMTestCaseGenerator) Generate(ctx context.Context, req *GenerateRequest) (*TestCase, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrInvalidGenerateRequest)
	}
	if strings.TrimSpace(req.Topic) == "" {
		return nil, fmt.Errorf("%w: empty Topic", ErrInvalidGenerateRequest)
	}
	if g.adapter == nil || g.adapter.Ask == nil {
		return nil, fmt.Errorf("%w: nil adapter or Ask callback", ErrAdapterNotConfigured)
	}

	prompt := buildGeneratePrompt(req)

	// Honour ctx before the (potentially long) Ask call so a
	// pre-cancelled context fails fast rather than spinning up the
	// adapter.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := g.adapter.Ask(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("debate/testing: adapter.Ask failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cleaned := stripJSONFences(raw)
	var parsed llmTestCaseResponse
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, fmt.Errorf("%w: json.Unmarshal: %v (raw=%q)",
			ErrLLMOutputInvalid, err, truncate(raw, 256))
	}

	name := strings.TrimSpace(parsed.Name)
	if name == "" {
		// Fallback so the validator's name-non-empty rule does not
		// always reject otherwise-valid LLM output that happens to
		// omit the name.
		name = deriveFallbackName(req)
	}

	tc := &TestCase{
		Name:           name,
		Description:    name, // BasicTestCaseValidator inspects Description
		Input:          parsed.Input,
		ExpectedOutput: parsed.ExpectedOutput,
		Notes:          parsed.Notes,
		Category:       req.Category,
		Difficulty:     req.Difficulty,
	}

	if g.validator != nil {
		if err := g.validator.Validate(ctx, tc); err != nil {
			return nil, fmt.Errorf("%w: validator rejected case: %v",
				ErrLLMOutputInvalid, err)
		}
	}

	return tc, nil
}

// GenerateBatch produces up to `count` test cases for the supplied
// request, fanning out parallel Generate calls bounded by
// batchConcurrency.
//
// Returns:
//   - (nil, ctx.Err()) if ctx is already cancelled.
//   - (nil, ErrInvalidGenerateRequest) if count <= 0 OR the request is
//     malformed (nil / empty Topic).
//   - (nil, ErrAdapterNotConfigured) if the adapter is missing.
//   - ([]*TestCase, nil) on full success — len == count.
//   - (partial slice, ErrPartialBatch wrapping the FIRST observed
//     failure) when at least one Generate call failed but at least one
//     succeeded. The partial slice preserves request order
//     (successful slot i appears before successful slot j if i < j).
//   - (nil, ErrPartialBatch wrapping the first failure) when every
//     Generate call failed.
//
// ctx cancellation: the dispatcher checks ctx between launches and the
// goroutines themselves honour the shared ctx (via Generate's own
// ctx.Err() guards), so cancellation is observed promptly.
func (g *LLMTestCaseGenerator) GenerateBatch(ctx context.Context, req *GenerateRequest, count int) ([]*TestCase, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if count <= 0 {
		return nil, fmt.Errorf("%w: count must be > 0, got %d",
			ErrInvalidGenerateRequest, count)
	}
	// Validate request once up-front so we don't spin N goroutines just
	// to have them all fail identically.
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrInvalidGenerateRequest)
	}
	if strings.TrimSpace(req.Topic) == "" {
		return nil, fmt.Errorf("%w: empty Topic", ErrInvalidGenerateRequest)
	}
	if g.adapter == nil || g.adapter.Ask == nil {
		return nil, fmt.Errorf("%w: nil adapter or Ask callback", ErrAdapterNotConfigured)
	}

	type slot struct {
		tc  *TestCase
		err error
	}

	results := make([]slot, count)
	sem := make(chan struct{}, batchConcurrency)
	var wg sync.WaitGroup

	for i := 0; i < count; i++ {
		if err := ctx.Err(); err != nil {
			// Drain any already-running goroutines before returning.
			wg.Wait()
			return nil, err
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			tc, err := g.Generate(ctx, req)
			results[i] = slot{tc: tc, err: err}
		}(i)
	}
	wg.Wait()

	// Collect successes (preserving request order) and the first error
	// (also in request order so the message is deterministic).
	var successes []*TestCase
	var firstErr error
	for _, s := range results {
		if s.err != nil {
			if firstErr == nil {
				firstErr = s.err
			}
			continue
		}
		successes = append(successes, s.tc)
	}

	if firstErr == nil {
		return successes, nil
	}
	// Partial OR total failure — caller can tell from len(successes).
	// Wrap BOTH ErrPartialBatch and the underlying first error so that
	// errors.Is matches against either sentinel — operators care about
	// ErrPartialBatch ("not full success") while integration code may
	// also care about the underlying transport / parser sentinel.
	if len(successes) == 0 {
		return nil, fmt.Errorf("%w: 0/%d succeeded, first error: %w",
			ErrPartialBatch, count, firstErr)
	}
	return successes, fmt.Errorf("%w: %d/%d succeeded, first error: %w",
		ErrPartialBatch, len(successes), count, firstErr)
}

// buildGeneratePrompt renders the user-facing prompt the generator
// sends to the LLM. Format is documented inline so future tweaks
// preserve backwards-compatibility with downstream model fine-tunes.
func buildGeneratePrompt(req *GenerateRequest) string {
	var b strings.Builder
	b.WriteString("Generate a single test case for the following:\n")
	b.WriteString("Topic: ")
	b.WriteString(req.Topic)
	b.WriteByte('\n')
	if req.Language != "" {
		b.WriteString("Language: ")
		b.WriteString(req.Language)
		b.WriteByte('\n')
	}
	if req.Difficulty != "" {
		b.WriteString("Difficulty: ")
		b.WriteString(string(req.Difficulty))
		b.WriteByte('\n')
	}
	if req.Category != "" {
		b.WriteString("Category: ")
		b.WriteString(string(req.Category))
		b.WriteByte('\n')
	}
	if len(req.ContextHints) > 0 {
		b.WriteString("ContextHints: ")
		b.WriteString(flattenHints(req.ContextHints))
		b.WriteByte('\n')
	}
	b.WriteString("\nReturn ONLY a JSON object with fields:\n")
	b.WriteString(`{"name":"<short test name>","input":"<input>","expected_output":"<expected>","notes":"<optional reasoning>"}`)
	b.WriteByte('\n')
	return b.String()
}

// flattenHints renders the ContextHints map as "k1=v1, k2=v2" with
// keys sorted ascending for determinism (so the same request produces
// the same prompt across runs, which matters for prompt-caching).
func flattenHints(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Tiny inline insertion sort to avoid pulling sort just for this —
	// n is expected to be small (<10 hints).
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, ", ")
}

// stripJSONFences removes optional ```json … ``` (or bare ``` … ```)
// markdown fences from the LLM response. Many local models like to
// wrap JSON in fences even when explicitly told not to; this keeps
// the parser happy without resorting to a regex.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	// Drop the opening fence line ("```" or "```json" etc.).
	nl := strings.IndexByte(s, '\n')
	if nl < 0 {
		return s
	}
	s = s[nl+1:]
	// Drop a trailing closing fence if present (with or without
	// trailing whitespace / newlines after it).
	s = strings.TrimRight(s, " \t\r\n")
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

// deriveFallbackName builds a deterministic name from the request when
// the LLM omitted the name field. Format: "<topic-slug>-<difficulty>"
// truncated to 64 chars so it remains a sensible Description value.
func deriveFallbackName(req *GenerateRequest) string {
	base := strings.ToLower(strings.TrimSpace(req.Topic))
	base = strings.ReplaceAll(base, " ", "-")
	if req.Difficulty != "" {
		base = base + "-" + string(req.Difficulty)
	}
	if len(base) > 64 {
		base = base[:64]
	}
	if base == "" {
		base = "generated-case"
	}
	return base
}

// SandboxedTestExecutor runs generated test cases under resource caps.
type SandboxedTestExecutor struct {
	opts executorOptions
}

// NewSandboxedTestExecutor constructs an executor with the supplied opts.
func NewSandboxedTestExecutor(opts ...Option) *SandboxedTestExecutor {
	o := executorOptions{timeout: 10 * time.Second}
	for _, fn := range opts {
		fn(&o)
	}
	return &SandboxedTestExecutor{opts: o}
}

// Execute runs the supplied test case against the supplied solution in
// a sandbox and returns the result. Real implementation: spawns a real
// child process via os/exec, captures stdout/stderr/exit-code/wall-time,
// enforces RLIMIT_AS + RLIMIT_CPU on Linux (no-op on other platforms,
// with LimitsEnforced=false in the returned result), honours both ctx
// and the executor's configured Timeout (whichever fires first), and
// cleans up the tempdir on the way out.
//
// Pass/fail logic:
//   - Setup error (tempdir, file write, unsupported language) →
//     non-nil error return; result is nil.
//   - Child spawn error → non-nil error wrapping ErrSandboxSetup.
//   - Child ran but exited non-zero or was killed → result.Passed=false,
//     result.Error populated, nil top-level error.
//   - Child exit 0 + (no ExpectedOutput OR captured output matches
//     ExpectedOutput after trim) → result.Passed=true.
func (e *SandboxedTestExecutor) Execute(ctx context.Context, testCase *TestCase, solution *Solution) (*TestExecutionResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if testCase == nil {
		return nil, fmt.Errorf("%w: nil test case", ErrSandboxSetup)
	}
	if solution == nil {
		return nil, fmt.Errorf("%w: nil solution", ErrSandboxSetup)
	}

	lang := strings.ToLower(strings.TrimSpace(solution.Language))
	if !isSupportedLanguage(lang) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedLanguage, solution.Language)
	}

	// Build the sandbox tempdir under os.TempDir(). Mode 0700 so other
	// users on the host cannot read source-under-test.
	tempDir, err := os.MkdirTemp("", "debateorch-sandbox-")
	if err != nil {
		return nil, fmt.Errorf("%w: mkdir temp: %v", ErrSandboxSetup, err)
	}
	if err := os.Chmod(tempDir, 0o700); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("%w: chmod temp: %v", ErrSandboxSetup, err)
	}
	defer os.RemoveAll(tempDir)

	scriptPath, args, err := materialiseScript(tempDir, lang, solution.Code)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSandboxSetup, err)
	}

	// Apply the executor timeout on top of ctx. Whichever fires first
	// terminates the child via os/exec's Cancel hook (which we set to
	// killProcessGroup on linux + the default Cmd cancellation
	// elsewhere).
	runCtx := ctx
	var cancel context.CancelFunc
	if e.opts.timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, e.opts.timeout)
		defer cancel()
	}

	memBytes := e.opts.memoryLimit
	cpuSeconds := int64(0)
	if e.opts.cpuLimit > 0 {
		// Round UP so fractional-core requests still get >= 1 second
		// of CPU time before RLIMIT_CPU bites. Sub-second caps are
		// not expressible via RLIMIT_CPU (seconds granularity).
		cpuSeconds = int64(math.Ceil(e.opts.cpuLimit))
		if cpuSeconds < 1 {
			cpuSeconds = 1
		}
	}

	// On Linux, wrap with prlimit so rlimits apply to the child only
	// (never the parent). On other platforms or when prlimit is
	// unavailable, returns argv unchanged with enforced=false. The
	// returned enforced flag is recorded on the result so callers can
	// detect missing kernel-level isolation without inspecting GOOS.
	finalArgv, rlimitsEnforced := wrapWithRlimits(args, memBytes, cpuSeconds)

	cmd := exec.CommandContext(runCtx, finalArgv[0], finalArgv[1:]...)
	cmd.Dir = tempDir
	cmd.Env = append(os.Environ(), "SANDBOX_SCRIPT="+scriptPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	prepareSandboxAttr(cmd)

	// CommandContext default cancel sends os.Kill (SIGKILL) to the
	// process itself; we override so the entire pgrp dies on linux. On
	// non-linux this is a no-op and we fall back to the default.
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			killProcessGroup(cmd.Process.Pid)
			return cmd.Process.Kill()
		}
		return nil
	}

	start := time.Now()
	runErr := cmd.Run()
	duration := time.Since(start)

	// LimitsEnforced semantics: report the actual enforcement state of
	// THIS run. True iff a cap was requested AND the wrapper applied
	// it successfully. A run with no caps requested returns
	// LimitsEnforced=false regardless of platform — callers shouldn't
	// infer "isolated" from a no-op configuration.
	result := &TestExecutionResult{
		TestCaseID:     testCase.ID,
		Output:         stdout.String(),
		Stderr:         stderr.String(),
		Duration:       duration,
		LimitsEnforced: rlimitsEnforced,
	}

	// Decode the run outcome.
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	} else {
		result.ExitCode = -1
	}

	// Timeout / ctx-cancel detection.
	if runCtx.Err() != nil {
		result.TimedOut = true
	}

	if runErr != nil {
		// Distinguish "process couldn't be started" (setup-class) from
		// "process exited non-zero / was signalled" (test-failure
		// class). os/exec returns *exec.ExitError for the latter.
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			result.Passed = false
			if result.TimedOut {
				result.Error = fmt.Sprintf("terminated by timeout/ctx cancel after %s", duration)
			} else {
				result.Error = fmt.Sprintf("exit code %d: %s", result.ExitCode, truncate(stderr.String(), 512))
			}
			return result, nil
		}
		// Start() failure or other non-ExitError → genuine setup
		// failure.
		return nil, fmt.Errorf("%w: %v", ErrSandboxSetup, runErr)
	}

	// Clean exit 0: optionally compare against ExpectedOutput.
	if expected, ok := stringExpectedOutput(testCase.ExpectedOutput); ok {
		got := strings.TrimRight(stdout.String(), "\r\n")
		want := strings.TrimRight(expected, "\r\n")
		if got == want {
			result.Passed = true
		} else {
			result.Passed = false
			result.Error = fmt.Sprintf("output mismatch: want %q, got %q", want, got)
		}
	} else {
		result.Passed = true
	}

	return result, nil
}

// isSupportedLanguage returns true iff materialiseScript knows how to
// turn solution code in the supplied language into a child-process
// invocation. Centralised so callers and tests share the same source
// of truth.
func isSupportedLanguage(lang string) bool {
	switch lang {
	case "go", "golang", "bash", "shell", "sh", "python", "python3", "py":
		return true
	}
	return false
}

// materialiseScript writes solution code into tempDir using the
// canonical filename for the language and returns the full path + the
// argv to spawn. Caller is responsible for tempDir cleanup.
func materialiseScript(tempDir, lang, code string) (string, []string, error) {
	switch lang {
	case "go", "golang":
		path := filepath.Join(tempDir, "main.go")
		if err := os.WriteFile(path, []byte(code), 0o600); err != nil {
			return "", nil, fmt.Errorf("write main.go: %w", err)
		}
		return path, []string{"go", "run", path}, nil
	case "bash", "shell", "sh":
		path := filepath.Join(tempDir, "script.sh")
		if err := os.WriteFile(path, []byte(code), 0o700); err != nil {
			return "", nil, fmt.Errorf("write script.sh: %w", err)
		}
		return path, []string{"bash", path}, nil
	case "python", "python3", "py":
		path := filepath.Join(tempDir, "script.py")
		if err := os.WriteFile(path, []byte(code), 0o600); err != nil {
			return "", nil, fmt.Errorf("write script.py: %w", err)
		}
		return path, []string{"python3", path}, nil
	}
	return "", nil, fmt.Errorf("%w: %q", ErrUnsupportedLanguage, lang)
}

// stringExpectedOutput extracts a string from TestCase.ExpectedOutput
// when the case author supplied one as a plain string. Non-string
// values (numeric, struct, nil) yield ok=false → caller treats the
// case as "exit 0 is sufficient, no output assertion".
func stringExpectedOutput(v interface{}) (string, bool) {
	if v == nil {
		return "", false
	}
	if s, ok := v.(string); ok {
		return s, true
	}
	return "", false
}

// truncate caps a string at n bytes, appending an ellipsis marker so
// callers can tell the result was clipped. Used for embedding stderr
// snippets in Result.Error without blowing up the response size when
// the child screamed at length.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…[truncated]"
}

// ContrastiveAnalyzerConfig configures DifferentialContrastiveAnalyzer.
// Threshold tunes the ConsistencyScore floor below which the analyser
// flags a "low-consistency" advisory note; Verbose toggles detailed
// per-pair Notes entries in the produced report.
type ContrastiveAnalyzerConfig struct {
	// Threshold is the ConsistencyScore floor (0.0–1.0). Reports with
	// score < Threshold receive a "low-consistency" Notes entry. Zero
	// disables the advisory (no Notes entry from this rule).
	Threshold float64
	// Verbose toggles inclusion of per-pair diagnostic lines in
	// Notes. Off by default to keep reports tight in batch runs.
	Verbose bool
}

// DivergencePair identifies a pair of result indices that disagreed on
// some axis, along with a short detail string describing what differed.
type DivergencePair struct {
	// I is the lower index of the pair (0 <= I < J).
	I int
	// J is the higher index of the pair.
	J int
	// Detail is a short human-readable description of the divergence
	// (e.g. "exit codes 0 vs 1" or "stdout differs (8 vs 12 bytes)").
	Detail string
}

// ContrastiveReport is the structured output of
// DifferentialContrastiveAnalyzer.Analyze. Field semantics:
//   - ResultCount: number of TestExecutionResults considered.
//   - AllAgreeOnExitCode / AllAgreeOnStdout / AllAgreeOnStderr: true
//     iff every pair agreed on that axis.
//   - OutputDivergence / ExitCodeDivergence: enumerated pairs that
//     disagreed on the named axis.
//   - ConsistencyScore: 1.0 - (divergent_pairs / total_pairs) where a
//     pair is "divergent" if it disagreed on ANY of exit code / stdout
//     / stderr. 1.0 == perfect agreement; 0.0 == every pair disagreed
//     on at least one axis.
//   - Notes: free-form advisory strings (verbose details,
//     low-consistency warning, etc.).
type ContrastiveReport struct {
	ResultCount         int
	AllAgreeOnExitCode  bool
	AllAgreeOnStdout    bool
	AllAgreeOnStderr    bool
	OutputDivergence    []DivergencePair
	ExitCodeDivergence  []DivergencePair
	StderrDivergence    []DivergencePair
	ConsistencyScore    float64
	Notes               []string
}

// DifferentialContrastiveAnalyzer compares multiple
// TestExecutionResults (from running the same test case across
// different implementations / solutions) and surfaces behavioural
// drift in the form of a structured ContrastiveReport.
type DifferentialContrastiveAnalyzer struct {
	cfg *ContrastiveAnalyzerConfig
}

// NewDifferentialContrastiveAnalyzer constructs an analyser. Pass nil
// to use zero-valued defaults (Threshold=0, Verbose=false).
func NewDifferentialContrastiveAnalyzer(cfg *ContrastiveAnalyzerConfig) *DifferentialContrastiveAnalyzer {
	return &DifferentialContrastiveAnalyzer{cfg: cfg}
}

// Analyze compares the supplied TestExecutionResults pairwise and
// returns a ContrastiveReport summarising agreement on exit code,
// stdout, and stderr.
//
// Validation:
//   - results must contain at least 2 entries → else ErrInsufficientResults.
//   - every result must be non-nil → else ErrInsufficientResults
//     (a nil result cannot participate in comparison).
//
// Semantics:
//   - Stdout / Stderr comparison is exact byte equality (case-sensitive).
//   - ExitCode comparison is integer equality.
//   - ConsistencyScore = 1.0 - (divergent_pairs / total_pairs), where
//     total_pairs = n*(n-1)/2 and divergent_pairs counts pairs that
//     disagreed on ANY of the three axes.
//   - ctx.Done() is honoured between pair comparisons; returns ctx.Err()
//     if cancellation is observed.
func (a *DifferentialContrastiveAnalyzer) Analyze(ctx context.Context, results []*TestExecutionResult) (*ContrastiveReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(results) < 2 {
		return nil, fmt.Errorf("%w: got %d, need >= 2", ErrInsufficientResults, len(results))
	}
	for i, r := range results {
		if r == nil {
			return nil, fmt.Errorf("%w: results[%d] is nil", ErrInsufficientResults, i)
		}
	}

	report := &ContrastiveReport{
		ResultCount:        len(results),
		AllAgreeOnExitCode: true,
		AllAgreeOnStdout:   true,
		AllAgreeOnStderr:   true,
	}

	totalPairs := len(results) * (len(results) - 1) / 2
	divergentPairs := 0

	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			ri, rj := results[i], results[j]
			divergent := false

			if ri.ExitCode != rj.ExitCode {
				report.AllAgreeOnExitCode = false
				report.ExitCodeDivergence = append(report.ExitCodeDivergence, DivergencePair{
					I: i, J: j,
					Detail: fmt.Sprintf("exit codes %d vs %d", ri.ExitCode, rj.ExitCode),
				})
				divergent = true
			}
			if ri.Output != rj.Output {
				report.AllAgreeOnStdout = false
				report.OutputDivergence = append(report.OutputDivergence, DivergencePair{
					I: i, J: j,
					Detail: fmt.Sprintf("stdout differs (%d vs %d bytes)", len(ri.Output), len(rj.Output)),
				})
				divergent = true
			}
			if ri.Stderr != rj.Stderr {
				report.AllAgreeOnStderr = false
				report.StderrDivergence = append(report.StderrDivergence, DivergencePair{
					I: i, J: j,
					Detail: fmt.Sprintf("stderr differs (%d vs %d bytes)", len(ri.Stderr), len(rj.Stderr)),
				})
				divergent = true
			}
			if divergent {
				divergentPairs++
				if a.cfg != nil && a.cfg.Verbose {
					report.Notes = append(report.Notes,
						fmt.Sprintf("pair (%d,%d) diverged", i, j))
				}
			}
		}
	}

	if totalPairs > 0 {
		report.ConsistencyScore = 1.0 - float64(divergentPairs)/float64(totalPairs)
	} else {
		report.ConsistencyScore = 1.0
	}

	if a.cfg != nil && a.cfg.Threshold > 0 && report.ConsistencyScore < a.cfg.Threshold {
		report.Notes = append(report.Notes,
			fmt.Sprintf("low-consistency: score %.3f below threshold %.3f",
				report.ConsistencyScore, a.cfg.Threshold))
	}

	return report, nil
}

// BasicTestCaseValidator runs deterministic sanity checks over a
// TestCase definition. It is intentionally lightweight: it does NOT
// execute the case, only inspects its fields.
type BasicTestCaseValidator struct{}

// NewBasicTestCaseValidator constructs a validator. The validator is
// stateless; the constructor exists for API symmetry with the rest of
// the package.
func NewBasicTestCaseValidator() *BasicTestCaseValidator {
	return &BasicTestCaseValidator{}
}

// Validate inspects the test case and returns the first validation
// error encountered, or nil if all checks pass.
//
// Rules (checked in order):
//  1. ctx not cancelled → else ctx.Err().
//  2. testCase non-nil → else ErrInvalidName (a nil case has no name).
//  3. Description (used as the "name" for now; TestCase has no
//     dedicated Name field) non-empty after trim → else ErrInvalidName.
//  4. At least one of Input or ExpectedOutput non-nil → else
//     ErrNoTestPayload.
//  5. If Timeout != 0: must be > 0 AND <= maxTestCaseTimeout (1 hour) →
//     else ErrInvalidTimeout.
//  6. If Category != "": must be one of the known TestCategory
//     constants → else ErrInvalidCategory.
func (v *BasicTestCaseValidator) Validate(ctx context.Context, testCase *TestCase) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if testCase == nil {
		return fmt.Errorf("%w: nil test case", ErrInvalidName)
	}
	if strings.TrimSpace(testCase.Description) == "" {
		return fmt.Errorf("%w: description is empty", ErrInvalidName)
	}
	if testCase.Input == nil && testCase.ExpectedOutput == nil {
		return fmt.Errorf("%w: both Input and ExpectedOutput are nil", ErrNoTestPayload)
	}
	if testCase.Timeout != 0 {
		if testCase.Timeout <= 0 {
			return fmt.Errorf("%w: timeout must be > 0, got %s",
				ErrInvalidTimeout, testCase.Timeout)
		}
		if testCase.Timeout > maxTestCaseTimeout {
			return fmt.Errorf("%w: timeout %s exceeds max %s",
				ErrInvalidTimeout, testCase.Timeout, maxTestCaseTimeout)
		}
	}
	if testCase.Category != "" && !isKnownCategory(testCase.Category) {
		return fmt.Errorf("%w: %q", ErrInvalidCategory, testCase.Category)
	}
	return nil
}

// isKnownCategory returns true iff cat is one of the four registered
// TestCategory constants. Centralised so Validate and any future
// category-aware code share a single source of truth.
func isKnownCategory(cat TestCategory) bool {
	switch cat {
	case CategoryFunctional, CategoryEdgeCase, CategoryPerformance, CategorySecurity:
		return true
	}
	return false
}
