// Package stats provides thread-safe statistics collection and calculation
// for HTTP stress test results, including latency percentiles.
package stats

import (
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

// reservoirSize controls the maximum number of latency samples retained for
// percentile calculation. Reservoir sampling guarantees that every request has
// an equal probability of being represented, so percentile estimates remain
// statistically accurate. The trade-off is memory vs accuracy: 10,000 float64
// values ≈ 80 KB, which provides sub-1% relative error on p99 for typical
// workloads while keeping memory bounded regardless of total request count.
const reservoirSize = 10000

// Collector collects and calculates statistics for stress test results.
// It is thread-safe and designed to handle concurrent result recording.
// Uses reservoir sampling to bound memory for latency percentiles.
type Collector struct {
	mu                sync.Mutex
	successes         int64
	failures          int64
	totalCount        int64           // Total requests recorded
	reservoir         []float64       // Reservoir-sampled latencies (max reservoirSize)
	latencySum        float64         // Running sum for average calculation
	statusCount       map[int]int     // Distribution of HTTP status codes
	errorMessages     map[string]int  // Error message frequency
	minLatency        float64
	maxLatency        float64
	firstLatency      bool
	startTime         int64           // Unix timestamp when first record was added
	throughput        map[int]int     // Per-second request counts (second offset -> count)
	totalResponseSize int64           // Total response body bytes received
}

// NewCollector creates a new statistics collector.
func NewCollector(initialCapacity int) *Collector {
	cap := initialCapacity
	if cap > reservoirSize {
		cap = reservoirSize
	}
	return &Collector{
		reservoir:     make([]float64, 0, cap),
		statusCount:   make(map[int]int),
		errorMessages: make(map[string]int),
		throughput:    make(map[int]int),
		firstLatency:  true,
	}
}

// Record adds a request result to the collector in a thread-safe manner.
func (c *Collector) Record(statusCode int, elapsed float64, ok bool, errorMsg string, responseSize int64) {
	now := time.Now().Unix() // Computed before lock to reduce mutex contention
	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalCount++
	c.latencySum += elapsed
	c.totalResponseSize += responseSize
	c.statusCount[statusCode]++

	// Track throughput per second
	if c.startTime == 0 {
		c.startTime = now
	}
	sec := int(now - c.startTime)
	c.throughput[sec]++

	// Reservoir sampling: keep exactly reservoirSize samples with uniform probability.
	// When total requests exceed reservoirSize, each new sample replaces an existing
	// one with probability reservoirSize/totalCount, ensuring unbiased representation.
	// Histogram and percentile data are approximate when total > reservoirSize.
	if int(c.totalCount) <= reservoirSize {
		c.reservoir = append(c.reservoir, elapsed)
	} else {
		j := rand.IntN(int(c.totalCount))
		if j < reservoirSize {
			c.reservoir[j] = elapsed
		}
	}

	if errorMsg != "" {
		c.errorMessages[errorMsg]++
	}

	if c.firstLatency {
		c.minLatency = elapsed
		c.maxLatency = elapsed
		c.firstLatency = false
	} else {
		if elapsed < c.minLatency {
			c.minLatency = elapsed
		}
		if elapsed > c.maxLatency {
			c.maxLatency = elapsed
		}
	}

	if ok {
		c.successes++
	} else {
		c.failures++
	}
}

// ErrorEntry represents an error message and its occurrence count.
type ErrorEntry struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

// HistogramBucket represents a single bucket in a latency histogram.
type HistogramBucket struct {
	MinSec float64 `json:"min_sec"`
	MaxSec float64 `json:"max_sec"`
	Count  int     `json:"count"`
}

// ThroughputEntry records requests completed in a one-second interval.
type ThroughputEntry struct {
	Second   int `json:"second"`
	Requests int `json:"requests"`
}

// Statistics holds the calculated final statistics from a stress test run.
type Statistics struct {
	Successes          int64             `json:"successes"`
	Failures           int64             `json:"failures"`
	Total              int64             `json:"total"`
	SuccessRate        float64           `json:"success_rate"`
	StatusCount        map[int]int       `json:"status_count"`
	MinLatency         float64           `json:"min_latency"`
	MaxLatency         float64           `json:"max_latency"`
	AvgLatency         float64           `json:"avg_latency"`
	P50Latency         float64           `json:"p50_latency"`
	P90Latency         float64           `json:"p90_latency"`
	P95Latency         float64           `json:"p95_latency"`
	P99Latency         float64           `json:"p99_latency"`
	TopErrors          []ErrorEntry      `json:"top_errors,omitempty"`
	// Histogram buckets use reservoir-sampled data and are approximate
	// when total requests exceed 10,000.
	Histogram          []HistogramBucket `json:"histogram,omitempty"`
	Throughput         []ThroughputEntry `json:"throughput,omitempty"`
	AvgResponseBytes   int64             `json:"avg_response_bytes"`
	TotalResponseBytes int64             `json:"total_response_bytes"`
}

// GetStatistics calculates and returns final statistics from all collected results.
func (c *Collector) GetStatistics() Statistics {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.totalCount == 0 {
		return Statistics{
			StatusCount: make(map[int]int),
		}
	}

	// Sort reservoir for percentile calculation
	sorted := make([]float64, len(c.reservoir))
	copy(sorted, c.reservoir)
	sort.Float64s(sorted)

	// Average from running sum (exact, not sampled)
	avgLatency := c.latencySum / float64(c.totalCount)

	p50 := percentile(sorted, 0.50)
	p90 := percentile(sorted, 0.90)
	p95 := percentile(sorted, 0.95)
	p99 := percentile(sorted, 0.99)

	statusCountCopy := make(map[int]int)
	for k, v := range c.statusCount {
		statusCountCopy[k] = v
	}

	// Top errors
	var topErrors []ErrorEntry
	for msg, count := range c.errorMessages {
		topErrors = append(topErrors, ErrorEntry{Message: msg, Count: count})
	}
	sort.Slice(topErrors, func(i, j int) bool {
		return topErrors[i].Count > topErrors[j].Count
	})
	if len(topErrors) > 5 {
		topErrors = topErrors[:5]
	}

	// Build histogram from sorted reservoir
	histogram := buildHistogram(sorted, c.minLatency, c.maxLatency)

	// Build throughput timeline
	var throughput []ThroughputEntry
	if len(c.throughput) > 0 {
		maxSec := 0
		for s := range c.throughput {
			if s > maxSec {
				maxSec = s
			}
		}
		throughput = make([]ThroughputEntry, 0, maxSec+1)
		for s := 0; s <= maxSec; s++ {
			throughput = append(throughput, ThroughputEntry{Second: s + 1, Requests: c.throughput[s]})
		}
	}

	successRate := float64(c.successes) / float64(c.totalCount) * 100

	var avgResponseBytes int64
	if c.totalCount > 0 {
		avgResponseBytes = c.totalResponseSize / c.totalCount
	}

	return Statistics{
		Successes:          c.successes,
		Failures:           c.failures,
		Total:              c.totalCount,
		SuccessRate:        successRate,
		StatusCount:        statusCountCopy,
		MinLatency:         c.minLatency,
		MaxLatency:         c.maxLatency,
		AvgLatency:         avgLatency,
		P50Latency:         p50,
		P90Latency:         p90,
		P95Latency:         p95,
		P99Latency:         p99,
		TopErrors:          topErrors,
		Histogram:          histogram,
		Throughput:         throughput,
		AvgResponseBytes:   avgResponseBytes,
		TotalResponseBytes: c.totalResponseSize,
	}
}

// buildHistogram creates 10 equal-width buckets spanning [min, max].
func buildHistogram(sorted []float64, minVal, maxVal float64) []HistogramBucket {
	if len(sorted) == 0 {
		return nil
	}

	const numBuckets = 10
	span := maxVal - minVal
	if span <= 0 {
		return []HistogramBucket{{MinSec: minVal, MaxSec: maxVal, Count: len(sorted)}}
	}

	bucketWidth := span / numBuckets
	buckets := make([]HistogramBucket, numBuckets)
	for i := range buckets {
		buckets[i].MinSec = minVal + float64(i)*bucketWidth
		buckets[i].MaxSec = minVal + float64(i+1)*bucketWidth
	}

	for _, v := range sorted {
		idx := int((v - minVal) / bucketWidth)
		if idx >= numBuckets {
			idx = numBuckets - 1
		}
		if idx < 0 {
			idx = 0
		}
		buckets[idx].Count++
	}

	// Remove trailing empty buckets
	last := len(buckets) - 1
	for last > 0 && buckets[last].Count == 0 {
		last--
	}
	return buckets[:last+1]
}

// percentile calculates percentile using linear interpolation method.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	n := float64(len(sorted))
	position := (n - 1) * p
	lower := int(position)
	upper := lower + 1

	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}

	weight := position - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}
