// Package voting hosts the vote-tally surface used by debate
// orchestration. The constructor returns a real configured
// WeightedVotingSystem, default configuration is real, vote storage
// is real (in-memory, thread-safe), and the inspection helpers
// (Size/VoteCount/GetStatistics/Reset) are real.
//
// Tally algorithms:
//   - Calculate            — real weighted-confidence sum over AddVote'd
//                            votes (alphabetical tie-break for determinism)
//   - CalculateBordaCount  — real Borda count over voter -> ranking
//                            map (top of an N-list scores N-1 points)
//   - CalculateCondorcet   — real pairwise-comparison Condorcet winner;
//                            returns ErrNoCondorcetWinner on a cycle
//   - CalculatePlurality   — real first-past-the-post count over the
//                            AddVote'd votes
//   - CalculateUnanimous   — real unanimity check over the AddVote'd
//                            votes; returns ErrNoUnanimity otherwise
//
// Tally remains an explicit honest stub (NotYetImplemented) because
// its design surface is still under reconstruction — see
// RECONSTRUCTION_ROADMAP.md.
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

// Sentinel errors returned by the advanced tally algorithms when the
// algorithm itself completes but the input does not yield a winner.
// Callers receive a non-nil Result with the populated Tally so they
// can inspect why no winner emerged.
var (
	// ErrNoCondorcetWinner is returned by CalculateCondorcet when no
	// choice beats every other choice in pairwise comparison
	// (a Condorcet cycle / paradox).
	ErrNoCondorcetWinner = errors.New("debate/voting: no Condorcet winner (cyclic preferences)")
	// ErrNoUnanimity is returned by CalculateUnanimous when the votes
	// are not unanimous on a single choice.
	ErrNoUnanimity = errors.New("debate/voting: votes are not unanimous")
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
// rankings (voter -> ordered choice list, index 0 = most preferred).
//
// For each ranking of N choices, the choice at index 0 receives N-1
// points, index 1 receives N-2, ..., index N-1 receives 0. Points
// are summed per choice across all voters; the choice with the
// highest total wins. Ties are broken alphabetically for
// determinism. The returned Result.Tally maps choice -> total Borda
// points; Result.TotalVotes is the number of voters who submitted a
// ranking; Result.Confidence is winner_points / max_possible_points
// (1.0 when the winner is top-ranked by every voter).
func (s *WeightedVotingSystem) CalculateBordaCount(ctx context.Context,
	rankings map[string][]string) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rankings) == 0 {
		return &Result{Tally: map[string]float64{}}, nil
	}
	tally := make(map[string]float64)
	// Score each voter's ranking individually so voters with
	// different ranking lengths are still scored consistently
	// (top of an N-list gets N-1 regardless of the global choice
	// universe size).
	var totalVoters int
	var maxPossibleForWinner float64
	for _, ranked := range rankings {
		if len(ranked) == 0 {
			continue
		}
		totalVoters++
		n := len(ranked)
		for i, choice := range ranked {
			points := float64(n - 1 - i)
			tally[choice] += points
		}
		// Each voter could give at most (n-1) points to a single
		// choice if it was at index 0.
		maxPossibleForWinner += float64(n - 1)
	}
	if totalVoters == 0 {
		return &Result{Tally: tally}, nil
	}
	// Determine winner deterministically (alphabetical on tie).
	keys := make([]string, 0, len(tally))
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var winner string
	best := -1.0
	for _, k := range keys {
		if tally[k] > best {
			best = tally[k]
			winner = k
		}
	}
	confidence := 0.0
	if maxPossibleForWinner > 0 {
		confidence = best / maxPossibleForWinner
	}
	return &Result{
		WinningChoice: winner,
		TotalVotes:    totalVoters,
		Tally:         tally,
		Confidence:    confidence,
	}, nil
}

// CalculateCondorcet runs the Condorcet tally via pairwise
// comparison over the supplied rankings (voter -> ordered choice
// list, index 0 = most preferred).
//
// For every pair (A, B) we count voters who rank A above B. A
// Condorcet winner is a choice that beats every other choice in
// these pairwise contests. If such a choice exists, it is returned
// as Result.WinningChoice with Confidence = wins/(N-1) where N is
// the number of distinct choices. If no Condorcet winner exists
// (a cycle), the returned Result still contains the per-choice win
// counts in Tally, WinningChoice is empty, and the error is
// ErrNoCondorcetWinner so callers can distinguish "algorithm
// failed" from "no winner exists".
func (s *WeightedVotingSystem) CalculateCondorcet(ctx context.Context,
	rankings map[string][]string) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(rankings) == 0 {
		return &Result{Tally: map[string]float64{}}, nil
	}
	// Collect the universe of choices.
	choiceSet := make(map[string]struct{})
	for _, ranked := range rankings {
		for _, c := range ranked {
			choiceSet[c] = struct{}{}
		}
	}
	choices := make([]string, 0, len(choiceSet))
	for c := range choiceSet {
		choices = append(choices, c)
	}
	sort.Strings(choices)

	// rankIndex[voter][choice] = position in voter's ranking, lower is better.
	// Choices a voter did not rank are treated as tied last.
	rankIndex := make(map[string]map[string]int, len(rankings))
	for voter, ranked := range rankings {
		idx := make(map[string]int, len(ranked))
		for i, c := range ranked {
			idx[c] = i
		}
		rankIndex[voter] = idx
	}
	notRanked := len(choices) // any unranked choice gets a position equal to len(choices) (worse than every ranked one).

	// pairwise[a][b] = number of voters who prefer a over b strictly.
	pairwise := make(map[string]map[string]int, len(choices))
	for _, a := range choices {
		pairwise[a] = make(map[string]int, len(choices))
	}
	for _, voterIdx := range rankIndex {
		for _, a := range choices {
			posA, okA := voterIdx[a]
			if !okA {
				posA = notRanked
			}
			for _, b := range choices {
				if a == b {
					continue
				}
				posB, okB := voterIdx[b]
				if !okB {
					posB = notRanked
				}
				if posA < posB {
					pairwise[a][b]++
				}
			}
		}
	}

	// Determine wins per choice (a beats b if pairwise[a][b] > pairwise[b][a]).
	tally := make(map[string]float64, len(choices))
	var winner string
	for _, a := range choices {
		wins := 0
		for _, b := range choices {
			if a == b {
				continue
			}
			if pairwise[a][b] > pairwise[b][a] {
				wins++
			}
		}
		tally[a] = float64(wins)
		if wins == len(choices)-1 {
			winner = a
		}
	}

	totalVoters := 0
	for _, ranked := range rankings {
		if len(ranked) > 0 {
			totalVoters++
		}
	}

	if winner == "" {
		return &Result{
			WinningChoice: "",
			TotalVotes:    totalVoters,
			Tally:         tally,
			Confidence:    0.0,
		}, ErrNoCondorcetWinner
	}
	confidence := 1.0
	if len(choices) > 1 {
		confidence = tally[winner] / float64(len(choices)-1)
	}
	return &Result{
		WinningChoice: winner,
		TotalVotes:    totalVoters,
		Tally:         tally,
		Confidence:    confidence,
	}, nil
}

// CalculatePlurality runs the plurality (first-past-the-post) tally
// over the votes currently held by the system (added via AddVote).
//
// Each vote contributes 1 to its Choice. The choice with the most
// votes wins. Ties are broken using the configured
// TieBreakMethod (TieBreakByHighestConfidence picks the choice with
// the higher aggregate confidence among the tied set), falling back
// to alphabetical order for determinism.
func (s *WeightedVotingSystem) CalculatePlurality(ctx context.Context) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.votes) == 0 {
		return &Result{Tally: map[string]float64{}}, nil
	}
	tally := make(map[string]float64)
	confSum := make(map[string]float64)
	confN := make(map[string]int)
	for _, v := range s.votes {
		if v == nil {
			continue
		}
		tally[v.Choice]++
		confSum[v.Choice] += v.Confidence
		confN[v.Choice]++
	}
	if len(tally) == 0 {
		return &Result{Tally: tally, TotalVotes: len(s.votes)}, nil
	}
	keys := make([]string, 0, len(tally))
	for k := range tally {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	// First pass: find the top vote count.
	var top float64 = -1
	for _, k := range keys {
		if tally[k] > top {
			top = tally[k]
		}
	}
	// Second pass: collect tied leaders.
	var leaders []string
	for _, k := range keys {
		if tally[k] == top {
			leaders = append(leaders, k)
		}
	}
	winner := leaders[0]
	if len(leaders) > 1 && s.Config.EnableTieBreaking &&
		s.Config.TieBreakMethod == TieBreakByHighestConfidence {
		bestConf := -1.0
		for _, k := range leaders {
			c := 0.0
			if n := confN[k]; n > 0 {
				c = confSum[k] / float64(n)
			}
			if c > bestConf {
				bestConf = c
				winner = k
			}
		}
	}
	winnerConf := 0.0
	if n := confN[winner]; n > 0 {
		winnerConf = confSum[winner] / float64(n)
	}
	return &Result{
		WinningChoice: winner,
		TotalVotes:    len(s.votes),
		Tally:         tally,
		Confidence:    winnerConf,
	}, nil
}

// CalculateUnanimous returns the unanimous choice across the votes
// currently held by the system (added via AddVote).
//
// If every vote shares the same Choice, the returned Result has
// WinningChoice = that choice and Confidence = 1.0. If any two
// votes disagree, the Result still contains the per-choice counts
// in Tally, WinningChoice is empty, and the error is ErrNoUnanimity
// so callers can distinguish "algorithm failed" from "no consensus".
func (s *WeightedVotingSystem) CalculateUnanimous(ctx context.Context) (*Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.votes) == 0 {
		return &Result{Tally: map[string]float64{}}, nil
	}
	tally := make(map[string]float64)
	for _, v := range s.votes {
		if v == nil {
			continue
		}
		tally[v.Choice]++
	}
	if len(tally) == 1 {
		var winner string
		for k := range tally {
			winner = k
		}
		return &Result{
			WinningChoice: winner,
			TotalVotes:    len(s.votes),
			Tally:         tally,
			Confidence:    1.0,
		}, nil
	}
	return &Result{
		WinningChoice: "",
		TotalVotes:    len(s.votes),
		Tally:         tally,
		Confidence:    0.0,
	}, ErrNoUnanimity
}
