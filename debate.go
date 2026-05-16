// Package debate is the root of the DebateOrchestrator module. It
// hosts the in-memory LessonBank used by the rest of the system. The
// implementation is REAL but minimal — lessons are kept in process
// memory; durable storage and semantic search are tracked in
// RECONSTRUCTION_ROADMAP.md.
package debate

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Lesson is a single piece of knowledge extracted from a debate.
type Lesson struct {
	// ID is the lesson identifier.
	ID string
	// Topic is the topic the lesson was derived from.
	Topic string
	// Content is the lesson text.
	Content string
	// Confidence is the 0..1 confidence in the lesson.
	Confidence float64
	// CreatedAt is the UTC creation timestamp.
	CreatedAt time.Time
}

// LessonBankConfig configures a LessonBank at construction time.
type LessonBankConfig struct {
	// EnableSemanticSearch toggles vector search support. The current
	// implementation does substring search regardless of this flag.
	EnableSemanticSearch bool
	// StoragePath is the on-disk path for persistence. Empty means
	// memory-only. Persistence is not yet implemented.
	StoragePath string
}

// DefaultLessonBankConfig returns the canonical default configuration.
func DefaultLessonBankConfig() LessonBankConfig {
	return LessonBankConfig{
		EnableSemanticSearch: false,
		StoragePath:          "",
	}
}

// LessonBank is a thread-safe in-memory store of Lessons. It also
// holds session-level metadata describing the currently active debate
// (ID, topic, status, conclusion, completion time) so callers don't
// have to thread that data through additional channels.
type LessonBank struct {
	cfg  LessonBankConfig
	opts []interface{}

	mu      sync.RWMutex
	lessons map[string]Lesson

	id          string
	topic       string
	status      string
	conclusion  string
	completedAt time.Time
}

// Options returns the caller-supplied option slice from construction.
// Returned slice is read-only to callers (a defensive copy).
func (b *LessonBank) Options() []interface{} {
	if len(b.opts) == 0 {
		return nil
	}
	out := make([]interface{}, len(b.opts))
	copy(out, b.opts)
	return out
}

// NewLessonBank constructs a LessonBank from the supplied configuration.
//
// The variadic opts slot accepts optional caller-supplied dependencies such
// as a logger and a persistence backend. The current implementation is
// memory-only so any opts passed are stored and exposed via the Options
// accessor but not otherwise consumed — they exist so callers can wire
// future durability/logging hooks without re-spelling the constructor.
// The function returns a single value (no error) so callers do not need
// two-variable assignment for the common in-memory case.
func NewLessonBank(cfg LessonBankConfig, opts ...interface{}) *LessonBank {
	return &LessonBank{
		cfg:     cfg,
		lessons: make(map[string]Lesson),
		status:  "initialised",
		opts:    opts,
	}
}

// Add records a lesson. An empty ID is rejected.
func (b *LessonBank) Add(lesson Lesson) error {
	if lesson.ID == "" {
		return errors.New("debate: lesson ID required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lessons[lesson.ID] = lesson
	return nil
}

// Get returns a lesson by ID or an error if unknown.
func (b *LessonBank) Get(id string) (Lesson, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	l, ok := b.lessons[id]
	if !ok {
		return Lesson{}, fmt.Errorf("debate: lesson %q not found", id)
	}
	return l, nil
}

// List returns a snapshot of every lesson in the bank.
func (b *LessonBank) List() []Lesson {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Lesson, 0, len(b.lessons))
	for _, l := range b.lessons {
		out = append(out, l)
	}
	return out
}

// Count returns the number of stored lessons.
func (b *LessonBank) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.lessons)
}

// Search returns every lesson whose Content or Topic contains the
// supplied query substring. Case-insensitive.
func (b *LessonBank) Search(query string) []Lesson {
	q := strings.ToLower(query)
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Lesson, 0, len(b.lessons))
	for _, l := range b.lessons {
		if strings.Contains(strings.ToLower(l.Content), q) ||
			strings.Contains(strings.ToLower(l.Topic), q) {
			out = append(out, l)
		}
	}
	return out
}

// Confidence returns the recorded confidence for a lesson, or 0 if unknown.
func (b *LessonBank) Confidence(id string) float64 {
	l, err := b.Get(id)
	if err != nil {
		return 0
	}
	return l.Confidence
}

// ID returns the session identifier the bank is associated with.
func (b *LessonBank) ID() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.id
}

// Topic returns the topic the bank is associated with.
func (b *LessonBank) Topic() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.topic
}

// Status returns the bank's current status string.
func (b *LessonBank) Status() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.status
}

// Conclusion returns the recorded conclusion (empty until set).
func (b *LessonBank) Conclusion() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.conclusion
}

// CompletedAt returns the recorded completion timestamp.
func (b *LessonBank) CompletedAt() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.completedAt
}

// SetSession records session-level metadata on the bank.
func (b *LessonBank) SetSession(id, topic, status, conclusion string, completedAt time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.id = id
	b.topic = topic
	b.status = status
	b.conclusion = conclusion
	b.completedAt = completedAt
}
