package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"api-stress-test/internal/request"
	"api-stress-test/internal/stats"
	"api-stress-test/internal/ui"
)

const (
	terminationCompleted    = "completed"
	terminationDuration     = "duration_elapsed"
	terminationGraceExpired = "shutdown_grace_expired"
	terminationInterrupt    = "sigint"
	terminationTerminate    = "sigterm"
	terminationParentCancel = "parent_cancelled"
	terminationWarmupFailed = "warmup_failed"
)

type runtimeDependencies struct {
	now          func() time.Time
	isTerminal   func(io.Writer) bool
	signalSource func() (<-chan os.Signal, func())
	writeReport  func(string, []byte) error
	newLimiter   func(float64) (requestLimiter, error)
}

type requestLimiter interface {
	Wait(context.Context) bool
	Stop()
}

func defaultRuntimeDependencies() runtimeDependencies {
	return runtimeDependencies{
		now:        time.Now,
		isTerminal: defaultTerminalDetector,
		signalSource: func() (<-chan os.Signal, func()) {
			ch := make(chan os.Signal, 2)
			signal.Notify(ch, os.Interrupt, syscall.SIGTERM)
			return ch, func() { signal.Stop(ch) }
		},
		writeReport: writeReportFile,
		newLimiter: func(rate float64) (requestLimiter, error) {
			return request.NewRateLimiter(rate)
		},
	}
}

func defaultTerminalDetector(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

type terminationEvent struct {
	reason string
	code   int
}

type terminationController struct {
	mu          sync.Mutex
	event       terminationEvent
	first       chan struct{}
	second      chan struct{}
	done        chan struct{}
	closeOnce   sync.Once
	firstOnce   sync.Once
	secondOnce  sync.Once
	stopSignals func()
}

func newTerminationController(ctx context.Context, source func() (<-chan os.Signal, func())) *terminationController {
	if source == nil {
		source = defaultRuntimeDependencies().signalSource
	}
	signals, stopSignals := source()
	c := &terminationController{
		first:       make(chan struct{}),
		second:      make(chan struct{}),
		done:        make(chan struct{}),
		stopSignals: stopSignals,
	}
	if ctx.Err() != nil {
		c.setFirst(terminationEvent{reason: terminationParentCancel, code: 130})
	}
	go c.watch(ctx, signals)
	return c
}

func (c *terminationController) watch(ctx context.Context, signals <-chan os.Signal) {
	select {
	case <-ctx.Done():
		c.setFirst(terminationEvent{reason: terminationParentCancel, code: 130})
	case sig := <-signals:
		c.setFirst(eventForSignal(sig))
	case <-c.done:
		return
	}

	select {
	case <-signals:
		c.secondOnce.Do(func() { close(c.second) })
		if c.stopSignals != nil {
			c.stopSignals()
		}
	case <-c.done:
	}
}

func eventForSignal(sig os.Signal) terminationEvent {
	if sig == syscall.SIGTERM {
		return terminationEvent{reason: terminationTerminate, code: 143}
	}
	return terminationEvent{reason: terminationInterrupt, code: 130}
}

func (c *terminationController) setFirst(event terminationEvent) {
	c.firstOnce.Do(func() {
		c.mu.Lock()
		c.event = event
		c.mu.Unlock()
		close(c.first)
	})
}

func (c *terminationController) Event() terminationEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.event
}

func (c *terminationController) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
		if c.stopSignals != nil {
			c.stopSignals()
		}
	})
}

type terminationError struct {
	event terminationEvent
}

func (e *terminationError) Error() string { return "stress test terminated: " + e.event.reason }
func (e *terminationError) ExitCode() int { return e.event.code }

type exitCodeError struct {
	code int
	err  error
}

func (e *exitCodeError) Error() string { return e.err.Error() }
func (e *exitCodeError) Unwrap() error { return e.err }
func (e *exitCodeError) ExitCode() int { return e.code }

type synchronizedWriter struct {
	mu         sync.Mutex
	w          io.Writer
	err        error
	isTerminal bool
}

func newSynchronizedWriter(w io.Writer, isTerminal bool) *synchronizedWriter {
	return &synchronizedWriter{w: w, isTerminal: isTerminal}
}

func (w *synchronizedWriter) TerminalOutput() bool { return w.isTerminal }

func (w *synchronizedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
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

func (w *synchronizedWriter) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func runStressTestWithDependencies(ctx context.Context, opts StressTestOptions, deps runtimeDependencies) error {
	if ctx == nil {
		ctx = context.Background()
	}
	opts = normalizeStressTestOptions(opts)
	if err := validateStressTestOptions(opts); err != nil {
		return err
	}
	if deps.now == nil {
		deps.now = time.Now
	}
	if deps.isTerminal == nil {
		deps.isTerminal = defaultTerminalDetector
	}
	if deps.writeReport == nil {
		deps.writeReport = writeReportFile
	}
	if deps.newLimiter == nil {
		deps.newLimiter = defaultRuntimeDependencies().newLimiter
	}

	rawWriter := opts.Writer
	if rawWriter == nil {
		rawWriter = io.Discard
	}
	outputIsTerminal := deps.isTerminal(rawWriter)
	writer := newSynchronizedWriter(rawWriter, outputIsTerminal)
	isJSON := opts.OutputFormat == "json"
	isDuration := opts.Duration > 0
	effectiveGrace := opts.Timeout
	if opts.ShutdownGrace != nil {
		effectiveGrace = *opts.ShutdownGrace
	}

	controller := newTerminationController(ctx, deps.signalSource)
	defer controller.Close()
	baseContext := context.WithoutCancel(ctx)

	transport := &http.Transport{
		MaxIdleConns:        opts.Concurrency,
		MaxIdleConnsPerHost: opts.Concurrency,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   opts.DisableKeepalive,
	}
	defer transport.CloseIdleConnections()
	if opts.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	if opts.Proxy != "" {
		proxyURL, err := url.Parse(opts.Proxy)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}
		transport.Proxy = http.ProxyURL(proxyURL)
	}
	client := &http.Client{Transport: transport, Timeout: opts.Timeout}
	if opts.DisableRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}

	if !isJSON {
		duration := ""
		if isDuration {
			duration = opts.Duration.String()
		}
		if err := ui.PrintHeader(writer, ui.HeaderConfig{
			URL: opts.TargetURL, Method: opts.Method, TotalRequests: opts.TotalRequests,
			Concurrency: opts.Concurrency, TimeoutSec: opts.Timeout.Seconds(), Rate: opts.Rate,
			IsDurationMode: isDuration, Duration: duration, BodyLen: len(opts.Body), ContentType: opts.ContentType,
		}); err != nil {
			return err
		}
	}

	limiter, err := deps.newLimiter(opts.Rate)
	if err != nil {
		return fmt.Errorf("creating rate limiter: %w", err)
	}
	defer limiter.Stop()
	matcher := request.PrepareBodyMatcher(opts.ExpectBody)
	warmup := ui.WarmupSummary{}
	if opts.Warmup > 0 {
		if !isJSON {
			_, _ = fmt.Fprintf(writer, "Warming up for %s...\n", opts.Warmup)
		}
		warmup = runWarmup(baseContext, controller, deps.now, client, limiter, matcher, opts)
		if event := controller.Event(); event.code != 0 {
			return finishRun(writer, deps, opts, effectiveGrace, warmup, stats.Statistics{}, 0, 0, event.reason, &terminationError{event: event})
		}
		if warmup.Successes == 0 {
			failure := fmt.Errorf("warmup completed with zero successful requests")
			return finishRun(writer, deps, opts, effectiveGrace, warmup, stats.Statistics{}, 0, 0, terminationWarmupFailed, failure)
		}
		if !isJSON {
			_, _ = fmt.Fprintf(writer, "Warm-up complete: %d succeeded, %d failed, %d cancelled.\n", warmup.Successes, warmup.Failures, warmup.Cancelled)
		}
	}
	if event := controller.Event(); event.code != 0 {
		return finishRun(writer, deps, opts, effectiveGrace, warmup, stats.Statistics{}, 0, 0, event.reason, &terminationError{event: event})
	}

	measurement := runMeasurement(baseContext, controller, writer, deps, client, limiter, matcher, opts, effectiveGrace)
	var finalErr error
	if measurement.event.code != 0 {
		finalErr = &terminationError{event: measurement.event}
	} else if measurement.statistics.Failures > 0 {
		finalErr = fmt.Errorf("%d out of %d requests failed", measurement.statistics.Failures, measurement.statistics.Completed)
	}
	return finishRun(
		writer, deps, opts, effectiveGrace, warmup, measurement.statistics,
		measurement.totalTime, measurement.drainTime, measurement.reason, finalErr,
	)
}

func runWarmup(
	base context.Context,
	controller *terminationController,
	now func() time.Time,
	client *http.Client,
	limiter requestLimiter,
	matcher *request.BodyMatcher,
	opts StressTestOptions,
) ui.WarmupSummary {
	started := time.Now()
	ctx, cancel := context.WithTimeout(base, opts.Warmup)
	defer cancel()
	go func() {
		select {
		case <-controller.first:
			cancel()
		case <-ctx.Done():
		}
	}()

	results := make(chan request.Result, opts.Concurrency)
	var workers sync.WaitGroup
	for range opts.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for limiter.Wait(ctx) {
				result := executeRequestSafely(now, ctx, client, matcher, opts)
				results <- result
				if !result.OK && result.Elapsed < 0.01 {
					select {
					case <-time.After(10 * time.Millisecond):
					case <-ctx.Done():
					}
				}
			}
		}()
	}
	go func() {
		workers.Wait()
		close(results)
	}()

	summary := ui.WarmupSummary{}
	for result := range results {
		summary.Total++
		switch {
		case result.OK:
			summary.Successes++
		case result.ErrorKind == request.ErrorKindCancellation:
			summary.Cancelled++
		default:
			summary.Failures++
		}
	}
	summary.DurationSeconds = time.Since(started).Seconds()
	return summary
}

func executeRequestSafely(
	now func() time.Time,
	ctx context.Context,
	client *http.Client,
	matcher *request.BodyMatcher,
	opts StressTestOptions,
) (result request.Result) {
	defer func() {
		if recovered := recover(); recovered != nil {
			result = request.Result{
				CompletedAt: now(), Error: fmt.Sprintf("panic: %v", recovered),
				ErrorKind: request.ErrorKindRecoveredPanic,
			}
		}
	}()
	return request.ExecuteRequestWithMatcher(
		ctx, client, opts.Method, opts.TargetURL, opts.Headers, opts.Body,
		opts.ContentType, opts.ExpectStatus, matcher,
	)
}

type measurementResult struct {
	statistics stats.Statistics
	totalTime  float64
	drainTime  float64
	reason     string
	event      terminationEvent
}

func runMeasurement(
	base context.Context,
	controller *terminationController,
	writer *synchronizedWriter,
	deps runtimeDependencies,
	client *http.Client,
	limiter requestLimiter,
	matcher *request.BodyMatcher,
	opts StressTestOptions,
	grace time.Duration,
) measurementResult {
	started := deps.now()
	collector := stats.NewCollector(opts.TotalRequests, started)
	durationCtx := base
	cancelDuration := func() {}
	if opts.Duration > 0 {
		durationCtx, cancelDuration = context.WithTimeout(base, opts.Duration)
	}
	defer cancelDuration()
	scheduleCtx, cancelSchedule := context.WithCancel(durationCtx)
	defer cancelSchedule()
	activeCtx, cancelActive := context.WithCancel(base)
	defer cancelActive()

	jobs := make(chan struct{})
	results := make(chan request.Result, opts.Concurrency)
	var startGate sync.Mutex
	schedulingStopped := false
	var durationCh <-chan struct{}
	if opts.Duration > 0 {
		durationCh = durationCtx.Done()
	}
	var workers sync.WaitGroup
	for range opts.Concurrency {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for range jobs {
				startGate.Lock()
				stopped := schedulingStopped || scheduleCtx.Err() != nil
				startGate.Unlock()
				if stopped {
					continue
				}
				result := executeRequestSafely(deps.now, activeCtx, client, matcher, opts)
				results <- result
			}
		}()
	}
	feedDone := make(chan struct{})
	go func() {
		defer close(jobs)
		defer close(feedDone)
		for sent := 0; opts.Duration > 0 || sent < opts.TotalRequests; sent++ {
			if !limiter.Wait(scheduleCtx) {
				return
			}
			select {
			case jobs <- struct{}{}:
			case <-scheduleCtx.Done():
				return
			}
		}
	}()
	go func() {
		workers.Wait()
		close(results)
	}()

	var progress *ui.Progress
	if opts.OutputFormat != "json" && writer.TerminalOutput() {
		progress = ui.NewProgress(writer, int64(opts.TotalRequests), opts.Duration > 0, opts.Duration)
		progress.Start()
	}

	reason := terminationCompleted
	var event terminationEvent
	var drainStarted time.Time
	var graceTimer *time.Timer
	var graceCh <-chan time.Time
	firstCh := controller.first
	secondCh := controller.second
	feedCh := feedDone
	plannedCancellation := atomic.Bool{}

	startDrain := func(nextReason string) {
		if drainStarted.IsZero() {
			drainStarted = deps.now()
			reason = nextReason
			startGate.Lock()
			schedulingStopped = true
			cancelSchedule()
			startGate.Unlock()
			if grace == 0 {
				plannedCancellation.Store(true)
				cancelActive()
				reason = terminationGraceExpired
				return
			}
			graceTimer = time.NewTimer(grace)
			graceCh = graceTimer.C
		}
	}

	for results != nil {
		select {
		case result, ok := <-results:
			if !ok {
				if reason == terminationCompleted && errors.Is(durationCtx.Err(), context.DeadlineExceeded) {
					startDrain(terminationDuration)
				}
				results = nil
				continue
			}
			outcome := stats.OutcomeFailure
			if result.OK {
				outcome = stats.OutcomeSuccess
			} else if plannedCancellation.Load() && result.ErrorKind == request.ErrorKindCancellation {
				outcome = stats.OutcomeCancelled
			}
			collector.Record(stats.Sample{
				StatusCode: result.StatusCode, TotalLatency: result.Elapsed, TTFB: result.TTFB,
				CompletedAt: result.CompletedAt, Outcome: outcome, Error: result.Error,
				ErrorKind: string(result.ErrorKind), ResponseBytes: result.ResponseSize,
			})
			if progress != nil {
				progress.Add(1)
			}
		case <-durationCh:
			durationCh = nil
			startDrain(terminationDuration)
		case <-firstCh:
			firstCh = nil
			event = controller.Event()
			startDrain(event.reason)
			if opts.OutputFormat != "json" {
				_, _ = fmt.Fprintln(writer, "Stopping new requests; draining active requests...")
			}
		case <-secondCh:
			secondCh = nil
			if event.code == 0 {
				event = controller.Event()
				startDrain(event.reason)
			}
			plannedCancellation.Store(true)
			cancelActive()
		case <-graceCh:
			graceCh = nil
			plannedCancellation.Store(true)
			cancelActive()
			if event.code == 0 {
				reason = terminationGraceExpired
			}
		case <-feedCh:
			feedCh = nil
		}
	}
	if graceTimer != nil {
		graceTimer.Stop()
	}
	if progress != nil {
		progress.Stop()
	}
	if current := controller.Event(); current.code != 0 {
		event = current
		reason = current.reason
	}

	finished := deps.now()
	drainTime := 0.0
	if !drainStarted.IsZero() {
		drainTime = finished.Sub(drainStarted).Seconds()
	}
	return measurementResult{
		statistics: collector.GetStatistics(), totalTime: finished.Sub(started).Seconds(),
		drainTime: drainTime, reason: reason, event: event,
	}
}

func finishRun(
	writer *synchronizedWriter,
	deps runtimeDependencies,
	opts StressTestOptions,
	grace time.Duration,
	warmup ui.WarmupSummary,
	statistics stats.Statistics,
	totalTime, drainTime float64,
	reason string,
	resultErr error,
) error {
	reqPerSec := 0.0
	if totalTime > 0 {
		reqPerSec = float64(statistics.Total) / totalTime
	}
	legacy := ui.TestConfig{
		URL: opts.TargetURL, Method: opts.Method, Concurrency: opts.Concurrency,
		Timeout: opts.Timeout.Seconds(), Rate: opts.Rate,
	}
	effective := ui.EffectiveConfig{
		URL: opts.TargetURL, Method: opts.Method, Concurrency: opts.Concurrency,
		Timeout: opts.Timeout.Seconds(), Rate: opts.Rate, Warmup: opts.Warmup.String(),
		ShutdownGrace: grace.String(), Insecure: opts.Insecure,
		DisableKeepalive: opts.DisableKeepalive, DisableRedirects: opts.DisableRedirects,
		ExpectStatus: opts.ExpectStatus, ExpectBody: opts.ExpectBody != "",
	}
	if opts.Duration > 0 {
		legacy.Duration = opts.Duration.String()
		effective.Duration = opts.Duration.String()
	} else {
		legacy.Requests = opts.TotalRequests
		effective.Requests = opts.TotalRequests
	}
	output := ui.JSONOutput{
		SchemaVersion: ui.JSONSchemaVersion, Config: legacy, EffectiveConfig: effective,
		Warmup: warmup, Statistics: statistics, TotalTime: totalTime, DrainTime: drainTime,
		ReqPerSec: reqPerSec, TerminationReason: reason,
	}

	var outputErr error
	if opts.OutputFormat == "json" {
		outputErr = ui.PrintJSONResult(writer, output)
	} else {
		outputErr = ui.PrintTextResult(writer, statistics, totalTime, reqPerSec)
	}
	if writerErr := writer.Err(); outputErr == nil && writerErr != nil {
		outputErr = writerErr
	}
	if opts.OutputFile != "" {
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			outputErr = errors.Join(outputErr, fmt.Errorf("marshal output file JSON: %w", err))
		} else if err := deps.writeReport(opts.OutputFile, data); err != nil {
			outputErr = errors.Join(outputErr, err)
		}
	}
	if resultErr != nil && outputErr != nil {
		var coded interface{ ExitCode() int }
		if errors.As(resultErr, &coded) {
			return &exitCodeError{code: coded.ExitCode(), err: errors.Join(resultErr, outputErr)}
		}
	}
	return errors.Join(resultErr, outputErr)
}
