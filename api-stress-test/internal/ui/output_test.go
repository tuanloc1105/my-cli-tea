package ui

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"api-stress-test/internal/stats"
)

func TestPrintHeader(t *testing.T) {
	var buf bytes.Buffer
	cfg := HeaderConfig{
		URL:           "http://example.com",
		Method:        "GET",
		TotalRequests: 100,
		Concurrency:   10,
		TimeoutSec:    5.0,
	}
	PrintHeader(&buf, cfg)
	out := buf.String()

	if !strings.Contains(out, "http://example.com") {
		t.Error("expected URL in header")
	}
	if !strings.Contains(out, "GET") {
		t.Error("expected method in header")
	}
	if !strings.Contains(out, "10") {
		t.Error("expected concurrency in header")
	}
}

func TestPrintHeader_RateShown(t *testing.T) {
	var buf bytes.Buffer
	cfg := HeaderConfig{
		URL:           "http://example.com",
		Method:        "GET",
		TotalRequests: 100,
		Concurrency:   10,
		TimeoutSec:    5.0,
		Rate:          50,
	}
	PrintHeader(&buf, cfg)
	out := buf.String()

	if !strings.Contains(out, "50") {
		t.Error("expected rate in header")
	}
	if !strings.Contains(out, "Rate limit") {
		t.Error("expected 'Rate limit' label in header")
	}
}

func TestPrintHeader_DurationMode(t *testing.T) {
	var buf bytes.Buffer
	cfg := HeaderConfig{
		URL:            "http://example.com",
		Method:         "GET",
		Concurrency:    10,
		TimeoutSec:     5.0,
		IsDurationMode: true,
		Duration:       "30s",
	}
	PrintHeader(&buf, cfg)
	out := buf.String()

	if !strings.Contains(out, "30s") {
		t.Error("expected duration in header")
	}
	if strings.Contains(out, "Total requests") {
		t.Error("should not show total requests in duration mode")
	}
}

func TestPrintTextResult(t *testing.T) {
	stat := stats.Statistics{
		Successes:          90,
		Failures:           10,
		Total:              100,
		SuccessRate:        90.0,
		StatusCount:        map[int]int{200: 90, 500: 10},
		MinLatency:         0.01,
		MaxLatency:         0.5,
		AvgLatency:         0.1,
		P50Latency:         0.08,
		P90Latency:         0.2,
		P95Latency:         0.3,
		P99Latency:         0.45,
		TotalResponseBytes: 1024 * 100,
		AvgResponseBytes:   1024,
	}

	var buf bytes.Buffer
	PrintTextResult(&buf, stat, 10.0, 10.0)
	out := buf.String()

	expected := []string{
		"Stress test finished",
		"Successes",
		"Failures",
		"Success rate",
		"90.0%",
		"p50",
		"p90",
		"p95",
		"p99",
		"Data transferred",
	}
	for _, s := range expected {
		if !strings.Contains(out, s) {
			t.Errorf("expected %q in output", s)
		}
	}
}

func TestPrintJSONResult(t *testing.T) {
	output := JSONOutput{
		Config: TestConfig{
			URL:         "http://example.com",
			Method:      "GET",
			Requests:    100,
			Concurrency: 10,
			Timeout:     5.0,
		},
		Statistics: stats.Statistics{
			Successes:   100,
			Total:       100,
			SuccessRate: 100.0,
			StatusCount: map[int]int{200: 100},
		},
		TotalTime: 1.0,
		ReqPerSec: 100.0,
	}

	var buf bytes.Buffer
	err := PrintJSONResult(&buf, output)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if decoded.Statistics.Total != 100 {
		t.Errorf("total = %d, want 100", decoded.Statistics.Total)
	}
	if decoded.ReqPerSec != 100.0 {
		t.Errorf("req/s = %f, want 100.0", decoded.ReqPerSec)
	}
}

func TestColorWriterDisabled(t *testing.T) {
	var buf bytes.Buffer
	cw := newColorWriter(&buf)
	if cw.enabled {
		t.Error("colorWriter should be disabled for bytes.Buffer")
	}
	result := cw.colorize(colorRed, "test")
	if result != "test" {
		t.Errorf("expected plain 'test', got %q", result)
	}
}

func TestColorWriterNO_COLOR(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	var buf bytes.Buffer
	cw := newColorWriter(&buf)
	if cw.enabled {
		t.Error("colorWriter should be disabled when NO_COLOR is set")
	}
	result := cw.colorize(colorRed, "test")
	if result != "test" {
		t.Errorf("expected plain 'test', got %q", result)
	}
}

func TestColorWriterFORCE_COLOR(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	var buf bytes.Buffer
	cw := newColorWriter(&buf)
	if !cw.enabled {
		t.Error("colorWriter should be enabled when FORCE_COLOR is set")
	}
	result := cw.colorize(colorRed, "test")
	if !strings.Contains(result, "\033[31m") {
		t.Errorf("expected ANSI red in output, got %q", result)
	}
}

func TestColorWriterNO_COLOR_TakesPrecedence(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "1")
	var buf bytes.Buffer
	cw := newColorWriter(&buf)
	if cw.enabled {
		t.Error("NO_COLOR should take precedence over FORCE_COLOR")
	}
}

func TestRenderBar(t *testing.T) {
	tests := []struct {
		name string
		pct  float64
		want string
	}{
		{"0%", 0, "[                    ]"},
		{"50%", 50, "[==========          ]"},
		{"100%", 100, "[====================]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := renderBar(tt.pct, 20)
			if got != tt.want {
				t.Errorf("renderBar(%f, 20) = %q, want %q", tt.pct, got, tt.want)
			}
		})
	}
}
