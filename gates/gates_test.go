package gates

import (
	"context"
	"testing"
)

func TestApprovalGateCheck(t *testing.T) {
	g := NewApprovalGate(GateConfig{Enabled: true, Name: "smoke"})
	ok, err := g.Check(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Fatal("expected gate to allow under permissive baseline")
	}
}

func TestApprovalGateCancellation(t *testing.T) {
	g := NewApprovalGate(GateConfig{Enabled: true})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := g.Check(ctx, nil); err == nil {
		t.Fatal("expected context cancellation to propagate")
	}
}
