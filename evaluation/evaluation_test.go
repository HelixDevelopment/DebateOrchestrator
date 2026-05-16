package evaluation

import (
	"context"
	"strings"
	"testing"
)

func TestBenchmarkBridgeIsHonestStub(t *testing.T) {
	b := NewBenchmarkBridge()
	if _, err := b.RunBenchmark(context.Background(), "humaneval"); err == nil || !strings.Contains(err.Error(), "NotYetImplemented") {
		t.Fatalf("expected NotYetImplemented stub error, got %v", err)
	}
}
