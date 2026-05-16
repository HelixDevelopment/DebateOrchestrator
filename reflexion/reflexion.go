// Package reflexion hosts the Reflexion-loop primitives — episodic
// memory, reflection generation, accumulated cross-session wisdom,
// and the iterative reflexion driver itself.
//
// Constructors return real-but-empty values; in-memory storage is
// real (thread-safe maps with deterministic bounds); inspection and
// reset helpers are real. Execution methods that would normally
// invoke an LLM (ReflectionGenerator.Generate, ReflexionLoop.Execute)
// run a deterministic fallback path so callers can stage code today
// — fallbacks carry the
// `// TODO(reconstruction-phase-2): real implementation pending`
// marker.
//
// Legacy compatibility entry-points (.Append, .Run, .Persist) are
// now REAL thin wrappers around the modern API:
//   - EpisodicMemoryBuffer.Append wraps Store with type assertion.
//   - ReflexionLoop.Run wraps Execute with type assertion.
//   - AccumulatedWisdom.Persist writes the wisdom store to disk as
//     newline-delimited JSON via WithPersistencePath, or returns
//     ErrNoPersistencePath when no path was configured.
package reflexion

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ReflexionConfig configures the reflexion loop primitives.
type ReflexionConfig struct {
	// MaxIterations caps the number of reflection rounds (legacy name).
	MaxIterations int
	// MaxAttempts caps the number of code-generation attempts (new
	// debate-runner name; takes precedence over MaxIterations when
	// non-zero).
	MaxAttempts int
	// MaxMemoryEntries caps the episodic memory buffer.
	MaxMemoryEntries int
	// ConfidenceThreshold is the minimum reflection confidence at
	// which the loop exits early.
	ConfidenceThreshold float64
	// Timeout caps the wall-clock time per loop.
	Timeout time.Duration
}

// DefaultReflexionConfig returns a conservative default configuration.
func DefaultReflexionConfig() ReflexionConfig {
	return ReflexionConfig{
		MaxIterations:       3,
		MaxAttempts:         3,
		MaxMemoryEntries:    64,
		ConfidenceThreshold: 0.95,
		Timeout:             30 * time.Second,
	}
}

// =============================================================================
// Data types
// =============================================================================

// TestResult captures the outcome of a single in-loop test execution.
type TestResult struct {
	// Name identifies the test.
	Name string
	// Passed records whether the test passed.
	Passed bool
	// Output is the captured test output.
	Output string
	// Error is the captured failure message, if any.
	Error string
	// Duration is the wall-clock duration of the test.
	Duration time.Duration
}

// Reflection captures the LLM's (or fallback's) analysis of an
// attempt's failure.
type Reflection struct {
	// RootCause is the diagnosed root cause.
	RootCause string
	// WhatWentWrong is the human-readable failure summary.
	WhatWentWrong string
	// WhatToChangeNext is the suggested course of action.
	WhatToChangeNext string
	// ConfidenceInFix is the reflection's own self-reported confidence.
	ConfidenceInFix float64
}

// ReflectionRequest is the input to ReflectionGenerator.Generate.
type ReflectionRequest struct {
	// Code is the candidate code under reflection.
	Code string
	// TestResults is the structured test outcome map.
	TestResults map[string]interface{}
	// ErrorMessages is the list of captured error messages.
	ErrorMessages []string
	// TaskDescription is the human-readable task description.
	TaskDescription string
	// AttemptNumber is the 1-based attempt counter.
	AttemptNumber int
}

// Episode is a single record in the episodic memory buffer.
type Episode struct {
	// AgentID identifies the agent the episode was produced by.
	AgentID string
	// SessionID identifies the debate session.
	SessionID string
	// TaskDescription is the human-readable task description.
	TaskDescription string
	// AttemptNumber is the 1-based attempt counter.
	AttemptNumber int
	// Code is the candidate code for this attempt.
	Code string
	// TestResults is the structured test-outcome map.
	TestResults map[string]interface{}
	// FailureAnalysis is the captured failure analysis string.
	FailureAnalysis string
	// Reflection is the bound Reflection (may be nil).
	Reflection *Reflection
	// Confidence is the attempt's confidence.
	Confidence float64
	// Timestamp is the episode-creation timestamp.
	Timestamp time.Time
}

// Wisdom is a cross-episode pattern extracted by AccumulatedWisdom.
type Wisdom struct {
	// ID is the wisdom record identifier.
	ID string `json:"id,omitempty"`
	// Pattern is the wisdom pattern (free-form text).
	Pattern string `json:"pattern,omitempty"`
	// Description is the free-form human-readable description of the
	// wisdom record.
	Description string `json:"description,omitempty"`
	// Frequency is the number of episodes the pattern was derived from.
	Frequency int `json:"frequency,omitempty"`
	// Confidence is the wisdom's self-reported confidence (capped at 1.0).
	Confidence float64 `json:"confidence,omitempty"`
	// Source identifies the wisdom's provenance (e.g. "extracted",
	// "manual", "imported").
	Source string `json:"source,omitempty"`
	// Domain is the wisdom domain (e.g. "code", "review").
	Domain string `json:"domain,omitempty"`
	// Tags is the wisdom tag list.
	Tags []string `json:"tags,omitempty"`
	// UseCount is the number of times the wisdom has been applied.
	UseCount int `json:"use_count,omitempty"`
	// SuccessRate is the ratio of successful applications.
	SuccessRate float64 `json:"success_rate,omitempty"`
	// Impact is the wisdom's impact score.
	Impact float64 `json:"impact,omitempty"`
	// Timestamp is the wisdom creation/extraction timestamp.
	Timestamp time.Time `json:"timestamp,omitempty"`

	// successHits / failureHits keep the running counts used by
	// RecordUsage. Exposed via SuccessRate.
	successHits int
	failureHits int
}

// ReflexionTask is the input to ReflexionLoop.Execute.
type ReflexionTask struct {
	// Description is the human-readable task description.
	Description string
	// InitialCode is the seed code the loop starts from.
	InitialCode string
	// Language is the source-code language identifier.
	Language string
	// AgentID identifies the agent that owns the task.
	AgentID string
	// SessionID identifies the debate session.
	SessionID string
	// CodeGenerator is the callback the loop uses to produce a new
	// candidate after each reflection round.
	CodeGenerator func(ctx context.Context, taskDescription string,
		priorReflections []*Reflection) (string, error)
}

// ReflexionResult is the outcome of a ReflexionLoop.Execute call.
type ReflexionResult struct {
	// AllPassed records whether the final attempt's tests all passed.
	AllPassed bool
	// Attempts is the number of code-generation attempts made.
	Attempts int
	// Reflections is the list of reflections generated.
	Reflections []*Reflection
	// Episodes is the list of episodes stored in memory.
	Episodes []*Episode
	// FinalConfidence is the final reflection's confidence.
	FinalConfidence float64
}

// =============================================================================
// Interfaces
// =============================================================================

// LLMClient is the abstraction the reflection generator uses to talk
// to a backing LLM.
type LLMClient interface {
	// Complete returns the model's completion for the supplied prompt.
	Complete(ctx context.Context, prompt string) (string, error)
}

// TestExecutor is the abstract dependency the reflexion loop uses to
// re-run candidate solutions against synthetic tests between
// reflections.
type TestExecutor interface {
	// Execute runs the test bank for the supplied code in the supplied
	// language and returns the per-test results.
	Execute(ctx context.Context, code string, language string) ([]*TestResult, error)
}

// =============================================================================
// EpisodicMemoryBuffer
// =============================================================================

// EpisodicMemoryBuffer accumulates Episodes with bounded capacity.
type EpisodicMemoryBuffer struct {
	// Capacity is the hard cap on stored entries (FIFO eviction).
	Capacity int

	mu       sync.RWMutex
	episodes []*Episode
}

// NewEpisodicMemoryBuffer constructs a buffer sized to capacity.
// A capacity of zero or below disables retention.
func NewEpisodicMemoryBuffer(capacity int) *EpisodicMemoryBuffer {
	if capacity < 0 {
		capacity = 0
	}
	return &EpisodicMemoryBuffer{Capacity: capacity}
}

// Store records an Episode in the buffer. When the buffer is full
// the oldest episode is evicted (FIFO).
func (b *EpisodicMemoryBuffer) Store(ep *Episode) error {
	if ep == nil {
		return errors.New("debate/reflexion: Store nil episode")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Capacity == 0 {
		return nil
	}
	b.episodes = append(b.episodes, ep)
	if len(b.episodes) > b.Capacity {
		drop := len(b.episodes) - b.Capacity
		b.episodes = b.episodes[drop:]
	}
	return nil
}

// Append records an episodic memory entry. This is the legacy
// API surface: it accepts an interface{} for backwards compatibility
// with callers that pass an untyped payload. The argument MUST be a
// non-nil *Episode; any other type returns ErrInvalidEpisode.
//
// Append is a real thin wrapper over Store — same FIFO eviction,
// same thread-safety, same Capacity contract.
func (b *EpisodicMemoryBuffer) Append(ctx context.Context, entry interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if entry == nil {
		return ErrInvalidEpisode
	}
	ep, ok := entry.(*Episode)
	if !ok || ep == nil {
		return ErrInvalidEpisode
	}
	return b.Store(ep)
}

// ErrInvalidEpisode is returned when EpisodicMemoryBuffer.Append is
// invoked with anything other than a non-nil *Episode.
var ErrInvalidEpisode = errors.New("debate/reflexion: EpisodicMemoryBuffer.Append requires a non-nil *Episode")

// Size returns the number of episodes currently held.
func (b *EpisodicMemoryBuffer) Size() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.episodes)
}

// GetAll returns a snapshot of all episodes in insertion order.
func (b *EpisodicMemoryBuffer) GetAll() []*Episode {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*Episode, len(b.episodes))
	copy(out, b.episodes)
	return out
}

// GetByAgent returns all episodes produced by the given agent ID.
func (b *EpisodicMemoryBuffer) GetByAgent(agentID string) []*Episode {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*Episode, 0)
	for _, ep := range b.episodes {
		if ep.AgentID == agentID {
			out = append(out, ep)
		}
	}
	return out
}

// GetRecent returns the most recent `n` episodes in reverse-insertion
// order (most-recent first).
func (b *EpisodicMemoryBuffer) GetRecent(n int) []*Episode {
	if n <= 0 {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	start := len(b.episodes) - n
	if start < 0 {
		start = 0
	}
	tail := b.episodes[start:]
	out := make([]*Episode, len(tail))
	for i, ep := range tail {
		out[len(tail)-1-i] = ep
	}
	return out
}

// GetRelevant returns up to `limit` episodes ranked by token-overlap
// (Jaccard) similarity between the query and each episode's text
// content (TaskDescription + FailureAnalysis + Reflection root cause +
// what-went-wrong + what-to-change-next).
//
// Real relevance algorithm:
//   - Tokenize the query and each candidate's combined content into
//     lowercase words (split on whitespace + punctuation; drop tokens
//     shorter than 3 characters).
//   - For each candidate compute Jaccard similarity:
//     |query_tokens ∩ candidate_tokens| / |query_tokens ∪ candidate_tokens|.
//   - Rank descending by similarity; tie-break by recency (later
//     Timestamp first).
//   - Return the top `limit` candidates with similarity > 0.
//
// If `query` is empty an empty slice is returned (honest — nothing
// matches no query). Honors ctx.Done().
func (b *EpisodicMemoryBuffer) GetRelevant(ctx context.Context, query string, limit int) []*Episode {
	if err := ctx.Err(); err != nil {
		return nil
	}
	if limit <= 0 {
		return nil
	}
	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return nil
	}
	b.mu.RLock()
	snapshot := make([]*Episode, len(b.episodes))
	copy(snapshot, b.episodes)
	b.mu.RUnlock()

	type scored struct {
		ep    *Episode
		score float64
	}
	scoredAll := make([]scored, 0, len(snapshot))
	for _, ep := range snapshot {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if ep == nil {
			continue
		}
		cTokens := tokenize(episodeContent(ep))
		if len(cTokens) == 0 {
			continue
		}
		s := jaccard(qTokens, cTokens)
		if s <= 0 {
			continue
		}
		scoredAll = append(scoredAll, scored{ep: ep, score: s})
	}
	sort.SliceStable(scoredAll, func(i, j int) bool {
		if scoredAll[i].score != scoredAll[j].score {
			return scoredAll[i].score > scoredAll[j].score
		}
		return scoredAll[i].ep.Timestamp.After(scoredAll[j].ep.Timestamp)
	})
	if len(scoredAll) > limit {
		scoredAll = scoredAll[:limit]
	}
	out := make([]*Episode, len(scoredAll))
	for i, s := range scoredAll {
		out[i] = s.ep
	}
	return out
}

// episodeContent returns the concatenated text content of an episode
// used by the token-overlap relevance scorer.
func episodeContent(ep *Episode) string {
	if ep == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(ep.TaskDescription)
	sb.WriteString(" ")
	sb.WriteString(ep.FailureAnalysis)
	if ep.Reflection != nil {
		sb.WriteString(" ")
		sb.WriteString(ep.Reflection.RootCause)
		sb.WriteString(" ")
		sb.WriteString(ep.Reflection.WhatWentWrong)
		sb.WriteString(" ")
		sb.WriteString(ep.Reflection.WhatToChangeNext)
	}
	return sb.String()
}

// tokenize splits the supplied text into a set of lowercase tokens,
// dropping any token shorter than 3 characters. Splitting happens on
// any rune that is neither a letter nor a digit so punctuation is
// dropped naturally.
func tokenize(text string) map[string]struct{} {
	tokens := make(map[string]struct{})
	if text == "" {
		return tokens
	}
	lower := strings.ToLower(text)
	splitter := func(r rune) bool {
		return !isWord(r)
	}
	for _, raw := range strings.FieldsFunc(lower, splitter) {
		if len(raw) < 3 {
			continue
		}
		tokens[raw] = struct{}{}
	}
	return tokens
}

// isWord reports whether the rune is a letter or a digit (i.e.
// belongs to a token). Pure-ASCII fast path keeps the hot loop cheap.
func isWord(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z':
		return true
	case r >= 'A' && r <= 'Z':
		return true
	case r >= '0' && r <= '9':
		return true
	}
	return false
}

// jaccard returns the Jaccard similarity between two token sets.
func jaccard(a, b map[string]struct{}) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	// Iterate the smaller set for the intersection count.
	small, large := a, b
	if len(b) < len(a) {
		small, large = b, a
	}
	inter := 0
	for tok := range small {
		if _, ok := large[tok]; ok {
			inter++
		}
	}
	if inter == 0 {
		return 0
	}
	union := len(a) + len(b) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

// =============================================================================
// ReflectionGenerator
// =============================================================================

// ReflectionGenerator produces reflections from in-loop attempts.
type ReflectionGenerator struct {
	llmClient interface{}
}

// NewReflectionGenerator constructs a ReflectionGenerator bound to the
// supplied LLM client (typed as interface{} so callers can pass
// either a typed LLMClient implementation or nil during early wiring).
func NewReflectionGenerator(llmClient interface{}) *ReflectionGenerator {
	return &ReflectionGenerator{llmClient: llmClient}
}

// Generate produces a Reflection over the supplied request.
//
// When the bound LLM client implements LLMClient, the generator
// calls it and parses the response with the canonical newline-
// delimited "KEY: value" format. If the LLM returns an error OR
// the client is not a typed LLMClient, the generator falls back
// to a deterministic real-but-minimal analysis based on the
// supplied error messages. Real LLM-based generation is tracked in
// RECONSTRUCTION_ROADMAP.md.
func (g *ReflectionGenerator) Generate(ctx context.Context, input interface{}) (*Reflection, error) {
	// TODO(reconstruction-phase-2): real LLM-based generation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, ok := input.(*ReflectionRequest)
	if !ok || req == nil {
		// Untyped input — surface the legacy stub error so callers
		// migrating off the old API see an actionable failure.
		return nil, errors.New("debate/reflexion: ReflectionGenerator.Generate requires *ReflectionRequest — see RECONSTRUCTION_ROADMAP.md")
	}
	if client, ok := g.llmClient.(LLMClient); ok && client != nil {
		prompt := buildReflectionPrompt(req)
		raw, err := client.Complete(ctx, prompt)
		if err == nil {
			parsed := parseReflectionResponse(raw)
			if parsed != nil {
				return parsed, nil
			}
		}
	}
	return fallbackReflection(req), nil
}

func buildReflectionPrompt(req *ReflectionRequest) string {
	var sb strings.Builder
	sb.WriteString("Reflect on the following attempt and respond with\n")
	sb.WriteString("ROOT_CAUSE / WHAT_WENT_WRONG / WHAT_TO_CHANGE / CONFIDENCE lines.\n\n")
	sb.WriteString("TASK: ")
	sb.WriteString(req.TaskDescription)
	sb.WriteString("\nATTEMPT: ")
	sb.WriteString(fmt.Sprintf("%d", req.AttemptNumber))
	if len(req.ErrorMessages) > 0 {
		sb.WriteString("\nERRORS:\n")
		for _, e := range req.ErrorMessages {
			sb.WriteString(" - ")
			sb.WriteString(e)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

func parseReflectionResponse(raw string) *Reflection {
	if raw == "" {
		return nil
	}
	r := &Reflection{}
	for _, line := range strings.Split(raw, "\n") {
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(strings.ToUpper(key))
		val = strings.TrimSpace(val)
		switch key {
		case "ROOT_CAUSE":
			r.RootCause = val
		case "WHAT_WENT_WRONG":
			r.WhatWentWrong = val
		case "WHAT_TO_CHANGE", "WHAT_TO_CHANGE_NEXT":
			r.WhatToChangeNext = val
		case "CONFIDENCE":
			var f float64
			_, _ = fmt.Sscanf(val, "%f", &f)
			r.ConfidenceInFix = f
		}
	}
	if r.RootCause == "" && r.WhatWentWrong == "" && r.WhatToChangeNext == "" {
		return nil
	}
	if r.ConfidenceInFix == 0 {
		r.ConfidenceInFix = 0.5
	}
	return r
}

func fallbackReflection(req *ReflectionRequest) *Reflection {
	rc := "Deterministic fallback: see captured error messages"
	if len(req.ErrorMessages) > 0 {
		rc = "Deterministic fallback: " + req.ErrorMessages[0]
	}
	return &Reflection{
		RootCause:        rc,
		WhatWentWrong:    "Attempt produced failing tests (LLM unavailable; deterministic analysis)",
		WhatToChangeNext: "Re-run with corrected logic; reflexion fallback cannot diagnose deeper",
		ConfidenceInFix:  0.3,
	}
}

// =============================================================================
// ReflexionLoop
// =============================================================================

// ReflexionLoop drives iterations of reflection over a memory buffer,
// re-executing candidate solutions through the supplied test executor
// between reflection rounds.
type ReflexionLoop struct {
	cfg      ReflexionConfig
	gen      *ReflectionGenerator
	executor TestExecutor
	memory   *EpisodicMemoryBuffer
}

// NewReflexionLoop constructs a ReflexionLoop.
func NewReflexionLoop(
	cfg ReflexionConfig,
	gen *ReflectionGenerator,
	executor TestExecutor,
	memory *EpisodicMemoryBuffer,
) *ReflexionLoop {
	return &ReflexionLoop{cfg: cfg, gen: gen, executor: executor, memory: memory}
}

// Run executes the reflexion loop end-to-end against a free-form
// input. This is the legacy API surface: it accepts an interface{}
// for backwards compatibility. The argument MUST be a non-nil
// *ReflexionTask; any other type returns ErrInvalidReflexionTask.
//
// Run is a real thin wrapper over Execute — same maxAttempts logic,
// same early-exit semantics, same return data. The interface{}
// return is the *ReflexionResult Execute produces.
func (l *ReflexionLoop) Run(ctx context.Context, input interface{}) (interface{}, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, ErrInvalidReflexionTask
	}
	task, ok := input.(*ReflexionTask)
	if !ok || task == nil {
		return nil, ErrInvalidReflexionTask
	}
	return l.Execute(ctx, task)
}

// ErrInvalidReflexionTask is returned when ReflexionLoop.Run is
// invoked with anything other than a non-nil *ReflexionTask.
var ErrInvalidReflexionTask = errors.New("debate/reflexion: ReflexionLoop.Run requires a non-nil *ReflexionTask")

// Execute drives the reflexion loop for the supplied task.
//
// The runner is real-but-minimal: it calls the task's CodeGenerator
// to produce a candidate, dispatches the executor, captures the
// per-attempt episode, asks the generator for a reflection, and
// retries up to MaxAttempts. Early-exit kicks in when all tests
// pass. Real provider-aware retry, parallel exploration, and
// wisdom-aware seeding are tracked in RECONSTRUCTION_ROADMAP.md.
func (l *ReflexionLoop) Execute(ctx context.Context, task *ReflexionTask) (*ReflexionResult, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if task == nil {
		return nil, errors.New("debate/reflexion: ReflexionLoop.Execute nil task")
	}
	if l.executor == nil {
		return nil, errors.New("debate/reflexion: ReflexionLoop.Execute requires TestExecutor")
	}
	maxAttempts := l.cfg.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = l.cfg.MaxIterations
	}
	if maxAttempts <= 0 {
		maxAttempts = 1
	}

	res := &ReflexionResult{}
	currentCode := task.InitialCode
	var priorReflections []*Reflection
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if task.CodeGenerator != nil {
			code, err := task.CodeGenerator(ctx, task.Description, priorReflections)
			if err != nil {
				return res, fmt.Errorf("debate/reflexion: code generator failed at attempt %d: %w", attempt, err)
			}
			currentCode = code
		}
		results, err := l.executor.Execute(ctx, currentCode, task.Language)
		if err != nil {
			return res, fmt.Errorf("debate/reflexion: test executor failed at attempt %d: %w", attempt, err)
		}
		allPassed := true
		errMsgs := make([]string, 0)
		for _, tr := range results {
			if tr == nil {
				continue
			}
			if !tr.Passed {
				allPassed = false
				if tr.Error != "" {
					errMsgs = append(errMsgs, tr.Error)
				}
			}
		}
		res.Attempts = attempt
		ep := &Episode{
			AgentID:         task.AgentID,
			SessionID:       task.SessionID,
			TaskDescription: task.Description,
			AttemptNumber:   attempt,
			Code:            currentCode,
			Timestamp:       time.Now(),
		}
		if allPassed {
			ep.Confidence = 1.0
			res.AllPassed = true
			res.FinalConfidence = 1.0
			if l.memory != nil {
				_ = l.memory.Store(ep)
				res.Episodes = append(res.Episodes, ep)
			}
			return res, nil
		}
		if l.gen != nil {
			req := &ReflectionRequest{
				Code:            currentCode,
				ErrorMessages:   errMsgs,
				TaskDescription: task.Description,
				AttemptNumber:   attempt,
			}
			reflection, err := l.gen.Generate(ctx, req)
			if err == nil && reflection != nil {
				ep.Reflection = reflection
				ep.Confidence = reflection.ConfidenceInFix
				res.Reflections = append(res.Reflections, reflection)
				priorReflections = append(priorReflections, reflection)
				res.FinalConfidence = reflection.ConfidenceInFix
			}
		}
		if l.memory != nil {
			_ = l.memory.Store(ep)
			res.Episodes = append(res.Episodes, ep)
		}
		if l.cfg.ConfidenceThreshold > 0 && res.FinalConfidence >= l.cfg.ConfidenceThreshold {
			return res, nil
		}
	}
	return res, nil
}

// =============================================================================
// AccumulatedWisdom
// =============================================================================

// AccumulatedWisdom is the long-running cross-debate knowledge store.
type AccumulatedWisdom struct {
	mu              sync.RWMutex
	store           []*Wisdom
	byID            map[string]*Wisdom
	seedKey         int
	persistencePath string
}

// WisdomOption configures an AccumulatedWisdom at construction time.
type WisdomOption func(*AccumulatedWisdom)

// WithPersistencePath directs Persist to write the wisdom store to
// the supplied file path as a single JSON document. The file is
// truncated + rewritten on every Persist call (atomic via tempfile +
// rename to avoid partial writes).
func WithPersistencePath(path string) WisdomOption {
	return func(a *AccumulatedWisdom) {
		a.persistencePath = path
	}
}

// NewAccumulatedWisdom constructs an empty AccumulatedWisdom with
// the supplied options.
func NewAccumulatedWisdom(opts ...WisdomOption) *AccumulatedWisdom {
	a := &AccumulatedWisdom{byID: make(map[string]*Wisdom)}
	for _, opt := range opts {
		if opt != nil {
			opt(a)
		}
	}
	return a
}

// ErrNoPersistencePath is returned by Persist when the wisdom store
// was constructed without WithPersistencePath.
var ErrNoPersistencePath = errors.New("debate/reflexion: AccumulatedWisdom has no persistence path — supply WithPersistencePath at construction")

// Persist writes the wisdom store to the configured persistence path
// as a single JSON document. Atomic via tempfile + rename. Returns
// ErrNoPersistencePath when no path was configured; returns ctx.Err()
// when the context is cancelled; surfaces marshalling / I/O errors
// otherwise.
func (a *AccumulatedWisdom) Persist(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.RLock()
	path := a.persistencePath
	if path == "" {
		a.mu.RUnlock()
		return ErrNoPersistencePath
	}
	snapshot := make([]*Wisdom, len(a.store))
	copy(snapshot, a.store)
	a.mu.RUnlock()

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("debate/reflexion: AccumulatedWisdom.Persist marshal: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("debate/reflexion: AccumulatedWisdom.Persist write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("debate/reflexion: AccumulatedWisdom.Persist rename: %w", err)
	}
	return nil
}

// Store appends a Wisdom record to the store. If the record carries
// no ID, a deterministic ID is generated from the pattern.
func (a *AccumulatedWisdom) Store(w *Wisdom) error {
	if w == nil {
		return errors.New("debate/reflexion: AccumulatedWisdom.Store nil wisdom")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if w.ID == "" {
		w.ID = generateWisdomID(w.Pattern, a.seedKey)
		a.seedKey++
	}
	a.byID[w.ID] = w
	a.store = append(a.store, w)
	return nil
}

// Size returns the number of wisdom records held.
func (a *AccumulatedWisdom) Size() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.store)
}

// GetAll returns a snapshot of every wisdom record.
func (a *AccumulatedWisdom) GetAll() []*Wisdom {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Wisdom, len(a.store))
	copy(out, a.store)
	return out
}

// GetRelevant returns up to `limit` wisdom records ranked by
// token-overlap (Jaccard) similarity between the query and each
// wisdom's text content (Pattern + Description + Domain + joined Tags).
//
// Real relevance algorithm:
//   - Tokenize the query and each wisdom's combined content into
//     lowercase words (split on whitespace + punctuation; drop tokens
//     shorter than 3 characters).
//   - For each candidate compute Jaccard similarity:
//     |query_tokens ∩ candidate_tokens| / |query_tokens ∪ candidate_tokens|.
//   - Rank descending by similarity; tie-break by recency (later
//     Timestamp first).
//   - Return the top `limit` candidates with similarity > 0.
//
// If `query` is empty an empty slice is returned. Honors ctx.Done().
func (a *AccumulatedWisdom) GetRelevant(ctx context.Context, query string, limit int) []*Wisdom {
	if err := ctx.Err(); err != nil {
		return nil
	}
	if limit <= 0 {
		return nil
	}
	qTokens := tokenize(query)
	if len(qTokens) == 0 {
		return nil
	}
	a.mu.RLock()
	snapshot := make([]*Wisdom, len(a.store))
	copy(snapshot, a.store)
	a.mu.RUnlock()

	type scored struct {
		w     *Wisdom
		score float64
	}
	scoredAll := make([]scored, 0, len(snapshot))
	for _, w := range snapshot {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if w == nil {
			continue
		}
		cTokens := tokenize(wisdomContent(w))
		if len(cTokens) == 0 {
			continue
		}
		s := jaccard(qTokens, cTokens)
		if s <= 0 {
			continue
		}
		scoredAll = append(scoredAll, scored{w: w, score: s})
	}
	sort.SliceStable(scoredAll, func(i, j int) bool {
		if scoredAll[i].score != scoredAll[j].score {
			return scoredAll[i].score > scoredAll[j].score
		}
		return scoredAll[i].w.Timestamp.After(scoredAll[j].w.Timestamp)
	})
	if len(scoredAll) > limit {
		scoredAll = scoredAll[:limit]
	}
	out := make([]*Wisdom, len(scoredAll))
	for i, s := range scoredAll {
		out[i] = s.w
	}
	return out
}

// wisdomContent returns the concatenated text content of a wisdom
// record used by the token-overlap relevance scorer.
func wisdomContent(w *Wisdom) string {
	if w == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(w.Pattern)
	sb.WriteString(" ")
	sb.WriteString(w.Description)
	sb.WriteString(" ")
	sb.WriteString(w.Domain)
	for _, tag := range w.Tags {
		sb.WriteString(" ")
		sb.WriteString(tag)
	}
	return sb.String()
}

// RecordUsage records the outcome of applying a wisdom record. The
// SuccessRate field is updated to reflect the cumulative ratio.
func (a *AccumulatedWisdom) RecordUsage(id string, success bool) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	w, ok := a.byID[id]
	if !ok {
		return fmt.Errorf("debate/reflexion: AccumulatedWisdom.RecordUsage unknown wisdom %q", id)
	}
	w.UseCount++
	if success {
		w.successHits++
	} else {
		w.failureHits++
	}
	total := w.successHits + w.failureHits
	if total > 0 {
		w.SuccessRate = float64(w.successHits) / float64(total)
	}
	return nil
}

// ExtractFromEpisodes mines recurring root-cause patterns out of the
// supplied episodes and stores one Wisdom record per group of two or
// more episodes that share a root cause.
//
// Real pattern miner:
//   - Episodes are grouped by their Reflection.RootCause (or, when
//     Reflection or RootCause is empty, by the first significant
//     token of the FailureAnalysis field).
//   - Single-occurrence root causes are skipped — extraction requires
//     a pattern to repeat at least twice.
//   - For each surviving group a Wisdom is emitted with:
//     Pattern     = group key (root cause / outcome token)
//     Description = "Observed in N episodes: " + joined first-line summaries
//     Frequency   = group size
//     Confidence  = min(1.0, len(group) / 5.0)
//     Source      = "extracted"
//     Tags        = unique tokens common to every episode in the group
//   - Each Wisdom is persisted via Store, which assigns a deterministic
//     ID when one is not already present.
//
// Returns the freshly-extracted slice in deterministic (sorted key) order.
// Honors ctx.Done().
func (a *AccumulatedWisdom) ExtractFromEpisodes(ctx context.Context, episodes []*Episode) ([]*Wisdom, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	groups := make(map[string][]*Episode)
	for _, ep := range episodes {
		if ep == nil {
			continue
		}
		key := episodeGroupKey(ep)
		if key == "" {
			continue
		}
		groups[key] = append(groups[key], ep)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]*Wisdom, 0)
	for _, k := range keys {
		if err := ctx.Err(); err != nil {
			return out, err
		}
		eps := groups[k]
		if len(eps) < 2 {
			continue
		}
		confidence := float64(len(eps)) / 5.0
		if confidence > 1.0 {
			confidence = 1.0
		}
		w := &Wisdom{
			Pattern:     k,
			Description: buildExtractedDescription(eps),
			Frequency:   len(eps),
			Confidence:  confidence,
			Source:      "extracted",
			Domain:      "code",
			Tags:        commonTokens(eps),
			Timestamp:   time.Now(),
		}
		if err := a.Store(w); err != nil {
			return out, err
		}
		out = append(out, w)
	}
	return out, nil
}

// episodeGroupKey returns the group key for an episode used by the
// pattern miner: prefer Reflection.RootCause; fall back to the first
// significant token of FailureAnalysis.
func episodeGroupKey(ep *Episode) string {
	if ep == nil {
		return ""
	}
	if ep.Reflection != nil {
		rc := strings.TrimSpace(ep.Reflection.RootCause)
		if rc != "" {
			return rc
		}
	}
	for _, tok := range strings.FieldsFunc(strings.ToLower(ep.FailureAnalysis), func(r rune) bool { return !isWord(r) }) {
		if len(tok) >= 3 {
			return tok
		}
	}
	return ""
}

// buildExtractedDescription produces the human-readable description
// for a Wisdom extracted from a group of episodes. Each episode
// contributes its first-line summary (TaskDescription preferred,
// FailureAnalysis as fallback).
func buildExtractedDescription(eps []*Episode) string {
	summaries := make([]string, 0, len(eps))
	for _, ep := range eps {
		if ep == nil {
			continue
		}
		summary := firstLine(ep.TaskDescription)
		if summary == "" {
			summary = firstLine(ep.FailureAnalysis)
		}
		if summary == "" && ep.Reflection != nil {
			summary = firstLine(ep.Reflection.WhatWentWrong)
		}
		if summary == "" {
			summary = "(no summary)"
		}
		summaries = append(summaries, summary)
	}
	return fmt.Sprintf("Observed in %d episodes: %s", len(eps), strings.Join(summaries, " | "))
}

// firstLine returns the first non-empty trimmed line of the supplied
// text.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// commonTokens returns the sorted token intersection across every
// episode's combined content. Returns nil when the intersection is
// empty.
func commonTokens(eps []*Episode) []string {
	if len(eps) == 0 {
		return nil
	}
	var inter map[string]struct{}
	for _, ep := range eps {
		if ep == nil {
			continue
		}
		toks := tokenize(episodeContent(ep))
		if inter == nil {
			inter = make(map[string]struct{}, len(toks))
			for t := range toks {
				inter[t] = struct{}{}
			}
			continue
		}
		for t := range inter {
			if _, ok := toks[t]; !ok {
				delete(inter, t)
			}
		}
		if len(inter) == 0 {
			return nil
		}
	}
	if len(inter) == 0 {
		return nil
	}
	out := make([]string, 0, len(inter))
	for t := range inter {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func generateWisdomID(pattern string, seed int) string {
	h := sha1.New()
	_, _ = h.Write([]byte(fmt.Sprintf("%s|%d", pattern, seed)))
	return "w-" + hex.EncodeToString(h.Sum(nil)[:6])
}
