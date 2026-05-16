package protocol

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
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

// =============================================================================
// Execute orchestration runtime — real-runtime tests (Phase 2 promotion)
// =============================================================================

// validDebateConfig returns a Config that passes Execute's validation
// gate (non-empty Topic, MaxRounds > 0) without engaging early exit
// unless the caller overrides EnableEarlyExit/MinConsensusScore.
func validDebateConfig() Config {
	cfg := DefaultDebateConfig()
	cfg.Topic = "Should we ship the feature today?"
	cfg.Context = "test-context"
	cfg.MaxRounds = 1
	cfg.EnableEarlyExit = false
	cfg.MinConsensusScore = 1.1 // unreachable by construction
	return cfg
}

// constantInvoker returns an AgentInvokerFunc that always echoes the
// supplied content prefixed with the agent ID for traceability.
func constantInvoker(content string) AgentInvoker {
	return AgentInvokerFunc(func(ctx context.Context, agent *topology.Agent,
		prompt string, debateCtx DebateContext) (*PhaseResponse, error) {
		return &PhaseResponse{
			AgentID: agent.ID,
			Content: fmt.Sprintf("%s-says-%s", agent.ID, content),
			Phase:   debateCtx.CurrentPhase,
		}, nil
	})
}

func TestProtocolExecute_InvalidConfig(t *testing.T) {
	cfg := validDebateConfig()
	cfg.Topic = "" // trip the validator
	p := NewProtocol(cfg)
	if err := p.RegisterAgent("a", constantInvoker("X")); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	res, err := p.Execute(context.Background())
	if err == nil {
		t.Fatalf("Execute(empty Topic): expected ErrInvalidConfig, got nil err (result=%+v)", res)
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Execute(empty Topic): expected errors.Is(err, ErrInvalidConfig), got %v", err)
	}

	cfg2 := validDebateConfig()
	cfg2.MaxRounds = 0
	p2 := NewProtocol(cfg2)
	if err := p2.RegisterAgent("a", constantInvoker("X")); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}
	if _, err := p2.Execute(context.Background()); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Execute(MaxRounds=0): expected ErrInvalidConfig, got %v", err)
	}
}

func TestProtocolExecute_NoAgents(t *testing.T) {
	p := NewProtocol(validDebateConfig())
	_, err := p.Execute(context.Background())
	if !errors.Is(err, ErrNoAgentsConfigured) {
		t.Fatalf("Execute(no agents): expected ErrNoAgentsConfigured, got %v", err)
	}
}

func TestProtocolExecute_SingleAgentSingleRound(t *testing.T) {
	cfg := validDebateConfig()
	cfg.MaxRounds = 1
	p := NewProtocol(cfg)
	if err := p.RegisterAgent("agent-A", constantInvoker("X")); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	res, err := p.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res == nil {
		t.Fatalf("Execute: nil result")
	}
	if !res.Success {
		t.Fatalf("Execute: Success=false, want true")
	}
	if res.RoundsCompleted != 1 {
		t.Fatalf("Execute: RoundsCompleted=%d, want 1", res.RoundsCompleted)
	}
	if got, want := len(res.Phases), len(executionPhases); got != want {
		t.Fatalf("Execute: len(Phases)=%d, want %d", got, want)
	}
	for i, ph := range res.Phases {
		if len(ph.Responses) != 1 {
			t.Fatalf("Execute: phase[%d] (%s) responses=%d, want 1",
				i, ph.Phase, len(ph.Responses))
		}
		resp := ph.Responses[0]
		if resp.AgentID != "agent-A" {
			t.Fatalf("Execute: phase[%d] AgentID=%q, want agent-A", i, resp.AgentID)
		}
		if !strings.Contains(resp.Content, "agent-A-says") {
			t.Fatalf("Execute: phase[%d] Content=%q, want substring agent-A-says",
				i, resp.Content)
		}
		if resp.Timestamp.IsZero() {
			t.Fatalf("Execute: phase[%d] Timestamp zero, want non-zero", i)
		}
		if resp.Phase != ph.Phase {
			t.Fatalf("Execute: phase[%d] resp.Phase=%q, want %q",
				i, resp.Phase, ph.Phase)
		}
	}
	if res.Metrics == nil {
		t.Fatalf("Execute: Metrics nil")
	}
	wantInvocations := 1 * len(executionPhases) // 1 agent x phases x 1 round
	if res.Metrics.TotalInvocations != wantInvocations {
		t.Fatalf("Execute: Metrics.TotalInvocations=%d, want %d",
			res.Metrics.TotalInvocations, wantInvocations)
	}
	if res.Metrics.TotalResponses != wantInvocations {
		t.Fatalf("Execute: Metrics.TotalResponses=%d, want %d",
			res.Metrics.TotalResponses, wantInvocations)
	}
	if res.Duration <= 0 {
		t.Fatalf("Execute: Duration=%v, want >0", res.Duration)
	}
	if res.ID == "" {
		t.Fatalf("Execute: ID empty")
	}
}

func TestProtocolExecute_MultiRoundProgression(t *testing.T) {
	// Track per-round invocation count to confirm we really ran 3
	// rounds (not just reported 3).
	var rounds [4]int32 // index 1..3 used
	invoker := AgentInvokerFunc(func(ctx context.Context, agent *topology.Agent,
		prompt string, debateCtx DebateContext) (*PhaseResponse, error) {
		if debateCtx.Round >= 1 && debateCtx.Round <= 3 {
			atomic.AddInt32(&rounds[debateCtx.Round], 1)
		}
		// Distinct content per round so consensus never converges.
		return &PhaseResponse{
			AgentID: agent.ID,
			Content: fmt.Sprintf("%s-round-%d-phase-%s",
				agent.ID, debateCtx.Round, debateCtx.CurrentPhase),
		}, nil
	})

	cfg := validDebateConfig()
	cfg.MaxRounds = 3
	// Force completion of all rounds (no early exit).
	cfg.EnableEarlyExit = false

	p := NewProtocol(cfg)
	if err := p.RegisterAgent("a1", invoker); err != nil {
		t.Fatalf("RegisterAgent a1: %v", err)
	}
	if err := p.RegisterAgent("a2", invoker); err != nil {
		t.Fatalf("RegisterAgent a2: %v", err)
	}

	res, err := p.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.RoundsCompleted != 3 {
		t.Fatalf("Execute: RoundsCompleted=%d, want 3", res.RoundsCompleted)
	}
	if res.EarlyExit {
		t.Fatalf("Execute: EarlyExit=true, want false (cfg disables it)")
	}
	wantPhases := 3 * len(executionPhases)
	if got := len(res.Phases); got != wantPhases {
		t.Fatalf("Execute: len(Phases)=%d, want %d", got, wantPhases)
	}
	// Per-round invocation count: 2 agents * len(executionPhases).
	wantPerRound := int32(2 * len(executionPhases))
	for r := 1; r <= 3; r++ {
		got := atomic.LoadInt32(&rounds[r])
		if got != wantPerRound {
			t.Fatalf("Execute: round %d invocations=%d, want %d",
				r, got, wantPerRound)
		}
	}
}

func TestProtocolExecute_EarlyExitOnConsensus(t *testing.T) {
	// All agents return the SAME content so consensus confidence == 1.0
	// from round 1 onward.
	unanimous := AgentInvokerFunc(func(ctx context.Context, agent *topology.Agent,
		prompt string, debateCtx DebateContext) (*PhaseResponse, error) {
		return &PhaseResponse{
			AgentID: agent.ID,
			Content: "we-all-agree",
		}, nil
	})

	cfg := validDebateConfig()
	cfg.MaxRounds = 5
	cfg.EnableEarlyExit = true
	cfg.MinConsensusScore = 0.5

	p := NewProtocol(cfg)
	if err := p.RegisterAgent("a1", unanimous); err != nil {
		t.Fatalf("RegisterAgent a1: %v", err)
	}
	if err := p.RegisterAgent("a2", unanimous); err != nil {
		t.Fatalf("RegisterAgent a2: %v", err)
	}
	if err := p.RegisterAgent("a3", unanimous); err != nil {
		t.Fatalf("RegisterAgent a3: %v", err)
	}

	res, err := p.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.EarlyExit {
		t.Fatalf("Execute: EarlyExit=false, want true")
	}
	if res.RoundsCompleted >= cfg.MaxRounds {
		t.Fatalf("Execute: RoundsCompleted=%d, want < MaxRounds=%d",
			res.RoundsCompleted, cfg.MaxRounds)
	}
	if res.RoundsCompleted != 1 {
		t.Fatalf("Execute: RoundsCompleted=%d, want 1 (unanimous from round 1)",
			res.RoundsCompleted)
	}
	if res.FinalConsensus == nil {
		t.Fatalf("Execute: FinalConsensus nil")
	}
	if res.FinalConsensus.Choice != "we-all-agree" {
		t.Fatalf("Execute: Choice=%q, want we-all-agree", res.FinalConsensus.Choice)
	}
	if res.FinalConsensus.Confidence < 0.5 {
		t.Fatalf("Execute: Confidence=%v, want >= 0.5", res.FinalConsensus.Confidence)
	}
	// Each round runs len(executionPhases) phases, and each phase
	// records one contribution per agent — so the contributor list
	// contains 3 agents x len(executionPhases) entries.
	wantContribs := 3 * len(executionPhases)
	if len(res.FinalConsensus.Contributors) != wantContribs {
		t.Fatalf("Execute: Contributors=%v len=%d, want %d entries",
			res.FinalConsensus.Contributors,
			len(res.FinalConsensus.Contributors), wantContribs)
	}
	if res.EarlyExitReason == "" {
		t.Fatalf("Execute: EarlyExitReason empty")
	}
}

func TestProtocolExecute_CtxCancel(t *testing.T) {
	// Slow invoker — sleeps 50ms per call; ctx cancels after 10ms.
	slow := AgentInvokerFunc(func(ctx context.Context, agent *topology.Agent,
		prompt string, debateCtx DebateContext) (*PhaseResponse, error) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
			return &PhaseResponse{
				AgentID: agent.ID,
				Content: "slow-result",
			}, nil
		}
	})

	cfg := validDebateConfig()
	cfg.MaxRounds = 5
	p := NewProtocol(cfg)
	if err := p.RegisterAgent("slow-1", slow); err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(),
		10*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := p.Execute(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("Execute(ctx cancel): expected non-nil error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) &&
		!errors.Is(err, context.Canceled) {
		t.Fatalf("Execute(ctx cancel): expected ctx error, got %v", err)
	}
	// MaxRounds=5 with len(executionPhases) phases * 50ms each would
	// take >=1s if not cancelled. Cancellation must abort fast.
	if elapsed > 500*time.Millisecond {
		t.Fatalf("Execute(ctx cancel): elapsed=%v, want < 500ms (cancellation not honoured)",
			elapsed)
	}
}

func TestProtocol_RegisterAgent_Validation(t *testing.T) {
	p := NewProtocol(validDebateConfig())
	if err := p.RegisterAgent("", constantInvoker("X")); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("RegisterAgent(\"\"): expected ErrInvalidConfig, got %v", err)
	}
	if err := p.RegisterAgent("a", nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("RegisterAgent(nil): expected ErrInvalidConfig, got %v", err)
	}
	if err := p.RegisterAgent("a", constantInvoker("X")); err != nil {
		t.Fatalf("RegisterAgent(valid): unexpected err %v", err)
	}
	if err := p.RegisterAgent("b", constantInvoker("Y")); err != nil {
		t.Fatalf("RegisterAgent(valid b): unexpected err %v", err)
	}
	ids := p.Agents()
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "b" {
		t.Fatalf("Agents(): got %v, want [a b] sorted", ids)
	}
}
