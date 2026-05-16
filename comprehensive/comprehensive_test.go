package comprehensive

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// newReadyManager constructs an IntegrationManager with two registered
// agents so ExecuteDebate (and therefore StreamDebate) can run end-to-end.
func newReadyManager(t *testing.T) *IntegrationManager {
	t.Helper()
	m, err := NewIntegrationManager(DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewIntegrationManager: %v", err)
	}
	if err := m.RegisterAgent(NewAgent(RoleGenerator, "p", "m", 0.8)); err != nil {
		t.Fatalf("RegisterAgent generator: %v", err)
	}
	if err := m.RegisterAgent(NewAgent(RoleCritic, "q", "n", 0.7)); err != nil {
		t.Fatalf("RegisterAgent critic: %v", err)
	}
	return m
}

func TestNewAgent(t *testing.T) {
	a := NewAgent(RoleArchitect, "openai", "gpt-4", 0.9)
	if a.Role != RoleArchitect || a.Provider != "openai" {
		t.Fatalf("unexpected agent: %+v", a)
	}
	if !strings.Contains(a.ID, "architect") {
		t.Fatalf("ID missing role: %q", a.ID)
	}
}

func TestIntegrationManagerExecute(t *testing.T) {
	m := newReadyManager(t)
	resp, err := m.ExecuteDebate(context.Background(), &DebateRequest{Topic: "t"})
	if err != nil {
		t.Fatalf("ExecuteDebate: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success")
	}
}

func TestStreamDebate_NilHandlerErrors(t *testing.T) {
	m := newReadyManager(t)
	resp, err := m.StreamDebate(context.Background(), &DebateRequest{Topic: "t"}, nil)
	if err == nil {
		t.Fatal("expected error for nil handler")
	}
	if !errors.Is(err, ErrNilStreamHandler) {
		t.Fatalf("expected ErrNilStreamHandler via errors.Is, got %v", err)
	}
	if resp != nil {
		t.Fatalf("expected nil response on nil-handler error, got %+v", resp)
	}
}

func TestStreamDebate_EmitsStartedAndCompleted(t *testing.T) {
	m := newReadyManager(t)
	var events []*StreamEvent
	handler := func(e *StreamEvent) error {
		// Defensive copy so later events can't mutate captured state.
		cp := *e
		events = append(events, &cp)
		return nil
	}
	resp, err := m.StreamDebate(context.Background(), &DebateRequest{Topic: "t"}, handler)
	if err != nil {
		t.Fatalf("StreamDebate: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("expected real successful response, got %+v", resp)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events (started, completed), got %d", len(events))
	}
	if events[0].Type != "started" {
		t.Fatalf("expected first event type=started, got %q", events[0].Type)
	}
	if events[len(events)-1].Type != "completed" {
		t.Fatalf("expected last event type=completed, got %q", events[len(events)-1].Type)
	}
	if events[len(events)-1].Progress != 1.0 {
		t.Fatalf("expected completed.Progress==1.0, got %v", events[len(events)-1].Progress)
	}

	// Anti-bluff: prove the stream contains REAL per-agent events
	// whose content was extracted from the orchestrator response.
	var sawAgentResponse bool
	for _, e := range events {
		if e.Type == "agent_response" {
			sawAgentResponse = true
			if e.AgentID == "" {
				t.Fatalf("agent_response event missing AgentID: %+v", e)
			}
			if e.Content == "" {
				t.Fatalf("agent_response event missing Content (real orchestrator data expected): %+v", e)
			}
		}
	}
	if !sawAgentResponse {
		t.Fatal("expected at least one agent_response event extracted from real orchestrator phases")
	}
}

func TestStreamDebate_ProgressMonotonic(t *testing.T) {
	m := newReadyManager(t)
	var events []*StreamEvent
	handler := func(e *StreamEvent) error {
		cp := *e
		events = append(events, &cp)
		return nil
	}
	if _, err := m.StreamDebate(context.Background(), &DebateRequest{Topic: "monotonic"}, handler); err != nil {
		t.Fatalf("StreamDebate: %v", err)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	for i := 1; i < len(events); i++ {
		prev := events[i-1].Progress
		cur := events[i].Progress
		if cur < prev {
			t.Fatalf("progress regressed at event %d: prev=%v cur=%v (type %q -> %q)",
				i, prev, cur, events[i-1].Type, events[i].Type)
		}
		if cur < 0 || cur > 1 {
			t.Fatalf("progress out of [0,1] at event %d (type %q): %v", i, events[i].Type, cur)
		}
	}
}

func TestStreamDebate_HandlerErrorAborts(t *testing.T) {
	m := newReadyManager(t)
	abort := errors.New("intentional handler abort")
	var count int
	handler := func(e *StreamEvent) error {
		count++
		if count == 2 {
			return abort
		}
		return nil
	}
	resp, err := m.StreamDebate(context.Background(), &DebateRequest{Topic: "abort"}, handler)
	if err == nil {
		t.Fatal("expected error from handler abort")
	}
	if !errors.Is(err, abort) {
		t.Fatalf("expected returned error to wrap the handler error via %%w, got %v", err)
	}
	if count < 2 {
		t.Fatalf("expected at least 2 handler invocations before abort, got %d", count)
	}
	// resp may be nil (typical, since abort happened before the
	// orchestrator's completion) — we don't require it to be non-nil.
	_ = resp
}

func TestStreamDebate_CtxCancelEmitsCancelled(t *testing.T) {
	m := newReadyManager(t)
	ctx, cancel := context.WithCancel(context.Background())
	var events []*StreamEvent
	var cancelled bool
	handler := func(e *StreamEvent) error {
		cp := *e
		events = append(events, &cp)
		// After the first event, cancel the context so the streaming
		// loop sees a cancelled ctx on its next iteration.
		if !cancelled {
			cancelled = true
			cancel()
		}
		return nil
	}
	_, err := m.StreamDebate(ctx, &DebateRequest{Topic: "cancel"}, handler)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected ctx.Err() (context.Canceled), got %v", err)
	}
	var sawCancelled bool
	for _, e := range events {
		if e.Type == "cancelled" {
			sawCancelled = true
			break
		}
	}
	if !sawCancelled {
		t.Fatalf("expected a 'cancelled' event in the stream, got events: %s", eventTypes(events))
	}
}

func TestStreamDebateRequest_UnwrapsAndDelegates(t *testing.T) {
	m := newReadyManager(t)
	var events []*StreamEvent
	handler := func(e *StreamEvent) error {
		cp := *e
		events = append(events, &cp)
		return nil
	}
	sreq := &DebateStreamRequest{
		DebateRequest: &DebateRequest{Topic: "unwrap"},
		Stream:        true,
		StreamHandler: handler,
	}
	resp, err := m.StreamDebateRequest(context.Background(), sreq)
	if err != nil {
		t.Fatalf("StreamDebateRequest: %v", err)
	}
	if resp == nil || !resp.Success {
		t.Fatalf("expected real successful response, got %+v", resp)
	}
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events, got %d", len(events))
	}
	if events[0].Type != "started" || events[len(events)-1].Type != "completed" {
		t.Fatalf("expected first=started last=completed, got first=%q last=%q",
			events[0].Type, events[len(events)-1].Type)
	}

	// Nil request branch.
	if _, err := m.StreamDebateRequest(context.Background(), nil); !errors.Is(err, ErrNilStreamRequest) {
		t.Fatalf("expected ErrNilStreamRequest for nil request, got %v", err)
	}

	// Nil handler inside request must cascade to ErrNilStreamHandler.
	bad := &DebateStreamRequest{DebateRequest: &DebateRequest{Topic: "x"}, Stream: true, StreamHandler: nil}
	if _, err := m.StreamDebateRequest(context.Background(), bad); !errors.Is(err, ErrNilStreamHandler) {
		t.Fatalf("expected ErrNilStreamHandler when wrapped handler is nil, got %v", err)
	}
}

func TestInvokerRegistry(t *testing.T) {
	called := false
	if err := RegisterInvoker("p", "m", func(_ context.Context, _ string) (string, error) {
		called = true
		return "ok", nil
	}); err != nil {
		t.Fatalf("RegisterInvoker: %v", err)
	}
	inv := LookupInvoker("p", "m")
	if inv == nil {
		t.Fatal("LookupInvoker returned nil")
	}
	if _, err := inv(context.Background(), "x"); err != nil {
		t.Fatalf("inv: %v", err)
	}
	if !called {
		t.Fatal("invoker was not called")
	}
	SetFallbackInvoker(func(_ context.Context, _ string) (string, error) {
		return "fallback", nil
	})
	if inv := LookupInvoker("missing", "missing"); inv == nil {
		t.Fatal("expected fallback invoker")
	}
}

// eventTypes is a small helper that formats the Type field of a slice
// of events for diagnostic output.
func eventTypes(evts []*StreamEvent) string {
	var b strings.Builder
	for i, e := range evts {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(e.Type)
	}
	return b.String()
}
