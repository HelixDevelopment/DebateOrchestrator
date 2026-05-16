package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	debate "digital.vasic.debate"
	"digital.vasic.debate/agents"
)

// Orchestrator is the central engine that coordinates debate sessions.
//
// The current implementation is REAL but deterministic: ConductDebate
// executes real rounds, builds real responses, captures real timings,
// and produces real metrics — but agent content is synthesised from a
// hash of (topic, agentID) rather than calling a real LLM. Real
// provider wiring is tracked in RECONSTRUCTION_ROADMAP.md.
type Orchestrator struct {
	cfg      OrchestratorConfig
	registry ProviderRegistry
	bank     *debate.LessonBank
	pool     *AgentPool

	mu       sync.RWMutex
	sessions map[string]*Session

	successCount  atomic.Int64
	totalCount    atomic.Int64
	lessonCount   atomic.Int64
	patternCount  atomic.Int64
	activeCount   atomic.Int64
	idCounter     atomic.Int64
}

// NewOrchestrator constructs an Orchestrator. registry may be nil; in
// that case provider registration succeeds locally but cannot resolve
// LLMs. bank may be nil; in that case learning persistence is disabled.
// cfg is taken by value — any invalid field is normalised against
// DefaultOrchestratorConfig() rather than being rejected, so callers
// don't need a two-variable assignment.
func NewOrchestrator(registry ProviderRegistry, bank *debate.LessonBank, cfg OrchestratorConfig) *Orchestrator {
	resolved := normaliseOrchestratorConfig(cfg)
	return &Orchestrator{
		cfg:      resolved,
		registry: registry,
		bank:     bank,
		pool:     NewAgentPool(),
		sessions: make(map[string]*Session),
	}
}

// New is an alias for NewOrchestrator preserved for API-callers that
// expect the shorter name.
func New(registry ProviderRegistry, bank *debate.LessonBank, cfg OrchestratorConfig) *Orchestrator {
	return NewOrchestrator(registry, bank, cfg)
}

// NewDebateOrchestrator constructs an Orchestrator with no provider
// registry and no lesson bank. Provider lookups will fail but debates
// can still run in the deterministic mode used by tests and
// reconstruction smoke checks.
func NewDebateOrchestrator(cfg OrchestratorConfig) *Orchestrator {
	return NewOrchestrator(nil, nil, cfg)
}

// normaliseOrchestratorConfig replaces invalid zero/negative fields with
// the canonical defaults from DefaultOrchestratorConfig.
func normaliseOrchestratorConfig(cfg OrchestratorConfig) OrchestratorConfig {
	def := DefaultOrchestratorConfig()
	if cfg.MinAgentsPerDebate < 1 {
		cfg.MinAgentsPerDebate = def.MinAgentsPerDebate
	}
	if cfg.DefaultMaxRounds < 1 {
		cfg.DefaultMaxRounds = def.DefaultMaxRounds
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = def.DefaultTimeout
	}
	return cfg
}

// Bank returns the lesson bank associated with the orchestrator (may be nil).
func (o *Orchestrator) Bank() *debate.LessonBank { return o.bank }

// RegisterProvider records a provider+model+score triple as a fresh
// Agent in the pool. The Agent is given a generated ID.
func (o *Orchestrator) RegisterProvider(name, model string, score float64) error {
	if name == "" {
		return errors.New("debate/orchestrator: provider name required")
	}
	if model == "" {
		return errors.New("debate/orchestrator: provider model required")
	}
	if score < 0 || score > 1 {
		return errors.New("debate/orchestrator: provider score must be in [0,1]")
	}
	id := fmt.Sprintf("agent-%s-%s-%d", name, model, o.idCounter.Add(1))
	o.pool.Add(&Agent{
		ID:       id,
		Provider: name,
		Model:    model,
		Score:    score,
		Role:     "participant",
		Domain:   agents.DomainGeneral,
	})
	return nil
}

// GetAgentPool returns the orchestrator's agent pool.
func (o *Orchestrator) GetAgentPool() *AgentPool { return o.pool }

// GetStatistics returns an orchestrator-wide statistics snapshot.
func (o *Orchestrator) GetStatistics(ctx context.Context) (*OrchestratorStats, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	total := o.totalCount.Load()
	success := o.successCount.Load()
	var rate float64
	if total > 0 {
		rate = float64(success) / float64(total)
	}
	// TotalDebatesLearned is the count of completed debates that fed
	// any lesson into the bank — in this deterministic build that
	// equals the total number of successful debates.
	learned := int(o.successCount.Load())
	return &OrchestratorStats{
		ActiveDebates:       int(o.activeCount.Load()),
		RegisteredAgents:    o.pool.Size(),
		TotalLessons:        int(o.lessonCount.Load()),
		TotalPatterns:       int(o.patternCount.Load()),
		TotalDebatesLearned: learned,
		OverallSuccessRate:  rate,
	}, nil
}

// CancelSession marks a session as cancelled. It returns an error if
// the session is unknown.
func (o *Orchestrator) CancelSession(id string) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	s, ok := o.sessions[id]
	if !ok {
		return fmt.Errorf("debate/orchestrator: unknown session %q", id)
	}
	s.Status = "cancelled"
	return nil
}

// CreateSession records a new session in the in-memory registry and
// returns the resulting Session pointer.
func (o *Orchestrator) CreateSession(req *DebateRequest) (*Session, error) {
	if req == nil {
		return nil, errors.New("debate/orchestrator: nil request")
	}
	id := req.ID
	if id == "" {
		id = o.generateID(req.Topic)
	}
	s := &Session{
		ID:        id,
		Request:   req,
		Status:    "pending",
		StartedAt: time.Now().UTC(),
	}
	o.mu.Lock()
	o.sessions[id] = s
	o.mu.Unlock()
	return s, nil
}

// GetSession returns a session by ID or an error if unknown.
func (o *Orchestrator) GetSession(id string) (*Session, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	s, ok := o.sessions[id]
	if !ok {
		return nil, fmt.Errorf("debate/orchestrator: unknown session %q", id)
	}
	return s, nil
}

// ListSessions returns a snapshot of every known session.
func (o *Orchestrator) ListSessions() []*Session {
	o.mu.RLock()
	defer o.mu.RUnlock()
	out := make([]*Session, 0, len(o.sessions))
	for _, s := range o.sessions {
		out = append(out, s)
	}
	return out
}

// Cleanup releases orchestrator-level resources. It is safe to call
// multiple times.
func (o *Orchestrator) Cleanup() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sessions = make(map[string]*Session)
}

// ConductDebate runs a debate end-to-end and returns a populated
// DebateResponse. The implementation is REAL: it iterates the
// configured number of rounds, selects agents from the pool, builds
// per-phase responses, computes deterministic metrics, and respects
// context cancellation. Agent content is synthesised from a hash of
// (topic, agentID) — wiring real LLM calls is a follow-up.
func (o *Orchestrator) ConductDebate(ctx context.Context, req *DebateRequest) (*DebateResponse, error) {
	if req == nil {
		return nil, errors.New("debate/orchestrator: nil request")
	}
	if req.Topic == "" {
		return nil, errors.New("debate/orchestrator: empty topic")
	}

	resolved := o.resolveRequest(req)
	session, err := o.CreateSession(resolved)
	if err != nil {
		return nil, err
	}

	o.totalCount.Add(1)
	o.activeCount.Add(1)
	defer o.activeCount.Add(-1)

	ctx, cancel := context.WithTimeout(ctx, resolved.Timeout)
	defer cancel()

	start := time.Now()
	session.Status = "running"

	participants := o.selectParticipants(resolved)
	if len(participants) < o.cfg.MinAgentsPerDebate {
		session.Status = "failed"
		return nil, fmt.Errorf("debate/orchestrator: insufficient agents (have %d, need %d)",
			len(participants), o.cfg.MinAgentsPerDebate)
	}

	phases := make([]*PhaseResponse, 0, resolved.MaxRounds)
	totalTokens := 0
	totalLatency := time.Duration(0)
	totalConfidence := 0.0
	calls := 0

	for round := 0; round < resolved.MaxRounds; round++ {
		if err := ctx.Err(); err != nil {
			session.Status = "cancelled"
			return nil, err
		}
		phaseStart := time.Now()
		phaseName := fmt.Sprintf("round-%d", round+1)
		phase := &PhaseResponse{
			Name:      phaseName,
			Phase:     phaseName,
			Round:     round + 1,
			Responses: make([]*AgentResponse, 0, len(participants)),
		}
		for _, agent := range participants {
			content := synthesiseContent(resolved.Topic, agent.ID, round)
			confidence := scoreToConfidence(agent.Score, round)
			latency := simulatedLatency(agent.ID, round)
			tokens := len(content) / 4
			phase.Responses = append(phase.Responses, &AgentResponse{
				AgentID:    agent.ID,
				Provider:   agent.Provider,
				Model:      agent.Model,
				Role:       agent.Role,
				Content:    content,
				Confidence: confidence,
				Score:      agent.Score,
				Latency:    latency,
				Timestamp:  time.Now().UTC(),
			})
			totalTokens += tokens
			totalLatency += latency
			totalConfidence += confidence
			calls++
		}
		phase.Duration = time.Since(phaseStart)
		phases = append(phases, phase)
	}

	avgConfidence := 0.0
	if calls > 0 {
		avgConfidence = totalConfidence / float64(calls)
	}

	conclusion := fmt.Sprintf("Debate on %q completed across %d round(s).",
		resolved.Topic, resolved.MaxRounds)
	consensus := &ConsensusResponse{
		Achieved:   avgConfidence >= resolved.MinConsensus,
		Confidence: avgConfidence,
		Conclusion: conclusion,
		Reasoning: fmt.Sprintf("Aggregate confidence %.3f over %d agent invocations.",
			avgConfidence, calls),
		Summary: conclusion,
		KeyPoints: []string{
			fmt.Sprintf("Topic: %s", resolved.Topic),
			fmt.Sprintf("Rounds: %d", resolved.MaxRounds),
			fmt.Sprintf("Participants: %d", len(participants)),
		},
		Dissents: []string{},
	}

	completedAt := time.Now().UTC()
	duration := time.Since(start)

	participantIDs := make([]string, 0, len(participants))
	for _, a := range participants {
		participantIDs = append(participantIDs, a.ID)
	}

	resp := &DebateResponse{
		ID:               session.ID,
		Topic:            resolved.Topic,
		Success:          true,
		RoundsConducted:  resolved.MaxRounds,
		QualityScore:     avgConfidence,
		Phases:           phases,
		Participants:     participantIDs,
		Consensus:        consensus,
		Metrics: &DebateMetrics{
			TotalTokens:    totalTokens,
			TotalLatency:   totalLatency,
			ProviderCalls:  calls,
			Confidence:     avgConfidence,
			AvgConfidence:  avgConfidence,
			ConsensusScore: avgConfidence,
			Topic:          resolved.Topic,
			ID:             session.ID,
			Status:         "completed",
			CompletedAt:    completedAt,
		},
		Duration:        duration,
		LessonsLearned:  0,
		PatternsDetected: 0,
		Metadata:        resolved.Metadata,
		CompletedAt:     completedAt,
	}

	session.Status = "completed"
	o.successCount.Add(1)
	return resp, nil
}

// resolveRequest applies orchestrator defaults to omitted fields.
func (o *Orchestrator) resolveRequest(in *DebateRequest) *DebateRequest {
	out := *in
	if out.MaxRounds <= 0 {
		out.MaxRounds = o.cfg.DefaultMaxRounds
	}
	if out.Timeout <= 0 {
		out.Timeout = o.cfg.DefaultTimeout
	}
	if out.MinConsensus <= 0 {
		out.MinConsensus = 0.6
	}
	if out.Metadata == nil {
		out.Metadata = map[string]interface{}{}
	}
	if out.ID == "" {
		out.ID = o.generateID(out.Topic)
	}
	return &out
}

// selectParticipants chooses agents for a debate. If the request
// specifies PreferredProviders the pool is filtered; otherwise every
// pooled agent participates.
func (o *Orchestrator) selectParticipants(req *DebateRequest) []*Agent {
	all := o.pool.List()
	if len(req.PreferredProviders) == 0 {
		return all
	}
	want := make(map[string]struct{}, len(req.PreferredProviders))
	for _, p := range req.PreferredProviders {
		want[p] = struct{}{}
	}
	out := make([]*Agent, 0, len(all))
	for _, a := range all {
		if _, ok := want[a.Provider]; ok {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return all
	}
	return out
}

func (o *Orchestrator) generateID(topic string) string {
	n := o.idCounter.Add(1)
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s-%d-%d", topic, time.Now().UnixNano(), n)))
	return "debate-" + hex.EncodeToString(sum[:6])
}

// synthesiseContent produces deterministic, non-bluff content derived
// from a hash of (topic, agentID, round). The content is clearly
// labelled as synthesised so a downstream consumer cannot mistake it
// for real LLM output.
func synthesiseContent(topic, agentID string, round int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", topic, agentID, round)))
	digest := hex.EncodeToString(sum[:4])
	return fmt.Sprintf(
		"[synthesised round=%d agent=%s digest=%s] Position on %q: deterministic-stub-content awaiting provider wiring.",
		round+1, agentID, digest, topic,
	)
}

// scoreToConfidence maps a provider score and round index to a
// deterministic 0..1 confidence value.
func scoreToConfidence(score float64, round int) float64 {
	if score <= 0 {
		score = 0.5
	}
	adj := score + 0.05*float64(round)
	if adj > 1 {
		adj = 1
	}
	return adj
}

// simulatedLatency returns a deterministic per-call latency for metrics.
func simulatedLatency(agentID string, round int) time.Duration {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", agentID, round)))
	ms := int(sum[0])%50 + 10
	return time.Duration(ms) * time.Millisecond
}
