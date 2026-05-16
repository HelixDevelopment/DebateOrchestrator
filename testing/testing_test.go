package testing

import (
	"context"
	"strings"
	stdtesting "testing"
	"time"
)

func TestExecutorOptions(t *stdtesting.T) {
	e := NewSandboxedTestExecutor(WithTimeout(2*time.Second), WithMemoryLimit(1024), WithCPULimit(0.5))
	if e == nil {
		t.Fatal("nil executor")
	}
}

func TestStubsReturnNotYetImplemented(t *stdtesting.T) {
	ctx := context.Background()
	adapter := NewProviderAdapter(func(context.Context, string) (string, error) { return "", nil })
	g := NewLLMTestCaseGenerator(adapter, NewBasicTestCaseValidator())
	if _, err := g.Generate(ctx, "x"); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Generate: %v", err)
	}
	if _, err := g.GenerateBatch(ctx, &GenerateRequest{AgentID: "a"}, 3); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("GenerateBatch: %v", err)
	}
	e := NewSandboxedTestExecutor()
	if _, err := e.Execute(ctx, &TestCase{ID: "t"}, &Solution{ID: "s"}); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Execute: %v", err)
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
