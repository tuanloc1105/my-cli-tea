package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"api-stress-test/internal/ui"
)

func TestValidateURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"valid http", "http://example.com", false},
		{"valid https", "https://example.com/path?q=1", false},
		{"valid with port", "http://localhost:8080/api", false},
		{"empty", "", true},
		{"missing scheme", "example.com", true},
		{"ftp scheme", "ftp://example.com", true},
		{"missing host", "http://", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateURL(%q) error = %v, wantErr %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

func TestValidateMethod(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		wantErr bool
	}{
		{"GET", "GET", false},
		{"POST", "POST", false},
		{"PUT", "PUT", false},
		{"DELETE", "DELETE", false},
		{"PATCH", "PATCH", false},
		{"HEAD", "HEAD", false},
		{"OPTIONS", "OPTIONS", false},
		{"lowercase get", "get", false},
		{"mixed case", "Post", false},
		{"invalid", "INVALID", true},
		{"empty", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMethod(tt.method)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMethod(%q) error = %v, wantErr %v", tt.method, err, tt.wantErr)
			}
		})
	}
}

// runTest is a helper that calls RunStressTest with common defaults.
func runTest(t *testing.T, buf *bytes.Buffer, url, method string, requests, concurrency int, timeout time.Duration, headers map[string]string, body []byte, contentType string, rate float64, duration time.Duration, output string) error {
	t.Helper()
	return RunStressTest(StressTestOptions{
		Writer:        buf,
		TargetURL:     url,
		Method:        method,
		TotalRequests: requests,
		Concurrency:   concurrency,
		Timeout:       timeout,
		Headers:       headers,
		Body:          body,
		ContentType:   contentType,
		Rate:          rate,
		Duration:      duration,
		OutputFormat:  output,
	})
}

func TestRunStressTest_BasicSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := runTest(t, &buf, server.URL, "GET", 10, 2, 5*time.Second, nil, nil, "", 0, 0, "text")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Stress test finished") {
		t.Errorf("expected 'Stress test finished' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Successes") {
		t.Errorf("expected 'Successes' in output")
	}
}

func TestRunStressTest_AllFailures(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := runTest(t, &buf, server.URL, "GET", 10, 2, 5*time.Second, nil, nil, "", 0, 0, "text")

	if err == nil {
		t.Fatal("expected error for all-failure test")
	}
	if !strings.Contains(err.Error(), "10 out of 10 requests failed") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestRunStressTest_JSONOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := runTest(t, &buf, server.URL, "GET", 10, 2, 5*time.Second, nil, nil, "", 0, 0, "json")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var output ui.JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("invalid JSON output: %v\nraw: %s", err, buf.String())
	}
	if output.Statistics.Total != 10 {
		t.Errorf("total = %d, want 10", output.Statistics.Total)
	}
	if output.Statistics.Successes != 10 {
		t.Errorf("successes = %d, want 10", output.Statistics.Successes)
	}
	if output.Config.URL != server.URL {
		t.Errorf("config URL = %q, want %q", output.Config.URL, server.URL)
	}
	if output.ReqPerSec <= 0 {
		t.Errorf("req/s = %f, want > 0", output.ReqPerSec)
	}
}

func TestRunStressTest_DurationMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	_ = runTest(t, &buf, server.URL, "GET", 100, 2, 5*time.Second, nil, nil, "", 0, 1*time.Second, "json")

	var output ui.JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("invalid JSON output: %v", err)
	}
	if output.Statistics.Total == 0 {
		t.Error("expected some requests in duration mode")
	}
	if output.Config.Duration == "" {
		t.Error("expected duration in config")
	}
}

func TestRunStressTest_RateLimiting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	start := time.Now()
	err := runTest(t, &buf, server.URL, "GET", 5, 2, 5*time.Second, nil, nil, "", 10, 0, "json")
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First request is immediate, then 4 waits at 100ms each ≈ 400ms
	if elapsed < 300*time.Millisecond {
		t.Errorf("rate limiting too fast: %v (expected >= 300ms)", elapsed)
	}
}

func TestRunStressTest_WithBody(t *testing.T) {
	var receivedBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 1024)
		n, _ := r.Body.Read(b)
		receivedBody = string(b[:n])
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	body := []byte(`{"test":true}`)
	err := runTest(t, &buf, server.URL, "POST", 1, 1, 5*time.Second, nil, body, "application/json", 0, 0, "json")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedBody != `{"test":true}` {
		t.Errorf("received body = %q, want %q", receivedBody, `{"test":true}`)
	}
}

func TestRunStressTest_ExpectStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated) // 201
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := RunStressTest(StressTestOptions{
		Writer:        &buf,
		TargetURL:     server.URL,
		Method:        "GET",
		TotalRequests: 5,
		Concurrency:   1,
		Timeout:       5 * time.Second,
		OutputFormat:  "json",
		ExpectStatus:  200,
	})

	if err == nil {
		t.Fatal("expected error when expect-status doesn't match")
	}
}

func TestRunStressTest_ExpectBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","data":"hello"}`))
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := RunStressTest(StressTestOptions{
		Writer:        &buf,
		TargetURL:     server.URL,
		Method:        "GET",
		TotalRequests: 5,
		Concurrency:   1,
		Timeout:       5 * time.Second,
		OutputFormat:  "json",
		ExpectBody:    "missing",
	})

	if err == nil {
		t.Fatal("expected error when expect-body doesn't match")
	}
}

func TestRunStressTest_WithHeaders(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	headers := map[string]string{"Authorization": "Bearer test-token"}
	err := RunStressTest(StressTestOptions{
		Writer:        &buf,
		TargetURL:     server.URL,
		Method:        "GET",
		TotalRequests: 1,
		Concurrency:   1,
		Timeout:       5 * time.Second,
		Headers:       headers,
		OutputFormat:  "json",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedAuth != "Bearer test-token" {
		t.Errorf("Authorization header = %q, want %q", receivedAuth, "Bearer test-token")
	}
}

func TestRunStressTest_Warmup(t *testing.T) {
	var requestCount atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := RunStressTest(StressTestOptions{
		Writer:        &buf,
		TargetURL:     server.URL,
		Method:        "GET",
		TotalRequests: 5,
		Concurrency:   1,
		Timeout:       5 * time.Second,
		OutputFormat:  "json",
		Warmup:        500 * time.Millisecond,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var output ui.JSONOutput
	if err := json.Unmarshal(buf.Bytes(), &output); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// Stats should only count 5 test requests, not warmup requests
	if output.Statistics.Total != 5 {
		t.Errorf("total = %d, want 5 (warmup should not be counted)", output.Statistics.Total)
	}
	// Total HTTP requests should be more than 5 (warmup + test)
	if requestCount.Load() <= 5 {
		t.Errorf("requestCount = %d, expected > 5 (should include warmup)", requestCount.Load())
	}
}

func TestRunStressTest_DisableKeepalive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := RunStressTest(StressTestOptions{
		Writer:           &buf,
		TargetURL:        server.URL,
		Method:           "GET",
		TotalRequests:    5,
		Concurrency:      1,
		Timeout:          5 * time.Second,
		OutputFormat:     "json",
		DisableKeepalive: true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunStressTest_DisableRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer server.Close()

	var buf bytes.Buffer
	err := RunStressTest(StressTestOptions{
		Writer:           &buf,
		TargetURL:        server.URL,
		Method:           "GET",
		TotalRequests:    5,
		Concurrency:      1,
		Timeout:          5 * time.Second,
		OutputFormat:     "json",
		DisableRedirects: true,
	})

	// All should fail since 302 is not 2xx
	if err == nil {
		t.Fatal("expected error for redirect responses without following")
	}

	var output ui.JSONOutput
	if jsonErr := json.Unmarshal(buf.Bytes(), &output); jsonErr != nil {
		t.Fatalf("invalid JSON: %v", jsonErr)
	}
	if output.Statistics.StatusCount[302] != 5 {
		t.Errorf("expected 5 302 responses, got %v", output.Statistics.StatusCount)
	}
}

func TestRunStressTest_OutputFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	dir := t.TempDir()
	outFile := filepath.Join(dir, "result.json")

	var buf bytes.Buffer
	err := RunStressTest(StressTestOptions{
		Writer:        &buf,
		TargetURL:     server.URL,
		Method:        "GET",
		TotalRequests: 5,
		Concurrency:   1,
		Timeout:       5 * time.Second,
		OutputFormat:  "text",
		OutputFile:    outFile,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read output file: %v", err)
	}

	var output ui.JSONOutput
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("invalid JSON in output file: %v", err)
	}
	if output.Statistics.Total != 5 {
		t.Errorf("total = %d, want 5", output.Statistics.Total)
	}
}

func TestCommandForwardsFlagsContextAndWriter(t *testing.T) {
	ctx := context.WithValue(context.Background(), commandContextKey{}, "value")
	var receivedContext context.Context
	var received StressTestOptions
	run := func(ctx context.Context, options StressTestOptions) error {
		receivedContext = ctx
		received = options
		_, _ = options.Writer.Write([]byte("runner output\n"))
		return nil
	}

	code, stdout, stderr := runCommand(t, ctx, []string{
		"--url", "https://example.com/path",
		"--method", "post",
		"--requests", "7",
		"--concurrency", "3",
		"--timeout", "1.5",
		"--headers", "X-Test:value;Accept:application/json",
		"--body", `{"ok":true}`,
		"--content-type", "application/custom",
		"--rate", "2.5",
		"--insecure",
		"--disable-keepalive",
		"--disable-redirects",
		"--expect-status", "201",
		"--expect-body", "ok",
		"--warmup", "250ms",
		"--output", "json",
		"--output-file", "results.json",
		"--proxy", "http://localhost:8080",
		"ignored", "arguments",
	}, run)

	if code != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q, want 0/empty", code, stderr)
	}
	if stdout != "runner output\n" {
		t.Fatalf("stdout = %q, want runner output", stdout)
	}
	if receivedContext.Value(commandContextKey{}) != "value" {
		t.Fatal("runner did not receive command context")
	}
	if received.TargetURL != "https://example.com/path" || received.Method != "POST" {
		t.Fatalf("target/method = %q/%q", received.TargetURL, received.Method)
	}
	if received.TotalRequests != 7 || received.Concurrency != 3 || received.Timeout != 1500*time.Millisecond {
		t.Fatalf("load options = %+v", received)
	}
	if received.Headers["X-Test"] != "value" || received.Headers["Accept"] != "application/json" {
		t.Fatalf("headers = %#v", received.Headers)
	}
	if string(received.Body) != `{"ok":true}` || received.ContentType != "application/custom" {
		t.Fatalf("body/content type = %q/%q", received.Body, received.ContentType)
	}
	if received.Rate != 2.5 || received.OutputFormat != "json" || received.Warmup != 250*time.Millisecond {
		t.Fatalf("rate/output/warmup = %+v", received)
	}
	if !received.Insecure || !received.DisableKeepalive || !received.DisableRedirects {
		t.Fatalf("transport flags = %+v", received)
	}
	if received.ExpectStatus != 201 || received.ExpectBody != "ok" || received.OutputFile != "results.json" || received.Proxy != "http://localhost:8080" {
		t.Fatalf("validation/output/proxy = %+v", received)
	}
}

func TestCommandDefaultsFallbacksAndStateIsolation(t *testing.T) {
	var received []StressTestOptions
	run := func(_ context.Context, options StressTestOptions) error {
		received = append(received, options)
		return nil
	}

	code, _, stderr := runCommand(t, context.Background(), []string{
		"--url", "http://example.com", "--method", "post", "--requests", "0", "--concurrency", "0", "--insecure", "--output", "json",
	}, run)
	if code != 0 || stderr != "" {
		t.Fatalf("first exit/stderr = %d/%q", code, stderr)
	}
	code, _, stderr = runCommand(t, context.Background(), []string{"--url", "http://example.com"}, run)
	if code != 0 || stderr != "" {
		t.Fatalf("second exit/stderr = %d/%q", code, stderr)
	}

	if len(received) != 2 {
		t.Fatalf("runner calls = %d, want 2", len(received))
	}
	if received[0].TotalRequests != 100 || received[0].Concurrency != 10 || received[0].Method != "POST" || !received[0].Insecure || received[0].OutputFormat != "json" {
		t.Fatalf("fallback options = %+v", received[0])
	}
	if received[1].TotalRequests != 100 || received[1].Concurrency != 10 || received[1].Method != "GET" || received[1].Insecure || received[1].OutputFormat != "text" || received[1].Timeout != 5*time.Second {
		t.Fatalf("default options leaked state = %+v", received[1])
	}
}

func TestCommandHelpAndFlags(t *testing.T) {
	called := false
	run := func(context.Context, StressTestOptions) error {
		called = true
		return nil
	}
	code, stdout, stderr := runCommand(t, context.Background(), []string{"--help"}, run)
	if code != 0 || stderr != "" {
		t.Fatalf("exit/stderr = %d/%q, want 0/empty", code, stderr)
	}
	if called {
		t.Fatal("runner called for help")
	}
	if !strings.Contains(stdout, "Usage:") || !strings.Contains(stdout, "--output-file") {
		t.Fatalf("help output = %q", stdout)
	}

	root := newRootCommand(run, &bytes.Buffer{}, &bytes.Buffer{})
	for _, name := range []string{
		"url", "method", "requests", "concurrency", "timeout", "headers", "data", "json-body", "json-file", "body", "file", "content-type",
		"rate", "duration", "insecure", "disable-keepalive", "disable-redirects", "proxy", "expect-status", "expect-body", "warmup", "output", "output-file",
	} {
		if root.Flags().Lookup(name) == nil {
			t.Errorf("missing flag --%s", name)
		}
	}
}

func TestCommandErrorsRenderOnceOnStderr(t *testing.T) {
	originalArgs := os.Args
	os.Args = []string{"host-process", "--host-only"}
	t.Cleanup(func() { os.Args = originalArgs })

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing URL", args: nil, want: "required flag"},
		{name: "invalid method", args: []string{"--url", "http://example.com", "--method", "bad"}, want: "unsupported HTTP method"},
		{name: "invalid output", args: []string{"--url", "http://example.com", "--output", "yaml"}, want: "unsupported output format"},
		{name: "invalid timeout", args: []string{"--url", "http://example.com", "--timeout", "0"}, want: "timeout must be positive"},
		{name: "explicit zero rate", args: []string{"--url", "http://example.com", "--rate", "0"}, want: "rate must be positive"},
		{name: "excessive concurrency", args: []string{"--url", "http://example.com", "--concurrency", "10001"}, want: "concurrency too high"},
		{name: "invalid duration", args: []string{"--url", "http://example.com", "--duration", "bad"}, want: "invalid duration"},
		{name: "invalid warmup", args: []string{"--url", "http://example.com", "--warmup", "bad"}, want: "invalid warmup duration"},
		{name: "mutually exclusive body flags", args: []string{"--url", "http://example.com", "--body", "x", "--json-body", `{}`}, want: "none of the others"},
		{name: "missing body file", args: []string{"--url", "http://example.com", "--file", filepath.Join(t.TempDir(), "missing")}, want: "preparing body"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			run := func(context.Context, StressTestOptions) error {
				called = true
				return nil
			}
			code, stdout, stderr := runCommand(t, context.Background(), tt.args, run)
			if code != 1 {
				t.Fatalf("exit code = %d, want 1", code)
			}
			if called {
				t.Fatal("runner called for invalid command")
			}
			if stdout != "" {
				t.Fatalf("stdout = %q, want empty", stdout)
			}
			if strings.Count(stderr, "Error:") != 1 || !strings.Contains(stderr, tt.want) {
				t.Fatalf("stderr = %q, want one error containing %q", stderr, tt.want)
			}
			if strings.Contains(stderr, "Usage:") {
				t.Fatalf("stderr unexpectedly contains usage: %q", stderr)
			}
		})
	}
}

func TestCommandRunnerErrorAndNilInputs(t *testing.T) {
	run := func(context.Context, StressTestOptions) error {
		return errors.New("stress failed")
	}
	code, stdout, stderr := runCommand(t, nil, []string{"--url", "http://example.com"}, run)
	if code != 1 || stdout != "" || stderr != "Error: stress failed\n" {
		t.Fatalf("exit/stdout/stderr = %d/%q/%q", code, stdout, stderr)
	}
	if code := executeContext(nil, []string{"--url", "http://example.com"}, nil, nil, func(ctx context.Context, options StressTestOptions) error {
		if ctx == nil || options.Writer == nil {
			t.Fatal("nil context or writer reached runner")
		}
		return nil
	}); code != 0 {
		t.Fatalf("nil-input exit code = %d, want 0", code)
	}
}

func TestRunStressTestUsesParentContext(t *testing.T) {
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	err := runStressTest(ctx, StressTestOptions{
		Writer:        &output,
		TargetURL:     server.URL,
		Method:        "GET",
		TotalRequests: 100,
		Concurrency:   2,
		Timeout:       time.Second,
		OutputFormat:  "text",
	})
	if err != nil {
		t.Fatalf("runStressTest returned error: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatalf("requests = %d, want 0 after parent cancellation", requests.Load())
	}
	if !strings.Contains(output.String(), "No requests were executed.") {
		t.Fatalf("output = %q, want cancellation summary", output.String())
	}
}

func runCommand(t *testing.T, ctx context.Context, args []string, run runner) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := executeContext(ctx, args, &stdout, &stderr, run)
	return code, stdout.String(), stderr.String()
}

type commandContextKey struct{}
