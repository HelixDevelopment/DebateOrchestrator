package evaluation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBenchmarkBridge_ConstructorReturnsNonNil(t *testing.T) {
	if NewBenchmarkBridge() == nil {
		t.Fatal("NewBenchmarkBridge: nil")
	}
}

func TestBenchmarkBridge_RunBenchmark_CapturesDuration(t *testing.T) {
	b := NewBenchmarkBridge()
	ctx := context.Background()
	const sleep = 5 * time.Millisecond
	res, err := b.RunBenchmark(ctx, "sleep", func() error {
		time.Sleep(sleep)
		return nil
	})
	if err != nil {
		t.Fatalf("RunBenchmark: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.Name != "sleep" {
		t.Fatalf("Name = %q, want %q", res.Name, "sleep")
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations = %d, want 1", res.Iterations)
	}
	if res.MeanDuration < sleep {
		t.Fatalf("MeanDuration = %v, expected >= %v", res.MeanDuration, sleep)
	}
	if !res.Successful {
		t.Fatalf("Successful = false, want true (Error=%q)", res.Error)
	}
}

func TestBenchmarkBridge_RunBenchmark_PropagatesClosureError(t *testing.T) {
	b := NewBenchmarkBridge()
	wantErr := errors.New("boom")
	res, err := b.RunBenchmark(context.Background(), "fail", func() error {
		return wantErr
	})
	if err != nil {
		t.Fatalf("RunBenchmark unexpected outer error: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if res.Successful {
		t.Fatal("Successful = true, want false")
	}
	if res.Error != wantErr.Error() {
		t.Fatalf("Error = %q, want %q", res.Error, wantErr.Error())
	}
}

func TestBenchmarkBridge_RunBenchmark_NilClosure(t *testing.T) {
	b := NewBenchmarkBridge()
	if _, err := b.RunBenchmark(context.Background(), "nil", nil); !errors.Is(err, ErrNilClosure) {
		t.Fatalf("expected ErrNilClosure, got %v", err)
	}
}

func TestBenchmarkBridge_RunBenchmark_CancelledContext(t *testing.T) {
	b := NewBenchmarkBridge()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.RunBenchmark(ctx, "cancel", func() error { return nil }); err == nil {
		t.Fatal("expected ctx error, got nil")
	}
}

func TestBenchmarkBridge_RunBenchmarks_Aggregate(t *testing.T) {
	b := NewBenchmarkBridge()
	const iter = 5
	res, err := b.RunBenchmarks(context.Background(), "agg", func() error {
		time.Sleep(time.Millisecond)
		return nil
	}, iter)
	if err != nil {
		t.Fatalf("RunBenchmarks: %v", err)
	}
	if res.Iterations != iter {
		t.Fatalf("Iterations = %d, want %d", res.Iterations, iter)
	}
	if res.MinDuration <= 0 {
		t.Fatalf("MinDuration = %v, want >0", res.MinDuration)
	}
	if res.MaxDuration < res.MinDuration {
		t.Fatalf("MaxDuration %v < MinDuration %v", res.MaxDuration, res.MinDuration)
	}
	if res.MeanDuration < res.MinDuration || res.MeanDuration > res.MaxDuration {
		t.Fatalf("Mean %v outside [min %v, max %v]", res.MeanDuration, res.MinDuration, res.MaxDuration)
	}
	if res.MedianDuration < res.MinDuration || res.MedianDuration > res.MaxDuration {
		t.Fatalf("Median %v outside [min %v, max %v]", res.MedianDuration, res.MinDuration, res.MaxDuration)
	}
	if !res.Successful {
		t.Fatalf("Successful=false (Error=%q)", res.Error)
	}
}

func TestBenchmarkBridge_RunBenchmarks_IterationsClampedToOne(t *testing.T) {
	b := NewBenchmarkBridge()
	res, err := b.RunBenchmarks(context.Background(), "clamp", func() error { return nil }, 0)
	if err != nil {
		t.Fatalf("RunBenchmarks: %v", err)
	}
	if res.Iterations != 1 {
		t.Fatalf("Iterations under 0-input: got %d, want 1", res.Iterations)
	}
}

func TestBenchmarkBridge_RunBenchmarks_ShortCircuitsOnError(t *testing.T) {
	b := NewBenchmarkBridge()
	calls := 0
	res, err := b.RunBenchmarks(context.Background(), "short", func() error {
		calls++
		if calls == 2 {
			return errors.New("fail-on-2")
		}
		return nil
	}, 5)
	if err != nil {
		t.Fatalf("RunBenchmarks: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 (short-circuit on first error)", calls)
	}
	if res.Successful {
		t.Fatal("Successful = true, want false")
	}
	if res.Iterations != 2 {
		t.Fatalf("Iterations = %d, want 2", res.Iterations)
	}
}

func TestBenchmarkBridge_RunBenchmarks_AllocationDeltaPositive(t *testing.T) {
	b := NewBenchmarkBridge()
	// Sink prevents escape-analysis from stack-allocating the buffer.
	var sink [][]byte
	res, err := b.RunBenchmarks(context.Background(), "alloc", func() error {
		buf := make([]byte, 64*1024)
		buf[0] = byte(len(sink))
		sink = append(sink, buf)
		return nil
	}, 10)
	if err != nil {
		t.Fatalf("RunBenchmarks: %v", err)
	}
	if res.BytesAllocated == 0 {
		t.Fatalf("BytesAllocated = 0, expected >0 for 10x64KB allocations (sink len=%d)", len(sink))
	}
}

func TestBenchmarkBridge_RunBenchmark_StringRendersNonEmpty(t *testing.T) {
	b := NewBenchmarkBridge()
	res, err := b.RunBenchmark(context.Background(), "str", func() error { return nil })
	if err != nil {
		t.Fatalf("RunBenchmark: %v", err)
	}
	if s := res.String(); s == "" {
		t.Fatal("String() empty")
	}
	var nilRes *BenchmarkResult
	if got := nilRes.String(); got == "" {
		t.Fatal("nil String() empty")
	}
}
