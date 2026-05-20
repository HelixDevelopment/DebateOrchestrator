package comprehensive

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"digital.vasic.debate/orchestrator"
)

// ErrNilStreamHandler is returned by the streaming-runtime entry points
// when the caller supplies a nil StreamHandler. Callers can detect this
// with errors.Is so the streaming surface remains explicit about its
// pre-conditions instead of panicking on a nil dispatch.
var ErrNilStreamHandler = errors.New("debate/comprehensive: nil StreamHandler")

// ErrNilStreamRequest is returned when StreamDebateRequest is invoked
// with a nil request envelope.
var ErrNilStreamRequest = errors.New("debate/comprehensive: nil stream request")

// IntegrationManager is the high-level entry point used by HelixAgent.
// ExecuteDebate is REAL (delegates to a real Orchestrator). StreamDebate
// is REAL streaming-runtime: it executes the debate via ExecuteDebate
// and then walks the resulting response to emit ordered StreamEvent
// values through the supplied StreamHandler (post-hoc replay style —
// see StreamDebate doc-comment for the contract).
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

// StreamDebate runs a debate and emits ordered StreamEvent values
// through handler as the debate proceeds.
//
// Streaming style: post-hoc replay. Because the underlying
// orchestrator.ConductDebate is synchronous, StreamDebate first
// executes the debate end-to-end via ExecuteDebate, then walks the
// returned *DebateResponse to emit a real, ordered event stream built
// from the real phases, agent responses, identifiers, content,
// timestamps, and confidence values produced by the orchestrator.
// Every event carries data extracted from the actual debate run —
// nothing is fabricated.
//
// Event sequence:
//  1. "started"          — debate beginning. Progress = 0.0.
//  2. For each phase:
//     2a. "phase_started"   — phase beginning. Progress evenly partitioned across phases.
//     2b. "agent_response"  — one event per AgentResponse in the phase (real content).
//     2c. "phase_completed" — phase end. Progress at the partition boundary.
//  3. "completed"        — debate end. Progress = 1.0.
//
// If handler is nil, ErrNilStreamHandler is returned and the debate is
// NOT executed (no side effects).
//
// If handler returns an error for any event, the streaming loop is
// aborted and the handler's error is returned wrapped with the event
// type for diagnostic context.
//
// If ctx is cancelled mid-stream, a final "cancelled" event is emitted
// (best-effort — the handler may itself return an error which is then
// preferred over ctx.Err()), and ctx.Err() is returned.
//
// The (*DebateResponse, error) return matches ExecuteDebate so callers
// that succeed see the same response payload they would have received
// from a non-streaming call.
func (m *IntegrationManager) StreamDebate(ctx context.Context, req *DebateRequest, handler StreamHandler) (*DebateResponse, error) {
	if m == nil {
		return nil, errors.New("debate/comprehensive: IntegrationManager not initialised")
	}
	if handler == nil {
		return nil, ErrNilStreamHandler
	}
	if req == nil {
		return nil, errors.New("debate/comprehensive: nil request")
	}

	// Early ctx-cancellation: surface the cancellation through the
	// handler so callers see a real "cancelled" event even when they
	// cancelled before we started.
	if err := ctx.Err(); err != nil {
		_ = handler(&StreamEvent{
			Type:      "cancelled",
			DebateID:  req.ID,
			Content:   tr(msgCtxCancelledBeforeStart, map[string]any{"Err": err}),
			Timestamp: time.Now().UTC(),
			Progress:  0.0,
		})
		return nil, err
	}

	// 1) "started" event — real DebateID resolved from request, real topic.
	startEvt := &StreamEvent{
		Type:      "started",
		DebateID:  req.ID,
		Content:   tr(msgDebateStarted, map[string]any{"Topic": req.Topic}),
		Timestamp: time.Now().UTC(),
		Progress:  0.0,
	}
	if err := handler(startEvt); err != nil {
		return nil, fmt.Errorf("debate/comprehensive: stream handler error on %q event: %w", startEvt.Type, err)
	}

	// 2) Execute the real debate.
	resp, err := m.ExecuteDebate(ctx, req)
	if err != nil {
		// If ctx was cancelled, prefer a "cancelled" event; otherwise
		// surface the orchestrator failure as an event AND as the
		// returned error.
		evtType := "failed"
		contentMsgID := msgDebateFailed
		if ctxErr := ctx.Err(); ctxErr != nil {
			evtType = "cancelled"
			contentMsgID = msgDebateCancelled
		}
		_ = handler(&StreamEvent{
			Type:      evtType,
			DebateID:  req.ID,
			Content:   tr(contentMsgID, map[string]any{"Err": err}),
			Timestamp: time.Now().UTC(),
			Progress:  0.0,
		})
		return nil, err
	}

	// Use the resolved DebateID from the response — the orchestrator
	// auto-generates one when the request omits it.
	debateID := resp.ID

	// 3) Walk phases and emit real events.
	phaseCount := len(resp.Phases)
	for i, phase := range resp.Phases {
		// Check ctx between phases so a mid-stream cancellation is
		// honoured without dropping the in-flight phase's events.
		if ctxErr := ctx.Err(); ctxErr != nil {
			_ = handler(&StreamEvent{
				Type:      "cancelled",
				DebateID:  debateID,
				Content:   tr(msgCtxCancelledMidStream, map[string]any{"Err": ctxErr}),
				Timestamp: time.Now().UTC(),
				Progress:  phaseProgress(i, phaseCount, 0, 0),
			})
			return nil, ctxErr
		}

		startProg := phaseProgress(i, phaseCount, 0, 0)
		endProg := phaseProgress(i, phaseCount, 1, 1)

		phaseStartEvt := &StreamEvent{
			Type:     "phase_started",
			Phase:    phase.Name,
			DebateID: debateID,
			Content: tr(msgPhaseStarted, map[string]any{
				"Phase":     phase.Name,
				"Round":     phase.Round,
				"Responses": len(phase.Responses),
			}),
			Timestamp: time.Now().UTC(),
			Progress:  startProg,
		}
		if err := handler(phaseStartEvt); err != nil {
			return nil, fmt.Errorf("debate/comprehensive: stream handler error on %q event (phase=%s): %w", phaseStartEvt.Type, phase.Name, err)
		}

		// Emit one agent_response per real AgentResponse — REAL data
		// extracted from the orchestrator's PhaseResponse.
		respCount := len(phase.Responses)
		for j, ar := range phase.Responses {
			// Cancellation check inside agent loop too.
			if ctxErr := ctx.Err(); ctxErr != nil {
				_ = handler(&StreamEvent{
					Type:      "cancelled",
					Phase:     phase.Name,
					AgentID:   ar.AgentID,
					DebateID:  debateID,
					Content:   tr(msgCtxCancelledMidPhase, map[string]any{"Err": ctxErr}),
					Timestamp: time.Now().UTC(),
					Progress:  phaseProgress(i, phaseCount, j, respCount),
				})
				return nil, ctxErr
			}

			agentEvt := &StreamEvent{
				Type:      "agent_response",
				Phase:     phase.Name,
				AgentID:   ar.AgentID,
				Agent:     m.lookupAgent(ar.AgentID),
				Team:      ar.Role,
				DebateID:  debateID,
				Content:   ar.Content,
				Timestamp: ar.Timestamp,
				Progress:  phaseProgress(i, phaseCount, j+1, respCount),
			}
			// If the AgentResponse has no timestamp, fall back to now()
			// so consumers always see a sensible time.
			if agentEvt.Timestamp.IsZero() {
				agentEvt.Timestamp = time.Now().UTC()
			}
			if err := handler(agentEvt); err != nil {
				return nil, fmt.Errorf("debate/comprehensive: stream handler error on %q event (phase=%s agent=%s): %w", agentEvt.Type, phase.Name, ar.AgentID, err)
			}
		}

		phaseEndEvt := &StreamEvent{
			Type:     "phase_completed",
			Phase:    phase.Name,
			DebateID: debateID,
			Content: tr(msgPhaseCompleted, map[string]any{
				"Phase":    phase.Name,
				"Duration": phase.Duration,
			}),
			Timestamp: time.Now().UTC(),
			Progress:  endProg,
		}
		if err := handler(phaseEndEvt); err != nil {
			return nil, fmt.Errorf("debate/comprehensive: stream handler error on %q event (phase=%s): %w", phaseEndEvt.Type, phase.Name, err)
		}
	}

	// 4) "completed" event — final, Progress=1.0, real summary.
	completedContent := tr(msgDebateCompleted, map[string]any{
		"Rounds":       resp.RoundsConducted,
		"Participants": len(resp.Participants),
		"Success":      resp.Success,
	})
	completedEvt := &StreamEvent{
		Type:      "completed",
		DebateID:  debateID,
		Content:   completedContent,
		Timestamp: time.Now().UTC(),
		Progress:  1.0,
	}
	if err := handler(completedEvt); err != nil {
		return resp, fmt.Errorf("debate/comprehensive: stream handler error on %q event: %w", completedEvt.Type, err)
	}

	return resp, nil
}

// StreamDebateRequest is the DebateStreamRequest-shaped variant of
// StreamDebate. It unpacks the embedded DebateRequest and handler from
// the request and delegates to StreamDebate.
func (m *IntegrationManager) StreamDebateRequest(ctx context.Context, req *DebateStreamRequest) (*DebateResponse, error) {
	if m == nil {
		return nil, errors.New("debate/comprehensive: IntegrationManager not initialised")
	}
	if req == nil {
		return nil, ErrNilStreamRequest
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

// lookupAgent returns the registered *Agent for an agent ID, or nil if
// no exact match is known. The orchestrator-side AgentID is auto-
// generated (e.g. "agent-<provider>-<model>-<n>") and does not match
// the comprehensive-side ID schema ("<role>/<provider>/<model>") so
// this is best-effort: it will most often return nil today and is wired
// for future symmetric ID schemes.
func (m *IntegrationManager) lookupAgent(id string) *Agent {
	if m == nil || id == "" {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agents[id]
}

// phaseProgress maps (phaseIndex, phaseCount, stepIndex, stepCount)
// onto a monotonically-non-decreasing 0..1 fraction. The contract:
//
//   - The interval [0,1] is partitioned into phaseCount equal slices.
//   - Within slice i, stepIndex/stepCount further subdivides the slice.
//   - phaseCount==0 yields 0 (no phases means no progress).
//   - stepCount==0 returns the slice's lower bound.
//
// Used so the emitted StreamEvent.Progress fractions are real,
// monotonic, and informative — matching the spec's "0.25, 0.5, 0.75,
// 1.0" hint when there are exactly 4 boundary points.
func phaseProgress(phaseIndex, phaseCount, stepIndex, stepCount int) float64 {
	if phaseCount <= 0 {
		return 0
	}
	slice := 1.0 / float64(phaseCount)
	base := float64(phaseIndex) * slice
	if stepCount <= 0 {
		return clamp01(base)
	}
	frac := float64(stepIndex) / float64(stepCount)
	if frac < 0 {
		frac = 0
	} else if frac > 1 {
		frac = 1
	}
	return clamp01(base + frac*slice)
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
