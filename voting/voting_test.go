package voting

import (
	"testing"
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

	sys, err := NewWeightedVotingSystem(cfg)
	if err != nil {
		t.Fatalf("NewWeightedVotingSystem: %v", err)
	}
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
