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
// marker. The original NotYetImplemented stubs (.Append, .Run,
// .Persist) are preserved under their original names for
// backwards compatibility.
package reflexion

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
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
	ID string
	// Pattern is the wisdom pattern (free-form text).
	Pattern string
	// Frequency is the number of episodes the pattern was derived from.
	Frequency int
	// Domain is the wisdom domain (e.g. "code", "review").
	Domain string
	// Tags is the wisdom tag list.
	Tags []string
	// UseCount is the number of times the wisdom has been applied.
	UseCount int
	// SuccessRate is the ratio of successful applications.
	SuccessRate float64
	// Impact is the wisdom's impact score.
	Impact float64

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

// Append records an episodic memory entry (legacy stub preserved for
// backwards compatibility — returns NotYetImplemented).
func (b *EpisodicMemoryBuffer) Append(ctx context.Context, entry interface{}) error {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return err
	}
	_ = entry
	return errors.New("debate/reflexion: EpisodicMemoryBuffer.Append NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

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

// GetRelevant returns up to `n` episodes whose TaskDescription,
// FailureAnalysis, or Reflection root cause contains `query`
// (case-insensitive substring match).
//
// Honest-stub relevance: this is a deterministic substring-based
// shortlist, not a real embedding search. Real relevance is tracked
// in RECONSTRUCTION_ROADMAP.md.
func (b *EpisodicMemoryBuffer) GetRelevant(query string, n int) []*Episode {
	// TODO(reconstruction-phase-2): real implementation pending
	if n <= 0 {
		return nil
	}
	q := strings.ToLower(query)
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*Episode, 0, n)
	for _, ep := range b.episodes {
		if matchesEpisode(ep, q) {
			out = append(out, ep)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}

func matchesEpisode(ep *Episode, q string) bool {
	if q == "" {
		return false
	}
	if strings.Contains(strings.ToLower(ep.TaskDescription), q) {
		return true
	}
	if strings.Contains(strings.ToLower(ep.FailureAnalysis), q) {
		return true
	}
	if ep.Reflection != nil &&
		strings.Contains(strings.ToLower(ep.Reflection.RootCause), q) {
		return true
	}
	return false
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
// input (legacy stub preserved for backwards compatibility — returns
// NotYetImplemented).
func (l *ReflexionLoop) Run(ctx context.Context, input interface{}) (interface{}, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	_ = input
	return nil, errors.New("debate/reflexion: ReflexionLoop.Run NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
}

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
	mu      sync.RWMutex
	store   []*Wisdom
	byID    map[string]*Wisdom
	seedKey int
}

// NewAccumulatedWisdom constructs an empty AccumulatedWisdom.
func NewAccumulatedWisdom() *AccumulatedWisdom {
	return &AccumulatedWisdom{byID: make(map[string]*Wisdom)}
}

// Persist writes the wisdom store to durable storage (legacy stub
// preserved for backwards compatibility — returns NotYetImplemented).
func (a *AccumulatedWisdom) Persist(ctx context.Context) error {
	// TODO(reconstruction-phase-2): real implementation pending
	if err := ctx.Err(); err != nil {
		return err
	}
	return errors.New("debate/reflexion: AccumulatedWisdom.Persist NotYetImplemented — see RECONSTRUCTION_ROADMAP.md")
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

// GetRelevant returns up to `n` wisdom records whose pattern, domain,
// or tags contain `query` (case-insensitive substring match).
//
// Honest-stub relevance: deterministic substring shortlist; real
// embedding-based relevance is tracked in RECONSTRUCTION_ROADMAP.md.
func (a *AccumulatedWisdom) GetRelevant(query string, n int) []*Wisdom {
	// TODO(reconstruction-phase-2): real implementation pending
	if n <= 0 {
		return nil
	}
	q := strings.ToLower(query)
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Wisdom, 0, n)
	for _, w := range a.store {
		if matchesWisdom(w, q) {
			out = append(out, w)
			if len(out) >= n {
				break
			}
		}
	}
	return out
}

func matchesWisdom(w *Wisdom, q string) bool {
	if q == "" {
		return false
	}
	if strings.Contains(strings.ToLower(w.Pattern), q) {
		return true
	}
	if strings.Contains(strings.ToLower(w.Domain), q) {
		return true
	}
	for _, tag := range w.Tags {
		if strings.Contains(strings.ToLower(tag), q) {
			return true
		}
	}
	return false
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

// ExtractFromEpisodes derives wisdom records from a list of episodes
// by grouping their reflection root causes and emitting one Wisdom
// per group of two or more episodes.
//
// Honest-stub extraction: pattern = root cause; domain = "code";
// tags = derived from agent IDs; frequency = group size. Real
// pattern mining is tracked in RECONSTRUCTION_ROADMAP.md.
func (a *AccumulatedWisdom) ExtractFromEpisodes(episodes []*Episode) ([]*Wisdom, error) {
	// TODO(reconstruction-phase-2): real implementation pending
	groups := make(map[string][]*Episode)
	for _, ep := range episodes {
		if ep == nil || ep.Reflection == nil || ep.Reflection.RootCause == "" {
			continue
		}
		groups[ep.Reflection.RootCause] = append(groups[ep.Reflection.RootCause], ep)
	}
	out := make([]*Wisdom, 0)
	// Sort keys for deterministic output.
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		eps := groups[k]
		if len(eps) < 2 {
			continue
		}
		tags := uniqueAgentIDs(eps)
		w := &Wisdom{
			Pattern:   k,
			Frequency: len(eps),
			Domain:    "code",
			Tags:      tags,
		}
		if err := a.Store(w); err != nil {
			return out, err
		}
		out = append(out, w)
	}
	return out, nil
}

func uniqueAgentIDs(eps []*Episode) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, ep := range eps {
		if ep == nil {
			continue
		}
		if _, ok := seen[ep.AgentID]; ok {
			continue
		}
		seen[ep.AgentID] = struct{}{}
		out = append(out, ep.AgentID)
	}
	sort.Strings(out)
	return out
}

func generateWisdomID(pattern string, seed int) string {
	h := sha1.New()
	_, _ = h.Write([]byte(fmt.Sprintf("%s|%d", pattern, seed)))
	return "w-" + hex.EncodeToString(h.Sum(nil)[:6])
}
