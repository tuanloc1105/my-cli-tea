// Package request provides HTTP request execution and body preparation
// functionality for the API stress test tool.
package request

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// maxResponseDrain is the maximum number of bytes to read/drain from a response
// body. Set to 4 MB to handle larger API responses while still bounding memory.
const maxResponseDrain = 4 << 20

// Result holds the result of a single HTTP request execution.
// It contains the request outcome, status code, latency, and any error information.
type Result struct {
	OK           bool    // true if status code is 2xx
	StatusCode   int     // HTTP status code (0 if request failed)
	Elapsed      float64 // Request duration in seconds
	Error        string  // Error message if request failed
	ResponseSize int64   // Response body size in bytes
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
//   1. JSON body (from file or string) - validates JSON and sets Content-Type to application/json
//   2. Form data - encodes as application/x-www-form-urlencoded
//   3. Raw body (from file or string) - uses provided Content-Type or defaults to text/plain
// Returns the body bytes, content type, and any error encountered during processing.
func PrepareBody(
	jsonBody string, jsonFile string,
	formData map[string]string,
	rawBody string, rawFile string,
	contentTypeFlag string,
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
		ct := contentTypeFlag
		if ct == "" {
			ct = "text/plain"
		}
		return data, ct, nil
	}

	if rawBody != "" {
		ct := contentTypeFlag
		if ct == "" {
			ct = "text/plain"
		}
		return []byte(rawBody), ct, nil
	}

	return nil, "", nil
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
	startedAt := time.Now()

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), targetURL, reqBody)
	if err != nil {
		return Result{
			OK:      false,
			Elapsed: time.Since(startedAt).Seconds(),
			Error:   fmt.Sprintf("failed to create request: %v", err),
		}
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := client.Do(req)
	elapsed := time.Since(startedAt).Seconds()

	if err != nil {
		return Result{
			OK:      false,
			Elapsed: elapsed,
			Error:   normalizeError(err.Error()),
		}
	}
	defer resp.Body.Close()

	// Read limited body for validation or drain for connection reuse
	var respBody []byte
	var responseSize int64
	if expectBody != "" {
		respBody, _ = io.ReadAll(io.LimitReader(resp.Body, maxResponseDrain))
		responseSize = int64(len(respBody))
	} else {
		responseSize, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxResponseDrain))
	}

	statusCode := resp.StatusCode

	// Determine success
	var ok bool
	var errMsg string
	if expectStatus > 0 {
		ok = statusCode == expectStatus
		if !ok {
			errMsg = fmt.Sprintf("expected status %d, got %d", expectStatus, statusCode)
		}
	} else {
		ok = statusCode >= 200 && statusCode < 300
	}

	if ok && expectBody != "" {
		if !strings.Contains(string(respBody), expectBody) {
			ok = false
			if responseSize >= maxResponseDrain {
				errMsg = fmt.Sprintf("response body missing expected content (body truncated at %d bytes)", maxResponseDrain)
			} else {
				errMsg = "response body missing expected content"
			}
		}
	}

	return Result{
		OK:           ok,
		StatusCode:   statusCode,
		Elapsed:      elapsed,
		Error:        errMsg,
		ResponseSize: responseSize,
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
