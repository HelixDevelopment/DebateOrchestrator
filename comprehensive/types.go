// Package comprehensive bundles the orchestrator together with the
// supporting role/agent/invoker machinery used by HelixAgent. The core
// ExecuteDebate flow is REAL; StreamDebate is REAL streaming-runtime
// using post-hoc replay: ExecuteDebate runs end-to-end, then the
// orchestrator response is walked to emit ordered StreamEvent values
// (started → per-phase phase_started/agent_response/phase_completed →
// completed) carrying the real phase, agent, and content data through
// the supplied StreamHandler. See StreamDebate's doc-comment for the
// full event contract and cancellation semantics.
package comprehensive

import (
	"context"
	"time"

	"digital.vasic.debate/orchestrator"
)

// Role identifies the function an Agent plays inside a debate.
type Role string

// Enumerated Role values.
const (
	RoleArchitect   Role = "architect"
	RoleModerator   Role = "moderator"
	RoleGenerator   Role = "generator"
	RoleBlueTeam    Role = "blue_team"
	RoleCritic      Role = "critic"
	RoleTester      Role = "tester"
	RoleValidator   Role = "validator"
	RoleSecurity    Role = "security"
	RolePerformance Role = "performance"
	RoleRedTeam     Role = "red_team"
	RoleRefactoring Role = "refactoring"
)

// Agent is a single configured participant in a comprehensive debate.
type Agent struct {
	// ID is the agent identifier.
	ID string
	// Role is the agent's declared role.
	Role Role
	// Provider is the LLM provider name.
	Provider string
	// Model is the model identifier within the provider.
	Model string
	// Score is the provider-quality score in 0..1.
	Score float64
}

// NewAgent constructs an Agent with an auto-assigned ID composed from
// the role, provider, and model.
func NewAgent(role Role, provider, model string, score float64) *Agent {
	return &Agent{
		ID:       string(role) + "/" + provider + "/" + model,
		Role:     role,
		Provider: provider,
		Model:    model,
		Score:    score,
	}
}

// LLMInvoker is the canonical "call an LLM" callback used by
// comprehensive's invoker registry.
type LLMInvoker func(ctx context.Context, prompt string) (string, error)

// Config governs IntegrationManager behaviour at construction time.
type Config struct {
	// MaxAgents caps the agent pool size.
	MaxAgents int
	// DefaultTimeout is the per-debate timeout.
	DefaultTimeout time.Duration
	// EnableStreaming toggles the streaming surface.
	EnableStreaming bool
	// MaxRounds caps the per-debate round count.
	MaxRounds int
	// MinConsensus is the minimum consensus score required to terminate
	// a debate as "agreed" (range 0..1).
	MinConsensus float64
	// QualityThreshold is the minimum solution quality score required
	// to terminate a debate as "high-quality" (range 0..1).
	QualityThreshold float64
}

// DefaultConfig returns a conservative working configuration.
func DefaultConfig() *Config {
	return &Config{
		MaxAgents:        16,
		DefaultTimeout:   60 * time.Second,
		EnableStreaming:  false,
		MaxRounds:        3,
		MinConsensus:     0.7,
		QualityThreshold: 0.6,
	}
}

// StreamEvent is the unit of incremental data emitted by StreamDebate.
type StreamEvent struct {
	// Type is the event kind ("phase_start", "agent_token", "phase_end", ...).
	Type string
	// Phase is the phase name the event belongs to.
	Phase string
	// AgentID is the agent the event belongs to, if any.
	AgentID string
	// Agent is the full *Agent the event belongs to, if any. Callers
	// that already have an AgentID may leave this nil.
	Agent *Agent
	// Team is the role/team the agent belongs to (e.g. "red", "blue",
	// "synthesisers"). Empty when not applicable.
	Team string
	// DebateID is the identifier of the debate the event belongs to.
	DebateID string
	// Progress is a 0..1 fraction of how far the debate has advanced.
	Progress float64
	// Content is the textual payload.
	Content string
	// Timestamp is the UTC emission time.
	Timestamp time.Time
}

// StreamHandler consumes StreamEvent values as the debate progresses.
type StreamHandler func(event *StreamEvent) error

// DebateRequest re-exports orchestrator.DebateRequest so callers can
// import a single package.
type DebateRequest = orchestrator.DebateRequest

// DebateResponse re-exports orchestrator.DebateResponse.
type DebateResponse = orchestrator.DebateResponse

// IntegrationStatistics is a snapshot of integration-manager counters.
type IntegrationStatistics struct {
	// TotalDebates is the cumulative debate count.
	TotalDebates int
	// ActiveDebates is the in-flight count.
	ActiveDebates int
}

// DebateStreamRequest wraps a DebateRequest with stream-specific options
// so callers can express both the debate inputs and the streaming
// preferences in a single value.
type DebateStreamRequest struct {
	// DebateRequest is the embedded debate input. Required.
	*DebateRequest
	// Stream signals that the caller expects incremental emission.
	Stream bool
	// StreamHandler receives StreamEvent values as the debate progresses.
	StreamHandler StreamHandler
}
