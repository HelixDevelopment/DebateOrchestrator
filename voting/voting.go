// Package voting hosts the vote-tally surface used by debate
// orchestration. The current implementation is an honest stub: the
// constructor returns a real configured WeightedVotingSystem, default
// configuration is real, and Reset is a real no-op — but Tally
// returns an explicit NotYetImplemented error so callers cannot
// mistake stub aggregation for working consensus. Full
// implementation is tracked in RECONSTRUCTION_ROADMAP.md.
package voting

import (
	"context"
	"errors"
)

// Voting-method identifiers exposed as constants so callers can refer
// to them symbolically without string-literal drift.
const (
	// VotingMethodWeighted selects the weighted-confidence tally method.
	VotingMethodWeighted = "weighted"
	// TieBreakByHighestConfidence resolves ties by picking the choice
	// with the highest aggregate confidence.
	TieBreakByHighestConfidence = "highest_confidence"
	// TieBreakByMostVotes resolves ties by picking the choice with the
	// most raw votes.
	TieBreakByMostVotes = "most_votes"
)

// Config configures a voting system.
type Config struct {
	// Method is the voting method identifier (see VotingMethod*).
	Method string
	// MinAgents is the minimum number of agents required to tally.
	MinAgents int
	// TieBreaker is the tie-breaker identifier (see TieBreakBy*).
	TieBreaker string
}

// VotingConfig is an alias for Config preserved for callers that
// import it under the voting-domain name.
type VotingConfig = Config

// WeightedVotingSystem aggregates weighted votes into a single choice.
type WeightedVotingSystem struct {
	// Config is the configuration the system was constructed with.
	Config Config
}

// Vote is a single agent's vote.
type Vote struct {
	// AgentID identifies the voting agent.
	AgentID string
	// Choice is the choice the agent voted for.
	Choice string
	// Confidence is the agent's 0..1 confidence in the choice.
	Confidence float64
}

// NewWeightedVotingSystem constructs a WeightedVotingSystem.
func NewWeightedVotingSystem(cfg Config) (*WeightedVotingSystem, error) {
	return &WeightedVotingSystem{Config: cfg}, nil
}

// NewWeightedConfidence returns the default per-vote confidence
// weight (1.0).
func NewWeightedConfidence() float64 {
	return 1.0
}

// DefaultVotingConfig returns the canonical default voting config.
func DefaultVotingConfig() Config {
	return Config{
		Method:     VotingMethodWeighted,
		MinAgents:  2,
		TieBreaker: TieBreakByHighestConfidence,
	}
}

// Tally aggregates the supplied votes into a single winning choice.
func (s *WeightedVotingSystem) Tally(ctx context.Context, votes []Vote) (string, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return "", err
	}
	_ = votes
	return "", errors.New("debate/voting: Tally NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// Reset clears the system's internal state. The current
// implementation is a real no-op; once real state lands it will purge
// accumulated tallies.
func (s *WeightedVotingSystem) Reset() {
	// No-op: no internal state yet. Real reset arrives with the
	// real Tally implementation tracked in RECONSTRUCTION_ROADMAP.md.
}
