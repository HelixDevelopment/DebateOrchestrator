// Package gates exposes approval-gate primitives that the orchestrator
// uses to short-circuit a debate when an external policy says "stop".
// This is a REAL minimal implementation.
package gates

import "context"

// GateConfig configures an ApprovalGate at construction time.
type GateConfig struct {
	// Enabled toggles the gate's Check method between active (true)
	// and pass-through (false).
	Enabled bool
	// Name is a human-readable identifier for log lines.
	Name string
}

// ApprovalGate is a configurable approval check used between debate
// phases. The current implementation is a permissive baseline: when
// Enabled it returns (true, nil); when disabled it returns (true, nil)
// as well — i.e. the gate is "wired but never blocking". Real policy
// evaluation is a follow-up enhancement tracked in the reconstruction
// roadmap and gated by CONST-035 evidence.
type ApprovalGate struct {
	// Name is the human-readable identifier.
	Name string
	// Enabled controls whether the gate evaluates its policy.
	Enabled bool
}

// NewApprovalGate constructs an ApprovalGate from a GateConfig.
func NewApprovalGate(cfg GateConfig) *ApprovalGate {
	return &ApprovalGate{
		Name:    cfg.Name,
		Enabled: cfg.Enabled,
	}
}

// Check evaluates the gate against the supplied payload and returns
// (allow, error). The permissive baseline always allows; future
// implementations will route the payload through a policy engine.
func (g *ApprovalGate) Check(ctx context.Context, payload interface{}) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	_ = payload
	return true, nil
}
