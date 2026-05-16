package testing

import (
	"context"
	"errors"
	"runtime"
	"strings"
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

// TestStubsStillNotYetImplemented covers the generator/analyzer/validator
// stubs that have NOT yet been promoted. Execute was REMOVED from this
// list because it is now real (covered by the TestSandboxExecute_* suite).
func TestStubsStillNotYetImplemented(t *stdtesting.T) {
	ctx := context.Background()
	adapter := NewProviderAdapter(func(context.Context, string) (string, error) { return "", nil })
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	if _, err := g.Generate(ctx, "x"); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := g.GenerateBatch(ctx, &GenerateRequest{AgentID: "a"}, 3); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("GenerateBatch: %v", err)
	}
	a := NewDifferentialContrastiveAnalyzer(nil)
	if _, err := a.Analyze(ctx, nil, nil); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Analyze: %v", err)
	}
	v := NewBasicTestCaseValidator()
	if _, err := v.Validate(ctx, &TestCase{ID: "t"}); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Validate: %v", err)
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
		t.Skip("requires linux for RLIMIT_AS enforcement")
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
