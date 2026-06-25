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

// Execute sets up the Cobra root command and runs the CLI.
func Execute() {
	var (
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
	)

	rootCmd := &cobra.Command{
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
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := ValidateURL(targetURL); err != nil {
				return err
			}
			if err := ValidateMethod(method); err != nil {
				return err
			}
			if outputFormat != "text" && outputFormat != "json" {
				return fmt.Errorf("unsupported output format: %s (supported: text, json)", outputFormat)
			}

			parsedHeaders := request.ParseHeaders(headers)

			parsedData, err := request.ParseData(data)
			if err != nil {
				return fmt.Errorf("parsing --data: %w", err)
			}

			body, contentType, err := request.PrepareBody(jsonBody, jsonFile, parsedData, rawBody, rawFile, contentTypeFlag)
			if err != nil {
				return fmt.Errorf("preparing body: %w", err)
			}

			if timeout <= 0 {
				return fmt.Errorf("timeout must be positive (got %.2f)", timeout)
			}
			if cmd.Flags().Changed("rate") && rate <= 0 {
				return fmt.Errorf("rate must be positive when specified (got %.2f)", rate)
			}
			if concurrency > 10000 {
				return fmt.Errorf("concurrency too high: %d (max 10000)", concurrency)
			}

			if requests <= 0 {
				requests = 100
			}
			if concurrency <= 0 {
				concurrency = 10
			}

			var dur time.Duration
			if duration != "" {
				dur, err = time.ParseDuration(duration)
				if err != nil {
					return fmt.Errorf("invalid duration: %w", err)
				}
			}

			var warmupDur time.Duration
			if warmup != "" {
				warmupDur, err = time.ParseDuration(warmup)
				if err != nil {
					return fmt.Errorf("invalid warmup duration: %w", err)
				}
			}

			return RunStressTest(StressTestOptions{
				Writer:           os.Stdout,
				TargetURL:        targetURL,
				Method:           strings.ToUpper(method),
				TotalRequests:    requests,
				Concurrency:      concurrency,
				Timeout:          time.Duration(timeout * float64(time.Second)),
				Headers:          parsedHeaders,
				Body:             body,
				ContentType:      contentType,
				Rate:             rate,
				Duration:         dur,
				OutputFormat:     outputFormat,
				Insecure:         insecure,
				DisableKeepalive: disableKeepalive,
				DisableRedirects: disableRedirects,
				ExpectStatus:     expectStatus,
				ExpectBody:       expectBody,
				Warmup:           warmupDur,
				OutputFile:       outputFile,
				Proxy:            proxy,
			})
		},
	}

	// Required flag
	rootCmd.Flags().StringVar(&targetURL, "url", "", "Target URL (required)")
	_ = rootCmd.MarkFlagRequired("url")

	// Request options
	rootCmd.Flags().StringVar(&method, "method", "GET", "HTTP method (GET, POST, PUT, DELETE, PATCH, HEAD, OPTIONS)")
	rootCmd.Flags().IntVar(&requests, "requests", 100, "Total requests to send")
	rootCmd.Flags().IntVar(&concurrency, "concurrency", 10, "Number of concurrent workers")
	rootCmd.Flags().Float64Var(&timeout, "timeout", 5.0, "Timeout per request in seconds")
	rootCmd.Flags().StringVar(&headers, "headers", "", "Headers in 'key1:value1;key2:value2' format (semicolon-delimited; values may contain commas but not semicolons)")
	rootCmd.Flags().StringVar(&data, "data", "", "Form data in 'key1=value1&key2=value2' format")
	rootCmd.Flags().StringVar(&jsonBody, "json-body", "", "JSON body string")
	rootCmd.Flags().StringVar(&jsonFile, "json-file", "", "Path to JSON file for body")
	rootCmd.Flags().StringVar(&rawBody, "body", "", "Raw body string")
	rootCmd.Flags().StringVar(&rawFile, "file", "", "Path to file for body")
	rootCmd.Flags().StringVar(&contentTypeFlag, "content-type", "", "Explicit Content-Type header")

	// Load control
	rootCmd.Flags().Float64Var(&rate, "rate", 0, "Max requests per second (0 = unlimited)")
	rootCmd.Flags().StringVar(&duration, "duration", "", "Test duration (e.g., 30s, 1m) instead of fixed request count")

	// Transport tuning
	rootCmd.Flags().BoolVarP(&insecure, "insecure", "k", false, "Skip TLS certificate verification")
	rootCmd.Flags().BoolVar(&disableKeepalive, "disable-keepalive", false, "Disable HTTP keep-alive (new connection per request)")
	rootCmd.Flags().BoolVar(&disableRedirects, "disable-redirects", false, "Do not follow HTTP redirects")

	// Proxy
	rootCmd.Flags().StringVar(&proxy, "proxy", "", "HTTP proxy URL (e.g., http://proxy:8080)")

	// Response validation
	rootCmd.Flags().IntVar(&expectStatus, "expect-status", 0, "Expected HTTP status code (others count as failure)")
	rootCmd.Flags().StringVar(&expectBody, "expect-body", "", "Expected substring in response body")

	// Warm-up
	rootCmd.Flags().StringVar(&warmup, "warmup", "", "Warm-up duration before recording stats (e.g., 5s)")

	// Output
	rootCmd.Flags().StringVar(&outputFormat, "output", "text", "Output format: text or json")
	rootCmd.Flags().StringVar(&outputFile, "output-file", "", "Write JSON results to file (works with any output format)")

	// Mutual exclusivity
	rootCmd.MarkFlagsMutuallyExclusive("data", "json-body", "json-file", "body", "file")
	rootCmd.MarkFlagsMutuallyExclusive("requests", "duration")

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// RunStressTest runs the HTTP stress test and returns an error if there are failures.
// Output is written to opts.Writer; pass os.Stdout for normal CLI usage.
func RunStressTest(opts StressTestOptions) error {
	w := opts.Writer
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

	// Setup signal handling once for the entire test lifecycle
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	// Run warm-up phase (requests without recording stats)
	if opts.Warmup > 0 {
		if !isJSON {
			fmt.Fprintf(w, "Warming up for %s...\n", opts.Warmup)
		}
		warmCtx, warmCancel := context.WithTimeout(context.Background(), opts.Warmup)
		defer warmCancel()

		// Signal listener for warmup phase
		warmDone := make(chan struct{})
		go func() {
			defer close(warmDone)
			select {
			case sig := <-sigChan:
				warmCancel()
				// Re-enqueue so the main phase goroutine can also receive it
				select {
				case sigChan <- sig:
				default:
				}
			case <-warmCtx.Done():
			}
		}()

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
		warmCancel()   // Ensure warmCtx is done
		<-warmDone     // Wait for signal goroutine to exit before starting main phase

		if !isJSON {
			fmt.Fprintln(w, "Warm-up complete. Starting test...")
			fmt.Fprintln(w, strings.Repeat("-", 60))
		}
	}

	// Setup context with graceful shutdown
	var ctx context.Context
	var cancel context.CancelFunc
	if isDurationMode {
		ctx, cancel = context.WithTimeout(context.Background(), opts.Duration)
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}
	defer cancel()

	go func() {
		select {
		case <-sigChan:
			if !isJSON {
				fmt.Fprintln(w, "\nStopping requests... (waiting for active workers to finish)")
			}
			cancel()
		case <-ctx.Done():
		}
	}()

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
				if ctx.Err() != nil {
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
					results <- request.ExecuteRequest(ctx, client, opts.Method, opts.TargetURL, opts.Headers, opts.Body, opts.ContentType, opts.ExpectStatus, opts.ExpectBody)
				}()
			}
		}()
	}

	// Feed jobs
	go func() {
		defer close(jobs)
		if isDurationMode {
			for {
				if !limiter.Wait(ctx) {
					return
				}
				select {
				case jobs <- struct{}{}:
				case <-ctx.Done():
					return
				}
			}
		} else {
			for i := 0; i < opts.TotalRequests; i++ {
				if !limiter.Wait(ctx) {
					return
				}
				select {
				case jobs <- struct{}{}:
				case <-ctx.Done():
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
