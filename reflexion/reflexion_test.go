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
	if rel := a.GetRelevant("nil pointer", 5); len(rel) != 1 {
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
	out, err := a.ExtractFromEpisodes(episodes)
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
