package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestProvenanceTracker_ConstructorAndZeroState(t *testing.T) {
	tr := NewProvenanceTracker()
	if tr == nil {
		t.Fatal("nil tracker")
	}
	if got := tr.Count(); got != 0 {
		t.Fatalf("Count on fresh tracker: got %d, want 0", got)
	}
	if got := tr.GetEvents(); len(got) != 0 {
		t.Fatalf("GetEvents on fresh tracker: got %d, want 0", len(got))
	}
}

func TestProvenanceTracker_RecordAndGetEvents(t *testing.T) {
	tr := NewProvenanceTracker()
	ctx := context.Background()

	payloads := []string{"alpha", "beta", "gamma"}
	before := time.Now().UTC()
	for _, p := range payloads {
		if err := tr.Record(ctx, p); err != nil {
			t.Fatalf("Record(%q): unexpected error %v", p, err)
		}
	}
	after := time.Now().UTC()

	if got := tr.Count(); got != len(payloads) {
		t.Fatalf("Count after %d records: got %d, want %d", len(payloads), got, len(payloads))
	}
	events := tr.GetEvents()
	if len(events) != len(payloads) {
		t.Fatalf("GetEvents: got %d, want %d", len(events), len(payloads))
	}
	for i, ev := range events {
		got, ok := ev.Event.(string)
		if !ok {
			t.Fatalf("event[%d]: expected string payload, got %T", i, ev.Event)
		}
		if got != payloads[i] {
			t.Fatalf("event[%d]: payload = %q, want %q", i, got, payloads[i])
		}
		if ev.Timestamp.Before(before) || ev.Timestamp.After(after.Add(time.Second)) {
			t.Fatalf("event[%d]: timestamp %v outside [%v, %v]", i, ev.Timestamp, before, after)
		}
	}

	// Snapshot must be a copy — mutating it must not affect the
	// tracker's internal state.
	events[0].Event = "tampered"
	again := tr.GetEvents()
	if again[0].Event != "alpha" {
		t.Fatalf("GetEvents returned shared slice — internal state mutated")
	}
}

func TestProvenanceTracker_ContextCancel(t *testing.T) {
	tr := NewProvenanceTracker()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := tr.Record(ctx, "should-not-record"); err == nil {
		t.Fatal("Record on cancelled ctx: expected error, got nil")
	}
	if got := tr.Count(); got != 0 {
		t.Fatalf("Count after cancelled Record: got %d, want 0", got)
	}
}

func TestProvenanceTracker_CapacityEvictsFIFO(t *testing.T) {
	tr := NewProvenanceTracker(WithCapacity(3))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if err := tr.Record(ctx, i); err != nil {
			t.Fatalf("Record(%d): %v", i, err)
		}
	}
	if got := tr.Count(); got != 3 {
		t.Fatalf("Count under cap=3 after 5 records: got %d, want 3", got)
	}
	events := tr.GetEvents()
	wantFirst, wantLast := 2, 4
	if events[0].Event.(int) != wantFirst {
		t.Fatalf("FIFO eviction: events[0] = %v, want %d", events[0].Event, wantFirst)
	}
	if events[len(events)-1].Event.(int) != wantLast {
		t.Fatalf("FIFO eviction: events[last] = %v, want %d", events[len(events)-1].Event, wantLast)
	}
}

func TestProvenanceTracker_WithDiskLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "provenance.log")
	tr := NewProvenanceTracker(WithDiskLog(logPath))
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		if err := tr.Record(ctx, map[string]int{"i": i}); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}

	f, err := os.Open(logPath)
	if err != nil {
		t.Fatalf("open disk log: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	count := 0
	for sc.Scan() {
		var rec RecordedEvent
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Fatalf("disk log line %d not JSON: %v", count, err)
		}
		count++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if count != 4 {
		t.Fatalf("disk log line count: got %d, want 4", count)
	}
}

func TestProvenanceTracker_ConcurrentRecord(t *testing.T) {
	tr := NewProvenanceTracker()
	ctx := context.Background()
	var wg sync.WaitGroup
	workers := 8
	per := 25
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < per; i++ {
				if err := tr.Record(ctx, [2]int{id, i}); err != nil {
					t.Errorf("Record: %v", err)
					return
				}
			}
		}(w)
	}
	wg.Wait()
	if got, want := tr.Count(), workers*per; got != want {
		t.Fatalf("Count after concurrent records: got %d, want %d", got, want)
	}
}
