package voting

import (
	"context"
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

func TestAdvancedTallyStubsReturnNotYetImplemented(t *testing.T) {
	sys := NewWeightedVotingSystem(DefaultVotingConfig())
	ctx := context.Background()
	stubs := []struct {
		name string
		fn   func() error
	}{
		{"CalculateBordaCount", func() error {
			_, err := sys.CalculateBordaCount(ctx, map[string][]string{})
			return err
		}},
		{"CalculateCondorcet", func() error {
			_, err := sys.CalculateCondorcet(ctx, map[string][]string{})
			return err
		}},
		{"CalculatePlurality", func() error {
			_, err := sys.CalculatePlurality(ctx)
			return err
		}},
		{"CalculateUnanimous", func() error {
			_, err := sys.CalculateUnanimous(ctx)
			return err
		}},
	}
	for _, st := range stubs {
		err := st.fn()
		if err == nil {
			t.Fatalf("%s: expected stub error, got nil", st.name)
		}
		if !strings.Contains(err.Error(), "NotYetImplemented") {
			t.Fatalf("%s: expected NotYetImplemented sentinel, got %q", st.name, err.Error())
		}
	}
}
