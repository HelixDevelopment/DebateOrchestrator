package comprehensive

import (
	"context"
	"errors"
	"sync"

	"digital.vasic.debate/orchestrator"
)

// IntegrationManager is the high-level entry point used by HelixAgent.
// ExecuteDebate is REAL (delegates to a real Orchestrator). StreamDebate
// is an honest NotImplemented stub.
type IntegrationManager struct {
	cfg    *Config
	orch   *orchestrator.Orchestrator
	logger interface{}

	mu     sync.RWMutex
	agents map[string]*Agent
}

// NewIntegrationManager constructs an IntegrationManager. cfg may be nil
// in which case DefaultConfig is used. logger is an opaque, optional
// caller-provided logger (typed interface{} so DebateOrchestrator
// remains stdlib-only). Pass nil if no logger is desired.
func NewIntegrationManager(cfg *Config, logger interface{}) (*IntegrationManager, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	ocfg := orchestrator.DefaultOrchestratorConfig()
	if cfg.DefaultTimeout > 0 {
		ocfg.DefaultTimeout = cfg.DefaultTimeout
	}
	orch := orchestrator.NewOrchestrator(nil, nil, ocfg)
	return &IntegrationManager{
		cfg:    cfg,
		orch:   orch,
		logger: logger,
		agents: make(map[string]*Agent),
	}, nil
}

// ExecuteDebate runs a debate end-to-end. The agent pool of the
// underlying orchestrator is populated from any RegisterAgent calls.
func (m *IntegrationManager) ExecuteDebate(ctx context.Context, req *DebateRequest) (*DebateResponse, error) {
	if m == nil {
		return nil, errors.New("debate/comprehensive: IntegrationManager not initialised")
	}
	if req == nil {
		return nil, errors.New("debate/comprehensive: nil request")
	}
	return m.orch.ConductDebate(ctx, req)
}

// StreamDebate is a placeholder for the incremental streaming surface.
// It returns an explicit error so callers can detect non-availability
// instead of silently degrading.
func (m *IntegrationManager) StreamDebate(ctx context.Context, req *DebateRequest, handler StreamHandler) (*DebateResponse, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	_ = ctx
	_ = req
	_ = handler
	return nil, errors.New("debate/comprehensive: StreamDebate NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// StreamDebateRequest is the DebateStreamRequest-shaped variant of
// StreamDebate. It unpacks the embedded DebateRequest and handler from
// the request and delegates to StreamDebate. Today this returns the
// same honest NotYetImplemented error as StreamDebate; the indirection
// is intentional so callers can express a single struct payload while
// the real streaming pipeline is wired in.
func (m *IntegrationManager) StreamDebateRequest(ctx context.Context, req *DebateStreamRequest) (*DebateResponse, error) {
	if m == nil {
		return nil, errors.New("debate/comprehensive: IntegrationManager not initialised")
	}
	if req == nil {
		return nil, errors.New("debate/comprehensive: nil stream request")
	}
	return m.StreamDebate(ctx, req.DebateRequest, req.StreamHandler)
}

// GetAgentPool exposes the underlying orchestrator's agent pool.
func (m *IntegrationManager) GetAgentPool() *orchestrator.AgentPool {
	return m.orch.GetAgentPool()
}

// RegisterAgent records an Agent and forwards its provider/model/score
// triple to the underlying orchestrator.
func (m *IntegrationManager) RegisterAgent(agent *Agent) error {
	if m == nil {
		return errors.New("debate/comprehensive: IntegrationManager not initialised")
	}
	if agent == nil {
		return errors.New("debate/comprehensive: nil agent")
	}
	if m.cfg.MaxAgents > 0 && len(m.agents) >= m.cfg.MaxAgents {
		return errors.New("debate/comprehensive: agent pool full")
	}
	m.mu.Lock()
	m.agents[agent.ID] = agent
	m.mu.Unlock()
	return m.orch.RegisterProvider(agent.Provider, agent.Model, agent.Score)
}
