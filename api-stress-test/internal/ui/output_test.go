package ui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api-stress-test/internal/stats"
)

func TestPrintHeader(t *testing.T) {
	var buf bytes.Buffer
	err := PrintHeader(&buf, HeaderConfig{
		URL:           "http://example.com",
		Method:        "GET",
		TotalRequests: 100,
		Concurrency:   10,
		TimeoutSec:    5,
		Rate:          50,
	})
	if err != nil {
		t.Fatalf("PrintHeader: %v", err)
	}
	for _, want := range []string{"http://example.com", "GET", "Total requests", "Concurrency", "Rate limit"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("header missing %q", want)
		}
	}
}

func TestPrintHeaderDurationMode(t *testing.T) {
	var buf bytes.Buffer
	err := PrintHeader(&buf, HeaderConfig{
		URL:            "http://example.com",
		Method:         "GET",
		Concurrency:    10,
		TimeoutSec:     5,
		IsDurationMode: true,
		Duration:       "30s",
	})
	if err != nil {
		t.Fatalf("PrintHeader: %v", err)
	}
	if !strings.Contains(buf.String(), "30s") || strings.Contains(buf.String(), "Total requests") {
		t.Errorf("unexpected duration header:\n%s", buf.String())
	}
}

func TestPrintTextResultUsesExplicitMetricLabels(t *testing.T) {
	stat := stats.Statistics{
		Successes:          90,
		Failures:           10,
		Completed:          100,
		Cancelled:          2,
		Total:              102,
		SuccessRate:        90,
		StatusCount:        map[int]int{200: 90, 500: 10},
		LatencySamples:     102,
		MinLatency:         0.01,
		MaxLatency:         0.5,
		AvgLatency:         0.1,
		P50Latency:         0.08,
		P90Latency:         0.2,
		P95Latency:         0.3,
		P99Latency:         0.45,
		TTFBSamples:        100,
		MinTTFB:            0.005,
		MaxTTFB:            0.1,
		AvgTTFB:            0.02,
		P50TTFB:            0.015,
		P90TTFB:            0.03,
		P95TTFB:            0.04,
		P99TTFB:            0.08,
		TotalResponseBytes: 1024 * 100,
		AvgResponseBytes:   1024,
		Histogram: []stats.HistogramBucket{
			{MinSec: 0.01, MaxSec: 0.1, Count: 100},
		},
		HistogramSampling: stats.SamplingMetadata{SampleCount: 100, Population: 102, IsSampled: true},
	}

	var buf bytes.Buffer
	if err := PrintTextResult(&buf, stat, 10, 10); err != nil {
		t.Fatalf("PrintTextResult: %v", err)
	}
	for _, want := range []string{
		"Stress test finished",
		"Completed",
		"Cancelled",
		"Total latency (seconds)",
		"TTFB (seconds)",
		"Decoded response body data",
		"Avg decoded response body",
		"Sampled 100 of 102 attempts",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("text output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestPrintJSONResultGoldenV2(t *testing.T) {
	output := JSONOutput{
		SchemaVersion: JSONSchemaVersion,
		Config: TestConfig{
			URL:         "http://example.com",
			Method:      "GET",
			Requests:    2,
			Concurrency: 1,
			Timeout:     5,
		},
		EffectiveConfig: EffectiveConfig{
			URL:           "http://example.com",
			Method:        "GET",
			Requests:      2,
			Concurrency:   1,
			Timeout:       5,
			Warmup:        "1s",
			ShutdownGrace: "5s",
			ExpectStatus:  200,
			ExpectBody:    true,
		},
		Warmup: WarmupSummary{
			DurationSeconds: 1,
			Successes:       1,
			Total:           1,
		},
		Statistics: stats.Statistics{
			Successes:      1,
			Failures:       0,
			Completed:      1,
			Cancelled:      1,
			Total:          2,
			SuccessRate:    100,
			StatusCount:    map[int]int{200: 1},
			LatencySamples: 2,
			MinLatency:     0.1,
			MaxLatency:     0.3,
			AvgLatency:     0.2,
			P50Latency:     0.2,
			P90Latency:     0.28,
			P95Latency:     0.29,
			P99Latency:     0.298,
			TTFBSamples:    1,
			MinTTFB:        0.02,
			MaxTTFB:        0.02,
			AvgTTFB:        0.02,
			P50TTFB:        0.02,
			P90TTFB:        0.02,
			P95TTFB:        0.02,
			P99TTFB:        0.02,
			Histogram: []stats.HistogramBucket{
				{MinSec: 0.1, MaxSec: 0.3, Count: 2},
			},
			HistogramSampling: stats.SamplingMetadata{SampleCount: 2, Population: 2},
			Throughput: []stats.ThroughputEntry{
				{Second: 1, Requests: 2},
			},
			AvgResponseBytes:   5,
			TotalResponseBytes: 10,
		},
		TotalTime:         0.5,
		DrainTime:         0.1,
		ReqPerSec:         4,
		TerminationReason: "duration_complete",
	}

	var buf bytes.Buffer
	if err := PrintJSONResult(&buf, output); err != nil {
		t.Fatalf("PrintJSONResult: %v", err)
	}
	want, err := os.ReadFile(filepath.Join("testdata", "output_v2.golden.json"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("JSON output differs from golden\ngot:\n%s\nwant:\n%s", buf.Bytes(), want)
	}
}

func TestPrintJSONResultDefaultsSchemaVersion(t *testing.T) {
	var buf bytes.Buffer
	if err := PrintJSONResult(&buf, JSONOutput{}); err != nil {
		t.Fatalf("PrintJSONResult: %v", err)
	}
	if !strings.Contains(buf.String(), `"schema_version": 2`) {
		t.Errorf("missing schema v2 marker: %s", buf.String())
	}
}

var errWriterFailed = errors.New("writer failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriterFailed }

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) { return len(p) / 2, nil }

func TestRenderersPropagateWriterErrors(t *testing.T) {
	stat := stats.Statistics{
		Histogram:  []stats.HistogramBucket{{MinSec: 0, MaxSec: 1, Count: 1}},
		Throughput: []stats.ThroughputEntry{{Second: 1, Requests: 1}, {Second: 2, Requests: 1}, {Second: 3, Requests: 1}},
	}
	tests := []struct {
		name string
		call func() error
	}{
		{"header", func() error { return PrintHeader(failingWriter{}, HeaderConfig{}) }},
		{"text", func() error { return PrintTextResult(failingWriter{}, stat, 1, 1) }},
		{"JSON", func() error { return PrintJSONResult(failingWriter{}, JSONOutput{}) }},
		{"latency series", func() error {
			return printLatencySeries(failingWriter{}, newColorWriter(io.Discard), "latency", 1, 0, 0, 0, 0, 0, 0, 0)
		}},
		{"histogram", func() error {
			return printHistogram(&colorWriter{w: failingWriter{}}, stat.Histogram)
		}},
		{"throughput", func() error {
			return printThroughputTimeline(&colorWriter{w: failingWriter{}}, stat.Throughput)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); !errors.Is(err, errWriterFailed) {
				t.Fatalf("error = %v, want %v", err, errWriterFailed)
			}
		})
	}
}

func TestPrintJSONResultRejectsShortWrite(t *testing.T) {
	if err := PrintJSONResult(shortWriter{}, JSONOutput{}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("error = %v, want %v", err, io.ErrShortWrite)
	}
}

func TestColorWriterEnvironment(t *testing.T) {
	t.Run("plain buffer", func(t *testing.T) {
		cw := newColorWriter(&bytes.Buffer{})
		if cw.enabled || cw.colorize(colorRed, "test") != "test" {
			t.Error("buffer should not enable color")
		}
	})
	t.Run("force color", func(t *testing.T) {
		t.Setenv("FORCE_COLOR", "1")
		cw := newColorWriter(&bytes.Buffer{})
		if !cw.enabled || !strings.Contains(cw.colorize(colorRed, "test"), "\033[31m") {
			t.Error("FORCE_COLOR should enable color")
		}
	})
	t.Run("no color wins", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("FORCE_COLOR", "1")
		if newColorWriter(&bytes.Buffer{}).enabled {
			t.Error("NO_COLOR should take precedence")
		}
	})
}

func TestRenderBar(t *testing.T) {
	for _, tt := range []struct {
		percent float64
		want    string
	}{
		{0, "[                    ]"},
		{50, "[==========          ]"},
		{100, "[====================]"},
	} {
		if got := renderBar(tt.percent, 20); got != tt.want {
			t.Errorf("renderBar(%f) = %q, want %q", tt.percent, got, tt.want)
		}
	}
}
