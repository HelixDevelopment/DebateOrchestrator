package reflexion

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReflexionLegacyAPIs_AreRealWrappers(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultReflexionConfig()

	b := NewEpisodicMemoryBuffer(cfg.MaxMemoryEntries)
	if b.Capacity != cfg.MaxMemoryEntries {
		t.Fatalf("Capacity = %d, want %d", b.Capacity, cfg.MaxMemoryEntries)
	}

	// Append with non-*Episode payload → typed error sentinel.
	if err := b.Append(ctx, "not-an-episode"); !errors.Is(err, ErrInvalidEpisode) {
		t.Fatalf("Append(string): expected ErrInvalidEpisode, got %v", err)
	}
	if err := b.Append(ctx, nil); !errors.Is(err, ErrInvalidEpisode) {
		t.Fatalf("Append(nil): expected ErrInvalidEpisode, got %v", err)
	}

	// Append with valid *Episode → REAL Store path.
	if err := b.Append(ctx, &Episode{AgentID: "legacy-caller", TaskDescription: "t1"}); err != nil {
		t.Fatalf("Append(*Episode): unexpected error %v", err)
	}
	if got := b.Size(); got != 1 {
		t.Fatalf("Size after Append: got %d, want 1", got)
	}

	g := NewReflectionGenerator(nil)
	// Generate with nil input still reports a typed-error sentinel that
	// carries the actionable RECONSTRUCTION_ROADMAP.md pointer.
	if _, err := g.Generate(ctx, nil); err == nil ||
		!strings.Contains(err.Error(), "RECONSTRUCTION_ROADMAP.md") {
		t.Fatalf("ReflectionGenerator.Generate(nil): expected RECONSTRUCTION_ROADMAP.md pointer, got %v", err)
	}

	l := NewReflexionLoop(cfg, g, nil, b)
	// Run with non-*ReflexionTask → typed error sentinel.
	if _, err := l.Run(ctx, "not-a-task"); !errors.Is(err, ErrInvalidReflexionTask) {
		t.Fatalf("Run(string): expected ErrInvalidReflexionTask, got %v", err)
	}
	if _, err := l.Run(ctx, nil); !errors.Is(err, ErrInvalidReflexionTask) {
		t.Fatalf("Run(nil): expected ErrInvalidReflexionTask, got %v", err)
	}

	// Persist without path → typed error sentinel.
	a := NewAccumulatedWisdom()
	if err := a.Persist(ctx); !errors.Is(err, ErrNoPersistencePath) {
		t.Fatalf("Persist without path: expected ErrNoPersistencePath, got %v", err)
	}
}

func TestReflexionLoop_RunWrapsExecute(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultReflexionConfig()
	cfg.MaxAttempts = 1
	b := NewEpisodicMemoryBuffer(cfg.MaxMemoryEntries)
	g := NewReflectionGenerator(nil)
	exec := &stubExecutor{passes: true}
	l := NewReflexionLoop(cfg, g, exec, b)

	task := &ReflexionTask{
		Description: "wrap-test",
		AgentID:     "agent-1",
		SessionID:   "sess-1",
		Language:    "go",
		CodeGenerator: func(ctx context.Context, desc string, prior []*Reflection) (string, error) {
			return "package main", nil
		},
	}
	out, err := l.Run(ctx, task)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	res, ok := out.(*ReflexionResult)
	if !ok || res == nil {
		t.Fatalf("Run return: expected *ReflexionResult, got %T", out)
	}
	if !res.AllPassed {
		t.Fatalf("AllPassed = false, want true (stubExecutor returns pass)")
	}
	if res.Attempts != 1 {
		t.Fatalf("Attempts = %d, want 1", res.Attempts)
	}
}

type stubExecutor struct{ passes bool }

func (s *stubExecutor) Execute(ctx context.Context, code, lang string) ([]*TestResult, error) {
	return []*TestResult{{Name: "t1", Passed: s.passes}}, nil
}

func TestAccumulatedWisdom_PersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wisdom.json")
	a := NewAccumulatedWisdom(WithPersistencePath(path))
	w := &Wisdom{Pattern: "nil deref", Frequency: 4, Domain: "code", Tags: []string{"runtime"}}
	if err := a.Store(w); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := a.Persist(context.Background()); err != nil {
		t.Fatalf("Persist: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var decoded []*Wisdom
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("decoded len = %d, want 1", len(decoded))
	}
	if decoded[0].Pattern != "nil deref" {
		t.Fatalf("decoded pattern = %q, want %q", decoded[0].Pattern, "nil deref")
	}
	if decoded[0].ID == "" {
		t.Fatalf("decoded ID empty — Store should have generated one")
	}
}

func TestAccumulatedWisdom_PersistCancelledCtx(t *testing.T) {
	dir := t.TempDir()
	a := NewAccumulatedWisdom(WithPersistencePath(filepath.Join(dir, "x.json")))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := a.Persist(ctx); err == nil {
		t.Fatal("Persist on cancelled ctx: expected error, got nil")
	}
}

func TestEpisodicMemoryBuffer_StoreGetSize(t *testing.T) {
	b := NewEpisodicMemoryBuffer(3)
	for i := 0; i < 5; i++ {
		ep := &Episode{
			AgentID:         "a",
			TaskDescription: "task",
			Code:            "code",
			Confidence:      0.5,
			Timestamp:       time.Now(),
		}
		if err := b.Store(ep); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	if got := b.Size(); got != 3 {
		t.Fatalf("Size after 5 stores into cap=3: got %d, want 3 (FIFO eviction)", got)
	}
	if all := b.GetAll(); len(all) != 3 {
		t.Fatalf("GetAll: got %d, want 3", len(all))
	}
	if recent := b.GetRecent(2); len(recent) != 2 {
		t.Fatalf("GetRecent(2): got %d, want 2", len(recent))
	}
	if byAgent := b.GetByAgent("a"); len(byAgent) != 3 {
		t.Fatalf("GetByAgent(a): got %d, want 3", len(byAgent))
	}
	if byAgent := b.GetByAgent("ghost"); len(byAgent) != 0 {
		t.Fatalf("GetByAgent(ghost): got %d, want 0", len(byAgent))
	}
}

func TestReflectionGenerator_FallbackProducesReflection(t *testing.T) {
	g := NewReflectionGenerator(nil) // no LLM client → fallback path
	req := &ReflectionRequest{
		Code:            "func divide(a, b int) int { return a / b }",
		ErrorMessages:   []string{"runtime error: integer divide by zero"},
		TaskDescription: "Implement safe division function",
		AttemptNumber:   1,
	}
	r, err := g.Generate(context.Background(), req)
	if err != nil {
		t.Fatalf("Generate: unexpected error %v", err)
	}
	if r == nil || r.RootCause == "" || r.WhatToChangeNext == "" {
		t.Fatalf("Generate: fallback should populate RootCause + WhatToChangeNext; got %+v", r)
	}
	if r.ConfidenceInFix <= 0 {
		t.Fatalf("Generate: fallback should report >0 confidence, got %v", r.ConfidenceInFix)
	}
}

func TestAccumulatedWisdom_StoreRecordUsage(t *testing.T) {
	a := NewAccumulatedWisdom()
	w := &Wisdom{Pattern: "nil pointer dereference", Frequency: 3, Domain: "code", Tags: []string{"runtime"}}
	if err := a.Store(w); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if w.ID == "" {
		t.Fatalf("Store: expected generated ID, got empty")
	}
	if got := a.Size(); got != 1 {
		t.Fatalf("Size = %d, want 1", got)
	}
	if err := a.RecordUsage(w.ID, true); err != nil {
		t.Fatalf("RecordUsage true: %v", err)
	}
	if err := a.RecordUsage(w.ID, false); err != nil {
		t.Fatalf("RecordUsage false: %v", err)
	}
	all := a.GetAll()
	if len(all) != 1 {
		t.Fatalf("GetAll: got %d, want 1", len(all))
	}
	if all[0].UseCount != 2 {
		t.Fatalf("UseCount = %d, want 2", all[0].UseCount)
	}
	if all[0].SuccessRate < 0.49 || all[0].SuccessRate > 0.51 {
		t.Fatalf("SuccessRate = %v, want ~0.5", all[0].SuccessRate)
	}
	if rel := a.GetRelevant(context.Background(), "nil pointer dereference", 5); len(rel) != 1 {
		t.Fatalf("GetRelevant: got %d, want 1", len(rel))
	}
}

func TestAccumulatedWisdom_ExtractFromEpisodes(t *testing.T) {
	a := NewAccumulatedWisdom()
	episodes := []*Episode{
		{AgentID: "x", Reflection: &Reflection{RootCause: "nil deref"}},
		{AgentID: "y", Reflection: &Reflection{RootCause: "nil deref"}},
		{AgentID: "z", Reflection: &Reflection{RootCause: "isolated"}},
	}
	out, err := a.ExtractFromEpisodes(context.Background(), episodes)
	if err != nil {
		t.Fatalf("ExtractFromEpisodes: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("ExtractFromEpisodes: got %d patterns, want 1 (frequency>=2 filter)", len(out))
	}
	if out[0].Frequency != 2 {
		t.Fatalf("ExtractFromEpisodes: pattern frequency = %d, want 2", out[0].Frequency)
	}
}

// =============================================================================
// EpisodicMemoryBuffer.GetRelevant — real token-overlap (Jaccard)
// =============================================================================

func TestEpisodicMemoryBuffer_GetRelevantTokenOverlap(t *testing.T) {
	ctx := context.Background()
	b := NewEpisodicMemoryBuffer(10)
	// E0: pure unrelated content (no overlap with query)
	if err := b.Store(&Episode{
		TaskDescription: "render markdown table",
		Timestamp:       time.Unix(100, 0),
	}); err != nil {
		t.Fatalf("Store E0: %v", err)
	}
	// E1: one shared token ("database")
	if err := b.Store(&Episode{
		TaskDescription: "open database connection",
		Timestamp:       time.Unix(200, 0),
	}); err != nil {
		t.Fatalf("Store E1: %v", err)
	}
	// E2: TWO shared tokens with the query ("database" + "timeout")
	if err := b.Store(&Episode{
		TaskDescription: "diagnose database timeout in production",
		Timestamp:       time.Unix(300, 0),
	}); err != nil {
		t.Fatalf("Store E2: %v", err)
	}
	// E3: one shared token ("timeout")
	if err := b.Store(&Episode{
		TaskDescription: "handle network timeout gracefully",
		Timestamp:       time.Unix(400, 0),
	}); err != nil {
		t.Fatalf("Store E3: %v", err)
	}
	// E4: zero shared tokens (drops out)
	if err := b.Store(&Episode{
		TaskDescription: "format yaml output for clients",
		Timestamp:       time.Unix(500, 0),
	}); err != nil {
		t.Fatalf("Store E4: %v", err)
	}

	got := b.GetRelevant(ctx, "database timeout connection", 5)
	if len(got) == 0 {
		t.Fatalf("GetRelevant: got 0 results, want > 0")
	}
	// Manually computed expected ordering:
	//   query tokens = {database, timeout, connection}
	//   E0 tokens = {render, markdown, table}      → |∩|=0  → score=0     (excluded)
	//   E1 tokens = {open, database, connection}   → |∩|=2,|∪|=4 → 0.5    (top tie with E2)
	//   E2 tokens = {diagnose, database, timeout, production} → |∩|=2,|∪|=5 → 0.4
	//   E3 tokens = {handle, network, timeout, gracefully}    → |∩|=1,|∪|=6 → ~0.167
	//   E4 tokens = {format, yaml, output, for, clients}      → |∩|=0       (excluded)
	// Highest scoring should be E1 (open database connection) at 0.5.
	if got[0].TaskDescription != "open database connection" {
		t.Fatalf("top result = %q, want %q", got[0].TaskDescription, "open database connection")
	}
	// Verify excluded zero-overlap episodes are NOT in the result.
	for _, ep := range got {
		if ep.TaskDescription == "render markdown table" ||
			ep.TaskDescription == "format yaml output for clients" {
			t.Fatalf("zero-overlap episode %q leaked into results", ep.TaskDescription)
		}
	}
}

func TestEpisodicMemoryBuffer_GetRelevantTieBreakByRecency(t *testing.T) {
	ctx := context.Background()
	b := NewEpisodicMemoryBuffer(10)
	older := &Episode{
		TaskDescription: "same content here",
		Timestamp:       time.Unix(100, 0),
	}
	newer := &Episode{
		TaskDescription: "same content here",
		Timestamp:       time.Unix(900, 0),
	}
	if err := b.Store(older); err != nil {
		t.Fatalf("Store older: %v", err)
	}
	if err := b.Store(newer); err != nil {
		t.Fatalf("Store newer: %v", err)
	}
	got := b.GetRelevant(ctx, "same content here", 2)
	if len(got) != 2 {
		t.Fatalf("got %d, want 2", len(got))
	}
	if got[0] != newer {
		t.Fatalf("first result is the older episode — tie-break by recency failed")
	}
	if got[1] != older {
		t.Fatalf("second result is not the older episode — tie-break by recency failed")
	}
}

func TestEpisodicMemoryBuffer_GetRelevantEmptyQuery(t *testing.T) {
	ctx := context.Background()
	b := NewEpisodicMemoryBuffer(5)
	if err := b.Store(&Episode{TaskDescription: "anything"}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	if got := b.GetRelevant(ctx, "", 5); len(got) != 0 {
		t.Fatalf("empty query: got %d results, want 0 (honest no-query)", len(got))
	}
	if got := b.GetRelevant(ctx, "   ", 5); len(got) != 0 {
		t.Fatalf("whitespace-only query: got %d results, want 0", len(got))
	}
}

// =============================================================================
// AccumulatedWisdom.GetRelevant — real token-overlap (Jaccard)
// =============================================================================

func TestAccumulatedWisdom_GetRelevantTokenOverlap(t *testing.T) {
	ctx := context.Background()
	a := NewAccumulatedWisdom()
	w1 := &Wisdom{Pattern: "unrelated topic about widgets", Timestamp: time.Unix(100, 0)}
	w2 := &Wisdom{Pattern: "race condition in concurrent map writes", Timestamp: time.Unix(200, 0)}
	w3 := &Wisdom{Pattern: "race condition on shared counter", Description: "concurrent increment loses updates", Timestamp: time.Unix(300, 0)}
	w4 := &Wisdom{Pattern: "deadlock between goroutines", Timestamp: time.Unix(400, 0)}
	for _, w := range []*Wisdom{w1, w2, w3, w4} {
		if err := a.Store(w); err != nil {
			t.Fatalf("Store: %v", err)
		}
	}
	got := a.GetRelevant(ctx, "race condition concurrent", 4)
	if len(got) == 0 {
		t.Fatalf("got 0 results, want > 0")
	}
	// Query tokens = {race, condition, concurrent}
	//   w1: {unrelated, topic, about, widgets}                       → 0
	//   w2: {race, condition, concurrent, map, writes}               → 3/5 = 0.6
	//   w3: {race, condition, shared, counter, concurrent, increment, loses, updates} → 3/8 = 0.375
	//   w4: {deadlock, between, goroutines}                          → 0
	// Top = w2.
	if got[0] != w2 {
		t.Fatalf("top result = %+v, want %+v", got[0], w2)
	}
	for _, w := range got {
		if w == w1 || w == w4 {
			t.Fatalf("zero-overlap wisdom %+v leaked into results", w)
		}
	}
}

// =============================================================================
// AccumulatedWisdom.ExtractFromEpisodes — real pattern miner
// =============================================================================

func TestExtractFromEpisodes_SingleOccurrenceSkipped(t *testing.T) {
	ctx := context.Background()
	a := NewAccumulatedWisdom()
	out, err := a.ExtractFromEpisodes(ctx, []*Episode{
		{Reflection: &Reflection{RootCause: "lone-pattern-X"}, TaskDescription: "thing"},
	})
	if err != nil {
		t.Fatalf("ExtractFromEpisodes: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("single-occurrence pattern produced %d wisdoms, want 0", len(out))
	}
	if a.Size() != 0 {
		t.Fatalf("Size = %d, want 0 — single-occurrence must not be stored", a.Size())
	}
}

func TestExtractFromEpisodes_PatternEmerges(t *testing.T) {
	ctx := context.Background()
	a := NewAccumulatedWisdom()
	eps := []*Episode{
		{AgentID: "a", Reflection: &Reflection{RootCause: "race condition"}, TaskDescription: "increment shared counter from multiple goroutines"},
		{AgentID: "b", Reflection: &Reflection{RootCause: "race condition"}, TaskDescription: "increment shared counter from multiple goroutines"},
		{AgentID: "c", Reflection: &Reflection{RootCause: "race condition"}, TaskDescription: "increment shared counter from multiple goroutines"},
	}
	out, err := a.ExtractFromEpisodes(ctx, eps)
	if err != nil {
		t.Fatalf("ExtractFromEpisodes: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("got %d wisdoms, want 1", len(out))
	}
	w := out[0]
	if w.Pattern != "race condition" {
		t.Fatalf("Pattern = %q, want %q", w.Pattern, "race condition")
	}
	if w.Frequency != 3 {
		t.Fatalf("Frequency = %d, want 3", w.Frequency)
	}
	// Confidence = 3/5 = 0.6
	if w.Confidence < 0.59 || w.Confidence > 0.61 {
		t.Fatalf("Confidence = %v, want 0.6", w.Confidence)
	}
	if w.Source != "extracted" {
		t.Fatalf("Source = %q, want %q", w.Source, "extracted")
	}
	if !strings.Contains(w.Description, "Observed in 3 episodes:") {
		t.Fatalf("Description missing expected prefix; got %q", w.Description)
	}
	// Tags should include tokens common to ALL three episodes (since the
	// TaskDescription is identical, every long token is shared).
	if len(w.Tags) == 0 {
		t.Fatalf("Tags empty — expected intersection tokens from identical TaskDescriptions")
	}
	foundCounter := false
	for _, tag := range w.Tags {
		if tag == "counter" {
			foundCounter = true
		}
	}
	if !foundCounter {
		t.Fatalf("Tags %v missing expected shared token 'counter'", w.Tags)
	}
}

func TestExtractFromEpisodes_TwoDistinctPatterns(t *testing.T) {
	ctx := context.Background()
	a := NewAccumulatedWisdom()
	eps := []*Episode{
		{Reflection: &Reflection{RootCause: "race condition"}, TaskDescription: "shared map writes"},
		{Reflection: &Reflection{RootCause: "race condition"}, TaskDescription: "shared map writes"},
		{Reflection: &Reflection{RootCause: "race condition"}, TaskDescription: "shared map writes"},
		{Reflection: &Reflection{RootCause: "null deref"}, TaskDescription: "uninitialised pointer"},
		{Reflection: &Reflection{RootCause: "null deref"}, TaskDescription: "uninitialised pointer"},
	}
	out, err := a.ExtractFromEpisodes(ctx, eps)
	if err != nil {
		t.Fatalf("ExtractFromEpisodes: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d wisdoms, want 2", len(out))
	}
	patterns := map[string]int{}
	for _, w := range out {
		patterns[w.Pattern] = w.Frequency
		if w.Frequency < 2 {
			t.Fatalf("wisdom %q has freq %d, want >= 2", w.Pattern, w.Frequency)
		}
	}
	if patterns["race condition"] != 3 {
		t.Fatalf("race condition frequency = %d, want 3", patterns["race condition"])
	}
	if patterns["null deref"] != 2 {
		t.Fatalf("null deref frequency = %d, want 2", patterns["null deref"])
	}
}

func TestExtractFromEpisodes_StoresExtracted(t *testing.T) {
	ctx := context.Background()
	a := NewAccumulatedWisdom()
	pre := a.Size()
	eps := []*Episode{
		{Reflection: &Reflection{RootCause: "race condition"}, TaskDescription: "x"},
		{Reflection: &Reflection{RootCause: "race condition"}, TaskDescription: "x"},
		{Reflection: &Reflection{RootCause: "null deref"}, TaskDescription: "y"},
		{Reflection: &Reflection{RootCause: "null deref"}, TaskDescription: "y"},
	}
	out, err := a.ExtractFromEpisodes(ctx, eps)
	if err != nil {
		t.Fatalf("ExtractFromEpisodes: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("extracted = %d, want 2", len(out))
	}
	post := a.Size()
	if post-pre != len(out) {
		t.Fatalf("Size delta = %d, want %d — extracted wisdoms not stored", post-pre, len(out))
	}
	// Each extracted wisdom must be retrievable by its assigned ID.
	for _, w := range out {
		if w.ID == "" {
			t.Fatalf("extracted wisdom has empty ID — Store did not assign one")
		}
	}
}

func TestExtractFromEpisodes_CtxCancel(t *testing.T) {
	a := NewAccumulatedWisdom()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.ExtractFromEpisodes(ctx, []*Episode{
		{Reflection: &Reflection{RootCause: "race condition"}},
		{Reflection: &Reflection{RootCause: "race condition"}},
	})
	if err == nil {
		t.Fatal("ExtractFromEpisodes on cancelled ctx: expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ExtractFromEpisodes ctx error = %v, want %v", err, context.Canceled)
	}
}
