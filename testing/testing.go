// Package testing hosts the LLM-driven test-case generator, sandboxed
// executor, and contrastive analyser. The SandboxedTestExecutor.Execute
// method is the production implementation: it spawns real child
// processes via os/exec, captures stdout/stderr/exit-code/wall-time,
// and (on Linux) enforces RLIMIT_AS + RLIMIT_CPU via withRlimits.
// The remaining LLM-driven surfaces (LLMTestCaseGenerator,
// DifferentialContrastiveAnalyzer, BasicTestCaseValidator) are honest
// NotYetImplemented stubs tracked in RECONSTRUCTION_ROADMAP.md.
//
// Note: package name "testing" intentionally shadows stdlib testing —
// inside this package use stdtesting alias when needed; consumers
// import as digital.vasic.debate/testing.
package testing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
)

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
	// Categories selects which test categories to synthesise.
	Categories []TestCategory
}

// TestCase is a single synthetic test case produced by the generator.
type TestCase struct {
	// ID uniquely identifies the case within a batch.
	ID string
	// Description is the human-readable purpose of the test.
	Description string
	// Category is the kind of behaviour exercised.
	Category TestCategory
	// Input is the input payload supplied to the executor.
	Input interface{}
	// ExpectedOutput is the canonical reference output (if any).
	ExpectedOutput interface{}
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

// LLMTestCaseGenerator synthesises test cases via an LLMAdapter,
// validating the produced cases with a BasicTestCaseValidator.
type LLMTestCaseGenerator struct {
	adapter   LLMAdapter
	validator *BasicTestCaseValidator
}

// NewLLMTestCaseGenerator constructs a generator bound to the adapter
// and validator. The validator may be nil; callers that pass nil opt
// out of post-synthesis sanity checks.
func NewLLMTestCaseGenerator(adapter LLMAdapter, validator *BasicTestCaseValidator) *LLMTestCaseGenerator {
	return &LLMTestCaseGenerator{adapter: adapter, validator: validator}
}

// Generate produces a single test case for the supplied target description.
func (g *LLMTestCaseGenerator) Generate(ctx context.Context, target string) (*TestCase, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = target
	return nil, errors.New("debate/testing: LLMTestCaseGenerator.Generate NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// GenerateBatch produces `count` test cases for the supplied request.
func (g *LLMTestCaseGenerator) GenerateBatch(ctx context.Context, req *GenerateRequest, count int) ([]*TestCase, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _ = req, count
	return nil, errors.New("debate/testing: LLMTestCaseGenerator.GenerateBatch NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
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
// The current implementation accepts the value but ignores all fields
// until the real analyser lands (tracked in RECONSTRUCTION_ROADMAP.md).
type ContrastiveAnalyzerConfig struct {
	// Threshold is the minimum drift score considered significant.
	Threshold float64
	// Verbose toggles detailed diagnostic output.
	Verbose bool
}

// DifferentialContrastiveAnalyzer compares paired outputs to surface
// behavioural drift.
type DifferentialContrastiveAnalyzer struct {
	cfg *ContrastiveAnalyzerConfig
}

// NewDifferentialContrastiveAnalyzer constructs an analyser. Pass nil
// to use defaults.
func NewDifferentialContrastiveAnalyzer(cfg *ContrastiveAnalyzerConfig) *DifferentialContrastiveAnalyzer {
	return &DifferentialContrastiveAnalyzer{cfg: cfg}
}

// Analyze compares baseline vs candidate outputs.
func (a *DifferentialContrastiveAnalyzer) Analyze(ctx context.Context, baseline, candidate interface{}) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_, _ = baseline, candidate
	return nil, errors.New("debate/testing: DifferentialContrastiveAnalyzer.Analyze NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// BasicTestCaseValidator runs lightweight sanity checks over a case.
type BasicTestCaseValidator struct{}

// NewBasicTestCaseValidator constructs a validator.
func NewBasicTestCaseValidator() *BasicTestCaseValidator {
	return &BasicTestCaseValidator{}
}

// Validate inspects the test case and returns a diagnostic blob.
func (v *BasicTestCaseValidator) Validate(ctx context.Context, testCase *TestCase) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = testCase
	return nil, errors.New("debate/testing: BasicTestCaseValidator.Validate NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}
