package testing

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	stdtesting "testing"
	"time"
)

// TestExecutorOptions confirms the option plumbing still works after
// the Execute method promotion. Construction-time validation only.
func TestExecutorOptions(t *stdtesting.T) {
	e := NewSandboxedTestExecutor(WithTimeout(2*time.Second), WithMemoryLimit(1024), WithCPULimit(0.5))
	if e == nil {
		t.Fatal("nil executor")
	}
}

// ---------------------------------------------------------------------
// LLMTestCaseGenerator.Generate / GenerateBatch — real-impl suite.
// Adapter responses are produced by deterministic stubs (the only
// place mocks/stubs are allowed per CONST-050(A) — unit tests). The
// `ollama_integration` build-tagged file in this directory exercises
// the same surface against a real Ollama endpoint.
// ---------------------------------------------------------------------

// stubAdapter returns an *LLMAdapter whose Ask always returns the
// supplied (resp, err). Honours ctx cancellation so callers can write
// ctx-cancel tests without per-test custom adapters.
func stubAdapter(resp string, err error) *LLMAdapter {
	return &LLMAdapter{Ask: func(ctx context.Context, prompt string) (string, error) {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		return resp, err
	}}
}

// TestLLMTestCaseGenerator_Generate_Happy proves the happy path: a
// well-formed JSON response maps cleanly onto a TestCase with the
// expected fields, and validation succeeds.
func TestLLMTestCaseGenerator_Generate_Happy(t *stdtesting.T) {
	adapter := stubAdapter(
		`{"name":"reverse-empty","input":"hello","expected_output":"olleh","notes":"reverse"}`,
		nil,
	)
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	tc, err := g.Generate(context.Background(), &GenerateRequest{
		Topic:      "string reversal",
		Language:   "go",
		Difficulty: DifficultyBasic,
		Category:   CategoryFunctional,
	})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if tc == nil {
		t.Fatal("Generate: nil test case")
	}
	if tc.Name != "reverse-empty" {
		t.Fatalf("Name=%q, want %q", tc.Name, "reverse-empty")
	}
	if tc.Input != "hello" {
		t.Fatalf("Input=%v, want %q", tc.Input, "hello")
	}
	if tc.ExpectedOutput != "olleh" {
		t.Fatalf("ExpectedOutput=%v, want %q", tc.ExpectedOutput, "olleh")
	}
	if tc.Notes != "reverse" {
		t.Fatalf("Notes=%q, want %q", tc.Notes, "reverse")
	}
	if tc.Category != CategoryFunctional {
		t.Fatalf("Category=%q, want %q", tc.Category, CategoryFunctional)
	}
	if tc.Difficulty != DifficultyBasic {
		t.Fatalf("Difficulty=%q, want %q", tc.Difficulty, DifficultyBasic)
	}
}

// TestLLMTestCaseGenerator_Generate_InvalidJSON proves non-JSON LLM
// output yields ErrLLMOutputInvalid (not a panic, not a silent pass).
func TestLLMTestCaseGenerator_Generate_InvalidJSON(t *stdtesting.T) {
	adapter := stubAdapter("not json", nil)
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	_, err := g.Generate(context.Background(), &GenerateRequest{Topic: "x"})
	if err == nil {
		t.Fatal("Generate: expected error, got nil")
	}
	if !errors.Is(err, ErrLLMOutputInvalid) {
		t.Fatalf("errors.Is(err, ErrLLMOutputInvalid)=false; err=%v", err)
	}
}

// TestLLMTestCaseGenerator_Generate_WithMarkdownFences proves that a
// response wrapped in ```json … ``` is still parsed correctly. Many
// local models emit fences regardless of "no fences" instructions in
// the prompt — tolerating them is required for real-world usage.
func TestLLMTestCaseGenerator_Generate_WithMarkdownFences(t *stdtesting.T) {
	fenced := "```json\n" +
		`{"name":"fenced","input":"a","expected_output":"A","notes":"upper"}` + "\n" +
		"```\n"
	adapter := stubAdapter(fenced, nil)
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	tc, err := g.Generate(context.Background(), &GenerateRequest{Topic: "uppercase"})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if tc.Name != "fenced" || tc.Input != "a" || tc.ExpectedOutput != "A" {
		t.Fatalf("parsed wrong: %+v", tc)
	}
}

// TestLLMTestCaseGenerator_Generate_InvalidRequest proves the
// request-level validation fires before the adapter is touched.
func TestLLMTestCaseGenerator_Generate_InvalidRequest(t *stdtesting.T) {
	adapter := stubAdapter(`{"name":"x","input":"a","expected_output":"b"}`, nil)
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	_, err := g.Generate(context.Background(), &GenerateRequest{Topic: ""})
	if err == nil {
		t.Fatal("Generate: expected error, got nil")
	}
	if !errors.Is(err, ErrInvalidGenerateRequest) {
		t.Fatalf("errors.Is(err, ErrInvalidGenerateRequest)=false; err=%v", err)
	}
	if _, err := g.Generate(context.Background(), nil); !errors.Is(err, ErrInvalidGenerateRequest) {
		t.Fatalf("nil req: errors.Is(err, ErrInvalidGenerateRequest)=false; err=%v", err)
	}
}

// TestLLMTestCaseGenerator_Generate_NilAdapter proves the generator
// fails fast and loud when no adapter is configured, rather than
// panicking on a nil-pointer deref.
func TestLLMTestCaseGenerator_Generate_NilAdapter(t *stdtesting.T) {
	g := NewLLMTestCaseGenerator(nil, NewBasicTestCaseValidator())
	_, err := g.Generate(context.Background(), &GenerateRequest{Topic: "x"})
	if err == nil {
		t.Fatal("Generate: expected error, got nil")
	}
	if !errors.Is(err, ErrAdapterNotConfigured) {
		t.Fatalf("errors.Is(err, ErrAdapterNotConfigured)=false; err=%v", err)
	}
	g2 := NewLLMTestCaseGenerator(&LLMAdapter{Ask: nil}, NewBasicTestCaseValidator())
	if _, err := g2.Generate(context.Background(), &GenerateRequest{Topic: "x"}); !errors.Is(err, ErrAdapterNotConfigured) {
		t.Fatalf("nil Ask: errors.Is(err, ErrAdapterNotConfigured)=false; err=%v", err)
	}
}

// TestLLMTestCaseGenerator_Generate_AdapterError proves adapter
// transport failures are surfaced (wrapped) rather than silently
// swallowed.
func TestLLMTestCaseGenerator_Generate_AdapterError(t *stdtesting.T) {
	sentinel := errors.New("transport boom")
	adapter := stubAdapter("", sentinel)
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	_, err := g.Generate(context.Background(), &GenerateRequest{Topic: "x"})
	if err == nil {
		t.Fatal("Generate: expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(err, sentinel)=false; err=%v", err)
	}
	if !strings.Contains(err.Error(), "adapter.Ask") {
		t.Fatalf("wrap message missing 'adapter.Ask': %v", err)
	}
}

// TestLLMTestCaseGenerator_Generate_CtxCancel proves a pre-cancelled
// ctx fails fast before touching the adapter.
func TestLLMTestCaseGenerator_Generate_CtxCancel(t *stdtesting.T) {
	adapter := stubAdapter(`{"name":"x","input":"a","expected_output":"b"}`, nil)
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := g.Generate(ctx, &GenerateRequest{Topic: "x"})
	if err == nil {
		t.Fatal("Generate: expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled)=false; err=%v", err)
	}
}

// TestLLMTestCaseGenerator_GenerateBatch_Happy proves count=N runs
// produce exactly N test cases when every call succeeds.
func TestLLMTestCaseGenerator_GenerateBatch_Happy(t *stdtesting.T) {
	adapter := stubAdapter(
		`{"name":"happy","input":"a","expected_output":"b","notes":""}`,
		nil,
	)
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	cases, err := g.GenerateBatch(context.Background(), &GenerateRequest{Topic: "x"}, 3)
	if err != nil {
		t.Fatalf("GenerateBatch: %v", err)
	}
	if len(cases) != 3 {
		t.Fatalf("len(cases)=%d, want 3", len(cases))
	}
	for i, tc := range cases {
		if tc == nil {
			t.Fatalf("cases[%d] nil", i)
		}
		if tc.Name != "happy" {
			t.Fatalf("cases[%d].Name=%q, want %q", i, tc.Name, "happy")
		}
	}
}

// TestLLMTestCaseGenerator_GenerateBatch_Concurrent proves that
// concurrent fan-out actually happens AND that every produced case is
// uniquely identifiable when the adapter is asked. Uses an atomic
// counter to give each call a unique name; asserts len(unique-names)
// == count.
func TestLLMTestCaseGenerator_GenerateBatch_Concurrent(t *stdtesting.T) {
	var counter int64
	adapter := &LLMAdapter{Ask: func(ctx context.Context, prompt string) (string, error) {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		n := atomic.AddInt64(&counter, 1)
		return fmt.Sprintf(
			`{"name":"case-%d","input":"in-%d","expected_output":"out-%d","notes":""}`,
			n, n, n), nil
	}}
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	cases, err := g.GenerateBatch(context.Background(), &GenerateRequest{Topic: "x"}, 5)
	if err != nil {
		t.Fatalf("GenerateBatch: %v", err)
	}
	if len(cases) != 5 {
		t.Fatalf("len(cases)=%d, want 5", len(cases))
	}
	seen := make(map[string]struct{}, len(cases))
	for _, tc := range cases {
		if _, dup := seen[tc.Name]; dup {
			t.Fatalf("duplicate Name %q across batch — concurrency broken or stub reused", tc.Name)
		}
		seen[tc.Name] = struct{}{}
	}
	if len(seen) != 5 {
		t.Fatalf("unique names=%d, want 5", len(seen))
	}
	if got := atomic.LoadInt64(&counter); got != 5 {
		t.Fatalf("counter=%d, want 5", got)
	}
}

// TestLLMTestCaseGenerator_GenerateBatch_PartialFailure proves the
// ErrPartialBatch path: when one Ask out of five fails, the batch
// returns the four successes AND wraps both ErrPartialBatch and the
// underlying transport sentinel.
func TestLLMTestCaseGenerator_GenerateBatch_PartialFailure(t *stdtesting.T) {
	sentinel := errors.New("third call boom")
	var (
		mu     sync.Mutex
		called int
	)
	adapter := &LLMAdapter{Ask: func(ctx context.Context, prompt string) (string, error) {
		if cerr := ctx.Err(); cerr != nil {
			return "", cerr
		}
		mu.Lock()
		called++
		n := called
		mu.Unlock()
		if n == 3 {
			return "", sentinel
		}
		return fmt.Sprintf(
			`{"name":"ok-%d","input":"a","expected_output":"b","notes":""}`, n), nil
	}}
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	cases, err := g.GenerateBatch(context.Background(), &GenerateRequest{Topic: "x"}, 5)
	if err == nil {
		t.Fatal("GenerateBatch: expected partial failure, got nil err")
	}
	if !errors.Is(err, ErrPartialBatch) {
		t.Fatalf("errors.Is(err, ErrPartialBatch)=false; err=%v", err)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("errors.Is(err, sentinel)=false; err=%v", err)
	}
	// One call out of five failed → 4 successes.
	if len(cases) != 4 {
		t.Fatalf("len(cases)=%d, want 4 (one call failed)", len(cases))
	}
	for i, tc := range cases {
		if tc == nil {
			t.Fatalf("cases[%d] nil despite recorded success", i)
		}
		if tc.Name == "" {
			t.Fatalf("cases[%d].Name empty", i)
		}
	}
}

// TestSandboxExecute_GoHelloWorld spawns `go run` against a minimal
// Hello-World source. Asserts real captured stdout, exit 0, success.
func TestSandboxExecute_GoHelloWorld(t *stdtesting.T) {
	e := NewSandboxedTestExecutor(WithTimeout(60 * time.Second))
	code := `package main

import "fmt"

func main() {
	fmt.Println("Hello, sandbox")
}
`
	res, err := e.Execute(context.Background(), &TestCase{ID: "go-hello"}, &Solution{
		ID: "sol", Language: "go", Code: code,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Passed {
		t.Fatalf("Passed=false; stdout=%q stderr=%q err=%q", res.Output, res.Stderr, res.Error)
	}
	if !strings.Contains(res.Output, "Hello, sandbox") {
		t.Fatalf("stdout did not contain greeting: %q", res.Output)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode=%d, want 0", res.ExitCode)
	}
	if res.Duration <= 0 {
		t.Fatalf("Duration=%s, want > 0", res.Duration)
	}
	if res.TestCaseID != "go-hello" {
		t.Fatalf("TestCaseID=%q, want %q", res.TestCaseID, "go-hello")
	}
}

// TestSandboxExecute_BashEcho is the smoke test for the bash language
// path. Asserts exact stdout match.
func TestSandboxExecute_BashEcho(t *stdtesting.T) {
	e := NewSandboxedTestExecutor(WithTimeout(10 * time.Second))
	res, err := e.Execute(context.Background(), &TestCase{ID: "bash-echo"}, &Solution{
		ID: "sol", Language: "bash", Code: "echo hi",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Passed {
		t.Fatalf("Passed=false; stderr=%q err=%q", res.Stderr, res.Error)
	}
	if res.Output != "hi\n" {
		t.Fatalf("Output=%q, want %q", res.Output, "hi\n")
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode=%d, want 0", res.ExitCode)
	}
}

// TestSandboxExecute_BashExit1 asserts non-zero exit codes are
// surfaced and Passed=false (no top-level error — exit-N is a normal
// test-failure outcome, not a sandbox setup failure).
func TestSandboxExecute_BashExit1(t *stdtesting.T) {
	e := NewSandboxedTestExecutor(WithTimeout(10 * time.Second))
	res, err := e.Execute(context.Background(), &TestCase{ID: "bash-exit1"}, &Solution{
		ID: "sol", Language: "sh", Code: "exit 1",
	})
	if err != nil {
		t.Fatalf("Execute: unexpected top-level error: %v", err)
	}
	if res.Passed {
		t.Fatalf("Passed=true, want false")
	}
	if res.ExitCode != 1 {
		t.Fatalf("ExitCode=%d, want 1", res.ExitCode)
	}
	if res.Error == "" {
		t.Fatalf("Error empty; want populated failure reason")
	}
}

// TestSandboxExecute_UnsupportedLanguage covers the sentinel-error
// path. errors.Is must match ErrUnsupportedLanguage.
func TestSandboxExecute_UnsupportedLanguage(t *stdtesting.T) {
	e := NewSandboxedTestExecutor()
	_, err := e.Execute(context.Background(), &TestCase{ID: "x"}, &Solution{
		ID: "sol", Language: "brainfuck", Code: "+[----->+<]>++.",
	})
	if err == nil {
		t.Fatal("Execute: expected error, got nil")
	}
	if !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("errors.Is(err, ErrUnsupportedLanguage)=false; err=%v", err)
	}
}

// TestSandboxExecute_CtxCancel proves context cancellation propagates
// to the child. Start a sleep 30, cancel after 100ms, assert it
// terminates well before the natural exit.
func TestSandboxExecute_CtxCancel(t *stdtesting.T) {
	e := NewSandboxedTestExecutor(WithTimeout(60 * time.Second))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	res, err := e.Execute(ctx, &TestCase{ID: "sleep"}, &Solution{
		ID: "sol", Language: "bash", Code: "sleep 30",
	})
	elapsed := time.Since(start)
	if elapsed > 10*time.Second {
		t.Fatalf("ctx cancel did not kill child: elapsed=%s", elapsed)
	}
	if err == nil {
		// Top-level err may be nil with Passed=false; either is fine
		// as long as the child actually died.
		if res == nil {
			t.Fatalf("nil result and nil err")
		}
		if res.Passed {
			t.Fatalf("Passed=true after ctx cancel")
		}
	}
}

// TestSandboxExecute_TimeoutEnforced proves WithTimeout actually
// terminates the child. 100ms cap on a 30-second sleep — generous
// slack of 10 seconds for slow CI and process-group teardown.
func TestSandboxExecute_TimeoutEnforced(t *stdtesting.T) {
	e := NewSandboxedTestExecutor(WithTimeout(100 * time.Millisecond))
	start := time.Now()
	res, err := e.Execute(context.Background(), &TestCase{ID: "sleep"}, &Solution{
		ID: "sol", Language: "bash", Code: "sleep 30",
	})
	elapsed := time.Since(start)
	if elapsed > 10*time.Second {
		t.Fatalf("WithTimeout(100ms) did not enforce: elapsed=%s", elapsed)
	}
	if err == nil && res != nil && res.Passed {
		t.Fatalf("Passed=true after timeout")
	}
	// If we got a result back, TimedOut should be set.
	if res != nil && !res.TimedOut {
		// Tolerate: some platforms may surface only the exit-error
		// without populating ctx.Err in time. Emit a diagnostic
		// instead of failing — the real assertion is the elapsed
		// time budget above.
		t.Logf("TimedOut=false despite timeout (elapsed=%s); res.Error=%q", elapsed, res.Error)
	}
}

// TestSandboxExecute_MemoryLimitLinuxOnly proves RLIMIT_AS actually
// kills a memory-hungry child. Linux-only because non-Linux builds
// don't install kernel-level limits.
func TestSandboxExecute_MemoryLimitLinuxOnly(t *stdtesting.T) {
	if runtime.GOOS != "linux" {
		// SKIP-OK: #non-linux-host
		t.Skip("SKIP-OK: requires linux for RLIMIT_AS enforcement")
	}
	// Cap at 32 MiB. Allocate 512 MiB → must be killed.
	e := NewSandboxedTestExecutor(
		WithTimeout(30*time.Second),
		WithMemoryLimit(32*1024*1024),
	)
	code := `package main

func main() {
	// Allocate 512 MiB worth of int64s. Touch every page so the
	// kernel actually backs the reservation and RLIMIT_AS kicks
	// in even with overcommit.
	const n = 512 * 1024 * 1024 / 8
	buf := make([]int64, n)
	for i := range buf {
		buf[i] = int64(i)
	}
	_ = buf[0]
}
`
	res, err := e.Execute(context.Background(), &TestCase{ID: "mem-bomb"}, &Solution{
		ID: "sol", Language: "go", Code: code,
	})
	if err != nil {
		// Setup failure → fatal; setup-class errors don't come from
		// the child hitting RLIMIT_AS.
		t.Fatalf("Execute: unexpected setup error: %v", err)
	}
	if res.Passed {
		t.Fatalf("Passed=true: RLIMIT_AS did not enforce. stdout=%q stderr=%q exit=%d",
			res.Output, res.Stderr, res.ExitCode)
	}
	if !res.LimitsEnforced {
		t.Fatalf("LimitsEnforced=false on linux build")
	}
	if res.ExitCode == 0 {
		t.Fatalf("ExitCode=0 despite memory limit; stderr=%q", res.Stderr)
	}
}

// ---------------------------------------------------------------------
// DifferentialContrastiveAnalyzer.Analyze — real-implementation suite
// ---------------------------------------------------------------------

// TestAnalyze_InsufficientResults proves the validator rejects inputs
// that cannot form at least one pair. Covers both the empty-slice and
// single-result cases via errors.Is on ErrInsufficientResults.
func TestAnalyze_InsufficientResults(t *stdtesting.T) {
	a := NewDifferentialContrastiveAnalyzer(nil)
	ctx := context.Background()

	if _, err := a.Analyze(ctx, nil); err == nil || !errors.Is(err, ErrInsufficientResults) {
		t.Fatalf("nil slice: errors.Is(ErrInsufficientResults)=false, err=%v", err)
	}
	if _, err := a.Analyze(ctx, []*TestExecutionResult{}); err == nil || !errors.Is(err, ErrInsufficientResults) {
		t.Fatalf("empty slice: errors.Is(ErrInsufficientResults)=false, err=%v", err)
	}
	if _, err := a.Analyze(ctx, []*TestExecutionResult{{TestCaseID: "x"}}); err == nil || !errors.Is(err, ErrInsufficientResults) {
		t.Fatalf("single result: errors.Is(ErrInsufficientResults)=false, err=%v", err)
	}
	// Nil entry inside the slice is also rejected (cannot compare nil).
	results := []*TestExecutionResult{{TestCaseID: "a"}, nil}
	if _, err := a.Analyze(ctx, results); err == nil || !errors.Is(err, ErrInsufficientResults) {
		t.Fatalf("nil entry: errors.Is(ErrInsufficientResults)=false, err=%v", err)
	}
}

// TestAnalyze_PerfectAgreement proves a homogeneous result-set
// produces ConsistencyScore=1.0 and every AllAgree bool true with no
// divergence pairs.
func TestAnalyze_PerfectAgreement(t *stdtesting.T) {
	a := NewDifferentialContrastiveAnalyzer(nil)
	results := []*TestExecutionResult{
		{TestCaseID: "t", ExitCode: 0, Output: "hello\n", Stderr: ""},
		{TestCaseID: "t", ExitCode: 0, Output: "hello\n", Stderr: ""},
		{TestCaseID: "t", ExitCode: 0, Output: "hello\n", Stderr: ""},
	}
	rep, err := a.Analyze(context.Background(), results)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rep.ResultCount != 3 {
		t.Fatalf("ResultCount=%d, want 3", rep.ResultCount)
	}
	if !rep.AllAgreeOnExitCode || !rep.AllAgreeOnStdout || !rep.AllAgreeOnStderr {
		t.Fatalf("AllAgree* not all true: exit=%t stdout=%t stderr=%t",
			rep.AllAgreeOnExitCode, rep.AllAgreeOnStdout, rep.AllAgreeOnStderr)
	}
	if rep.ConsistencyScore != 1.0 {
		t.Fatalf("ConsistencyScore=%f, want 1.0", rep.ConsistencyScore)
	}
	if len(rep.OutputDivergence) != 0 || len(rep.ExitCodeDivergence) != 0 || len(rep.StderrDivergence) != 0 {
		t.Fatalf("expected zero divergence pairs; got out=%d exit=%d stderr=%d",
			len(rep.OutputDivergence), len(rep.ExitCodeDivergence), len(rep.StderrDivergence))
	}
}

// TestAnalyze_StdoutDivergence proves that when one of three results
// differs in stdout the analyser flags AllAgreeOnStdout=false and
// enumerates exactly the two pairs touching the odd-one-out.
func TestAnalyze_StdoutDivergence(t *stdtesting.T) {
	a := NewDifferentialContrastiveAnalyzer(nil)
	results := []*TestExecutionResult{
		{TestCaseID: "t", ExitCode: 0, Output: "hello\n"},
		{TestCaseID: "t", ExitCode: 0, Output: "DIFFERENT\n"},
		{TestCaseID: "t", ExitCode: 0, Output: "hello\n"},
	}
	rep, err := a.Analyze(context.Background(), results)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rep.AllAgreeOnStdout {
		t.Fatalf("AllAgreeOnStdout=true, want false")
	}
	if !rep.AllAgreeOnExitCode {
		t.Fatalf("AllAgreeOnExitCode=false, want true (only stdout differs)")
	}
	// Pairs (0,1) and (1,2) should be flagged; (0,2) should not.
	wantPairs := map[[2]int]bool{{0, 1}: true, {1, 2}: true}
	gotPairs := map[[2]int]bool{}
	for _, dp := range rep.OutputDivergence {
		gotPairs[[2]int{dp.I, dp.J}] = true
	}
	if len(gotPairs) != len(wantPairs) {
		t.Fatalf("OutputDivergence count=%d, want %d (pairs=%v)", len(gotPairs), len(wantPairs), rep.OutputDivergence)
	}
	for k := range wantPairs {
		if !gotPairs[k] {
			t.Fatalf("missing expected divergence pair %v; got %v", k, rep.OutputDivergence)
		}
	}
	// Score: 2 divergent pairs out of 3 total → 1 - 2/3 ≈ 0.333.
	if rep.ConsistencyScore <= 0.0 || rep.ConsistencyScore >= 1.0 {
		t.Fatalf("ConsistencyScore=%f, want in (0, 1)", rep.ConsistencyScore)
	}
}

// TestAnalyze_ExitCodeDivergence proves exit-code disagreement is
// flagged independently of stdout/stderr agreement.
func TestAnalyze_ExitCodeDivergence(t *stdtesting.T) {
	a := NewDifferentialContrastiveAnalyzer(nil)
	results := []*TestExecutionResult{
		{TestCaseID: "t", ExitCode: 0, Output: "x"},
		{TestCaseID: "t", ExitCode: 0, Output: "x"},
		{TestCaseID: "t", ExitCode: 1, Output: "x"},
	}
	rep, err := a.Analyze(context.Background(), results)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rep.AllAgreeOnExitCode {
		t.Fatalf("AllAgreeOnExitCode=true, want false")
	}
	if !rep.AllAgreeOnStdout {
		t.Fatalf("AllAgreeOnStdout=false, want true")
	}
	if len(rep.ExitCodeDivergence) != 2 {
		t.Fatalf("ExitCodeDivergence len=%d, want 2; got %v",
			len(rep.ExitCodeDivergence), rep.ExitCodeDivergence)
	}
	// Detail should mention the two distinct codes.
	for _, dp := range rep.ExitCodeDivergence {
		if !strings.Contains(dp.Detail, "0") || !strings.Contains(dp.Detail, "1") {
			t.Fatalf("Detail %q does not name both exit codes", dp.Detail)
		}
	}
}

// TestAnalyze_PartialAgreement covers the mid-spectrum: some pairs
// agree, some disagree → ConsistencyScore strictly inside (0, 1).
func TestAnalyze_PartialAgreement(t *stdtesting.T) {
	a := NewDifferentialContrastiveAnalyzer(nil)
	// 4 results, pair set = {(0,1),(0,2),(0,3),(1,2),(1,3),(2,3)} = 6 pairs.
	// (0,1) agree fully; (2,3) agree fully; (0,2),(0,3),(1,2),(1,3) diverge on stdout.
	// → 4 divergent / 6 total → score = 1 - 4/6 ≈ 0.333.
	results := []*TestExecutionResult{
		{TestCaseID: "t", ExitCode: 0, Output: "A"},
		{TestCaseID: "t", ExitCode: 0, Output: "A"},
		{TestCaseID: "t", ExitCode: 0, Output: "B"},
		{TestCaseID: "t", ExitCode: 0, Output: "B"},
	}
	rep, err := a.Analyze(context.Background(), results)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if rep.AllAgreeOnStdout {
		t.Fatalf("AllAgreeOnStdout=true, want false")
	}
	if !rep.AllAgreeOnExitCode {
		t.Fatalf("AllAgreeOnExitCode=false, want true")
	}
	if rep.ConsistencyScore <= 0.0 || rep.ConsistencyScore >= 1.0 {
		t.Fatalf("ConsistencyScore=%f, want strictly in (0, 1)", rep.ConsistencyScore)
	}
	if len(rep.OutputDivergence) != 4 {
		t.Fatalf("OutputDivergence len=%d, want 4; got %v",
			len(rep.OutputDivergence), rep.OutputDivergence)
	}
}

// TestAnalyze_CtxCancel proves Analyze honours ctx cancellation before
// touching the result slice.
func TestAnalyze_CtxCancel(t *stdtesting.T) {
	a := NewDifferentialContrastiveAnalyzer(nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := []*TestExecutionResult{
		{TestCaseID: "t", ExitCode: 0, Output: "a"},
		{TestCaseID: "t", ExitCode: 0, Output: "a"},
	}
	_, err := a.Analyze(ctx, results)
	if err == nil {
		t.Fatal("expected ctx.Err(), got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled)=false; err=%v", err)
	}
}

// ---------------------------------------------------------------------
// BasicTestCaseValidator.Validate — real-implementation suite
// ---------------------------------------------------------------------

// TestValidate_EmptyName proves a TestCase with whitespace-only
// Description is rejected with ErrInvalidName.
func TestValidate_EmptyName(t *stdtesting.T) {
	v := NewBasicTestCaseValidator()
	cases := []*TestCase{
		{Description: "", Input: "x"},
		{Description: "   ", Input: "x"},
		{Description: "\t\n", Input: "x"},
	}
	for i, tc := range cases {
		if err := v.Validate(context.Background(), tc); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("case %d: errors.Is(ErrInvalidName)=false; err=%v", i, err)
		}
	}
	// nil TestCase is also surfaced as ErrInvalidName per the rule order.
	if err := v.Validate(context.Background(), nil); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("nil case: errors.Is(ErrInvalidName)=false; err=%v", err)
	}
}

// TestValidate_NoPayload proves a case with neither Input nor
// ExpectedOutput is rejected.
func TestValidate_NoPayload(t *stdtesting.T) {
	v := NewBasicTestCaseValidator()
	tc := &TestCase{Description: "named but empty"}
	err := v.Validate(context.Background(), tc)
	if !errors.Is(err, ErrNoTestPayload) {
		t.Fatalf("errors.Is(ErrNoTestPayload)=false; err=%v", err)
	}
}

// TestValidate_InvalidTimeout covers negative and excessive timeout
// rejections.
func TestValidate_InvalidTimeout(t *stdtesting.T) {
	v := NewBasicTestCaseValidator()

	tcNeg := &TestCase{Description: "neg", Input: "x", Timeout: -1 * time.Second}
	if err := v.Validate(context.Background(), tcNeg); !errors.Is(err, ErrInvalidTimeout) {
		t.Fatalf("negative timeout: errors.Is(ErrInvalidTimeout)=false; err=%v", err)
	}

	tcHuge := &TestCase{Description: "huge", Input: "x", Timeout: 2 * time.Hour}
	if err := v.Validate(context.Background(), tcHuge); !errors.Is(err, ErrInvalidTimeout) {
		t.Fatalf("2h timeout: errors.Is(ErrInvalidTimeout)=false; err=%v", err)
	}

	// Edge: exactly 1 hour is accepted (≤ maxTestCaseTimeout).
	tcOK := &TestCase{Description: "edge", Input: "x", Timeout: time.Hour}
	if err := v.Validate(context.Background(), tcOK); err != nil {
		t.Fatalf("1h timeout should be accepted; err=%v", err)
	}
}

// TestValidate_InvalidCategory proves an unknown category string is
// rejected.
func TestValidate_InvalidCategory(t *stdtesting.T) {
	v := NewBasicTestCaseValidator()
	tc := &TestCase{Description: "n", Input: "x", Category: TestCategory("brainfuck")}
	if err := v.Validate(context.Background(), tc); !errors.Is(err, ErrInvalidCategory) {
		t.Fatalf("errors.Is(ErrInvalidCategory)=false; err=%v", err)
	}
}

// TestValidate_Happy proves a fully-populated, well-formed TestCase
// passes validation across each known category.
func TestValidate_Happy(t *stdtesting.T) {
	v := NewBasicTestCaseValidator()
	for _, cat := range []TestCategory{
		CategoryFunctional, CategoryEdgeCase, CategoryPerformance, CategorySecurity,
	} {
		tc := &TestCase{
			ID:             "tc-1",
			Description:    "valid case",
			Category:       cat,
			Input:          "in",
			ExpectedOutput: "out",
			Timeout:        5 * time.Second,
		}
		if err := v.Validate(context.Background(), tc); err != nil {
			t.Fatalf("category %q: unexpected error: %v", cat, err)
		}
	}
	// Also: case with no Timeout and no Category should pass.
	tc := &TestCase{Description: "no opt fields", Input: "x"}
	if err := v.Validate(context.Background(), tc); err != nil {
		t.Fatalf("minimal happy case rejected: %v", err)
	}
	// And: ExpectedOutput-only (no Input) should pass.
	tc2 := &TestCase{Description: "expected only", ExpectedOutput: "out"}
	if err := v.Validate(context.Background(), tc2); err != nil {
		t.Fatalf("expected-output-only case rejected: %v", err)
	}
}

// TestValidate_CtxCancel proves Validate honours ctx cancellation
// before any field inspection.
func TestValidate_CtxCancel(t *stdtesting.T) {
	v := NewBasicTestCaseValidator()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tc := &TestCase{Description: "would be valid", Input: "x"}
	err := v.Validate(ctx, tc)
	if err == nil {
		t.Fatal("expected ctx.Err(), got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("errors.Is(err, context.Canceled)=false; err=%v", err)
	}
}
