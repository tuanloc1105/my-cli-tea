// Package cmd provides the command-line interface and test execution logic
// for the API stress test tool.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"
	"time"

	"api-stress-test/internal/request"

	"github.com/spf13/cobra"
)

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
	ShutdownGrace    *time.Duration
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
	shutdownGrace    string
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
		_, _ = fmt.Fprintf(stderr, "Error: %v\n", err)
		var coded interface{ ExitCode() int }
		if errors.As(err, &coded) {
			return coded.ExitCode()
		}
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
			timeout, err := durationFromSeconds("timeout", options.timeout)
			if err != nil {
				return err
			}
			if err := validateRate(options.rate, cmd.Flags().Changed("rate")); err != nil {
				return err
			}
			duration, err := parsePositiveDuration("duration", options.duration)
			if err != nil {
				return err
			}
			warmup, err := parsePositiveDuration("warmup", options.warmup)
			if err != nil {
				return err
			}
			shutdownGrace, err := parseShutdownGrace(options.shutdownGrace)
			if err != nil {
				return err
			}

			headers := request.ParseHeaders(options.headers)
			data, err := request.ParseData(options.data)
			if err != nil {
				return fmt.Errorf("parsing --data: %w", err)
			}
			body, contentType, err := request.PrepareBodyWithHeaders(
				options.jsonBody, options.jsonFile, data, options.rawBody,
				options.rawFile, options.contentTypeFlag, headers,
			)
			if err != nil {
				return fmt.Errorf("preparing body: %w", err)
			}

			opts := normalizeStressTestOptions(StressTestOptions{
				Writer:           stdout,
				TargetURL:        options.targetURL,
				Method:           options.method,
				TotalRequests:    options.requests,
				Concurrency:      options.concurrency,
				Timeout:          timeout,
				Headers:          headers,
				Body:             body,
				ContentType:      contentType,
				Rate:             options.rate,
				Duration:         duration,
				OutputFormat:     options.outputFormat,
				Insecure:         options.insecure,
				DisableKeepalive: options.disableKeepalive,
				DisableRedirects: options.disableRedirects,
				ExpectStatus:     options.expectStatus,
				ExpectBody:       options.expectBody,
				Warmup:           warmup,
				ShutdownGrace:    shutdownGrace,
				OutputFile:       options.outputFile,
				Proxy:            options.proxy,
			})
			if err := validateStressTestOptions(opts); err != nil {
				return err
			}
			return run(cmd.Context(), opts)
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
	flags.StringVar(&options.shutdownGrace, "shutdown-grace", "", "Grace period for active requests after scheduling stops (default: request timeout)")
	flags.StringVar(&options.outputFormat, "output", "text", "Output format: text or json")
	flags.StringVar(&options.outputFile, "output-file", "", "Write JSON results to file (works with any output format)")
	root.MarkFlagsMutuallyExclusive("data", "json-body", "json-file", "body", "file")
	root.MarkFlagsMutuallyExclusive("requests", "duration")
	return root
}

func durationFromSeconds(name string, seconds float64) (time.Duration, error) {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return 0, fmt.Errorf("%s must be positive and finite (got %v)", name, seconds)
	}
	nanoseconds := seconds * float64(time.Second)
	if nanoseconds < 1 || nanoseconds > float64(math.MaxInt64) {
		return 0, fmt.Errorf("%s is outside the representable duration range (got %v)", name, seconds)
	}
	duration := time.Duration(nanoseconds)
	if duration <= 0 {
		return 0, fmt.Errorf("%s is outside the representable duration range (got %v)", name, seconds)
	}
	return duration, nil
}

func validateRate(rate float64, specified bool) error {
	if !specified && rate == 0 {
		return nil
	}
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
		return fmt.Errorf("rate must be positive and finite when specified (got %v)", rate)
	}
	intervalNanoseconds := float64(time.Second) / rate
	if intervalNanoseconds < 1 || intervalNanoseconds > float64(math.MaxInt64) {
		return fmt.Errorf("rate produces an unrepresentable interval (got %v)", rate)
	}
	return nil
}

func parsePositiveDuration(name, value string) (time.Duration, error) {
	if value == "" {
		return 0, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("invalid %s duration: %w", name, err)
	}
	if duration <= 0 {
		return 0, fmt.Errorf("%s duration must be positive (got %s)", name, value)
	}
	return duration, nil
}

func parseShutdownGrace(value string) (*time.Duration, error) {
	if value == "" {
		return nil, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return nil, fmt.Errorf("invalid shutdown grace duration: %w", err)
	}
	if duration < 0 {
		return nil, fmt.Errorf("shutdown grace must not be negative (got %s)", value)
	}
	return &duration, nil
}

func normalizeStressTestOptions(opts StressTestOptions) StressTestOptions {
	if opts.TotalRequests <= 0 {
		opts.TotalRequests = 100
	}
	if opts.Concurrency <= 0 {
		opts.Concurrency = 10
	}
	opts.Method = strings.ToUpper(opts.Method)
	return opts
}

func validateStressTestOptions(opts StressTestOptions) error {
	if err := ValidateURL(opts.TargetURL); err != nil {
		return err
	}
	if err := ValidateMethod(opts.Method); err != nil {
		return err
	}
	if opts.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive (got %s)", opts.Timeout)
	}
	if err := validateRate(opts.Rate, opts.Rate != 0); err != nil {
		return err
	}
	if opts.Duration < 0 {
		return fmt.Errorf("duration must not be negative (got %s)", opts.Duration)
	}
	if opts.Warmup < 0 {
		return fmt.Errorf("warmup must not be negative (got %s)", opts.Warmup)
	}
	if opts.ShutdownGrace != nil && *opts.ShutdownGrace < 0 {
		return fmt.Errorf("shutdown grace must not be negative (got %s)", *opts.ShutdownGrace)
	}
	if opts.Concurrency > 10000 {
		return fmt.Errorf("concurrency too high: %d (max 10000)", opts.Concurrency)
	}
	if opts.ExpectStatus != 0 && (opts.ExpectStatus < 100 || opts.ExpectStatus > 999) {
		return fmt.Errorf("expected status must be 0 or between 100 and 999 (got %d)", opts.ExpectStatus)
	}
	if opts.OutputFormat != "text" && opts.OutputFormat != "json" {
		return fmt.Errorf("unsupported output format: %s (supported: text, json)", opts.OutputFormat)
	}
	if opts.Proxy != "" {
		proxyURL, err := url.Parse(opts.Proxy)
		if err != nil {
			return fmt.Errorf("invalid proxy URL: %w", err)
		}
		switch proxyURL.Scheme {
		case "http", "https", "socks5", "socks5h":
		default:
			return fmt.Errorf("unsupported proxy URL scheme: %s", proxyURL.Scheme)
		}
		if proxyURL.Host == "" {
			return fmt.Errorf("proxy URL must contain a host")
		}
	}
	return nil
}

// RunStressTest runs the HTTP stress test and returns an error if it fails.
func RunStressTest(opts StressTestOptions) error {
	return runStressTest(context.Background(), opts)
}

func runStressTest(ctx context.Context, opts StressTestOptions) error {
	return runStressTestWithDependencies(ctx, opts, defaultRuntimeDependencies())
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
