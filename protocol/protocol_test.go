package protocol

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"digital.vasic.debate/topology"
)

func TestNewProtocolAndDefaultConfig(t *testing.T) {
	cfg := DefaultDebateConfig()
	if cfg.Name == "" {
		t.Fatalf("DefaultDebateConfig.Name empty")
	}
	if cfg.Version == "" {
		t.Fatalf("DefaultDebateConfig.Version empty")
	}
	if cfg.Timeout != 30*time.Second {
		t.Fatalf("DefaultDebateConfig.Timeout = %v, want 30s", cfg.Timeout)
	}
	if cfg.MaxRounds <= 0 {
		t.Fatalf("DefaultDebateConfig.MaxRounds = %d, want >0", cfg.MaxRounds)
	}
	if cfg.TopologyType == "" {
		t.Fatalf("DefaultDebateConfig.TopologyType empty")
	}

	p := NewProtocol(cfg)
	if p == nil {
		t.Fatalf("NewProtocol returned nil protocol")
	}
	if p.Name != cfg.Name {
		t.Fatalf("NewProtocol.Name = %q, want %q", p.Name, cfg.Name)
	}
}

func TestNewProtocolBindsTopologyAndInvoker(t *testing.T) {
	cfg := DefaultDebateConfig()
	topoCfg := topology.DefaultTopologyConfig(topology.TopologyGraphMesh)
	topo, err := topology.NewTopology(topology.TopologyGraphMesh, topoCfg)
	if err != nil {
		t.Fatalf("NewTopology: %v", err)
	}
	invoker := AgentInvokerFunc(func(ctx context.Context, agent *topology.Agent,
		prompt string, debateCtx DebateContext) (*PhaseResponse, error) {
		return &PhaseResponse{AgentID: agent.ID, Content: "ok"}, nil
	})

	p := NewProtocol(cfg, topo, invoker)
	if p.Topology != topo {
		t.Fatalf("NewProtocol did not bind Topology")
	}
	if p.Invoker == nil {
		t.Fatalf("NewProtocol did not bind Invoker")
	}
}

func TestProtocolExecuteIsHonestStub(t *testing.T) {
	p := NewProtocol(DefaultDebateConfig())
	result, err := p.Execute(context.Background())
	if err == nil {
		t.Fatalf("Protocol.Execute: expected stub error, got nil")
	}
	if !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Protocol.Execute: expected NotYetImplemented sentinel, got %q", err.Error())
	}
	if result == nil {
		t.Fatalf("Protocol.Execute: expected non-nil result with Success=false, got nil")
	}
	if result.Success {
		t.Fatalf("Protocol.Execute: stub must not report Success=true")
	}
}

func TestProtocolExecuteRequestIsHonestStub(t *testing.T) {
	p := NewProtocol(DefaultDebateConfig())
	_, err := p.ExecuteRequest(context.Background(), &Request{ID: "r1"})
	if err == nil {
		t.Fatalf("Protocol.ExecuteRequest: expected stub error, got nil")
	}
	if !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Protocol.ExecuteRequest: expected NotYetImplemented sentinel, got %q", err.Error())
	}
}

func TestProtocolExecuteCtxCancelled(t *testing.T) {
	p := NewProtocol(DefaultDebateConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := p.Execute(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Protocol.Execute(cancelled): expected context.Canceled, got %v", err)
	}
}

func TestGetStringHelper(t *testing.T) {
	m := map[string]interface{}{"k": "v", "i": 7}
	if got := GetString(m, "k"); got != "v" {
		t.Fatalf("GetString(k) = %q, want %q", got, "v")
	}
	if got := GetString(m, "missing"); got != "" {
		t.Fatalf("GetString(missing) = %q, want \"\"", got)
	}
	if got := GetString(m, "i"); got != "" {
		t.Fatalf("GetString(i): expected \"\" for non-string value, got %q", got)
	}
	if got := GetString(nil, "k"); got != "" {
		t.Fatalf("GetString(nil) = %q, want \"\"", got)
	}
}
