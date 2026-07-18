package stats

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func recordAt(c *Collector, start time.Time, offset time.Duration, total, ttfb float64, outcome Outcome, status int, err string, bytes int64) {
	c.Record(Sample{
		StatusCode:    status,
		TotalLatency:  total,
		TTFB:          ttfb,
		CompletedAt:   start.Add(offset),
		Outcome:       outcome,
		Error:         err,
		ResponseBytes: bytes,
	})
}

func TestCollectorAccountingAndIndependentLatencySeries(t *testing.T) {
	start := time.Unix(100, 0)
	c := NewCollector(10, start)
	recordAt(c, start, 100*time.Millisecond, 0.30, 0.05, OutcomeSuccess, 200, "", 100)
	recordAt(c, start, 200*time.Millisecond, 0.60, 0.10, OutcomeFailure, 500, "server error", 200)
	recordAt(c, start, 300*time.Millisecond, 0.15, 0, OutcomeCancelled, 0, "context canceled", 50)

	stat := c.GetStatistics()
	if stat.Total != 3 || stat.Completed != 2 || stat.Cancelled != 1 {
		t.Fatalf("accounting = total %d, completed %d, cancelled %d", stat.Total, stat.Completed, stat.Cancelled)
	}
	if stat.Successes != 1 || stat.Failures != 1 || stat.SuccessRate != 50 {
		t.Errorf("outcomes = successes %d, failures %d, rate %.1f", stat.Successes, stat.Failures, stat.SuccessRate)
	}
	if stat.Total != stat.Successes+stat.Failures+stat.Cancelled {
		t.Error("total accounting invariant violated")
	}
	if stat.Completed != stat.Successes+stat.Failures {
		t.Error("completed accounting invariant violated")
	}
	if stat.LatencySamples != 3 || stat.TTFBSamples != 2 {
		t.Errorf("sample counts = latency %d, TTFB %d", stat.LatencySamples, stat.TTFBSamples)
	}
	if !closeEnough(stat.AvgLatency, 0.35) || !closeEnough(stat.AvgTTFB, 0.075) {
		t.Errorf("averages = latency %f, TTFB %f", stat.AvgLatency, stat.AvgTTFB)
	}
	if stat.StatusCount[0] != 0 || len(stat.TopErrors) != 1 || stat.TopErrors[0].Message != "server error" {
		t.Errorf("planned cancellation leaked into failure groups: statuses=%v errors=%v", stat.StatusCount, stat.TopErrors)
	}
	if stat.TotalResponseBytes != 350 || stat.AvgResponseBytes != 116 {
		t.Errorf("decoded bytes = total %d, avg %d", stat.TotalResponseBytes, stat.AvgResponseBytes)
	}
}

func closeEnough(got, want float64) bool {
	difference := got - want
	return difference > -1e-12 && difference < 1e-12
}

func TestCollectorSuccessRateExcludesCancelled(t *testing.T) {
	start := time.Unix(200, 0)
	c := NewCollector(3, start)
	recordAt(c, start, 0, 0.1, 0, OutcomeSuccess, 200, "", 0)
	recordAt(c, start, 0, 0.1, 0, OutcomeCancelled, 0, "cancelled", 0)

	stat := c.GetStatistics()
	if stat.SuccessRate != 100 {
		t.Fatalf("success rate = %f, want 100", stat.SuccessRate)
	}
}

func TestCollectorTTFBSamplePresenceUsesResponseStatus(t *testing.T) {
	start := time.Unix(250, 0)
	c := NewCollector(2, start)
	recordAt(c, start, 0, 0.1, 0, OutcomeSuccess, 204, "", 0)
	recordAt(c, start, 0, 0.1, 0, OutcomeCancelled, 200, "cancelled during body", 0)

	stat := c.GetStatistics()
	if stat.TTFBSamples != 2 {
		t.Fatalf("TTFB samples = %d, want 2 responses with headers", stat.TTFBSamples)
	}
	if stat.StatusCount[204] != 1 || stat.StatusCount[200] != 1 || len(stat.TopErrors) != 0 {
		t.Errorf("status/error grouping = statuses %v, errors %v", stat.StatusCount, stat.TopErrors)
	}
}

func TestCollectorLatencyBoundsAndPercentiles(t *testing.T) {
	start := time.Unix(300, 0)
	c := NewCollector(100, start)
	for i := 1; i <= 100; i++ {
		value := float64(i) * 0.01
		recordAt(c, start, time.Duration(i)*time.Millisecond, value, value/2, OutcomeSuccess, 200, "", 0)
	}

	stat := c.GetStatistics()
	if stat.MinLatency != 0.01 || stat.MaxLatency != 1 {
		t.Errorf("total latency bounds = [%f, %f]", stat.MinLatency, stat.MaxLatency)
	}
	if stat.MinTTFB != 0.005 || stat.MaxTTFB != 0.5 {
		t.Errorf("TTFB bounds = [%f, %f]", stat.MinTTFB, stat.MaxTTFB)
	}
	if stat.P95Latency < 0.94 || stat.P95Latency > 0.96 {
		t.Errorf("p95 total latency = %f, want ~0.95", stat.P95Latency)
	}
	if stat.P95TTFB < 0.47 || stat.P95TTFB > 0.48 {
		t.Errorf("p95 TTFB = %f, want ~0.475", stat.P95TTFB)
	}
}

func TestCollectorReservoirMetadataIsIndependent(t *testing.T) {
	start := time.Unix(400, 0)
	c := NewCollector(100, start)
	const attempts = reservoirSize + 2000
	for i := 0; i < attempts; i++ {
		ttfb := 0.0
		status := 0
		outcome := OutcomeFailure
		if i%2 == 0 {
			ttfb = 0.01
			status = 200
			outcome = OutcomeSuccess
		}
		recordAt(c, start, 0, float64(i)*0.0001, ttfb, outcome, status, "", 0)
	}

	stat := c.GetStatistics()
	if stat.LatencySamples != attempts || stat.TTFBSamples != attempts/2 {
		t.Fatalf("exact sample counts = latency %d, TTFB %d", stat.LatencySamples, stat.TTFBSamples)
	}
	meta := stat.HistogramSampling
	if meta.SampleCount != reservoirSize || meta.Population != attempts || !meta.IsSampled {
		t.Errorf("histogram sampling metadata = %+v", meta)
	}
	count := 0
	for _, bucket := range stat.Histogram {
		count += bucket.Count
	}
	if count != reservoirSize {
		t.Errorf("histogram sample total = %d, want %d", count, reservoirSize)
	}
}

func TestCollectorThroughputStartsAtMeasuredOrigin(t *testing.T) {
	start := time.Unix(500, 0)
	c := NewCollector(3, start)
	recordAt(c, start, 2100*time.Millisecond, 0.1, 0.02, OutcomeSuccess, 200, "", 0)
	recordAt(c, start, 4200*time.Millisecond, 0.1, 0.02, OutcomeSuccess, 200, "", 0)

	stat := c.GetStatistics()
	want := []ThroughputEntry{
		{Second: 1, Requests: 0},
		{Second: 2, Requests: 0},
		{Second: 3, Requests: 1},
		{Second: 4, Requests: 0},
		{Second: 5, Requests: 1},
	}
	if len(stat.Throughput) != len(want) {
		t.Fatalf("throughput = %v, want %v", stat.Throughput, want)
	}
	for i := range want {
		if stat.Throughput[i] != want[i] {
			t.Errorf("throughput[%d] = %+v, want %+v", i, stat.Throughput[i], want[i])
		}
	}
}

func TestCollectorTopErrorsDeterministicOnTies(t *testing.T) {
	start := time.Unix(600, 0)
	c := NewCollector(10, start)
	for _, message := range []string{"zeta", "alpha", "middle"} {
		recordAt(c, start, 0, 0.1, 0, OutcomeFailure, 0, message, 0)
	}

	stat := c.GetStatistics()
	for i, want := range []string{"alpha", "middle", "zeta"} {
		if stat.TopErrors[i].Message != want {
			t.Fatalf("top errors = %v, want alphabetical tie order", stat.TopErrors)
		}
	}
}

func TestCollectorTopErrorsMaxFive(t *testing.T) {
	start := time.Unix(700, 0)
	c := NewCollector(10, start)
	for i := 0; i < 7; i++ {
		recordAt(c, start, 0, 0.1, 0, OutcomeFailure, 0, fmt.Sprintf("error-%d", i), 0)
	}
	if got := len(c.GetStatistics().TopErrors); got != 5 {
		t.Fatalf("top errors = %d, want 5", got)
	}
}

func TestCollectorConcurrency(t *testing.T) {
	start := time.Now()
	c := NewCollector(1000, start)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				recordAt(c, start, 0, 0.1, 0.02, OutcomeSuccess, 200, "", 0)
			}
		}()
	}
	wg.Wait()
	if got := c.GetStatistics().Total; got != 1000 {
		t.Fatalf("total = %d, want 1000", got)
	}
}

func TestCollectorNoRecords(t *testing.T) {
	stat := NewCollector(10, time.Now()).GetStatistics()
	if stat.Total != 0 || stat.Completed != 0 || stat.Cancelled != 0 {
		t.Errorf("unexpected accounting: %+v", stat)
	}
	if stat.StatusCount == nil {
		t.Error("status_count must encode as an empty object")
	}
}

func TestHistogramSingleValue(t *testing.T) {
	start := time.Unix(800, 0)
	c := NewCollector(10, start)
	for i := 0; i < 10; i++ {
		recordAt(c, start, 0, 0.5, 0, OutcomeSuccess, 200, "", 0)
	}
	stat := c.GetStatistics()
	if len(stat.Histogram) != 1 || stat.Histogram[0].Count != 10 {
		t.Fatalf("histogram = %+v", stat.Histogram)
	}
	if stat.HistogramSampling.IsSampled {
		t.Error("small complete histogram must not be marked sampled")
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		data     []float64
		p        float64
		expected float64
	}{
		{nil, 0.5, 0},
		{[]float64{1}, 0.5, 1},
		{[]float64{1, 2, 3, 4, 5}, 0, 1},
		{[]float64{1, 2, 3, 4, 5}, 1, 5},
		{[]float64{1, 2, 3, 4, 5}, 0.5, 3},
		{[]float64{1, 2, 3, 4}, 0.5, 2.5},
		{[]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0.9, 9.1},
	}
	for _, tt := range tests {
		if got := percentile(tt.data, tt.p); got != tt.expected {
			t.Errorf("percentile(%v, %f) = %f, want %f", tt.data, tt.p, got, tt.expected)
		}
	}
}

func BenchmarkCollectorRecord(b *testing.B) {
	start := time.Now()
	c := NewCollector(b.N, start)
	sample := Sample{
		StatusCode:    200,
		TotalLatency:  0.1,
		TTFB:          0.02,
		CompletedAt:   start,
		Outcome:       OutcomeSuccess,
		ResponseBytes: 100,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Record(sample)
	}
}

func TestCollectorRecordAllocations(t *testing.T) {
	start := time.Now()
	collector := NewCollector(1000, start)
	sample := Sample{
		StatusCode: 200, TotalLatency: 0.1, TTFB: 0.02, CompletedAt: start,
		Outcome: OutcomeSuccess, ResponseBytes: 100,
	}
	allocations := testing.AllocsPerRun(1000, func() { collector.Record(sample) })
	if allocations != 0 {
		t.Fatalf("Collector.Record allocations = %.2f, want 0", allocations)
	}
}
