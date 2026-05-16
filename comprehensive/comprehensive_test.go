package comprehensive

import (
	"context"
	"strings"
	"testing"
)

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
	m, err := NewIntegrationManager(DefaultConfig(), nil)
	if err != nil {
		t.Fatalf("NewIntegrationManager: %v", err)
	}
	if err := m.RegisterAgent(NewAgent(RoleGenerator, "p", "m", 0.8)); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if err := m.RegisterAgent(NewAgent(RoleCritic, "q", "n", 0.7)); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	resp, err := m.ExecuteDebate(context.Background(), &DebateRequest{Topic: "t"})
	if err != nil {
		t.Fatalf("ExecuteDebate: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success")
	}
}

func TestStreamDebateIsHonestStub(t *testing.T) {
	m, _ := NewIntegrationManager(DefaultConfig(), nil)
	_, err := m.StreamDebate(context.Background(), &DebateRequest{Topic: "t"}, nil)
	if err == nil {
		t.Fatal("expected NotYetImplemented error from StreamDebate stub")
	}
	if !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("expected NotYetImplemented sentinel, got %q", err.Error())
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
