package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"api-stress-test/internal/stats"
	"api-stress-test/internal/ui"
)

func baseLifecycleOptions(writer *bytes.Buffer, target string) StressTestOptions {
	return StressTestOptions{
		Writer: writer, TargetURL: target, Method: "GET", TotalRequests: 1,
		Concurrency: 1, Timeout: time.Second, OutputFormat: "json",
	}
}

func decodeLifecycleOutput(t *testing.T, buffer *bytes.Buffer) ui.JSONOutput {
	t.Helper()
	var output ui.JSONOutput
	if err := json.Unmarshal(buffer.Bytes(), &output); err != nil {
		t.Fatalf("decode output: %v\n%s", err, buffer.String())
	}
	return output
}

func TestRunStressTestNonTTYHasNoProgressControlCharacters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	opts.OutputFormat = "text"
	opts.TotalRequests = 3
	deps := defaultRuntimeDependencies()
	deps.isTerminal = func(io.Writer) bool { return false }
	if err := runStressTestWithDependencies(context.Background(), opts, deps); err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(output.String(), "\r\x1b") {
		t.Fatalf("non-TTY output contains terminal controls: %q", output.String())
	}
}

func TestSynchronizedWriterPreservesTerminalColorCapability(t *testing.T) {
	previous, hadPrevious := os.LookupEnv("NO_COLOR")
	if err := os.Unsetenv("NO_COLOR"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if hadPrevious {
			_ = os.Setenv("NO_COLOR", previous)
		} else {
			_ = os.Unsetenv("NO_COLOR")
		}
	})

	var output bytes.Buffer
	writer := newSynchronizedWriter(&output, true)
	if err := ui.PrintHeader(writer, ui.HeaderConfig{URL: "http://example.com", Method: http.MethodGet}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("terminal output has no ANSI color: %q", output.String())
	}
}

func TestWarmupUsesSharedRateLimiter(t *testing.T) {
	var starts atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		starts.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	opts.Warmup = time.Hour
	opts.Rate = 10
	deps := defaultRuntimeDependencies()
	deps.isTerminal = func(io.Writer) bool { return false }
	limiter := &scriptedLimiter{waits: []bool{true, true, false, true}}
	deps.newLimiter = func(rate float64) (requestLimiter, error) {
		if rate != opts.Rate {
			t.Fatalf("limiter rate = %v, want %v", rate, opts.Rate)
		}
		return limiter, nil
	}
	if err := runStressTestWithDependencies(context.Background(), opts, deps); err != nil {
		t.Fatal(err)
	}
	if starts.Load() != 3 {
		t.Fatalf("request starts = %d, want two warmup and one measured", starts.Load())
	}
	if !limiter.stopped.Load() {
		t.Fatal("shared limiter was not stopped")
	}
}

func TestWarmupZeroSuccessAbortsWithValidReport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	opts.Warmup = time.Hour
	deps := defaultRuntimeDependencies()
	deps.newLimiter = func(float64) (requestLimiter, error) {
		return &scriptedLimiter{waits: []bool{true, false}}, nil
	}
	err := runStressTestWithDependencies(context.Background(), opts, deps)
	if err == nil || !strings.Contains(err.Error(), "zero successful") {
		t.Fatalf("error = %v, want warmup failure", err)
	}
	report := decodeLifecycleOutput(t, &output)
	if report.TerminationReason != terminationWarmupFailed || report.Warmup.Failures == 0 || report.Statistics.Total != 0 {
		t.Fatalf("warmup report = %+v", report)
	}
}

func TestWarmupPartialFailureContinuesToMeasurement(t *testing.T) {
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if count.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	opts.Warmup = time.Hour
	opts.Rate = 100
	deps := defaultRuntimeDependencies()
	deps.newLimiter = func(float64) (requestLimiter, error) {
		return &scriptedLimiter{waits: []bool{true, true, false, true}}, nil
	}
	if err := runStressTestWithDependencies(context.Background(), opts, deps); err != nil {
		t.Fatal(err)
	}
	report := decodeLifecycleOutput(t, &output)
	if report.Warmup.Successes == 0 || report.Warmup.Failures == 0 || report.Statistics.Successes != 1 {
		t.Fatalf("partial warmup report = %+v", report)
	}
}

func TestExecuteRequestSafelyRecoversTransportPanic(t *testing.T) {
	completedAt := time.Unix(123, 0)
	client := &http.Client{Transport: panicRoundTripper{}}
	result := executeRequestSafely(func() time.Time { return completedAt }, context.Background(), client, nil, StressTestOptions{
		TargetURL: "http://example.com", Method: http.MethodGet, ExpectStatus: http.StatusOK,
	})
	if result.ErrorKind != "recovered_panic" || result.CompletedAt != completedAt || !strings.Contains(result.Error, "transport panic") {
		t.Fatalf("recovered result = %+v", result)
	}
}

func TestFinishRunRetainsExitCodeAndOutputError(t *testing.T) {
	outputFailure := errors.New("output failed")
	writer := newSynchronizedWriter(failingLifecycleWriter{err: outputFailure}, false)
	termination := &terminationError{event: terminationEvent{reason: terminationInterrupt, code: 130}}
	err := finishRun(writer, defaultRuntimeDependencies(), StressTestOptions{OutputFormat: "json"}, time.Second, ui.WarmupSummary{}, stats.Statistics{}, 0, 0, terminationInterrupt, termination)
	var coded interface{ ExitCode() int }
	if !errors.As(err, &coded) || coded.ExitCode() != 130 {
		t.Fatalf("error = %v, want exit code 130", err)
	}
	if !errors.Is(err, outputFailure) || !errors.Is(err, termination) {
		t.Fatalf("error = %v, want termination and output errors", err)
	}
}

func TestDurationDrainsActiveRequestWithinGrace(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	opts.Duration = 25 * time.Millisecond
	grace := 200 * time.Millisecond
	opts.ShutdownGrace = &grace
	done := make(chan error, 1)
	go func() { done <- runStressTest(context.Background(), opts) }()
	<-started
	time.Sleep(55 * time.Millisecond)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	report := decodeLifecycleOutput(t, &output)
	if report.TerminationReason != terminationDuration || report.Statistics.Successes == 0 || report.Statistics.Cancelled != 0 || report.DrainTime <= 0 {
		t.Fatalf("duration report = %+v", report)
	}
}

func TestDurationGraceExpiryIsPlannedCancellation(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	opts.Duration = 20 * time.Millisecond
	grace := 20 * time.Millisecond
	opts.ShutdownGrace = &grace
	if err := runStressTest(context.Background(), opts); err != nil {
		t.Fatalf("planned duration cancellation returned error: %v", err)
	}
	report := decodeLifecycleOutput(t, &output)
	if report.TerminationReason != terminationGraceExpired || report.Statistics.Cancelled != 1 || report.Statistics.Failures != 0 {
		t.Fatalf("grace-expiry report = %+v", report)
	}
}

func TestShortDurationAlwaysReportsDurationElapsed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	for iteration := 0; iteration < 50; iteration++ {
		var output bytes.Buffer
		opts := baseLifecycleOptions(&output, server.URL)
		opts.Duration = time.Millisecond
		opts.Rate = 1
		if err := runStressTest(context.Background(), opts); err != nil {
			t.Fatalf("iteration %d: %v", iteration, err)
		}
		report := decodeLifecycleOutput(t, &output)
		if report.TerminationReason != terminationDuration {
			t.Fatalf("iteration %d: termination reason = %q, want %q", iteration, report.TerminationReason, terminationDuration)
		}
	}
}

func TestSecondSignalCancelsActiveRequestAndSIGINTWins(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-r.Context().Done()
	}))
	defer server.Close()

	signals := make(chan os.Signal, 2)
	deps := defaultRuntimeDependencies()
	deps.isTerminal = func(io.Writer) bool { return false }
	deps.signalSource = func() (<-chan os.Signal, func()) { return signals, func() {} }
	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	grace := time.Second
	opts.ShutdownGrace = &grace
	done := make(chan error, 1)
	go func() { done <- runStressTestWithDependencies(context.Background(), opts, deps) }()
	<-started
	signals <- os.Interrupt
	signals <- os.Interrupt
	err := <-done
	var termination *terminationError
	if !errors.As(err, &termination) || termination.ExitCode() != 130 {
		t.Fatalf("error = %v, want SIGINT exit 130", err)
	}
	report := decodeLifecycleOutput(t, &output)
	if report.TerminationReason != terminationInterrupt || report.Statistics.Cancelled != 1 || report.Statistics.Failures != 0 {
		t.Fatalf("signal report = %+v", report)
	}
}

func TestSIGTERMMapsToExit143(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	signals := make(chan os.Signal, 1)
	deps := defaultRuntimeDependencies()
	deps.isTerminal = func(io.Writer) bool { return false }
	deps.signalSource = func() (<-chan os.Signal, func()) { return signals, func() {} }
	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	zero := time.Duration(0)
	opts.ShutdownGrace = &zero
	done := make(chan error, 1)
	go func() { done <- runStressTestWithDependencies(context.Background(), opts, deps) }()
	time.Sleep(10 * time.Millisecond)
	signals <- syscall.SIGTERM
	err := <-done
	var termination *terminationError
	if !errors.As(err, &termination) || termination.ExitCode() != 143 {
		t.Fatalf("error = %v, want SIGTERM exit 143", err)
	}
}

func TestExecuteContextUsesTypedTerminationExitCode(t *testing.T) {
	run := func(context.Context, StressTestOptions) error {
		return &terminationError{event: terminationEvent{reason: terminationTerminate, code: 143}}
	}
	code, _, stderr := runCommand(t, context.Background(), []string{"--url", "http://example.com"}, run)
	if code != 143 || !strings.Contains(stderr, terminationTerminate) {
		t.Fatalf("exit/stderr = %d/%q, want 143 with termination reason", code, stderr)
	}
}

func TestFixedModeStartsExactRequestCount(t *testing.T) {
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	var output bytes.Buffer
	opts := baseLifecycleOptions(&output, server.URL)
	opts.TotalRequests = 37
	opts.Concurrency = 8
	if err := runStressTest(context.Background(), opts); err != nil {
		t.Fatal(err)
	}
	if got := count.Load(); got != 37 {
		t.Fatalf("request starts = %d, want 37", got)
	}
}

func TestSchedulerIntegrationAllocations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	deps := defaultRuntimeDependencies()
	deps.isTerminal = func(io.Writer) bool { return false }
	deps.signalSource = func() (<-chan os.Signal, func()) {
		return make(chan os.Signal), func() {}
	}
	opts := StressTestOptions{
		Writer: io.Discard, TargetURL: server.URL, Method: "GET", TotalRequests: 8,
		Concurrency: 4, Timeout: time.Second, OutputFormat: "json",
	}
	allocations := testing.AllocsPerRun(10, func() {
		if err := runStressTestWithDependencies(context.Background(), opts, deps); err != nil {
			panic(err)
		}
	})
	if allocations > 1200 {
		t.Fatalf("scheduler integration allocations = %.2f, want <= 1200", allocations)
	}
}

func BenchmarkSchedulerIntegration(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	deps := defaultRuntimeDependencies()
	deps.isTerminal = func(io.Writer) bool { return false }
	deps.signalSource = func() (<-chan os.Signal, func()) {
		return make(chan os.Signal), func() {}
	}
	opts := StressTestOptions{
		Writer: io.Discard, TargetURL: server.URL, Method: "GET", TotalRequests: 1024,
		Concurrency: 32, Timeout: time.Second, OutputFormat: "json",
	}
	b.ReportAllocs()
	b.ReportMetric(float64(opts.TotalRequests), "requests/op")
	b.ResetTimer()
	for range b.N {
		if err := runStressTestWithDependencies(context.Background(), opts, deps); err != nil {
			b.Fatal(err)
		}
	}
}

type scriptedLimiter struct {
	waits   []bool
	index   atomic.Int64
	stopped atomic.Bool
}

func (l *scriptedLimiter) Wait(context.Context) bool {
	index := int(l.index.Add(1) - 1)
	return index < len(l.waits) && l.waits[index]
}

func (l *scriptedLimiter) Stop() {
	l.stopped.Store(true)
}

type panicRoundTripper struct{}

func (panicRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	panic("transport panic")
}

type failingLifecycleWriter struct {
	err error
}

func (w failingLifecycleWriter) Write([]byte) (int, error) {
	return 0, w.err
}
