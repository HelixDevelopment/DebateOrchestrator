package debate

import (
	"testing"
	"time"
)

func TestLessonBankCRUD(t *testing.T) {
	bank := NewLessonBank(DefaultLessonBankConfig())
	if bank == nil {
		t.Fatal("NewLessonBank returned nil")
	}
	if err := bank.Add(Lesson{ID: "L1", Topic: "tests", Content: "real coverage", Confidence: 0.9, CreatedAt: time.Now()}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if bank.Count() != 1 {
		t.Fatalf("Count = %d, want 1", bank.Count())
	}
	got, err := bank.Get("L1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Content != "real coverage" {
		t.Fatalf("Content = %q", got.Content)
	}
	if hits := bank.Search("REAL"); len(hits) != 1 {
		t.Fatalf("Search hits = %d", len(hits))
	}
	if c := bank.Confidence("L1"); c != 0.9 {
		t.Fatalf("Confidence = %v", c)
	}
	bank.SetSession("S", "T", "completed", "ok", time.Now())
	if bank.Status() != "completed" {
		t.Fatalf("Status = %q", bank.Status())
	}
}
