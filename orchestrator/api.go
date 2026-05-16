package orchestrator

import (
	"context"
	"errors"
	"time"
)

// APIAdapter exposes a simplified HTTP-friendly façade around an
// Orchestrator. It registers participants on demand and proxies the
// result of ConductDebate.
type APIAdapter struct {
	orch *Orchestrator
}

// NewAPIAdapter constructs an APIAdapter around the supplied Orchestrator.
func NewAPIAdapter(orch *Orchestrator) *APIAdapter {
	return &APIAdapter{orch: orch}
}

// CreateDebate registers any new participants from the request and
// then runs ConductDebate.
func (a *APIAdapter) CreateDebate(ctx context.Context, req *APICreateDebateRequest) (*DebateResponse, error) {
	if a == nil || a.orch == nil {
		return nil, errors.New("debate/orchestrator: APIAdapter not initialised")
	}
	if req == nil {
		return nil, errors.New("debate/orchestrator: nil API request")
	}
	for _, p := range req.Participants {
		if err := a.orch.RegisterProvider(p.EffectiveProvider(), p.EffectiveModel(), p.Score); err != nil {
			return nil, err
		}
	}
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = a.orch.cfg.DefaultTimeout
	}
	meta := req.Metadata
	if req.Strategy != "" {
		if meta == nil {
			meta = map[string]interface{}{}
		}
		meta["strategy"] = req.Strategy
	}
	dreq := &DebateRequest{
		ID:        req.DebateID,
		Topic:     req.Topic,
		MaxRounds: req.MaxRounds,
		Metadata:  meta,
		Timeout:   timeout,
	}
	return a.orch.ConductDebate(ctx, dreq)
}

// GetStatistics returns API-shaped statistics for the underlying orchestrator.
func (a *APIAdapter) GetStatistics(ctx context.Context) (*APIStatistics, error) {
	if a == nil || a.orch == nil {
		return nil, errors.New("debate/orchestrator: APIAdapter not initialised")
	}
	stats, err := a.orch.GetStatistics(ctx)
	if err != nil {
		return nil, err
	}
	total := int(a.orch.totalCount.Load())
	completed := int(a.orch.successCount.Load())
	return &APIStatistics{
		TotalDebates:     total,
		ActiveDebates:    stats.ActiveDebates,
		CompletedDebates: completed,
	}, nil
}

// NewProviderInvoker constructs a ProviderInvoker that resolves the
// named provider through the supplied registry and forwards the prompt.
// When provider, registry, or model is unusable the returned invoker
// returns an explicit error rather than silently succeeding.
func NewProviderInvoker(registry ProviderRegistry, name string) ProviderInvoker {
	return func(ctx context.Context, prompt string) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if registry == nil {
			return "", errors.New("debate/orchestrator: ProviderInvoker has no registry")
		}
		if name == "" {
			return "", errors.New("debate/orchestrator: ProviderInvoker has no provider name")
		}
		_, err := registry.GetProvider(name)
		if err != nil {
			return "", err
		}
		return "", errors.New("debate/orchestrator: ProviderInvoker provider-bridge NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
	}
}

// DefaultOrchestrator is a package-level singleton constructed with
// DefaultOrchestratorConfig and no registry. It exists so callers that
// want a quick handle don't have to wire one up explicitly.
var DefaultOrchestrator = NewOrchestrator(nil, nil, DefaultOrchestratorConfig())

// touch to satisfy unused-import on time when api.go is read in isolation.
var _ = time.Now
