// Package reflexion hosts the Reflexion-loop primitives. Constructors
// return real but empty values; execution methods are honest
// NotYetImplemented stubs. Real reflexion logic is tracked in
// RECONSTRUCTION_ROADMAP.md.
package reflexion

import (
	"context"
	"errors"
	"time"
)

// ReflexionConfig configures the reflexion loop primitives.
type ReflexionConfig struct {
	// MaxIterations caps the number of reflection rounds.
	MaxIterations int
	// MaxMemoryEntries caps the episodic memory buffer.
	MaxMemoryEntries int
	// Timeout caps the wall-clock time per loop.
	Timeout time.Duration
}

// DefaultReflexionConfig returns a conservative default configuration.
func DefaultReflexionConfig() ReflexionConfig {
	return ReflexionConfig{
		MaxIterations:    3,
		MaxMemoryEntries: 64,
		Timeout:          30 * time.Second,
	}
}

// TestExecutor is the abstract dependency the reflexion loop uses to
// re-run candidate solutions against synthetic tests between
// reflections. Defined as an empty interface for now so callers can
// pass nil during early wiring without breaking the constructor; the
// shape will tighten when the real loop lands.
type TestExecutor interface{}

// EpisodicMemoryBuffer accumulates episodic memories across iterations.
type EpisodicMemoryBuffer struct {
	// Capacity is the hard cap on stored entries (FIFO eviction).
	Capacity int
}

// NewEpisodicMemoryBuffer constructs a buffer sized to capacity.
// A capacity of zero or below disables retention.
func NewEpisodicMemoryBuffer(capacity int) *EpisodicMemoryBuffer {
	if capacity < 0 {
		capacity = 0
	}
	return &EpisodicMemoryBuffer{Capacity: capacity}
}

// Append records an episodic memory entry.
func (b *EpisodicMemoryBuffer) Append(ctx context.Context, entry interface{}) error {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = entry
	return errors.New("debate/reflexion: EpisodicMemoryBuffer.Append NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// ReflectionGenerator produces reflections from episodic memories
// using a pluggable LLM client. The client is held as interface{} so
// HelixAgent (and other consumers) can wire whichever client type
// they have available — the real loop will tighten this once it lands.
type ReflectionGenerator struct {
	llmClient interface{}
}

// NewReflectionGenerator constructs a ReflectionGenerator bound to the
// supplied LLM client. Pass nil during early wiring.
func NewReflectionGenerator(llmClient interface{}) *ReflectionGenerator {
	return &ReflectionGenerator{llmClient: llmClient}
}

// Generate produces a reflection over the supplied input.
func (g *ReflectionGenerator) Generate(ctx context.Context, input interface{}) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = input
	return nil, errors.New("debate/reflexion: ReflectionGenerator.Generate NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// ReflexionLoop drives iterations of reflection over a memory buffer,
// re-executing candidate solutions through the supplied test executor
// between reflection rounds.
type ReflexionLoop struct {
	cfg      ReflexionConfig
	gen      *ReflectionGenerator
	executor TestExecutor
	memory   *EpisodicMemoryBuffer
}

// NewReflexionLoop constructs a ReflexionLoop. The generator,
// executor, and memory may be nil during early wiring; the real loop
// will reject nils once it lands.
func NewReflexionLoop(
	cfg ReflexionConfig,
	gen *ReflectionGenerator,
	executor TestExecutor,
	memory *EpisodicMemoryBuffer,
) *ReflexionLoop {
	return &ReflexionLoop{cfg: cfg, gen: gen, executor: executor, memory: memory}
}

// Run executes the reflexion loop end-to-end.
func (l *ReflexionLoop) Run(ctx context.Context, input interface{}) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = input
	return nil, errors.New("debate/reflexion: ReflexionLoop.Run NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

// AccumulatedWisdom is the long-running cross-debate knowledge store.
type AccumulatedWisdom struct{}

// NewAccumulatedWisdom constructs an empty AccumulatedWisdom.
func NewAccumulatedWisdom() *AccumulatedWisdom {
	return &AccumulatedWisdom{}
}

// Persist writes the wisdom store to durable storage.
func (a *AccumulatedWisdom) Persist(ctx context.Context) error {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("debate/reflexion: AccumulatedWisdom.Persist NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}
