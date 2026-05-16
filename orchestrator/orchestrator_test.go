package orchestrator

import (
	"context"
	"testing"
	"time"
)

func TestConductDebateBasic(t *testing.T) {
	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig())
	if o == nil {
		t.Fatal("NewOrchestrator returned nil")
	}
	if err := o.RegisterProvider("openai", "gpt-4", 0.9); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	if err := o.RegisterProvider("ollama", "llama3.2", 0.7); err != nil {
		t.Fatalf("RegisterProvider: %v", err)
	}
	resp, err := o.ConductDebate(context.Background(), &DebateRequest{Topic: "test"})
	if err != nil {
		t.Fatalf("ConductDebate: %v", err)
	}
	if !resp.Success {
		t.Fatal("expected success")
	}
	if resp.RoundsConducted != o.cfg.DefaultMaxRounds {
		t.Fatalf("rounds = %d, want %d", resp.RoundsConducted, o.cfg.DefaultMaxRounds)
	}
	if resp.Metrics == nil || resp.Metrics.ProviderCalls == 0 {
		t.Fatal("expected non-zero metrics")
	}
	if resp.Duration <= 0 {
		t.Fatal("expected non-zero duration")
	}
}

func TestSessionLifecycle(t *testing.T) {
	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig())
	s, err := o.CreateSession(&DebateRequest{Topic: "x"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if _, err := o.GetSession(s.ID); err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if err := o.CancelSession(s.ID); err != nil {
		t.Fatalf("CancelSession: %v", err)
	}
}

func TestStatistics(t *testing.T) {
	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig())
	stats, err := o.GetStatistics(context.Background())
	if err != nil {
		t.Fatalf("GetStatistics: %v", err)
	}
	if stats == nil {
		t.Fatal("nil stats")
	}
}

func TestAPIAdapter(t *testing.T) {
	o := NewOrchestrator(nil, nil, DefaultOrchestratorConfig())
	a := NewAPIAdapter(o)
	resp, err := a.CreateDebate(context.Background(), &APICreateDebateRequest{
		Topic:     "api test",
		MaxRounds: 2,
		Participants: []APIParticipantConfig{
			{Provider: "p1", Model: "m1", Score: 0.8},
			{Provider: "p2", Model: "m2", Score: 0.6},
		},
	})
	if err != nil {
		t.Fatalf("CreateDebate: %v", err)
	}
	if resp.RoundsConducted != 2 {
		t.Fatalf("rounds = %d, want 2", resp.RoundsConducted)
	}
}

func TestDownSentinel(t *testing.T) {
	d := Down{}
	if err := d.ExecuteFlow(context.Background()); err != nil {
		t.Fatalf("ExecuteFlow: %v", err)
	}
	if err := d.ExecutePlan(context.Background()); err != nil {
		t.Fatalf("ExecutePlan: %v", err)
	}
	if err := d.ExecuteParallel(context.Background()); err != nil {
		t.Fatalf("ExecuteParallel: %v", err)
	}
}

func TestTimeoutHonoured(t *testing.T) {
	cfg := DefaultOrchestratorConfig()
	cfg.DefaultTimeout = 5 * time.Second
	o := NewOrchestrator(nil, nil, cfg)
	_ = o.RegisterProvider("a", "b", 0.9)
	_ = o.RegisterProvider("c", "d", 0.9)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if _, err := o.ConductDebate(ctx, &DebateRequest{Topic: "t"}); err != nil {
		t.Fatalf("ConductDebate: %v", err)
	}
}
