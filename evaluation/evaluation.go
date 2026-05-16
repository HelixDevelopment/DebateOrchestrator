// Package evaluation hosts the BenchmarkBridge. The current
// implementation is an honest NotYetImplemented stub; the constructor
// returns a real value but RunBenchmark returns an explicit error.
// Real benchmark execution is tracked in RECONSTRUCTION_ROADMAP.md.
package evaluation

import (
	"context"
	"errors"
)

// BenchmarkBridge connects the orchestrator to evaluation benchmarks.
type BenchmarkBridge struct{}

// NewBenchmarkBridge constructs a BenchmarkBridge.
func NewBenchmarkBridge() *BenchmarkBridge {
	return &BenchmarkBridge{}
}

// RunBenchmark executes the named benchmark.
func (b *BenchmarkBridge) RunBenchmark(ctx context.Context, name string) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = name
	return nil, errors.New("debate/evaluation: RunBenchmark NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}
