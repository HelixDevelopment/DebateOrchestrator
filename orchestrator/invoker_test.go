// SPDX-FileCopyrightText: 2026 Milos Vasic
// SPDX-License-Identifier: Apache-2.0

package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
)

// TestConductDebate_WithProviderInvoker_DispatchesToInvoker verifies
// that when WithProviderInvoker is wired, the orchestrator calls the
// invoker for every (round × agent) tuple, uses the invoker's
// returned text as AgentResponse.Content, and measures real
// wall-clock latency (Latency > 0).
//
// Previously the orchestrator unconditionally called synthesiseContent
// + simulatedLatency producing fake content + 0-latency sentinel. This
// test asserts the real-dispatch contract.
func TestConductDebate_WithProviderInvoker_DispatchesToInvoker(t *testing.T) {
	var calls atomic.Int64
	invoker := func(ctx context.Context, prompt string) (string, error) {
		n := calls.Add(1)
		return fmt.Sprintf("real-response-%d for prompt: %s", n, prompt[:min(40, len(prompt))]), nil
	}

	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig(),
		WithProviderInvoker(invoker))
	if err := o.RegisterProvider("openai", "gpt-4", 0.9); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := o.RegisterProvider("anthropic", "claude-3", 0.85); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	resp, err := o.ConductDebate(context.Background(), &DebateRequest{Topic: "test-invoker"})
	if err != nil {
		t.Fatalf("ConductDebate: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success")
	}

	// The invoker MUST have been called for every (round × agent).
	expectedCalls := int64(resp.RoundsConducted) * int64(len(resp.Participants))
	if got := calls.Load(); got != expectedCalls {
		t.Errorf("invoker calls = %d, want %d (rounds=%d × participants=%d)",
			got, expectedCalls, resp.RoundsConducted, len(resp.Participants))
	}

	// Every AgentResponse.Content MUST carry the real invoker's output
	// (prefix "real-response-"), NOT the synthesised stub marker.
	for _, phase := range resp.Phases {
		for _, ar := range phase.Responses {
			if !strings.HasPrefix(ar.Content, "real-response-") {
				t.Errorf("AgentResponse.Content = %q, expected real-response-* prefix; got the synthesised stub or invoker-error?",
					ar.Content)
			}
			if strings.Contains(ar.Content, "synthesised") {
				t.Errorf("AgentResponse.Content carries [synthesised...] marker — invoker dispatch was NOT used: %q", ar.Content)
			}
			// Real wall-clock latency MUST be > 0 (even a no-op
			// closure takes nanoseconds; the time.Now() bracket
			// captures it). 0 latency would indicate the
			// simulatedLatency sentinel is still wired.
			if ar.Latency <= 0 {
				t.Errorf("AgentResponse.Latency = %v, expected > 0 from real wall-clock measurement", ar.Latency)
			}
		}
	}
}

// TestConductDebate_WithoutInvoker_FallsBackToStub verifies that
// when no ProviderInvoker is wired, the orchestrator falls back to
// the synthesised-content stub (with explicit "[synthesised ...]"
// marker per §11.4 ACK-STUB) and 0-latency sentinel. This is the
// honest non-wired path — consumers can detect the stub by
// scanning Content for "synthesised".
func TestConductDebate_WithoutInvoker_FallsBackToStub(t *testing.T) {
	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig())
	if err := o.RegisterProvider("openai", "gpt-4", 0.9); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := o.RegisterProvider("anthropic", "claude-3", 0.85); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	resp, err := o.ConductDebate(context.Background(), &DebateRequest{Topic: "test-stub"})
	if err != nil {
		t.Fatalf("ConductDebate: %v", err)
	}

	// Every AgentResponse.Content MUST carry the [synthesised ...]
	// marker per §11.4 ACK-STUB disclosure. Latency MUST be 0
	// sentinel (no real call was made, so no real latency).
	for _, phase := range resp.Phases {
		for _, ar := range phase.Responses {
			if !strings.Contains(ar.Content, "synthesised") {
				t.Errorf("AgentResponse.Content = %q, expected [synthesised ...] marker per §11.4 ACK-STUB", ar.Content)
			}
			if ar.Latency != 0 {
				t.Errorf("AgentResponse.Latency = %v, expected 0 sentinel when no invoker wired", ar.Latency)
			}
		}
	}
}

// TestConductDebate_InvokerError_SurfacedInContent verifies that
// when the invoker returns an error, the orchestrator surfaces it
// via the [invoker-error: ...] marker in AgentResponse.Content
// (not silently fallen-through to the stub). The round continues
// so other agents can still contribute.
func TestConductDebate_InvokerError_SurfacedInContent(t *testing.T) {
	invoker := func(ctx context.Context, prompt string) (string, error) {
		return "", fmt.Errorf("simulated provider failure")
	}

	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig(),
		WithProviderInvoker(invoker))
	if err := o.RegisterProvider("openai", "gpt-4", 0.9); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := o.RegisterProvider("anthropic", "claude-3", 0.85); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}

	resp, err := o.ConductDebate(context.Background(), &DebateRequest{Topic: "test-error"})
	if err != nil {
		t.Fatalf("ConductDebate: %v", err)
	}

	for _, phase := range resp.Phases {
		for _, ar := range phase.Responses {
			if !strings.HasPrefix(ar.Content, "[invoker-error") {
				t.Errorf("AgentResponse.Content = %q, expected [invoker-error ...] marker; orchestrator may have silently fallen back to stub", ar.Content)
			}
			if !strings.Contains(ar.Content, "simulated provider failure") {
				t.Errorf("AgentResponse.Content = %q, expected to contain underlying error message", ar.Content)
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
