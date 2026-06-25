package stats

import (
	"sync"
	"testing"
)

func TestCollectorRecord(t *testing.T) {
	c := NewCollector(10)

	c.Record(200, 0.1, true, "", 100)
	c.Record(200, 0.2, true, "", 200)
	c.Record(500, 0.3, false, "server error", 0)
	c.Record(0, 0.05, false, "connection refused", 0)

	stat := c.GetStatistics()

	if stat.Total != 4 {
		t.Errorf("total = %d, want 4", stat.Total)
	}
	if stat.Successes != 2 {
		t.Errorf("successes = %d, want 2", stat.Successes)
	}
	if stat.Failures != 2 {
		t.Errorf("failures = %d, want 2", stat.Failures)
	}
	if stat.StatusCount[200] != 2 {
		t.Errorf("status 200 count = %d, want 2", stat.StatusCount[200])
	}
	if stat.StatusCount[500] != 1 {
		t.Errorf("status 500 count = %d, want 1", stat.StatusCount[500])
	}
	if stat.StatusCount[0] != 1 {
		t.Errorf("status 0 count = %d, want 1", stat.StatusCount[0])
	}
}

func TestCollectorMinMaxLatency(t *testing.T) {
	c := NewCollector(5)

	c.Record(200, 0.5, true, "", 0)
	c.Record(200, 0.1, true, "", 0)
	c.Record(200, 0.9, true, "", 0)

	stat := c.GetStatistics()

	if stat.MinLatency != 0.1 {
		t.Errorf("min latency = %f, want 0.1", stat.MinLatency)
	}
	if stat.MaxLatency != 0.9 {
		t.Errorf("max latency = %f, want 0.9", stat.MaxLatency)
	}
}

func TestCollectorAvgLatency(t *testing.T) {
	c := NewCollector(3)

	c.Record(200, 0.1, true, "", 0)
	c.Record(200, 0.2, true, "", 0)
	c.Record(200, 0.3, true, "", 0)

	stat := c.GetStatistics()

	expected := 0.2
	if diff := stat.AvgLatency - expected; diff > 0.0001 || diff < -0.0001 {
		t.Errorf("avg latency = %f, want %f", stat.AvgLatency, expected)
	}
}

func TestCollectorErrorTracking(t *testing.T) {
	c := NewCollector(10)

	for i := 0; i < 5; i++ {
		c.Record(0, 0.1, false, "connection refused", 0)
	}
	for i := 0; i < 3; i++ {
		c.Record(0, 0.1, false, "timeout", 0)
	}
	c.Record(0, 0.1, false, "dns error", 0)

	stat := c.GetStatistics()

	if len(stat.TopErrors) != 3 {
		t.Fatalf("got %d top errors, want 3", len(stat.TopErrors))
	}
	if stat.TopErrors[0].Message != "connection refused" || stat.TopErrors[0].Count != 5 {
		t.Errorf("top error = %v, want {connection refused, 5}", stat.TopErrors[0])
	}
	if stat.TopErrors[1].Message != "timeout" || stat.TopErrors[1].Count != 3 {
		t.Errorf("second error = %v, want {timeout, 3}", stat.TopErrors[1])
	}
}

func TestCollectorTopErrorsMaxFive(t *testing.T) {
	c := NewCollector(20)

	errors := []string{"err1", "err2", "err3", "err4", "err5", "err6", "err7"}
	for _, e := range errors {
		c.Record(0, 0.1, false, e, 0)
	}

	stat := c.GetStatistics()
	if len(stat.TopErrors) != 5 {
		t.Errorf("got %d top errors, want max 5", len(stat.TopErrors))
	}
}

func TestCollectorConcurrency(t *testing.T) {
	c := NewCollector(1000)
	var wg sync.WaitGroup

	numGoroutines := 10
	recordsPerGoroutine := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < recordsPerGoroutine; j++ {
				c.Record(200, 0.1, true, "", 0)
			}
		}()
	}

	wg.Wait()
	stat := c.GetStatistics()

	expected := int64(numGoroutines * recordsPerGoroutine)
	if stat.Total != expected {
		t.Errorf("total = %d, want %d", stat.Total, expected)
	}
}

func TestCollectorNoRecords(t *testing.T) {
	c := NewCollector(10)
	stat := c.GetStatistics()

	if stat.Total != 0 {
		t.Errorf("total = %d, want 0", stat.Total)
	}
	if stat.Successes != 0 || stat.Failures != 0 {
		t.Error("expected zero successes and failures")
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name     string
		data     []float64
		p        float64
		expected float64
	}{
		{"empty", []float64{}, 0.5, 0},
		{"single", []float64{1.0}, 0.5, 1.0},
		{"p0", []float64{1, 2, 3, 4, 5}, 0.0, 1.0},
		{"p100", []float64{1, 2, 3, 4, 5}, 1.0, 5.0},
		{"p50 odd", []float64{1, 2, 3, 4, 5}, 0.5, 3.0},
		{"p50 even", []float64{1, 2, 3, 4}, 0.5, 2.5},
		{"p90", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0.9, 9.1},
		{"p99", []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}, 0.99, 9.91},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := percentile(tt.data, tt.p)
			if diff := result - tt.expected; diff > 0.001 || diff < -0.001 {
				t.Errorf("percentile(%v, %f) = %f, want %f", tt.data, tt.p, result, tt.expected)
			}
		})
	}
}

func TestCollectorP95(t *testing.T) {
	c := NewCollector(100)
	for i := 1; i <= 100; i++ {
		c.Record(200, float64(i)*0.01, true, "", 0)
	}

	stat := c.GetStatistics()
	if stat.P95Latency < 0.94 || stat.P95Latency > 0.96 {
		t.Errorf("p95 = %f, want ~0.95", stat.P95Latency)
	}
}

func TestCollectorSuccessRate(t *testing.T) {
	c := NewCollector(10)
	for i := 0; i < 7; i++ {
		c.Record(200, 0.1, true, "", 0)
	}
	for i := 0; i < 3; i++ {
		c.Record(500, 0.1, false, "error", 0)
	}

	stat := c.GetStatistics()
	if stat.SuccessRate != 70.0 {
		t.Errorf("success rate = %f, want 70.0", stat.SuccessRate)
	}
}

func TestCollectorResponseSize(t *testing.T) {
	c := NewCollector(10)
	c.Record(200, 0.1, true, "", 1000)
	c.Record(200, 0.1, true, "", 2000)
	c.Record(200, 0.1, true, "", 3000)

	stat := c.GetStatistics()
	if stat.TotalResponseBytes != 6000 {
		t.Errorf("total bytes = %d, want 6000", stat.TotalResponseBytes)
	}
	if stat.AvgResponseBytes != 2000 {
		t.Errorf("avg bytes = %d, want 2000", stat.AvgResponseBytes)
	}
}

func TestCollectorReservoirSampling(t *testing.T) {
	c := NewCollector(100)
	for i := 0; i < 15000; i++ {
		c.Record(200, float64(i)*0.0001, true, "", 0)
	}

	stat := c.GetStatistics()
	if stat.Total != 15000 {
		t.Errorf("total = %d, want 15000", stat.Total)
	}
	if len(stat.Histogram) == 0 {
		t.Error("expected histogram even with reservoir sampling")
	}
}

func TestCollectorHistogramSingleValue(t *testing.T) {
	c := NewCollector(10)
	for i := 0; i < 10; i++ {
		c.Record(200, 0.5, true, "", 0)
	}

	stat := c.GetStatistics()
	if len(stat.Histogram) != 1 {
		t.Errorf("expected 1 histogram bucket for identical values, got %d", len(stat.Histogram))
	}
	if stat.Histogram[0].Count != 10 {
		t.Errorf("bucket count = %d, want 10", stat.Histogram[0].Count)
	}
}

func TestCollectorThroughputTimeline(t *testing.T) {
	c := NewCollector(10)
	for i := 0; i < 5; i++ {
		c.Record(200, 0.1, true, "", 0)
	}

	stat := c.GetStatistics()
	if len(stat.Throughput) == 0 {
		t.Error("expected throughput data")
	}
	totalReqs := 0
	for _, entry := range stat.Throughput {
		totalReqs += entry.Requests
	}
	if totalReqs != 5 {
		t.Errorf("throughput total = %d, want 5", totalReqs)
	}
}

func BenchmarkCollectorRecord(b *testing.B) {
	c := NewCollector(b.N)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Record(200, 0.1, true, "", 100)
	}
}
