// Package request provides HTTP request execution and body preparation
// functionality for the API stress test tool.
package request

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// ErrorKind classifies request failures independently from their display text.
type ErrorKind string

const (
	ErrorKindNone           ErrorKind = ""
	ErrorKindTransport      ErrorKind = "transport"
	ErrorKindTimeout        ErrorKind = "timeout"
	ErrorKindCancellation   ErrorKind = "cancellation"
	ErrorKindBodyRead       ErrorKind = "body_read"
	ErrorKindBodyClose      ErrorKind = "body_close"
	ErrorKindExpectation    ErrorKind = "expectation"
	ErrorKindRecoveredPanic ErrorKind = "recovered_panic"
)

// Result holds the result of a single HTTP request execution.
// It contains the request outcome, status code, latency, and any error information.
type Result struct {
	OK           bool      // true if status and body expectations passed
	StatusCode   int       // HTTP status code (0 if request failed before headers)
	Elapsed      float64   // Total request duration through response body completion
	TTFB         float64   // Time to response headers in seconds
	CompletedAt  time.Time // Time response processing completed
	Error        string    // Error message if request failed
	ErrorKind    ErrorKind // Structured failure category
	ResponseSize int64     // Decoded response body bytes read
}

// ParseHeaders parses HTTP headers from a semicolon-separated string format.
// Expected format: 'key1:value1;key2:value2'
// Semicolons are used as delimiters to allow commas in header values
// (e.g., 'Accept:text/html,application/json;Authorization:Bearer token').
// Returns an empty map if the input string is empty.
// Invalid entries (missing colon) are silently skipped.
func ParseHeaders(raw string) map[string]string {
	headers := make(map[string]string)
	if raw == "" {
		return headers
	}

	parts := strings.Split(raw, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		idx := strings.Index(part, ":")
		if idx == -1 {
			continue
		}

		key := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])
		if key != "" {
			headers[key] = value
		}
	}

	return headers
}

// ParseData parses form data from URL-encoded string format.
// Expected format: 'key1=value1&key2=value2'
// Returns nil, nil if the input string is empty.
// Returns an error if any entry is missing the '=' separator.
func ParseData(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, nil
	}

	data := make(map[string]string)
	parts := strings.Split(raw, "&")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		idx := strings.Index(part, "=")
		if idx == -1 {
			return nil, fmt.Errorf("invalid form data entry (missing '='): %q", part)
		}

		key := strings.TrimSpace(part[:idx])
		value := strings.TrimSpace(part[idx+1:])
		if key != "" {
			data[key] = value
		}
	}

	if len(data) == 0 {
		return nil, nil
	}

	return data, nil
}

// PrepareBody prepares the HTTP request body and determines the Content-Type header.
// It processes body sources in the following priority order:
//  1. JSON body (from file or string) - validates JSON and sets Content-Type to application/json
//  2. Form data - encodes as application/x-www-form-urlencoded
//  3. Raw body (from file or string) - uses provided Content-Type or defaults to text/plain
//
// Returns the body bytes, content type, and any error encountered during processing.
func PrepareBody(
	jsonBody string, jsonFile string,
	formData map[string]string,
	rawBody string, rawFile string,
	contentTypeFlag string,
) ([]byte, string, error) {
	body, inferredType, err := prepareBody(jsonBody, jsonFile, formData, rawBody, rawFile)
	if err != nil {
		return nil, "", err
	}
	return body, ResolveContentType(contentTypeFlag, nil, inferredType), nil
}

// PrepareBodyWithHeaders prepares a body and applies Content-Type precedence:
// explicit flag, parsed header, then body-type inference.
func PrepareBodyWithHeaders(
	jsonBody string, jsonFile string,
	formData map[string]string,
	rawBody string, rawFile string,
	contentTypeFlag string,
	headers map[string]string,
) ([]byte, string, error) {
	body, inferredType, err := prepareBody(jsonBody, jsonFile, formData, rawBody, rawFile)
	if err != nil {
		return nil, "", err
	}
	return body, ResolveContentType(contentTypeFlag, headers, inferredType), nil
}

func prepareBody(
	jsonBody string, jsonFile string,
	formData map[string]string,
	rawBody string, rawFile string,
) ([]byte, string, error) {
	// Priority 1: JSON body (highest priority, includes validation)
	// JSON from file takes precedence over JSON string
	if jsonFile != "" {
		data, err := os.ReadFile(jsonFile)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read JSON file: %w", err)
		}
		if !json.Valid(data) {
			return nil, "", fmt.Errorf("invalid JSON in file")
		}
		return data, "application/json", nil
	}

	if jsonBody != "" {
		data := []byte(strings.TrimSpace(jsonBody))
		if !json.Valid(data) {
			return nil, "", fmt.Errorf("invalid JSON string")
		}
		return data, "application/json", nil
	}

	// Priority 2: Form data (URL-encoded)
	if formData != nil {
		values := url.Values{}
		for k, v := range formData {
			values.Set(k, v)
		}
		return []byte(values.Encode()), "application/x-www-form-urlencoded", nil
	}

	// Priority 3: Raw body content (file or string)
	// Raw file takes precedence over raw string
	if rawFile != "" {
		data, err := os.ReadFile(rawFile)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read file: %w", err)
		}
		return data, "text/plain", nil
	}

	if rawBody != "" {
		return []byte(rawBody), "text/plain", nil
	}

	return nil, "", nil
}

// ResolveContentType applies explicit flag, parsed header, then inference.
func ResolveContentType(explicit string, headers map[string]string, inferred string) string {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit
	}
	for key, value := range headers {
		if strings.EqualFold(key, "Content-Type") {
			if value = strings.TrimSpace(value); value != "" {
				return value
			}
		}
	}
	return inferred
}

// ExecuteRequest executes a single HTTP request and measures its performance.
// expectStatus > 0 means only that specific status counts as success.
// expectBody non-empty means the response body must contain that substring.
func ExecuteRequest(
	ctx context.Context,
	client *http.Client,
	method, targetURL string,
	headers map[string]string,
	body []byte,
	contentType string,
	expectStatus int,
	expectBody string,
) Result {
	return ExecuteRequestWithMatcher(
		ctx, client, method, targetURL, headers, body, contentType,
		expectStatus, PrepareBodyMatcher(expectBody),
	)
}

// ExecuteRequestWithMatcher executes a request using a reusable body matcher.
func ExecuteRequestWithMatcher(
	ctx context.Context,
	client *http.Client,
	method, targetURL string,
	headers map[string]string,
	body []byte,
	contentType string,
	expectStatus int,
	expectBody *BodyMatcher,
) Result {
	startedAt := time.Now()
	result := Result{}
	finish := func() Result {
		result.CompletedAt = time.Now()
		result.Elapsed = result.CompletedAt.Sub(startedAt).Seconds()
		return result
	}

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), targetURL, reqBody)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		result.ErrorKind = classifyRequestError(err, ErrorKindTransport)
		return finish()
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := client.Do(req)
	if err != nil {
		result.Error = normalizeError(err.Error())
		result.ErrorKind = classifyRequestError(err, ErrorKindTransport)
		return finish()
	}
	result.TTFB = time.Since(startedAt).Seconds()
	result.StatusCode = resp.StatusCode

	matchState := expectBody.newState()
	readErr := observeResponseBody(resp.Body, &result.ResponseSize, &matchState)
	closeErr := resp.Body.Close()
	if readErr != nil {
		result.Error = normalizeError(readErr.Error())
		result.ErrorKind = classifyRequestError(readErr, ErrorKindBodyRead)
		return finish()
	}
	if closeErr != nil {
		result.Error = normalizeError(closeErr.Error())
		result.ErrorKind = classifyRequestError(closeErr, ErrorKindBodyClose)
		return finish()
	}

	// Determine success
	if expectStatus > 0 {
		result.OK = result.StatusCode == expectStatus
		if !result.OK {
			result.Error = fmt.Sprintf("expected status %d, got %d", expectStatus, result.StatusCode)
			result.ErrorKind = ErrorKindExpectation
		}
	} else {
		result.OK = result.StatusCode >= 200 && result.StatusCode < 300
	}

	if result.OK && expectBody != nil && !matchState.found {
		result.OK = false
		result.Error = "response body missing expected content"
		result.ErrorKind = ErrorKindExpectation
	}

	return finish()
}

func observeResponseBody(body io.Reader, size *int64, matcher *bodyMatchState) error {
	buffer := make([]byte, 32<<10)
	for {
		n, err := body.Read(buffer)
		if n > 0 {
			*size += int64(n)
			matcher.observe(buffer[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if n == 0 {
			continue
		}
	}
}

func classifyRequestError(err error, fallback ErrorKind) ErrorKind {
	switch {
	case errors.Is(err, context.Canceled):
		return ErrorKindCancellation
	case errors.Is(err, context.DeadlineExceeded):
		return ErrorKindTimeout
	default:
		var timeout interface{ Timeout() bool }
		if errors.As(err, &timeout) && timeout.Timeout() {
			return ErrorKindTimeout
		}
		return fallback
	}
}

// normalizeError maps verbose Go HTTP error messages to concise categories
// for better grouping in the Top Errors output.
func normalizeError(msg string) string {
	switch {
	case strings.Contains(msg, "context deadline exceeded"):
		return "request timeout"
	case strings.Contains(msg, "context canceled"):
		return "request cancelled"
	case strings.Contains(msg, "connection refused"):
		return "connection refused"
	case strings.Contains(msg, "no such host"):
		return "DNS resolution failed"
	case strings.Contains(msg, "connection reset"):
		return "connection reset"
	case strings.Contains(msg, "EOF"):
		return "connection closed (EOF)"
	case strings.Contains(msg, "TLS handshake"):
		return "TLS handshake failed"
	default:
		if len(msg) > 80 {
			return msg[:80] + "..."
		}
		return msg
	}
}
