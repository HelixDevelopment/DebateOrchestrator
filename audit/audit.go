// Package audit hosts the ProvenanceTracker — a real, thread-safe,
// append-only audit-event log used by the debate runtime to record
// who-did-what-when across a session.
//
// The tracker stores RecordedEvent values in memory; an optional
// disk-log option ALSO writes a newline-delimited JSON record for
// each event. Both surfaces carry positive runtime evidence: callers
// can read events back with GetEvents/Count and verify them against
// the on-disk log when WithDiskLog is configured.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

// RecordedEvent is a single audit-log entry. The Timestamp is set by
// the tracker when Record is invoked. Event is the caller-supplied
// payload (any JSON-serialisable value when WithDiskLog is used; any
// value at all when only the in-memory log is used).
type RecordedEvent struct {
	// Timestamp is when the tracker received the event (UTC).
	Timestamp time.Time `json:"timestamp"`
	// Event is the caller-supplied payload.
	Event interface{} `json:"event"`
}

// Option configures a ProvenanceTracker at construction time.
type Option func(*ProvenanceTracker)

// WithCapacity caps the in-memory ring buffer. When the cap is
// reached the oldest event is evicted (FIFO). A cap of 0 disables
// the cap (unbounded slice — caller's responsibility to bound memory
// out-of-band, typically by also setting WithDiskLog and rotating).
func WithCapacity(cap int) Option {
	return func(t *ProvenanceTracker) {
		if cap < 0 {
			cap = 0
		}
		t.capacity = cap
	}
}

// WithDiskLog directs the tracker to ALSO append every event to the
// supplied path as a newline-delimited JSON record. The file is
// opened with O_APPEND|O_CREATE|O_WRONLY at 0600. Errors during the
// disk write are surfaced from Record.
func WithDiskLog(path string) Option {
	return func(t *ProvenanceTracker) {
		t.diskLogPath = path
	}
}

// ProvenanceTracker records who-did-what-when audit events. The
// zero-value is not usable; always construct via NewProvenanceTracker.
type ProvenanceTracker struct {
	mu          sync.RWMutex
	events      []RecordedEvent
	capacity    int
	diskLogPath string
}

// NewProvenanceTracker constructs a ProvenanceTracker with the
// supplied options. A tracker with no options is an unbounded
// in-memory log.
func NewProvenanceTracker(opts ...Option) *ProvenanceTracker {
	t := &ProvenanceTracker{}
	for _, opt := range opts {
		if opt != nil {
			opt(t)
		}
	}
	return t
}

// Record timestamps the event, appends it to the in-memory log
// (honouring the configured capacity), and — when WithDiskLog was
// supplied — appends a JSON record to the disk log.
//
// Returns ctx.Err() if the context is cancelled, the disk-log
// error if the on-disk append fails, or nil on success.
func (t *ProvenanceTracker) Record(ctx context.Context, event interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	rec := RecordedEvent{
		Timestamp: time.Now().UTC(),
		Event:     event,
	}
	t.mu.Lock()
	t.events = append(t.events, rec)
	if t.capacity > 0 && len(t.events) > t.capacity {
		drop := len(t.events) - t.capacity
		t.events = t.events[drop:]
	}
	diskPath := t.diskLogPath
	t.mu.Unlock()

	if diskPath != "" {
		if err := appendDiskLog(diskPath, rec); err != nil {
			return fmt.Errorf("debate/audit: disk log append failed: %w", err)
		}
	}
	return nil
}

// GetEvents returns an immutable snapshot of every event recorded so
// far in insertion order. Mutating the returned slice does not
// affect the tracker.
func (t *ProvenanceTracker) GetEvents() []RecordedEvent {
	t.mu.RLock()
	defer t.mu.RUnlock()
	out := make([]RecordedEvent, len(t.events))
	copy(out, t.events)
	return out
}

// Count returns the number of events currently held in the in-memory
// log. When WithCapacity caps the buffer, Count reflects the post-
// eviction size, not the cumulative number of Record calls.
func (t *ProvenanceTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.events)
}

func appendDiskLog(path string, rec RecordedEvent) error {
	if path == "" {
		return errors.New("empty disk log path")
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}
