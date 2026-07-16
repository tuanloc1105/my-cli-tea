// Package cmd provides the command-line interface and test execution logic
// for the API stress test tool.
package cmd

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"api-stress-test/internal/request"
	"api-stress-test/internal/stats"
	"api-stress-test/internal/ui"

	"github.com/spf13/cobra"
)

// validMethods defines accepted HTTP methods.
var validMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "DELETE": true,
	"PATCH": true, "HEAD": true, "OPTIONS": true,
}

// StressTestOptions holds all configuration for running a stress test.
type StressTestOptions struct {
	Writer           io.Writer
	TargetURL        string
	Method           string
	TotalRequests    int
	Concurrency      int
	Timeout          time.Duration
	Headers          map[string]string
	Body             []byte
	ContentType      string
	Rate             float64
	Duration         time.Duration
	OutputFormat     string
	Insecure         bool
	DisableKeepalive bool
	DisableRedirects bool
	ExpectStatus     int
	ExpectBody       string
	Warmup           time.Duration
	OutputFile       string
	Proxy            string
}

type runner func(context.Context, StressTestOptions) error

type commandOptions struct {
	targetURL        string
	method           string
	requests         int
	concurrency      int
	timeout          float64
	headers          string
	data             string
	jsonBody         string
	jsonFile         string
	rawBody          string
	rawFile          string
	contentTypeFlag  string
	rate             float64
	duration         string
	outputFormat     string
	insecure         bool
	disableKeepalive bool
	disableRedirects bool
	expectStatus     int
	expectBody       string
	warmup           string
	outputFile       string
	proxy            string
}

// ExecuteContext runs api-stress-test with the supplied process arguments and
// streams. It returns the process exit code without terminating the caller.
func ExecuteContext(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return executeContext(ctx, args, stdout, stderr, runStressTest)
}

func executeContext(ctx context.Context, args []string, stdout, stderr io.Writer, run runner) int {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	if args == nil {
		args = []string{}
	}

	root := newRootCommand(run, stdout, stderr)
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func newRootCommand(run runner, stdout, stderr io.Writer) *cobra.Command {
	options := commandOptions{
		method:       "GET",
		requests:     100,
		concurrency:  10,
		timeout:      5,
		outputFormat: "text",
	}

	root := &cobra.Command{
		Use:   "api-stress-test",
		Short: "HTTP load/stress testing tool",
		Long:  "A CLI tool for HTTP load and stress testing with concurrent workers, latency percentiles, and detailed statistics.",
		Example: `  api-stress-test --url http://example.com/api --requests 1000 --concurrency 50
  api-stress-test --url http://example.com/api --method POST --json-body '{"key":"value"}'
  api-stress-test --url http://example.com/api --headers "Authorization:Bearer token;Accept:application/json"
  api-stress-test --url http://example.com/api --duration 30s --concurrency 20
  api-stress-test --url http://example.com/api --requests 500 --rate 50
  api-stress-test --url http://example.com/api --requests 100 --output json
  api-stress-test --url https://example.com/api --insecure --expect-status 200
  api-stress-test --url http://example.com/api --requests 50 --output-file result.json
  api-stress-test --url http://example.com/api --requests 50 --proxy http://proxy:8080`,
		Args:          cobra.ArbitraryArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if run == nil {
				return fmt.Errorf("stress test runner is nil")
			}
			if err := ValidateURL(options.targetURL); err != nil {
				return err
			}
			if err := ValidateMethod(options.method); err != nil {
				return err
			}
			if options.outputFormat != "text" && options.outputFormat != "json" {
				return fmt.Errorf("unsupported output format: %s (supported: text, json)", options.outputFormat)
			}

			parsedHeaders := request.ParseHeaders(options.headers)

			parsedData, err := request.ParseData(options.data)
			if err != nil {
				return fmt.Errorf("parsing --data: %w", err)
			}

			body, contentType, err := request.PrepareBody(options.jsonBody, options.jsonFile, parsedData, options.rawBody, options.rawFile, options.contentTypeFlag)
			if err != nil {
				return fmt.Errorf("preparing body: %w", err)
			}

			if options.timeout <= 0 {
				return fmt.Errorf("timeout must be positive (got %.2f)", options.timeout)
			}
			if cmd.Flags().Changed("rate") && options.rate <= 0 {
				return fmt.Errorf("rate must be positive when specified (got %.2f)", options.rate)
			}
			if options.concurrency > 10000 {
				return fmt.Errorf("concurrency too high: %d (max 10000)", options.concurrency)
			}

			if options.requests <= 0 {
				options.requests = 100
			}
			if options.concurrency <= 0 {
				options.concurrency = 10
			}

			var dur time.Duration
			if options.duration != "" {
				dur, err = time.ParseDuration(options.duration)
				if err != nil {
					return fmt.Errorf("invalid duration: %w", err)
				}
			}

			var warmupDur time.Duration
			if options.warmup != "" {
				warmupDur, err = time.ParseDuration(options.warmup)
				if err != nil {
					return fmt.Errorf("invalid warmup duration: %w", err)
				}
			}

			return run(cmd.Context(), StressTestOptions{
				Writer:           stdout,
				TargetURL:        options.targetURL,
				Method:           strings.ToUpper(options.method),
				TotalRequests:    options.requests,
				Concurrency:      options.concurrency,
				Timeout:          time.Duration(options.timeout * float64(time.Second)),
				Headers:          parsedHeaders,
				Body:             body,
				ContentType:      contentType,
				Rate:             options.rate,
				Duration:         dur,
				OutputFormat:     options.outputFormat,
				Insecure:         options.insecure,
				DisableKeepalive: options.disableKeepalive,
				DisableRedirects: options.disableRedirects,
				ExpectStatus:     options.expectStatus,
				ExpectBody:       options.expectBody,
				Warmup:           warmupDur,
				OutputFile:       options.outputFile,
				Proxy:            options.proxy,
			})
		},
	}
	root.SetOut(stdout)
	root.SetErr(stderr)

	flags := root.Flags()
	flags.StringVar(&options.targetURL, "url", "", "Target URL (required)")
	_ = root.MarkFlagRequired("url")

	flags.StringVar(&options.method, "method", "GET", "HTTP method (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)")
	flags.IntVar(&options.requests, "requests", 100, "Total requests to send")
	flags.IntVar(&options.concurrency, "concurrency", 10, "Number of concurrent workers")
	flags.Float64Var(&options.timeout, "timeout", 5.0, "Timeout per request in seconds")
	flags.StringVar(&options.headers, "headers", "", "Headers in 'key1:value1;key2:value2' format (semicolon-delimited; values may contain commas but not semicolons)")
	flags.StringVar(&options.data, "data", "", "Form data in 'key1=value1&key2=value2' format")
	flags.StringVar(&options.jsonBody, "json-body", "", "JSON body string")
	flags.StringVar(&options.jsonFile, "json-file", "", "Path to JSON file for body")
	flags.StringVar(&options.rawBody, "body", "", "Raw body string")
	flags.StringVar(&options.rawFile, "file", "", "Path to file for body")
	flags.StringVar(&options.contentTypeFlag, "content-type", "", "Explicit Content-Type header")

	flags.Float64Var(&options.rate, "rate", 0, "Max requests per second (0 = unlimited)")
	flags.StringVar(&options.duration, "duration", "", "Test duration (e.g., 30s, 1m) instead of fixed request count")

	flags.BoolVarP(&options.insecure, "insecure", "k", false, "Skip TLS certificate verification")
	flags.BoolVar(&options.disableKeepalive, "disable-keepalive", false, "Disable HTTP keep-alive (new connection per request)")
	flags.BoolVar(&options.disableRedirects, "disable-redirects", false, "Do not follow HTTP redirects")

	flags.StringVar(&options.proxy, "proxy", "", "HTTP proxy URL (e.g., http://proxy:8080)")

	flags.IntVar(&options.expectStatus, "expect-status", 0, "Expected HTTP status code (others count as failure)")
	flags.StringVar(&options.expectBody, "expect-body", "", "Expected substring in response body")

	flags.StringVar(&options.warmup, "warmup", "", "Warm-up duration before recording stats (e.g., 5s)")

	flags.StringVar(&options.outputFormat, "output", "text", "Output format: text or json")
	flags.StringVar(&options.outputFile, "output-file", "", "Write JSON results to file (works with any output format)")

	root.MarkFlagsMutuallyExclusive("data", "json-body", "json-file", "body", "file")
	root.MarkFlagsMutuallyExclusive("requests", "duration")

	return root
}

// RunStressTest runs the HTTP stress test and returns an error if there are failures.
// Output is written to opts.Writer; pass os.Stdout for normal CLI usage.
func RunStressTest(opts StressTestOptions) error {
	return runStressTest(context.Background(), opts)
}

func runStressTest(ctx context.Context, opts StressTestOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	w := opts.Writer
	if w == nil {
		w = io.Discard
	}
	isJSON := opts.OutputFormat == "json"
	isDurationMode := opts.Duration > 0

	if !isJSON {
		durationStr := ""
		if isDurationMode {
			durationStr = opts.Duration.String()
		}
		ui.PrintHeader(w, ui.HeaderConfig{
			URL:            opts.TargetURL,
			Method:         opts.Method,
			TotalRequests:  opts.TotalRequests,
			Concurrency:    opts.Concurrency,
			TimeoutSec:     opts.Timeout.Seconds(),
			Rate:           opts.Rate,
			IsDurationMode: isDurationMode,
			Duration:       durationStr,
			BodyLen:        len(opts.Body),
			ContentType:    opts.ContentType,
		})
	}

	// Configure HTTP Transport
	transport := &http.Transport{
		MaxIdleConns:        opts.Concurrency,
		MaxIdleConnsPerHost: opts.Concurrency,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   opts.DisableKeepalive,
	}
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

	client := &http.Client{
		Transport: transport,
		Timeout:   opts.Timeout,
	}
	if opts.DisableRedirects {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	// Derive signal handling from the caller's context for the full lifecycle.
	signalCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	completed := make(chan struct{})
	defer func() {
		close(completed)
		stopSignals()
	}()
	go func() {
		select {
		case <-signalCtx.Done():
			select {
			case <-completed:
				return
			default:
			}
			if ctx.Err() == nil && !isJSON {
				fmt.Fprintln(w, "\nStopping requests... (waiting for active workers to finish)")
			}
		case <-completed:
		}
	}()

	// Run warm-up phase (requests without recording stats)
	if opts.Warmup > 0 {
		if !isJSON {
			fmt.Fprintf(w, "Warming up for %s...\n", opts.Warmup)
		}
		warmCtx, warmCancel := context.WithTimeout(signalCtx, opts.Warmup)

		var warmWg sync.WaitGroup
		for i := 0; i < opts.Concurrency; i++ {
			warmWg.Add(1)
			go func() {
				defer warmWg.Done()
				for warmCtx.Err() == nil {
					res := request.ExecuteRequest(warmCtx, client, opts.Method, opts.TargetURL, opts.Headers, opts.Body, opts.ContentType, 0, "")
					if !res.OK && res.Elapsed < 0.01 {
						time.Sleep(10 * time.Millisecond)
					}
				}
			}()
		}
		warmWg.Wait()
		warmCancel()

		if !isJSON {
			fmt.Fprintln(w, "Warm-up complete. Starting test...")
			fmt.Fprintln(w, strings.Repeat("-", 60))
		}
	}

	// Setup context with graceful shutdown
	var testCtx context.Context
	var cancel context.CancelFunc
	if isDurationMode {
		testCtx, cancel = context.WithTimeout(signalCtx, opts.Duration)
	} else {
		testCtx, cancel = context.WithCancel(signalCtx)
	}
	defer cancel()

	startTime := time.Now()

	// Pre-allocate collector capacity
	initialCap := opts.TotalRequests
	if isDurationMode {
		initialCap = opts.Concurrency * 1000
	}
	collector := stats.NewCollector(initialCap)

	// Setup rate limiter
	limiter := request.NewRateLimiter(opts.Rate)
	defer limiter.Stop()

	// Setup live progress display
	var progress *ui.Progress
	if !isJSON {
		progress = ui.NewProgress(w, int64(opts.TotalRequests), isDurationMode, opts.Duration)
		progress.Start()
	}

	// Worker pool
	jobs := make(chan struct{}, opts.Concurrency*2)
	results := make(chan request.Result, opts.Concurrency*2)
	var wg sync.WaitGroup

	// Start workers
	for i := 0; i < opts.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range jobs {
				if testCtx.Err() != nil {
					return
				}
				func() {
					defer func() {
						if r := recover(); r != nil {
							results <- request.Result{
								OK:    false,
								Error: fmt.Sprintf("panic: %v", r),
							}
						}
					}()
					results <- request.ExecuteRequest(testCtx, client, opts.Method, opts.TargetURL, opts.Headers, opts.Body, opts.ContentType, opts.ExpectStatus, opts.ExpectBody)
				}()
			}
		}()
	}

	// Feed jobs
	go func() {
		defer close(jobs)
		if isDurationMode {
			for {
				if !limiter.Wait(testCtx) {
					return
				}
				select {
				case jobs <- struct{}{}:
				case <-testCtx.Done():
					return
				}
			}
		} else {
			for i := 0; i < opts.TotalRequests; i++ {
				if !limiter.Wait(testCtx) {
					return
				}
				select {
				case jobs <- struct{}{}:
				case <-testCtx.Done():
					return
				}
			}
		}
	}()

	// Close results when workers done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results
	batchSize := max(1, opts.Concurrency/2)
	batch := make([]request.Result, 0, batchSize)

	for res := range results {
		batch = append(batch, res)

		if len(batch) >= batchSize {
			for _, result := range batch {
				collector.Record(result.StatusCode, result.Elapsed, result.OK, result.Error, result.ResponseSize)
			}
			if progress != nil {
				progress.Add(int64(len(batch)))
			}
			batch = batch[:0]
		}
	}

	// Flush remaining batch
	if len(batch) > 0 {
		for _, result := range batch {
			collector.Record(result.StatusCode, result.Elapsed, result.OK, result.Error, result.ResponseSize)
		}
		if progress != nil {
			progress.Add(int64(len(batch)))
		}
	}

	// Stop progress display
	if progress != nil {
		progress.Stop()
	}

	totalTime := time.Since(startTime).Seconds()
	stat := collector.GetStatistics()

	if stat.Total == 0 {
		if !isJSON {
			fmt.Fprintln(w, "No requests were executed.")
		}
		return nil
	}

	var reqPerSec float64
	if totalTime > 0 {
		reqPerSec = float64(stat.Total) / totalTime
	}

	// Build JSON output (always needed for potential output-file)
	output := ui.JSONOutput{
		Config: ui.TestConfig{
			URL:         opts.TargetURL,
			Method:      opts.Method,
			Concurrency: opts.Concurrency,
			Timeout:     opts.Timeout.Seconds(),
		},
		Statistics: stat,
		TotalTime:  totalTime,
		ReqPerSec:  reqPerSec,
	}
	if isDurationMode {
		output.Config.Duration = opts.Duration.String()
	} else {
		output.Config.Requests = opts.TotalRequests
	}
	if opts.Rate > 0 {
		output.Config.Rate = opts.Rate
	}

	// Output results
	if isJSON {
		if err := ui.PrintJSONResult(w, output); err != nil {
			return err
		}
	} else {
		ui.PrintTextResult(w, stat, totalTime, reqPerSec)
	}

	// Write results to file if requested
	if opts.OutputFile != "" {
		jsonData, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output file JSON: %w", err)
		}
		if err := os.WriteFile(opts.OutputFile, jsonData, 0644); err != nil {
			return fmt.Errorf("failed to write output file: %w", err)
		}
	}

	if stat.Failures > 0 {
		return fmt.Errorf("%d out of %d requests failed", stat.Failures, stat.Total)
	}
	return nil
}

// ValidateURL validates that the URL is a valid HTTP/HTTPS URL.
func ValidateURL(urlStr string) error {
	if urlStr == "" {
		return fmt.Errorf("URL is required")
	}

	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		return fmt.Errorf("invalid URL format: %w", err)
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("URL scheme must be http or https")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("URL must contain a host")
	}

	return nil
}

// ValidateMethod validates that the HTTP method is supported.
func ValidateMethod(method string) error {
	if !validMethods[strings.ToUpper(method)] {
		return fmt.Errorf("unsupported HTTP method: %s (supported: GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)", method)
	}
	return nil
}
