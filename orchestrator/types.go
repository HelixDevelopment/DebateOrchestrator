// Package orchestrator implements the core debate-orchestration engine.
// The current implementation is REAL but deterministic: ConductDebate
// executes real rounds, builds real responses, captures real timings,
// but synthesises agent content from a hash of (topic, agentID) rather
// than calling real LLM providers. Real provider wiring is tracked in
// RECONSTRUCTION_ROADMAP.md and gated by CONST-035 evidence.
package orchestrator

import (
	"context"
	"time"

	"digital.vasic.debate/agents"
	"digital.vasic.debate/topology"
)

// OrchestratorConfig governs the behaviour of an Orchestrator instance.
type OrchestratorConfig struct {
	// EnableLearning toggles in-process learning persistence.
	EnableLearning bool
	// EnableCrossDebateLearning toggles transfer between debate sessions.
	EnableCrossDebateLearning bool
	// MinAgentsPerDebate is the lower bound on participants per round.
	MinAgentsPerDebate int
	// DefaultMaxRounds is the per-debate round cap when the request omits one.
	DefaultMaxRounds int
	// DefaultTimeout is the per-debate timeout when the request omits one.
	DefaultTimeout time.Duration
	// DefaultTopology is the topology used when the request omits one.
	DefaultTopology topology.TopologyType
}

// DefaultOrchestratorConfig returns a conservative working configuration.
// Returned by value so callers can mutate fields without disturbing
// any shared package-level state.
func DefaultOrchestratorConfig() OrchestratorConfig {
	return OrchestratorConfig{
		EnableLearning:            true,
		EnableCrossDebateLearning: false,
		MinAgentsPerDebate:        2,
		DefaultMaxRounds:          3,
		DefaultTimeout:            30 * time.Second,
		DefaultTopology:           topology.GraphMesh,
	}
}

// DebateRequest is the inbound request envelope for ConductDebate.
type DebateRequest struct {
	// ID is the caller-supplied debate identifier. Empty -> auto-generated.
	ID string
	// Topic is the question to be debated.
	Topic string
	// Context is supplementary background prepended to every prompt.
	Context string
	// Language is the BCP-47 language tag of the topic.
	Language string
	// MaxRounds caps the number of debate rounds.
	MaxRounds int
	// Timeout caps the wall-clock duration of the debate.
	Timeout time.Duration
	// Metadata is opaque caller state echoed into the response.
	Metadata map[string]interface{}
	// PreferredProviders is an ordered list of provider names to favour.
	PreferredProviders []string
	// MinConsensus is the 0..1 threshold required to mark consensus achieved.
	MinConsensus float64
	// EnableLearning overrides the orchestrator-level learning toggle when non-nil.
	EnableLearning *bool
}

// DebateResponse is the result envelope returned by ConductDebate.
type DebateResponse struct {
	// ID echoes the debate identifier.
	ID string
	// Topic echoes the debated topic.
	Topic string
	// Success indicates the debate ran to completion without errors.
	Success bool
	// RoundsConducted is the actual number of rounds executed.
	RoundsConducted int
	// QualityScore is the orchestrator's 0..1 estimate of debate quality.
	QualityScore float64
	// Phases is the ordered list of phase responses.
	Phases []*PhaseResponse
	// Participants is the list of agent IDs that participated.
	Participants []string
	// Consensus summarises whether the debate reached consensus.
	Consensus *ConsensusResponse
	// Metrics carries token / latency / call counters.
	Metrics *DebateMetrics
	// Duration is the wall-clock execution duration.
	Duration time.Duration
	// LessonsLearned is the number of lessons persisted during the debate.
	LessonsLearned int
	// PatternsDetected is the number of recurring patterns identified.
	PatternsDetected int
	// Metadata echoes the request metadata.
	Metadata map[string]interface{}
	// CompletedAt is the UTC completion timestamp.
	CompletedAt time.Time
}

// PhaseResponse captures the output of a single phase of debate.
type PhaseResponse struct {
	// Name is the human-readable phase name.
	Name string
	// Phase is a machine-readable phase identifier (e.g. "round-1",
	// "synthesis", "critique") used by downstream consumers that need
	// to filter or group phases.
	Phase string
	// Round is the 1-indexed round number this phase belongs to.
	Round int
	// Responses is the per-agent output for this phase.
	Responses []*AgentResponse
	// Duration is the wall-clock time spent in this phase.
	Duration time.Duration
}

// AgentResponse captures one agent's output during one phase.
type AgentResponse struct {
	// AgentID is the agent's unique identifier.
	AgentID string
	// Provider is the LLM provider name (e.g. "openai", "ollama").
	Provider string
	// Model is the model identifier within the provider (e.g. "gpt-4").
	Model string
	// Role is the human-readable role the agent played in this phase
	// (e.g. "participant", "critic", "synthesiser").
	Role string
	// Content is the agent's textual output.
	Content string
	// Confidence is the agent's 0..1 self-reported confidence.
	Confidence float64
	// Score is the quality score awarded to this response (0..1).
	Score float64
	// Latency is the wall-clock time this single response consumed.
	Latency time.Duration
	// Timestamp is the UTC emission time.
	Timestamp time.Time
}

// ConsensusResponse summarises whether the debate reached consensus.
type ConsensusResponse struct {
	// Achieved is true when MinConsensus is met or exceeded.
	Achieved bool
	// Confidence is the aggregated 0..1 confidence.
	Confidence float64
	// Conclusion is the synthesised conclusion sentence.
	Conclusion string
	// Reasoning is a short rationale for the conclusion.
	Reasoning string
	// Summary is a short prose summary of the consensus, suitable
	// for callers that want a single human-readable line.
	Summary string
	// KeyPoints is the ordered list of agreed-upon points.
	KeyPoints []string
	// Dissents is the ordered list of points that did not reach consensus.
	Dissents []string
}

// DebateMetrics aggregates quantitative telemetry over a debate.
type DebateMetrics struct {
	// TotalTokens is the sum of prompt + completion tokens.
	TotalTokens int
	// TotalLatency is the sum of per-call latencies.
	TotalLatency time.Duration
	// ProviderCalls is the count of provider invocations.
	ProviderCalls int
	// Confidence is the aggregate 0..1 confidence.
	Confidence float64
	// AvgConfidence is the mean per-call confidence (0..1).
	AvgConfidence float64
	// ConsensusScore is the 0..1 score awarded to the final consensus.
	ConsensusScore float64
	// Topic echoes the debate topic for downstream filtering.
	Topic string
	// ID echoes the debate identifier.
	ID string
	// Status is the terminal status string.
	Status string
	// CompletedAt is the UTC completion timestamp.
	CompletedAt time.Time
}

// OrchestratorStats is a snapshot of orchestrator-wide counters.
type OrchestratorStats struct {
	// ActiveDebates is the number of in-flight debates.
	ActiveDebates int
	// RegisteredAgents is the size of the agent pool.
	RegisteredAgents int
	// TotalLessons is the cumulative lesson count.
	TotalLessons int
	// TotalPatterns is the cumulative pattern count.
	TotalPatterns int
	// TotalDebatesLearned is the cumulative count of debates that
	// fed at least one lesson back into the lesson bank.
	TotalDebatesLearned int
	// OverallSuccessRate is the 0..1 fraction of debates that succeeded.
	OverallSuccessRate float64
}

// Session is the in-memory record of a single debate's lifecycle.
type Session struct {
	// ID is the session identifier.
	ID string
	// Request is the originating DebateRequest.
	Request *DebateRequest
	// Status is one of "pending", "running", "completed", "cancelled", "failed".
	Status string
	// StartedAt is the UTC start timestamp.
	StartedAt time.Time
}

// Agent is a single participant in the debate.
type Agent struct {
	// ID is the agent's unique identifier.
	ID string
	// Provider is the provider name (e.g. "openai", "ollama").
	Provider string
	// Model is the model identifier within the provider.
	Model string
	// Score is the provider-quality score in 0..1.
	Score float64
	// Role is the human-readable role.
	Role string
	// Domain is the agent's specialisation.
	Domain agents.DomainType
}

// Option is a functional option for the Orchestrator constructor.
type Option func(*Orchestrator)

// ProviderRegistry abstracts the provider lookup surface that the
// orchestrator needs at runtime. Implementations are supplied by the
// host application (HelixAgent in the canonical case).
type ProviderRegistry interface {
	// GetProvider resolves a provider by name. The return type is
	// interface{} so this package stays self-contained.
	GetProvider(name string) (interface{}, error)
	// GetProvidersByScore returns provider names sorted high-to-low by score.
	GetProvidersByScore() []string
	// IsProviderHealthy reports whether a provider is currently healthy.
	IsProviderHealthy(name string) bool
	// ListProviders returns the names of every registered provider.
	ListProviders() []string
	// RegisterProvider adds a provider under the given name.
	RegisterProvider(name string, provider interface{}) error
	// UnregisterProvider removes a provider.
	UnregisterProvider(name string) error
	// Count returns the number of registered providers.
	Count() int
}

// ProviderInvoker is the canonical "call a provider" callback used by
// callers that don't want a full ProviderRegistry surface.
type ProviderInvoker func(ctx context.Context, prompt string) (string, error)

// APICreateDebateRequest is the public HTTP-API request envelope.
type APICreateDebateRequest struct {
	// DebateID is the caller-supplied identifier. Empty -> auto-generated
	// by the orchestrator when the resulting DebateRequest is built.
	DebateID string
	// Topic is the debate topic.
	Topic string
	// MaxRounds caps the number of rounds.
	MaxRounds int
	// Timeout overrides the orchestrator's default per-debate timeout
	// when non-zero. Zero falls back to the orchestrator default.
	Timeout time.Duration
	// Strategy names the debate strategy the caller wants applied
	// (e.g. "mesh", "consensus", "majority"). Empty selects the
	// orchestrator default. Echoed into request metadata.
	Strategy string
	// Participants is the requested participant configuration.
	Participants []APIParticipantConfig
	// Metadata is opaque caller state.
	Metadata map[string]interface{}
}

// APIParticipantConfig describes one requested participant.
//
// Field aliases: LLMProvider / LLMModel are accepted alongside Provider /
// Model so callers that use either vocabulary can populate the same
// struct literal. Effective values are resolved by EffectiveProvider /
// EffectiveModel — when both forms are set the LLM-prefixed field wins
// because it is the more explicit spelling.
type APIParticipantConfig struct {
	// Name is the human-readable display name for the participant.
	Name string
	// Provider is the provider name.
	Provider string
	// Model is the model identifier within the provider.
	Model string
	// LLMProvider is an alias for Provider for callers that prefer the
	// LLM-prefixed vocabulary. If non-empty it takes precedence over
	// Provider when resolved via EffectiveProvider.
	LLMProvider string
	// LLMModel is an alias for Model for callers that prefer the
	// LLM-prefixed vocabulary. If non-empty it takes precedence over
	// Model when resolved via EffectiveModel.
	LLMModel string
	// Score is the provider-quality score in 0..1.
	Score float64
	// Role is the human-readable role.
	Role string
}

// EffectiveProvider returns the resolved provider name (LLMProvider
// takes precedence over Provider).
func (p APIParticipantConfig) EffectiveProvider() string {
	if p.LLMProvider != "" {
		return p.LLMProvider
	}
	return p.Provider
}

// EffectiveModel returns the resolved model identifier (LLMModel takes
// precedence over Model).
func (p APIParticipantConfig) EffectiveModel() string {
	if p.LLMModel != "" {
		return p.LLMModel
	}
	return p.Model
}

// APIStatistics is the snapshot returned by APIAdapter.GetStatistics.
type APIStatistics struct {
	// TotalDebates is the cumulative debate count.
	TotalDebates int
	// ActiveDebates is the in-flight count.
	ActiveDebates int
	// CompletedDebates is the completed count.
	CompletedDebates int
}

// Down is a sentinel used to express graceful-shutdown intent across
// the orchestrator's execution surfaces. The zero value is meaningful.
type Down struct{}

// ExecuteFlow performs the shutdown flow.
func (Down) ExecuteFlow(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// ExecutePlan performs the shutdown plan stage.
func (Down) ExecutePlan(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// ExecuteParallel performs the parallel shutdown stage.
func (Down) ExecuteParallel(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}
