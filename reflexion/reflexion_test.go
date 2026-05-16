package reflexion

import (
	"context"
	"strings"
	"testing"
)

func TestReflexionStubsReturnNotYetImplemented(t *testing.T) {
	ctx := context.Background()
	cfg := DefaultReflexionConfig()

	b := NewEpisodicMemoryBuffer(cfg.MaxMemoryEntries)
	if b.Capacity != cfg.MaxMemoryEntries {
		t.Fatalf("Capacity = %d, want %d", b.Capacity, cfg.MaxMemoryEntries)
	}
	if err := b.Append(ctx, nil); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("EpisodicMemoryBuffer.Append: %v", err)
	}
	g := NewReflectionGenerator(nil)
	if _, err := g.Generate(ctx, nil); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("ReflectionGenerator.Generate: %v", err)
	}
	l := NewReflexionLoop(cfg, g, nil, b)
	if _, err := l.Run(ctx, nil); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("ReflexionLoop.Run: %v", err)
	}
	a := NewAccumulatedWisdom()
	if err := a.Persist(ctx); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("AccumulatedWisdom.Persist: %v", err)
	}
}
