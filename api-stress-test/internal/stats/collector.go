// Package stats provides thread-safe statistics collection and calculation
// for HTTP stress test results, including latency percentiles.
package stats

import (
	"math/rand/v2"
	"sort"
	"sync"
	"time"
)

const reservoirSize = 10000

// Outcome classifies a measured request result for final accounting.
type Outcome string

const (
	OutcomeSuccess   Outcome = "success"
	OutcomeFailure   Outcome = "failure"
	OutcomeCancelled Outcome = "cancelled"
)

// Sample is one measured request attempt.
type Sample struct {
	StatusCode    int
	TotalLatency  float64
	TTFB          float64
	CompletedAt   time.Time
	Outcome       Outcome
	Error         string
	ErrorKind     string
	ResponseBytes int64
}

type latencySeries struct {
	count     int64
	sum       float64
	min       float64
	max       float64
	reservoir []float64
}

func newLatencySeries(initialCapacity int) latencySeries {
	if initialCapacity < 0 {
		initialCapacity = 0
	}
	if initialCapacity > reservoirSize {
		initialCapacity = reservoirSize
	}
	return latencySeries{reservoir: make([]float64, 0, initialCapacity)}
}

func (s *latencySeries) record(value float64) {
	s.count++
	s.sum += value
	if s.count == 1 {
		s.min = value
		s.max = value
	} else {
		if value < s.min {
			s.min = value
		}
		if value > s.max {
			s.max = value
		}
	}

	if s.count <= reservoirSize {
		s.reservoir = append(s.reservoir, value)
		return
	}
	j := rand.Int64N(s.count)
	if j < reservoirSize {
		s.reservoir[j] = value
	}
}

type seriesStatistics struct {
	count  int64
	min    float64
	max    float64
	avg    float64
	p50    float64
	p90    float64
	p95    float64
	p99    float64
	sorted []float64
}

func (s *latencySeries) statistics() seriesStatistics {
	sorted := append([]float64(nil), s.reservoir...)
	sort.Float64s(sorted)
	result := seriesStatistics{
		count:  s.count,
		min:    s.min,
		max:    s.max,
		p50:    percentile(sorted, 0.50),
		p90:    percentile(sorted, 0.90),
		p95:    percentile(sorted, 0.95),
		p99:    percentile(sorted, 0.99),
		sorted: sorted,
	}
	if s.count > 0 {
		result.avg = s.sum / float64(s.count)
	}
	return result
}

// Collector collects and calculates statistics for stress test results.
type Collector struct {
	mu                sync.Mutex
	measuredStart     time.Time
	successes         int64
	failures          int64
	cancelled         int64
	totalLatency      latencySeries
	ttfb              latencySeries
	statusCount       map[int]int
	errorMessages     map[string]int
	throughput        map[int]int
	totalResponseSize int64
}

// NewCollector creates a collector whose throughput timeline begins at measuredStart.
func NewCollector(initialCapacity int, measuredStart time.Time) *Collector {
	if measuredStart.IsZero() {
		measuredStart = time.Now()
	}
	return &Collector{
		measuredStart: measuredStart,
		totalLatency:  newLatencySeries(initialCapacity),
		ttfb:          newLatencySeries(initialCapacity),
		statusCount:   make(map[int]int),
		errorMessages: make(map[string]int),
		throughput:    make(map[int]int),
	}
}

// Record adds a request sample to the collector in a thread-safe manner.
func (c *Collector) Record(sample Sample) {
	completedAt := sample.CompletedAt
	if completedAt.IsZero() {
		completedAt = time.Now()
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.totalLatency.record(sample.TotalLatency)
	if sample.StatusCode != 0 {
		c.ttfb.record(sample.TTFB)
	}
	c.totalResponseSize += sample.ResponseBytes

	second := int(completedAt.Sub(c.measuredStart) / time.Second)
	if second < 0 {
		second = 0
	}
	c.throughput[second]++

	switch sample.Outcome {
	case OutcomeSuccess:
		c.successes++
		c.statusCount[sample.StatusCode]++
	case OutcomeCancelled:
		c.cancelled++
		if sample.StatusCode != 0 {
			c.statusCount[sample.StatusCode]++
		}
	default:
		c.failures++
		c.statusCount[sample.StatusCode]++
		if sample.Error != "" {
			c.errorMessages[sample.Error]++
		}
	}
}

// ErrorEntry represents an error message and its occurrence count.
type ErrorEntry struct {
	Message string `json:"message"`
	Count   int    `json:"count"`
}

// HistogramBucket represents a single bucket in a total-latency histogram.
type HistogramBucket struct {
	MinSec float64 `json:"min_sec"`
	MaxSec float64 `json:"max_sec"`
	Count  int     `json:"count"`
}

// SamplingMetadata describes the bounded sample used by the histogram.
type SamplingMetadata struct {
	SampleCount int   `json:"sample_count"`
	Population  int64 `json:"population"`
	IsSampled   bool  `json:"is_sampled"`
}

// ThroughputEntry records attempts completed in a one-second interval.
type ThroughputEntry struct {
	Second   int `json:"second"`
	Requests int `json:"requests"`
}

// Statistics holds the calculated final statistics from a stress test run.
type Statistics struct {
	Successes          int64             `json:"successes"`
	Failures           int64             `json:"failures"`
	Completed          int64             `json:"completed"`
	Cancelled          int64             `json:"cancelled"`
	Total              int64             `json:"total"`
	SuccessRate        float64           `json:"success_rate"`
	StatusCount        map[int]int       `json:"status_count"`
	LatencySamples     int64             `json:"latency_samples"`
	MinLatency         float64           `json:"min_latency"`
	MaxLatency         float64           `json:"max_latency"`
	AvgLatency         float64           `json:"avg_latency"`
	P50Latency         float64           `json:"p50_latency"`
	P90Latency         float64           `json:"p90_latency"`
	P95Latency         float64           `json:"p95_latency"`
	P99Latency         float64           `json:"p99_latency"`
	TTFBSamples        int64             `json:"ttfb_samples"`
	MinTTFB            float64           `json:"min_ttfb"`
	MaxTTFB            float64           `json:"max_ttfb"`
	AvgTTFB            float64           `json:"avg_ttfb"`
	P50TTFB            float64           `json:"p50_ttfb"`
	P90TTFB            float64           `json:"p90_ttfb"`
	P95TTFB            float64           `json:"p95_ttfb"`
	P99TTFB            float64           `json:"p99_ttfb"`
	TopErrors          []ErrorEntry      `json:"top_errors,omitempty"`
	Histogram          []HistogramBucket `json:"histogram,omitempty"`
	HistogramSampling  SamplingMetadata  `json:"histogram_sampling"`
	Throughput         []ThroughputEntry `json:"throughput,omitempty"`
	AvgResponseBytes   int64             `json:"avg_response_bytes"`
	TotalResponseBytes int64             `json:"total_response_bytes"`
}

// GetStatistics calculates and returns final statistics from all collected results.
func (c *Collector) GetStatistics() Statistics {
	c.mu.Lock()
	defer c.mu.Unlock()

	totalLatency := c.totalLatency.statistics()
	ttfb := c.ttfb.statistics()
	total := c.successes + c.failures + c.cancelled
	completed := c.successes + c.failures

	statusCount := make(map[int]int, len(c.statusCount))
	for status, count := range c.statusCount {
		statusCount[status] = count
	}

	topErrors := make([]ErrorEntry, 0, len(c.errorMessages))
	for message, count := range c.errorMessages {
		topErrors = append(topErrors, ErrorEntry{Message: message, Count: count})
	}
	sort.Slice(topErrors, func(i, j int) bool {
		if topErrors[i].Count == topErrors[j].Count {
			return topErrors[i].Message < topErrors[j].Message
		}
		return topErrors[i].Count > topErrors[j].Count
	})
	if len(topErrors) > 5 {
		topErrors = topErrors[:5]
	}

	var throughput []ThroughputEntry
	if len(c.throughput) > 0 {
		maxSecond := 0
		for second := range c.throughput {
			if second > maxSecond {
				maxSecond = second
			}
		}
		throughput = make([]ThroughputEntry, 0, maxSecond+1)
		for second := 0; second <= maxSecond; second++ {
			throughput = append(throughput, ThroughputEntry{
				Second:   second + 1,
				Requests: c.throughput[second],
			})
		}
	}

	var successRate float64
	if completed > 0 {
		successRate = float64(c.successes) / float64(completed) * 100
	}
	var avgResponseBytes int64
	if total > 0 {
		avgResponseBytes = c.totalResponseSize / total
	}

	return Statistics{
		Successes:      c.successes,
		Failures:       c.failures,
		Completed:      completed,
		Cancelled:      c.cancelled,
		Total:          total,
		SuccessRate:    successRate,
		StatusCount:    statusCount,
		LatencySamples: totalLatency.count,
		MinLatency:     totalLatency.min,
		MaxLatency:     totalLatency.max,
		AvgLatency:     totalLatency.avg,
		P50Latency:     totalLatency.p50,
		P90Latency:     totalLatency.p90,
		P95Latency:     totalLatency.p95,
		P99Latency:     totalLatency.p99,
		TTFBSamples:    ttfb.count,
		MinTTFB:        ttfb.min,
		MaxTTFB:        ttfb.max,
		AvgTTFB:        ttfb.avg,
		P50TTFB:        ttfb.p50,
		P90TTFB:        ttfb.p90,
		P95TTFB:        ttfb.p95,
		P99TTFB:        ttfb.p99,
		TopErrors:      topErrors,
		Histogram:      buildHistogram(totalLatency.sorted, totalLatency.min, totalLatency.max),
		HistogramSampling: SamplingMetadata{
			SampleCount: len(totalLatency.sorted),
			Population:  totalLatency.count,
			IsSampled:   int64(len(totalLatency.sorted)) < totalLatency.count,
		},
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

	for _, value := range sorted {
		index := int((value - minVal) / bucketWidth)
		if index >= numBuckets {
			index = numBuckets - 1
		}
		if index < 0 {
			index = 0
		}
		buckets[index].Count++
	}

	return buckets
}

// percentile calculates percentile using linear interpolation.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	position := float64(len(sorted)-1) * p
	lower := int(position)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	weight := position - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}
