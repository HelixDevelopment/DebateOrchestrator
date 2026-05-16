// Package evaluation hosts the BenchmarkBridge — a real, stdlib-only
// benchmark runner used by the debate runtime to measure the
// wall-clock and allocation cost of caller-supplied closures.
//
// Single-run benchmarks (RunBenchmark) execute the closure once and
// report elapsed time + bytes allocated. Aggregate benchmarks
// (RunBenchmarks) execute the closure N times and report min / max /
// mean / median wall-clock alongside the aggregate allocation total.
//
// The bridge intentionally does not depend on the `testing` package's
// benchmark machinery — that machinery is tied to `go test` and
// cannot be invoked at runtime. The bridge uses time.Now /
// runtime.ReadMemStats / runtime.GC to capture deterministic-enough
// numbers for orchestrator-visible scoring.
package evaluation

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"time"
)

// BenchmarkResult is the captured outcome of one benchmark run (or
// one aggregate over many runs). Successful is false when the
// supplied closure returned a non-nil error; Error then carries that
// error's message. Min/Mean/Median/Max are set for both single-run
// (in which case they all equal the lone sample) and aggregate runs.
type BenchmarkResult struct {
	// Name identifies the benchmark.
	Name string
	// Iterations is the number of times the closure was executed.
	// Always >=1.
	Iterations int
	// MeanDuration is the arithmetic-mean wall-clock duration.
	MeanDuration time.Duration
	// MinDuration is the minimum wall-clock duration observed.
	MinDuration time.Duration
	// MaxDuration is the maximum wall-clock duration observed.
	MaxDuration time.Duration
	// MedianDuration is the median wall-clock duration.
	MedianDuration time.Duration
	// BytesAllocated is the total heap allocation delta across all
	// iterations as reported by runtime.ReadMemStats.TotalAlloc. May
	// be a conservative over-estimate when the runtime triggers GC
	// during the benchmark.
	BytesAllocated uint64
	// Successful is true when every iteration's closure returned a
	// nil error.
	Successful bool
	// Error carries the first error encountered, if any.
	Error string
}

// BenchmarkBridge connects the orchestrator to evaluation benchmarks.
// The zero-value is usable; the constructor is preserved for parity
// with the rest of the debate runtime.
type BenchmarkBridge struct{}

// NewBenchmarkBridge constructs a BenchmarkBridge.
func NewBenchmarkBridge() *BenchmarkBridge {
	return &BenchmarkBridge{}
}

// RunBenchmark executes fn once and reports the captured wall-clock
// duration and heap-allocation delta as a *BenchmarkResult.
//
// Returns ctx.Err() if the context is cancelled, ErrNilClosure if fn
// is nil, or the populated result otherwise. The fn's own error is
// surfaced through result.Error (and Successful=false) — the result
// pointer is still returned so callers can inspect the partial
// timing data.
func (b *BenchmarkBridge) RunBenchmark(ctx context.Context, name string, fn func() error) (*BenchmarkResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, ErrNilClosure
	}
	sample, alloc, fnErr := runOnce(fn)
	res := &BenchmarkResult{
		Name:           name,
		Iterations:     1,
		MeanDuration:   sample,
		MinDuration:    sample,
		MaxDuration:    sample,
		MedianDuration: sample,
		BytesAllocated: alloc,
		Successful:     fnErr == nil,
	}
	if fnErr != nil {
		res.Error = fnErr.Error()
	}
	return res, nil
}

// RunBenchmarks executes fn `iterations` times and returns an
// aggregate result. iterations <=0 is treated as 1. The first
// closure error short-circuits the run; the partial result is still
// returned so callers can inspect timing collected before the
// failure.
func (b *BenchmarkBridge) RunBenchmarks(ctx context.Context, name string, fn func() error, iterations int) (*BenchmarkResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if fn == nil {
		return nil, ErrNilClosure
	}
	if iterations <= 0 {
		iterations = 1
	}
	samples := make([]time.Duration, 0, iterations)
	var (
		totalAlloc uint64
		fnErr      error
	)
	for i := 0; i < iterations; i++ {
		if err := ctx.Err(); err != nil {
			return aggregate(name, samples, totalAlloc, fnErr), err
		}
		sample, alloc, err := runOnce(fn)
		samples = append(samples, sample)
		totalAlloc += alloc
		if err != nil {
			fnErr = err
			break
		}
	}
	return aggregate(name, samples, totalAlloc, fnErr), nil
}

// ErrNilClosure is returned when a benchmark is requested with a nil
// closure.
var ErrNilClosure = errors.New("debate/evaluation: nil benchmark closure")

func runOnce(fn func() error) (time.Duration, uint64, error) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	err := fn()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	var alloc uint64
	if after.TotalAlloc >= before.TotalAlloc {
		alloc = after.TotalAlloc - before.TotalAlloc
	}
	return elapsed, alloc, err
}

func aggregate(name string, samples []time.Duration, totalAlloc uint64, fnErr error) *BenchmarkResult {
	res := &BenchmarkResult{
		Name:           name,
		Iterations:     len(samples),
		BytesAllocated: totalAlloc,
		Successful:     fnErr == nil,
	}
	if fnErr != nil {
		res.Error = fnErr.Error()
	}
	if len(samples) == 0 {
		return res
	}
	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	res.MinDuration = sorted[0]
	res.MaxDuration = sorted[len(sorted)-1]
	res.MedianDuration = sorted[len(sorted)/2]
	var sum time.Duration
	for _, s := range samples {
		sum += s
	}
	res.MeanDuration = time.Duration(int64(sum) / int64(len(samples)))
	return res
}

// String renders the result as a human-readable single-line summary.
// Provided for caller convenience when logging benchmark output;
// not used by the bridge itself.
func (r *BenchmarkResult) String() string {
	if r == nil {
		return "<nil BenchmarkResult>"
	}
	status := "OK"
	if !r.Successful {
		status = "FAIL:" + r.Error
	}
	return fmt.Sprintf(
		"%s iters=%d mean=%s min=%s med=%s max=%s alloc=%dB status=%s",
		r.Name, r.Iterations, r.MeanDuration, r.MinDuration, r.MedianDuration, r.MaxDuration, r.BytesAllocated, status,
	)
}
