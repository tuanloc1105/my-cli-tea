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

const JSONSchemaVersion = 2

const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorCyan   = "\033[36m"
)

type colorWriter struct {
	w       io.Writer
	enabled bool
}

type terminalOutput interface {
	TerminalOutput() bool
}

func newColorWriter(w io.Writer) *colorWriter {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return &colorWriter{w: w}
	}
	if _, ok := os.LookupEnv("FORCE_COLOR"); ok {
		return &colorWriter{w: w, enabled: true}
	}
	if terminal, ok := w.(terminalOutput); ok {
		return &colorWriter{w: w, enabled: terminal.TerminalOutput()}
	}
	f, ok := w.(*os.File)
	if !ok {
		return &colorWriter{w: w}
	}
	stat, err := f.Stat()
	if err != nil {
		return &colorWriter{w: w}
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
	default:
		return colorRed
	}
}

type trackingWriter struct {
	w   io.Writer
	err error
}

func (w *trackingWriter) Write(p []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.w.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.err = err
	}
	return n, err
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

// TestConfig is retained for compatibility with schema v1 consumers.
type TestConfig struct {
	URL         string  `json:"url"`
	Method      string  `json:"method"`
	Requests    int     `json:"requests,omitempty"`
	Duration    string  `json:"duration,omitempty"`
	Concurrency int     `json:"concurrency"`
	Timeout     float64 `json:"timeout_seconds"`
	Rate        float64 `json:"rate,omitempty"`
}

// EffectiveConfig records normalized runtime settings without request secrets.
type EffectiveConfig struct {
	URL              string  `json:"url"`
	Method           string  `json:"method"`
	Requests         int     `json:"requests,omitempty"`
	Duration         string  `json:"duration,omitempty"`
	Concurrency      int     `json:"concurrency"`
	Timeout          float64 `json:"timeout_seconds"`
	Rate             float64 `json:"rate,omitempty"`
	Warmup           string  `json:"warmup,omitempty"`
	ShutdownGrace    string  `json:"shutdown_grace"`
	Insecure         bool    `json:"insecure"`
	DisableKeepalive bool    `json:"disable_keepalive"`
	DisableRedirects bool    `json:"disable_redirects"`
	ExpectStatus     int     `json:"expect_status,omitempty"`
	ExpectBody       bool    `json:"expect_body_enabled"`
}

// WarmupSummary describes the unmeasured warmup phase.
type WarmupSummary struct {
	DurationSeconds float64 `json:"duration_seconds"`
	Successes       int64   `json:"successes"`
	Failures        int64   `json:"failures"`
	Cancelled       int64   `json:"cancelled"`
	Total           int64   `json:"total"`
}

// JSONOutput wraps the full schema v2 result document.
type JSONOutput struct {
	SchemaVersion     int              `json:"schema_version"`
	Config            TestConfig       `json:"config"`
	EffectiveConfig   EffectiveConfig  `json:"effective_config"`
	Warmup            WarmupSummary    `json:"warmup"`
	Statistics        stats.Statistics `json:"statistics"`
	TotalTime         float64          `json:"total_time_seconds"`
	DrainTime         float64          `json:"drain_time_seconds"`
	ReqPerSec         float64          `json:"requests_per_second"`
	TerminationReason string           `json:"termination_reason"`
}

// PrintHeader prints the test configuration before the test starts.
func PrintHeader(w io.Writer, cfg HeaderConfig) error {
	tracked := &trackingWriter{w: w}
	cw := newColorWriter(w)
	cw.w = tracked
	fmt.Fprintf(tracked, "%s : %s\n", cw.colorize(colorBold, "Target URL           "), cfg.URL)
	fmt.Fprintf(tracked, "%s : %s\n", cw.colorize(colorBold, "HTTP method          "), cfg.Method)
	if cfg.IsDurationMode {
		fmt.Fprintf(tracked, "%s : %s\n", cw.colorize(colorBold, "Duration             "), cfg.Duration)
	} else {
		fmt.Fprintf(tracked, "%s : %d\n", cw.colorize(colorBold, "Total requests       "), cfg.TotalRequests)
	}
	fmt.Fprintf(tracked, "%s : %d\n", cw.colorize(colorBold, "Concurrency (workers)"), cfg.Concurrency)
	fmt.Fprintf(tracked, "%s : %.1f seconds\n", cw.colorize(colorBold, "Timeout per request  "), cfg.TimeoutSec)
	if cfg.Rate > 0 {
		fmt.Fprintf(tracked, "%s : %.0f req/s\n", cw.colorize(colorBold, "Rate limit           "), cfg.Rate)
	}
	if cfg.BodyLen > 0 {
		fmt.Fprintf(tracked, "%s : %d bytes\n", cw.colorize(colorBold, "Body size            "), cfg.BodyLen)
		if cfg.ContentType != "" {
			fmt.Fprintf(tracked, "%s : %s\n", cw.colorize(colorBold, "Content-Type         "), cfg.ContentType)
		}
	}
	fmt.Fprintln(tracked, strings.Repeat("-", 60))
	return tracked.err
}

// PrintTextResult prints the test results in human-readable text format.
func PrintTextResult(w io.Writer, stat stats.Statistics, totalTime, reqPerSec float64) error {
	tracked := &trackingWriter{w: w}
	cw := newColorWriter(w)
	cw.w = tracked

	fmt.Fprintln(tracked)
	fmt.Fprintln(tracked, cw.colorize(colorBold, strings.Repeat("=", 60)))
	fmt.Fprintln(tracked, cw.colorize(colorBold, "Stress test finished"))
	fmt.Fprintln(tracked, cw.colorize(colorBold, strings.Repeat("=", 60)))
	fmt.Fprintf(tracked, "Total time            : %.4f seconds\n", totalTime)
	fmt.Fprintf(tracked, "Requests per second   : %.2f req/s\n", reqPerSec)
	fmt.Fprintf(tracked, "Completed             : %d\n", stat.Completed)
	fmt.Fprintf(tracked, "Successes             : %s\n", cw.colorize(colorGreen, fmt.Sprintf("%d", stat.Successes)))
	if stat.Failures > 0 {
		fmt.Fprintf(tracked, "Failures              : %s\n", cw.colorize(colorRed, fmt.Sprintf("%d", stat.Failures)))
	} else {
		fmt.Fprintf(tracked, "Failures              : %d\n", stat.Failures)
	}
	fmt.Fprintf(tracked, "Cancelled             : %d\n", stat.Cancelled)
	fmt.Fprintf(tracked, "Success rate          : %.1f%%\n", stat.SuccessRate)

	if stat.TotalResponseBytes > 0 {
		fmt.Fprintf(tracked, "Decoded response body data : %s\n", formatBytes(stat.TotalResponseBytes))
		fmt.Fprintf(tracked, "Avg decoded response body : %s\n", formatBytes(stat.AvgResponseBytes))
	}

	fmt.Fprintln(tracked, "Status codes          :")
	statusKeys := make([]int, 0, len(stat.StatusCount))
	for status := range stat.StatusCount {
		statusKeys = append(statusKeys, status)
	}
	sort.Ints(statusKeys)
	for _, status := range statusKeys {
		count := stat.StatusCount[status]
		label := "ERROR/NO STATUS"
		if status != 0 {
			label = fmt.Sprintf("%d", status)
		}
		color := cw.statusColor(status)
		if color != "" {
			fmt.Fprintf(tracked, "  %s%-15s%s %d\n", color, label, colorReset, count)
		} else {
			fmt.Fprintf(tracked, "  %-15s %d\n", label, count)
		}
	}

	if err := printLatencySeries(tracked, cw, "Total latency (seconds)", stat.LatencySamples,
		stat.MinLatency, stat.MaxLatency, stat.AvgLatency,
		stat.P50Latency, stat.P90Latency, stat.P95Latency, stat.P99Latency); err != nil {
		return err
	}
	if err := printLatencySeries(tracked, cw, "TTFB (seconds)", stat.TTFBSamples,
		stat.MinTTFB, stat.MaxTTFB, stat.AvgTTFB,
		stat.P50TTFB, stat.P90TTFB, stat.P95TTFB, stat.P99TTFB); err != nil {
		return err
	}

	if len(stat.Histogram) > 0 {
		fmt.Fprintln(tracked)
		fmt.Fprintln(tracked, cw.colorize(colorBold, "Total latency distribution"))
		if stat.HistogramSampling.IsSampled {
			fmt.Fprintf(tracked, "  Sampled %d of %d attempts\n",
				stat.HistogramSampling.SampleCount, stat.HistogramSampling.Population)
		}
		if err := printHistogram(cw, stat.Histogram); err != nil {
			return err
		}
	}
	if err := printThroughputTimeline(cw, stat.Throughput); err != nil {
		return err
	}

	if len(stat.TopErrors) > 0 {
		fmt.Fprintln(tracked)
		fmt.Fprintln(tracked, cw.colorize(colorBold, "Top Errors            :"))
		for _, entry := range stat.TopErrors {
			fmt.Fprintf(tracked, "  %s x %d\n", cw.colorize(colorRed, entry.Message), entry.Count)
		}
	}
	return tracked.err
}

func printLatencySeries(w io.Writer, cw *colorWriter, title string, samples int64, min, max, avg, p50, p90, p95, p99 float64) error {
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, cw.colorize(colorBold, title)); err != nil {
		return err
	}
	lines := []struct {
		label string
		value float64
	}{
		{"Min", min},
		{"Max", max},
		{"Average", avg},
		{"p50", p50},
		{"p90", p90},
		{"p95", p95},
		{"p99", p99},
	}
	if _, err := fmt.Fprintf(w, "  Samples             : %d\n", samples); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "  %-20s: %.4f\n", line.label, line.value); err != nil {
			return err
		}
	}
	return nil
}

func printHistogram(cw *colorWriter, buckets []stats.HistogramBucket) error {
	maxCount := 0
	total := 0
	for _, bucket := range buckets {
		if bucket.Count > maxCount {
			maxCount = bucket.Count
		}
		total += bucket.Count
	}
	if maxCount == 0 {
		return nil
	}

	const barWidth = 30
	for _, bucket := range buckets {
		bar := strings.Repeat("█", bucket.Count*barWidth/maxCount)
		percent := float64(bucket.Count) / float64(total) * 100
		if _, err := fmt.Fprintf(cw.w, "  [%.3f-%.3fs] %s %d (%.1f%%)\n",
			bucket.MinSec, bucket.MaxSec,
			cw.colorize(colorCyan, fmt.Sprintf("%-*s", barWidth, bar)),
			bucket.Count, percent); err != nil {
			return err
		}
	}
	return nil
}

func printThroughputTimeline(cw *colorWriter, throughput []stats.ThroughputEntry) error {
	if len(throughput) < 3 {
		return nil
	}

	maxRequests := 0
	for _, entry := range throughput {
		if entry.Requests > maxRequests {
			maxRequests = entry.Requests
		}
	}
	if maxRequests == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(cw.w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(cw.w, cw.colorize(colorBold, "Throughput timeline (req/s)")); err != nil {
		return err
	}
	const barWidth = 30
	for _, entry := range throughput {
		bar := strings.Repeat("█", entry.Requests*barWidth/maxRequests)
		if _, err := fmt.Fprintf(cw.w, "  [%3ds] %s %d\n",
			entry.Second,
			cw.colorize(colorCyan, fmt.Sprintf("%-*s", barWidth, bar)),
			entry.Requests); err != nil {
			return err
		}
	}
	return nil
}

func formatBytes(bytes int64) string {
	switch {
	case bytes >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		return fmt.Sprintf("%.2f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.2f KB", float64(bytes)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// PrintJSONResult prints the test results in JSON format.
func PrintJSONResult(w io.Writer, output JSONOutput) error {
	if output.SchemaVersion == 0 {
		output.SchemaVersion = JSONSchemaVersion
	}
	tracked := &trackingWriter{w: w}
	encoder := json.NewEncoder(tracked)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		return fmt.Errorf("failed to encode JSON output: %w", err)
	}
	if tracked.err != nil {
		return fmt.Errorf("failed to write JSON output: %w", tracked.err)
	}
	return nil
}
