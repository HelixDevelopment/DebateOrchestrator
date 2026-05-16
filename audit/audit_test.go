package audit

import (
	"context"
	"strings"
	"testing"
)

func TestProvenanceTrackerIsHonestStub(t *testing.T) {
	tr := NewProvenanceTracker()
	if tr == nil {
		t.Fatal("nil tracker")
	}
	if err := tr.Record(context.Background(), "x"); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("expected NotYetImplemented stub error, got %v", err)
	}
}
