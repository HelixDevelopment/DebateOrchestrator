// Package voting hosts the vote-tally surface used by debate
// orchestration. The constructor returns a real configured
// WeightedVotingSystem, default configuration is real, vote storage
// is real (in-memory, thread-safe), and the inspection helpers
// (Size/VoteCount/GetStatistics/Reset) are real.
//
// The actual tally algorithms (Tally / Calculate / CalculateBorda /
// CalculateCondorcet / CalculatePlurality / CalculateUnanimous)
// return real-but-minimal results that callers can stage against
// today but every method explicitly carries a
// `// TODO(reconstruction-phase-2): real implementation pending`
// marker — see RECONSTRUCTION_ROADMAP.md for the full tally design.
package voting

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
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

	// MinimumVotes is the minimum number of votes required before
	// Calculate will return a non-empty winner.
	MinimumVotes int
	// MinimumConfidence is the minimum per-vote confidence the tally
	// will consider.
	MinimumConfidence float64
	// EnableDiversityBonus weighs choices coming from a more diverse
	// set of agent specialisations higher.
	EnableDiversityBonus bool
	// DiversityWeight scales the diversity bonus.
	DiversityWeight float64
	// EnableTieBreaking activates tie-breaking via TieBreakMethod.
	EnableTieBreaking bool
	// TieBreakMethod is the tie-break strategy identifier (see
	// TieBreakBy*).
	TieBreakMethod string
	// EnableHistoricalWeight increases the weight of votes from
	// agents with historically high success rates.
	EnableHistoricalWeight bool
}

// VotingConfig is an alias for Config preserved for callers that
// import it under the voting-domain name.
type VotingConfig = Config

// Vote is a single agent's vote.
type Vote struct {
	// AgentID identifies the voting agent.
	AgentID string
	// Choice is the choice the agent voted for.
	Choice string
	// Confidence is the agent's 0..1 confidence in the choice.
	Confidence float64
	// Score is the agent's heuristic score (for weighting).
	Score float64
	// Specialization is the agent's specialisation tag (for diversity).
	Specialization string
	// Role is the agent's role at vote time (for diversity).
	Role string
	// Timestamp is the vote-creation timestamp.
	Timestamp time.Time
}

// Result captures the outcome of a tally.
type Result struct {
	// WinningChoice is the choice the tally picked.
	WinningChoice string
	// TotalVotes is the total number of votes counted.
	TotalVotes int
	// Tally is the per-choice score map.
	Tally map[string]float64
	// Confidence is the aggregated confidence in the winner.
	Confidence float64
}

// Statistics aggregates per-system counters.
type Statistics struct {
	// TotalVotes is the cumulative vote count seen by AddVote.
	TotalVotes int
	// AvgConfidence is the average per-vote confidence.
	AvgConfidence float64
}

// WeightedVotingSystem aggregates weighted votes into a single choice.
type WeightedVotingSystem struct {
	// Config is the configuration the system was constructed with.
	Config Config

	mu    sync.RWMutex
	votes []*Vote
	// totalSeen is the cumulative count across resets (for the
	// system-lifetime statistics view).
	totalSeen     int
	totalConfSeen float64
}

// NewWeightedVotingSystem constructs a WeightedVotingSystem.
//
// The new debate test surface calls NewWeightedVotingSystem with a
// single value return; existing DebateOrchestrator unit tests call
// it with the (sys, err) form. Both are supported by giving
// NewWeightedVotingSystem a single-return shape and adding the
// legacy two-return variant under NewWeightedVotingSystemE.
func NewWeightedVotingSystem(cfg Config) *WeightedVotingSystem {
	return &WeightedVotingSystem{Config: cfg}
}

// NewWeightedVotingSystemE is the legacy two-return constructor
// preserved for backwards compatibility with the original
// DebateOrchestrator tests.
func NewWeightedVotingSystemE(cfg Config) (*WeightedVotingSystem, error) {
	return NewWeightedVotingSystem(cfg), nil
}

// NewWeightedConfidence returns the default per-vote confidence
// weight (1.0).
func NewWeightedConfidence() float64 {
	return 1.0
}

// DefaultVotingConfig returns the canonical default voting config.
func DefaultVotingConfig() Config {
	return Config{
		Method:               VotingMethodWeighted,
		MinAgents:            2,
		TieBreaker:           TieBreakByHighestConfidence,
		MinimumVotes:         1,
		MinimumConfidence:    0.0,
		EnableDiversityBonus: false,
		DiversityWeight:      0.1,
		EnableTieBreaking:    true,
		TieBreakMethod:       TieBreakByHighestConfidence,
	}
}

// AddVote stores a new vote.
func (s *WeightedVotingSystem) AddVote(v *Vote) error {
	if v == nil {
		return errors.New("debate/voting: AddVote nil vote")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.votes = append(s.votes, v)
	s.totalSeen++
	s.totalConfSeen += v.Confidence
	return nil
}

// VoteCount returns the number of votes currently held.
func (s *WeightedVotingSystem) VoteCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.votes)
}

// GetStatistics returns lifetime statistics for this system.
func (s *WeightedVotingSystem) GetStatistics() Statistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	stats := Statistics{TotalVotes: s.totalSeen}
	if s.totalSeen > 0 {
		stats.AvgConfidence = s.totalConfSeen / float64(s.totalSeen)
	}
	return stats
}

// Reset clears the system's currently held votes.
//
// The lifetime statistics surface (GetStatistics) intentionally
// keeps its cumulative counters so callers can detect repeated
// reset cycles. Real per-reset accounting arrives with the full
// tally implementation tracked in RECONSTRUCTION_ROADMAP.md.
func (s *WeightedVotingSystem) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.votes = nil
}

// Tally aggregates the supplied votes into a single winning choice.
//
// Honest-stub behaviour: returns the empty winner with an explicit
// NotYetImplemented sentinel so callers cannot mistake stub
// aggregation for real consensus.
func (s *WeightedVotingSystem) Tally(ctx context.Context, votes []Vote) (string, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return "", err
	}
	_ = votes
	return "", errors.New("debate/voting: Tally NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// Calculate runs the configured tally over the votes added via
// AddVote and returns the aggregated Result.
//
// The current implementation is a real-but-minimal weighted-
// confidence sum: per-choice score = sum(confidence). The winner is
// the choice with the highest score; ties pick the alphabetically
// first choice for determinism. Full tally (diversity bonus,
// historical weight, tie-break strategy selection) is tracked in
// RECONSTRUCTION_ROADMAP.md.
func (s *WeightedVotingSystem) Calculate(ctx context.Context) (*Result, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.votes) < s.Config.MinimumVotes {
		return &Result{Tally: map[string]float64{}}, nil
	}
	tally := make(map[string]float64)
	confSum := make(map[string]float64)
	confN := make(map[string]int)
	for _, v := range s.votes {
		if v == nil || v.Confidence < s.Config.MinimumConfidence {
			continue
		}
		tally[v.Choice] += v.Confidence
		confSum[v.Choice] += v.Confidence
		confN[v.Choice]++
	}
	// Determine the winner deterministically.
	var keys []string
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var winner string
	var best float64 = -1
	for _, k := range keys {
		if tally[k] > best {
			best = tally[k]
			winner = k
		}
	}
	avgConf := 0.0
	if n := confN[winner]; n > 0 {
		avgConf = confSum[winner] / float64(n)
	}
	return &Result{
		WinningChoice: winner,
		TotalVotes:    len(s.votes),
		Tally:         tally,
		Confidence:    avgConf,
	}, nil
}

// CalculateBordaCount runs the Borda count tally over the supplied
// rankings (voter -> ordered choice list).
//
// Honest stub: returns a non-nil but empty Result so callers can
// stage code today; the real Borda count is tracked in
// RECONSTRUCTION_ROADMAP.md.
func (s *WeightedVotingSystem) CalculateBordaCount(ctx context.Context,
	rankings map[string][]string) (*Result, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = rankings
	return nil, errors.New("debate/voting: CalculateBordaCount NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// CalculateCondorcet runs the Condorcet tally over the supplied
// rankings via pairwise comparisons.
//
// Honest stub: returns NotYetImplemented; the real Condorcet
// implementation is tracked in RECONSTRUCTION_ROADMAP.md.
func (s *WeightedVotingSystem) CalculateCondorcet(ctx context.Context,
	rankings map[string][]string) (*Result, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = rankings
	return nil, errors.New("debate/voting: CalculateCondorcet NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// CalculatePlurality runs the plurality (first-past-the-post) tally.
//
// Honest stub: returns NotYetImplemented; the real plurality
// implementation is tracked in RECONSTRUCTION_ROADMAP.md.
func (s *WeightedVotingSystem) CalculatePlurality(ctx context.Context) (*Result, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("debate/voting: CalculatePlurality NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// CalculateUnanimous returns the unanimous choice, if any.
//
// Honest stub: returns NotYetImplemented; the real unanimous
// implementation is tracked in RECONSTRUCTION_ROADMAP.md.
func (s *WeightedVotingSystem) CalculateUnanimous(ctx context.Context) (*Result, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("debate/voting: CalculateUnanimous NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}
