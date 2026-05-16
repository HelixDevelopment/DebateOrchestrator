package voting

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewWeightedVotingSystemAndDefaultConfig(t *testing.T) {
	cfg := DefaultVotingConfig()
	if cfg.Method != VotingMethodWeighted {
		t.Fatalf("DefaultVotingConfig.Method = %q, want %q", cfg.Method, VotingMethodWeighted)
	}
	if cfg.MinAgents != 2 {
		t.Fatalf("DefaultVotingConfig.MinAgents = %d, want 2", cfg.MinAgents)
	}
	if cfg.TieBreaker != TieBreakByHighestConfidence {
		t.Fatalf("DefaultVotingConfig.TieBreaker = %q, want %q", cfg.TieBreaker, TieBreakByHighestConfidence)
	}

	sys := NewWeightedVotingSystem(cfg)
	if sys == nil {
		t.Fatalf("NewWeightedVotingSystem returned nil")
	}
	if sys.Config.Method != VotingMethodWeighted {
		t.Fatalf("sys.Config.Method = %q, want %q", sys.Config.Method, VotingMethodWeighted)
	}
	if w := NewWeightedConfidence(); w != 1.0 {
		t.Fatalf("NewWeightedConfidence = %v, want 1.0", w)
	}
	sys.Reset() // must not panic
}

func TestNewWeightedVotingSystemE_BackwardsCompat(t *testing.T) {
	sys, err := NewWeightedVotingSystemE(DefaultVotingConfig())
	if err != nil {
		t.Fatalf("NewWeightedVotingSystemE: unexpected error %v", err)
	}
	if sys == nil {
		t.Fatalf("NewWeightedVotingSystemE returned nil")
	}
}

func TestAddVoteAndStatistics(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	for i := 0; i < 4; i++ {
		if err := sys.AddVote(&Vote{
			AgentID:    "a",
			Choice:     "x",
			Confidence: 0.5,
			Timestamp:  time.Now(),
		}); err != nil {
			t.Fatalf("AddVote: %v", err)
		}
	}
	if got := sys.VoteCount(); got != 4 {
		t.Fatalf("VoteCount = %d, want 4", got)
	}
	stats := sys.GetStatistics()
	if stats.TotalVotes != 4 {
		t.Fatalf("Statistics.TotalVotes = %d, want 4", stats.TotalVotes)
	}
	if stats.AvgConfidence < 0.49 || stats.AvgConfidence > 0.51 {
		t.Fatalf("Statistics.AvgConfidence = %v, want ~0.5", stats.AvgConfidence)
	}
	sys.Reset()
	if got := sys.VoteCount(); got != 0 {
		t.Fatalf("VoteCount after Reset = %d, want 0", got)
	}
}

func TestCalculatePicksTopWeightedChoice(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	_ = sys.AddVote(&Vote{AgentID: "a", Choice: "alpha", Confidence: 0.9})
	_ = sys.AddVote(&Vote{AgentID: "b", Choice: "alpha", Confidence: 0.8})
	_ = sys.AddVote(&Vote{AgentID: "c", Choice: "beta", Confidence: 0.6})
	res, err := sys.Calculate(context.Background())
	if err != nil {
		t.Fatalf("Calculate: unexpected error %v", err)
	}
	if res.WinningChoice != "alpha" {
		t.Fatalf("WinningChoice = %q, want %q", res.WinningChoice, "alpha")
	}
	if res.TotalVotes != 3 {
		t.Fatalf("TotalVotes = %d, want 3", res.TotalVotes)
	}
}

func TestTallyIsHonestStub(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	_, err := sys.Tally(context.Background(), []Vote{{AgentID: "a", Choice: "x", Confidence: 1.0}})
	if err == nil {
		t.Fatalf("Tally: expected stub error, got nil")
	}
	if !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Tally: expected NotYetImplemented sentinel, got %q", err.Error())
	}
}

// TestCalculateBordaCount_PicksHighestScorer constructs a 3-voter,
// 3-choice scenario with an unambiguous Borda winner and asserts
// real Borda points are computed.
//
// Rankings (top -> bottom):
//
//	v1: alpha, beta,  gamma
//	v2: alpha, gamma, beta
//	v3: beta,  alpha, gamma
//
// Borda points (N=3, top=2, mid=1, bot=0):
//
//	alpha: 2 + 2 + 1 = 5
//	beta:  1 + 0 + 2 = 3
//	gamma: 0 + 1 + 0 = 1
//
// Winner: alpha with 5 points; max possible per voter = 2; total
// max for the winner across 3 voters = 6; expected confidence = 5/6.
func TestCalculateBordaCount_PicksHighestScorer(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	rankings := map[string][]string{
		"v1": {"alpha", "beta", "gamma"},
		"v2": {"alpha", "gamma", "beta"},
		"v3": {"beta", "alpha", "gamma"},
	}
	res, err := sys.CalculateBordaCount(context.Background(), rankings)
	if err != nil {
		t.Fatalf("CalculateBordaCount: unexpected error %v", err)
	}
	if res == nil {
		t.Fatalf("CalculateBordaCount: nil result")
	}
	if res.WinningChoice != "alpha" {
		t.Fatalf("WinningChoice = %q, want %q (tally=%v)", res.WinningChoice, "alpha", res.Tally)
	}
	if got := res.Tally["alpha"]; got != 5 {
		t.Fatalf("Tally[alpha] = %v, want 5 (tally=%v)", got, res.Tally)
	}
	if got := res.Tally["beta"]; got != 3 {
		t.Fatalf("Tally[beta] = %v, want 3 (tally=%v)", got, res.Tally)
	}
	if got := res.Tally["gamma"]; got != 1 {
		t.Fatalf("Tally[gamma] = %v, want 1 (tally=%v)", got, res.Tally)
	}
	if res.TotalVotes != 3 {
		t.Fatalf("TotalVotes = %d, want 3", res.TotalVotes)
	}
	// Confidence = winner_points / max_possible (3 voters * 2 each = 6).
	wantConf := 5.0 / 6.0
	if res.Confidence < wantConf-1e-9 || res.Confidence > wantConf+1e-9 {
		t.Fatalf("Confidence = %v, want %v", res.Confidence, wantConf)
	}
}

// TestCalculateBordaCount_EmptyRankings asserts that an empty input
// yields an empty (but non-nil) result rather than crashing.
func TestCalculateBordaCount_EmptyRankings(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	res, err := sys.CalculateBordaCount(context.Background(), map[string][]string{})
	if err != nil {
		t.Fatalf("CalculateBordaCount empty: unexpected error %v", err)
	}
	if res == nil {
		t.Fatalf("CalculateBordaCount empty: nil result")
	}
	if res.WinningChoice != "" {
		t.Fatalf("WinningChoice = %q, want empty", res.WinningChoice)
	}
	if len(res.Tally) != 0 {
		t.Fatalf("Tally = %v, want empty map", res.Tally)
	}
}

// TestCalculateCondorcet_HasWinner constructs a clean Condorcet
// winner: alpha beats beta and beats gamma in pairwise comparison.
//
//	v1: alpha, beta,  gamma
//	v2: alpha, gamma, beta
//	v3: beta,  alpha, gamma
//
// Pairwise:
//
//	alpha vs beta:  v1 yes, v2 yes, v3 no  -> 2-1 alpha wins
//	alpha vs gamma: v1 yes, v2 yes, v3 yes -> 3-0 alpha wins
//	beta  vs gamma: v1 yes, v2 no,  v3 yes -> 2-1 beta wins
//
// alpha wins both of its contests (2 wins) -> Condorcet winner.
func TestCalculateCondorcet_HasWinner(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	rankings := map[string][]string{
		"v1": {"alpha", "beta", "gamma"},
		"v2": {"alpha", "gamma", "beta"},
		"v3": {"beta", "alpha", "gamma"},
	}
	res, err := sys.CalculateCondorcet(context.Background(), rankings)
	if err != nil {
		t.Fatalf("CalculateCondorcet: unexpected error %v", err)
	}
	if res == nil {
		t.Fatalf("CalculateCondorcet: nil result")
	}
	if res.WinningChoice != "alpha" {
		t.Fatalf("WinningChoice = %q, want %q (tally=%v)", res.WinningChoice, "alpha", res.Tally)
	}
	// alpha beats every other choice (N-1 = 2 wins).
	if got := res.Tally["alpha"]; got != 2 {
		t.Fatalf("Tally[alpha] wins = %v, want 2 (tally=%v)", got, res.Tally)
	}
	// beta beats gamma only -> 1 win.
	if got := res.Tally["beta"]; got != 1 {
		t.Fatalf("Tally[beta] wins = %v, want 1 (tally=%v)", got, res.Tally)
	}
	// gamma loses to both -> 0 wins.
	if got := res.Tally["gamma"]; got != 0 {
		t.Fatalf("Tally[gamma] wins = %v, want 0 (tally=%v)", got, res.Tally)
	}
	if res.Confidence != 1.0 {
		t.Fatalf("Confidence = %v, want 1.0 (winner beats every other choice)", res.Confidence)
	}
}

// TestCalculateCondorcet_CyclicNoWinner constructs the classic
// Condorcet paradox / rock-paper-scissors cycle.
//
//	v1: alpha, beta,  gamma   (a>b, b>g, a>g)
//	v2: beta,  gamma, alpha   (b>g, g>a, b>a)
//	v3: gamma, alpha, beta    (g>a, a>b, g>b)
//
// Pairwise:
//
//	alpha vs beta:  v1 a, v2 b, v3 a   -> 2-1 alpha wins
//	beta  vs gamma: v1 b, v2 b, v3 g   -> 2-1 beta  wins
//	gamma vs alpha: v1 a, v2 g, v3 g   -> 2-1 gamma wins
//
// Each choice has exactly 1 pairwise win -> no Condorcet winner.
func TestCalculateCondorcet_CyclicNoWinner(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	rankings := map[string][]string{
		"v1": {"alpha", "beta", "gamma"},
		"v2": {"beta", "gamma", "alpha"},
		"v3": {"gamma", "alpha", "beta"},
	}
	res, err := sys.CalculateCondorcet(context.Background(), rankings)
	if !errors.Is(err, ErrNoCondorcetWinner) {
		t.Fatalf("CalculateCondorcet cycle: err = %v, want ErrNoCondorcetWinner", err)
	}
	if res == nil {
		t.Fatalf("CalculateCondorcet cycle: nil result (should be non-nil with populated tally)")
	}
	if res.WinningChoice != "" {
		t.Fatalf("WinningChoice = %q, want empty on cycle", res.WinningChoice)
	}
	// Each of the three has exactly one pairwise win in the cycle.
	for _, k := range []string{"alpha", "beta", "gamma"} {
		if got := res.Tally[k]; got != 1 {
			t.Fatalf("Tally[%s] wins = %v, want 1 (cyclic tally=%v)", k, got, res.Tally)
		}
	}
}

// TestCalculatePlurality_SimpleCount asserts the most-popular choice
// wins by raw vote count.
func TestCalculatePlurality_SimpleCount(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	for i := 0; i < 3; i++ {
		_ = sys.AddVote(&Vote{AgentID: "a", Choice: "alpha", Confidence: 0.5})
	}
	for i := 0; i < 2; i++ {
		_ = sys.AddVote(&Vote{AgentID: "b", Choice: "beta", Confidence: 0.9})
	}
	_ = sys.AddVote(&Vote{AgentID: "c", Choice: "gamma", Confidence: 0.7})

	res, err := sys.CalculatePlurality(context.Background())
	if err != nil {
		t.Fatalf("CalculatePlurality: unexpected error %v", err)
	}
	if res == nil {
		t.Fatalf("CalculatePlurality: nil result")
	}
	if res.WinningChoice != "alpha" {
		t.Fatalf("WinningChoice = %q, want %q (tally=%v)", res.WinningChoice, "alpha", res.Tally)
	}
	if got := res.Tally["alpha"]; got != 3 {
		t.Fatalf("Tally[alpha] = %v, want 3", got)
	}
	if got := res.Tally["beta"]; got != 2 {
		t.Fatalf("Tally[beta] = %v, want 2", got)
	}
	if got := res.Tally["gamma"]; got != 1 {
		t.Fatalf("Tally[gamma] = %v, want 1", got)
	}
	if res.TotalVotes != 6 {
		t.Fatalf("TotalVotes = %d, want 6", res.TotalVotes)
	}
}

// TestCalculatePlurality_TieBrokenByConfidence asserts the
// highest-confidence tie-breaker actually selects the tied leader
// with higher average confidence.
func TestCalculatePlurality_TieBrokenByConfidence(t *testing.T) {
	cfg := DefaultVotingConfig()
	cfg.EnableTieBreaking = true
	cfg.TieBreakMethod = TieBreakByHighestConfidence
	sys := NewWeightedVotingSystem(cfg)
	// 2 votes for "alpha" with low confidence, 2 for "beta" with high.
	_ = sys.AddVote(&Vote{AgentID: "a1", Choice: "alpha", Confidence: 0.3})
	_ = sys.AddVote(&Vote{AgentID: "a2", Choice: "alpha", Confidence: 0.3})
	_ = sys.AddVote(&Vote{AgentID: "b1", Choice: "beta", Confidence: 0.9})
	_ = sys.AddVote(&Vote{AgentID: "b2", Choice: "beta", Confidence: 0.9})

	res, err := sys.CalculatePlurality(context.Background())
	if err != nil {
		t.Fatalf("CalculatePlurality tie: unexpected error %v", err)
	}
	if res.WinningChoice != "beta" {
		t.Fatalf("WinningChoice = %q, want %q (tally=%v conf=%v)",
			res.WinningChoice, "beta", res.Tally, res.Confidence)
	}
}

// TestCalculateUnanimous_AllAgree asserts unanimity is recognised.
func TestCalculateUnanimous_AllAgree(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	for i := 0; i < 3; i++ {
		_ = sys.AddVote(&Vote{AgentID: "a", Choice: "alpha", Confidence: 0.8})
	}
	res, err := sys.CalculateUnanimous(context.Background())
	if err != nil {
		t.Fatalf("CalculateUnanimous unanimous: unexpected error %v", err)
	}
	if res == nil {
		t.Fatalf("CalculateUnanimous: nil result")
	}
	if res.WinningChoice != "alpha" {
		t.Fatalf("WinningChoice = %q, want %q", res.WinningChoice, "alpha")
	}
	if res.Confidence != 1.0 {
		t.Fatalf("Confidence = %v, want 1.0", res.Confidence)
	}
	if got := res.Tally["alpha"]; got != 3 {
		t.Fatalf("Tally[alpha] = %v, want 3", got)
	}
	if res.TotalVotes != 3 {
		t.Fatalf("TotalVotes = %d, want 3", res.TotalVotes)
	}
}

// TestCalculateUnanimous_Disagreement asserts non-unanimous votes
// surface ErrNoUnanimity with a populated tally for inspection.
func TestCalculateUnanimous_Disagreement(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	_ = sys.AddVote(&Vote{AgentID: "a", Choice: "alpha", Confidence: 0.8})
	_ = sys.AddVote(&Vote{AgentID: "b", Choice: "beta", Confidence: 0.7})
	_ = sys.AddVote(&Vote{AgentID: "c", Choice: "gamma", Confidence: 0.6})

	res, err := sys.CalculateUnanimous(context.Background())
	if !errors.Is(err, ErrNoUnanimity) {
		t.Fatalf("CalculateUnanimous disagree: err = %v, want ErrNoUnanimity", err)
	}
	if res == nil {
		t.Fatalf("CalculateUnanimous disagree: nil result (should be non-nil with tally)")
	}
	if res.WinningChoice != "" {
		t.Fatalf("WinningChoice = %q, want empty", res.WinningChoice)
	}
	if len(res.Tally) != 3 {
		t.Fatalf("Tally = %v, want 3 entries", res.Tally)
	}
}

// TestTallyRemainsHonestStub keeps coverage on the one method still
// pending real implementation (per RECONSTRUCTION_ROADMAP.md). The
// original TestTallyIsHonestStub already asserts this for Tally;
// this test additionally guarantees no future regression silently
// promotes Tally to "real" without removing the sentinel.
func TestTallyRemainsHonestStub(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	_, err := sys.Tally(context.Background(), []Vote{{AgentID: "a", Choice: "x", Confidence: 1.0}})
	if err == nil {
		t.Fatalf("Tally: expected stub error, got nil")
	}
	if !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("Tally: expected NotYetImplemented sentinel, got %q", err.Error())
	}
}
