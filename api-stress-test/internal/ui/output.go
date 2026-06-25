// Package ui handles output formatting for the API stress test tool.
package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"api-stress-test/internal/stats"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

// colorWriter wraps an io.Writer with a cached color-enabled flag,
// avoiding repeated Stat() syscalls per color operation.
type colorWriter struct {
	w       io.Writer
	enabled bool
}

// newColorWriter creates a colorWriter, checking once whether the writer is a terminal.
// Respects NO_COLOR (https://no-color.org/) and FORCE_COLOR environment variables.
func newColorWriter(w io.Writer) *colorWriter {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return &colorWriter{w: w, enabled: false}
	}
	if _, ok := os.LookupEnv("FORCE_COLOR"); ok {
		return &colorWriter{w: w, enabled: true}
	}
	f, ok := w.(*os.File)
	if !ok {
		return &colorWriter{w: w, enabled: false}
	}
	stat, err := f.Stat()
	if err != nil {
		return &colorWriter{w: w, enabled: false}
	}
	return &colorWriter{w: w, enabled: (stat.Mode() & os.ModeCharDevice) != 0}
}

func (cw *colorWriter) colorize(color, text string) string {
	if !cw.enabled {
		return text
	}
	return color + text + colorReset
}

func (cw *colorWriter) statusColor(status int) string {
	if !cw.enabled {
		return ""
	}
	switch {
	case status >= 200 && status < 300:
		return colorGreen
	case status >= 400 && status < 500:
		return colorYellow
	case status >= 500:
		return colorRed
	default:
		return colorRed
	}
}

// HeaderConfig holds the parameters for printing the test configuration header.
type HeaderConfig struct {
	URL            string
	Method         string
	TotalRequests  int
	Concurrency    int
	TimeoutSec     float64
	Rate           float64
	IsDurationMode bool
	Duration       string
	BodyLen        int
	ContentType    string
}

// TestConfig holds the test configuration for JSON output.
type TestConfig struct {
	URL         string  `json:"url"`
	Method      string  `json:"method"`
	Requests    int     `json:"requests,omitempty"`
	Duration    string  `json:"duration,omitempty"`
	Concurrency int     `json:"concurrency"`
	Timeout     float64 `json:"timeout_seconds"`
	Rate        float64 `json:"rate,omitempty"`
}

// JSONOutput wraps the full result for JSON output format.
type JSONOutput struct {
	Config     TestConfig       `json:"config"`
	Statistics stats.Statistics  `json:"statistics"`
	TotalTime  float64          `json:"total_time_seconds"`
	ReqPerSec  float64          `json:"requests_per_second"`
}

// PrintHeader prints the test configuration before the test starts.
func PrintHeader(w io.Writer, cfg HeaderConfig) {
	cw := newColorWriter(w)
	fmt.Fprintf(w, "%s : %s\n", cw.colorize(colorBold, "Target URL           "), cfg.URL)
	fmt.Fprintf(w, "%s : %s\n", cw.colorize(colorBold, "HTTP method          "), cfg.Method)
	if cfg.IsDurationMode {
		fmt.Fprintf(w, "%s : %s\n", cw.colorize(colorBold, "Duration             "), cfg.Duration)
	} else {
		fmt.Fprintf(w, "%s : %d\n", cw.colorize(colorBold, "Total requests       "), cfg.TotalRequests)
	}
	fmt.Fprintf(w, "%s : %d\n", cw.colorize(colorBold, "Concurrency (workers)"), cfg.Concurrency)
	fmt.Fprintf(w, "%s : %.1f seconds\n", cw.colorize(colorBold, "Timeout per request  "), cfg.TimeoutSec)
	if cfg.Rate > 0 {
		fmt.Fprintf(w, "%s : %.0f req/s\n", cw.colorize(colorBold, "Rate limit           "), cfg.Rate)
	}
	if cfg.BodyLen > 0 {
		fmt.Fprintf(w, "%s : %d bytes\n", cw.colorize(colorBold, "Body size            "), cfg.BodyLen)
		if cfg.ContentType != "" {
			fmt.Fprintf(w, "%s : %s\n", cw.colorize(colorBold, "Content-Type         "), cfg.ContentType)
		}
	}
	fmt.Fprintln(w, strings.Repeat("-", 60))
}

// PrintTextResult prints the test results in human-readable text format with colors.
func PrintTextResult(w io.Writer, stat stats.Statistics, totalTime, reqPerSec float64) {
	cw := newColorWriter(w)

	fmt.Fprintln(w)
	fmt.Fprintln(w, cw.colorize(colorBold, strings.Repeat("=", 60)))
	fmt.Fprintln(w, cw.colorize(colorBold, "Stress test finished"))
	fmt.Fprintln(w, cw.colorize(colorBold, strings.Repeat("=", 60)))
	fmt.Fprintf(w, "Total time            : %.4f seconds\n", totalTime)
	fmt.Fprintf(w, "Requests per second   : %.2f req/s\n", reqPerSec)
	fmt.Fprintf(w, "Successes             : %s\n", cw.colorize(colorGreen, fmt.Sprintf("%d", stat.Successes)))
	if stat.Failures > 0 {
		fmt.Fprintf(w, "Failures              : %s\n", cw.colorize(colorRed, fmt.Sprintf("%d", stat.Failures)))
	} else {
		fmt.Fprintf(w, "Failures              : %d\n", stat.Failures)
	}
	fmt.Fprintf(w, "Success rate          : %.1f%%\n", stat.SuccessRate)

	if stat.TotalResponseBytes > 0 {
		fmt.Fprintf(w, "Data transferred      : %s\n", formatBytes(stat.TotalResponseBytes))
		fmt.Fprintf(w, "Avg response size     : %s\n", formatBytes(stat.AvgResponseBytes))
	}

	fmt.Fprintln(w, "Status codes          :")

	var statusKeys []int
	for k := range stat.StatusCount {
		statusKeys = append(statusKeys, k)
	}
	sort.Ints(statusKeys)

	for _, status := range statusKeys {
		count := stat.StatusCount[status]
		label := "ERROR/NO STATUS"
		if status != 0 {
			label = fmt.Sprintf("%d", status)
		}
		sc := cw.statusColor(status)
		if sc != "" {
			fmt.Fprintf(w, "  %s%-15s%s %d\n", sc, label, colorReset, count)
		} else {
			fmt.Fprintf(w, "  %-15s %d\n", label, count)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, cw.colorize(colorBold, "Latency (seconds)"))
	fmt.Fprintf(w, "  Min                 : %.4f\n", stat.MinLatency)
	fmt.Fprintf(w, "  Max                 : %.4f\n", stat.MaxLatency)
	fmt.Fprintf(w, "  Average             : %.4f\n", stat.AvgLatency)
	fmt.Fprintf(w, "  p50                 : %.4f\n", stat.P50Latency)
	fmt.Fprintf(w, "  p90                 : %.4f\n", stat.P90Latency)
	fmt.Fprintf(w, "  p95                 : %.4f\n", stat.P95Latency)
	fmt.Fprintf(w, "  p99                 : %.4f\n", stat.P99Latency)

	// Histogram
	if len(stat.Histogram) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, cw.colorize(colorBold, "Latency distribution"))
		printHistogram(cw, stat.Histogram)
	}

	// Throughput timeline for tests longer than 2 seconds
	printThroughputTimeline(cw, stat.Throughput)

	if len(stat.TopErrors) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, cw.colorize(colorBold, "Top Errors            :"))
		for _, e := range stat.TopErrors {
			fmt.Fprintf(w, "  %s x %d\n", cw.colorize(colorRed, e.Message), e.Count)
		}
	}
}

// printHistogram renders an ASCII histogram.
func printHistogram(cw *colorWriter, buckets []stats.HistogramBucket) {
	maxCount := 0
	for _, b := range buckets {
		if b.Count > maxCount {
			maxCount = b.Count
		}
	}
	if maxCount == 0 {
		return
	}

	const barWidth = 30
	total := 0
	for _, b := range buckets {
		total += b.Count
	}

	for _, b := range buckets {
		barLen := b.Count * barWidth / maxCount
		bar := strings.Repeat("█", barLen)
		pct := float64(b.Count) / float64(total) * 100
		fmt.Fprintf(cw.w, "  [%.3f-%.3fs] %s %d (%.1f%%)\n",
			b.MinSec, b.MaxSec,
			cw.colorize(colorCyan, fmt.Sprintf("%-*s", barWidth, bar)),
			b.Count, pct)
	}
}

// printThroughputTimeline renders a per-second throughput bar chart for tests > 2 seconds.
func printThroughputTimeline(cw *colorWriter, throughput []stats.ThroughputEntry) {
	if len(throughput) < 3 {
		return
	}

	fmt.Fprintln(cw.w)
	fmt.Fprintln(cw.w, cw.colorize(colorBold, "Throughput timeline (req/s)"))

	maxReqs := 0
	for _, t := range throughput {
		if t.Requests > maxReqs {
			maxReqs = t.Requests
		}
	}
	if maxReqs == 0 {
		return
	}

	const barWidth = 30
	for _, t := range throughput {
		barLen := t.Requests * barWidth / maxReqs
		bar := strings.Repeat("█", barLen)
		fmt.Fprintf(cw.w, "  [%3ds] %s %d\n",
			t.Second,
			cw.colorize(colorCyan, fmt.Sprintf("%-*s", barWidth, bar)),
			t.Requests)
	}
}

// formatBytes formats a byte count into a human-readable string.
func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// PrintJSONResult prints the test results in JSON format.
func PrintJSONResult(w io.Writer, output JSONOutput) error {
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON output: %w", err)
	}
	fmt.Fprintln(w, string(data))
	return nil
}
