package request

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseHeaders(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: map[string]string{},
		},
		{
			name:     "single header",
			input:    "Authorization:Bearer token",
			expected: map[string]string{"Authorization": "Bearer token"},
		},
		{
			name:     "multiple headers",
			input:    "Authorization:Bearer token;Content-Type:application/json",
			expected: map[string]string{"Authorization": "Bearer token", "Content-Type": "application/json"},
		},
		{
			name:     "value with commas preserved",
			input:    "Accept:text/html,application/json;Authorization:Bearer xyz",
			expected: map[string]string{"Accept": "text/html,application/json", "Authorization": "Bearer xyz"},
		},
		{
			name:     "whitespace trimmed",
			input:    " Authorization : Bearer token ; Accept : */* ",
			expected: map[string]string{"Authorization": "Bearer token", "Accept": "*/*"},
		},
		{
			name:     "entry without colon skipped",
			input:    "Authorization:Bearer token;invalidentry;Accept:*/*",
			expected: map[string]string{"Authorization": "Bearer token", "Accept": "*/*"},
		},
		{
			name:     "value with colons",
			input:    "X-Custom:a:b:c",
			expected: map[string]string{"X-Custom": "a:b:c"},
		},
		{
			name:     "empty entries skipped",
			input:    "Authorization:Bearer token;;Accept:*/*",
			expected: map[string]string{"Authorization": "Bearer token", "Accept": "*/*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseHeaders(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("got %d headers, want %d", len(result), len(tt.expected))
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("header %q = %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

func TestParseData(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected map[string]string
		wantNil  bool
		wantErr  bool
	}{
		{
			name:    "empty string",
			input:   "",
			wantNil: true,
		},
		{
			name:     "single pair",
			input:    "key=value",
			expected: map[string]string{"key": "value"},
		},
		{
			name:     "multiple pairs",
			input:    "name=John&age=30&city=NYC",
			expected: map[string]string{"name": "John", "age": "30", "city": "NYC"},
		},
		{
			name:    "entry without equals returns error",
			input:   "key=value&invalid&other=data",
			wantErr: true,
		},
		{
			name:     "whitespace trimmed",
			input:    " key = value & other = data ",
			expected: map[string]string{"key": "value", "other": "data"},
		},
		{
			name:    "all entries invalid returns error",
			input:   "noequalssign",
			wantErr: true,
		},
		{
			name:     "value with equals sign",
			input:    "key=val=ue",
			expected: map[string]string{"key": "val=ue"},
		},
		{
			name:     "empty entries skipped",
			input:    "key=value&&other=data",
			expected: map[string]string{"key": "value", "other": "data"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseData(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantNil {
				if result != nil {
					t.Errorf("got %v, want nil", result)
				}
				return
			}
			if len(result) != len(tt.expected) {
				t.Errorf("got %d entries, want %d", len(result), len(tt.expected))
				return
			}
			for k, v := range tt.expected {
				if result[k] != v {
					t.Errorf("data[%q] = %q, want %q", k, result[k], v)
				}
			}
		})
	}
}

func TestPrepareBody(t *testing.T) {
	t.Run("no body sources", func(t *testing.T) {
		body, ct, err := PrepareBody("", "", nil, "", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if body != nil || ct != "" {
			t.Errorf("expected nil body and empty content type, got body=%v ct=%q", body, ct)
		}
	})

	t.Run("json body string", func(t *testing.T) {
		body, ct, err := PrepareBody(`{"key":"value"}`, "", nil, "", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(body) != `{"key":"value"}` {
			t.Errorf("body = %q, want %q", body, `{"key":"value"}`)
		}
		if ct != "application/json" {
			t.Errorf("content-type = %q, want %q", ct, "application/json")
		}
	})

	t.Run("invalid json string", func(t *testing.T) {
		_, _, err := PrepareBody("{invalid", "", nil, "", "", "")
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("json file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.json")
		os.WriteFile(path, []byte(`{"test":true}`), 0644)

		body, ct, err := PrepareBody("", path, nil, "", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(body) != `{"test":true}` {
			t.Errorf("body = %q, want %q", body, `{"test":true}`)
		}
		if ct != "application/json" {
			t.Errorf("content-type = %q, want %q", ct, "application/json")
		}
	})

	t.Run("invalid json file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "bad.json")
		os.WriteFile(path, []byte(`not json`), 0644)

		_, _, err := PrepareBody("", path, nil, "", "", "")
		if err == nil {
			t.Fatal("expected error for invalid JSON file")
		}
	})

	t.Run("form data", func(t *testing.T) {
		formData := map[string]string{"key": "value", "foo": "bar"}
		body, ct, err := PrepareBody("", "", formData, "", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ct != "application/x-www-form-urlencoded" {
			t.Errorf("content-type = %q, want %q", ct, "application/x-www-form-urlencoded")
		}
		if len(body) == 0 {
			t.Error("expected non-empty body for form data")
		}
	})

	t.Run("raw body default content type", func(t *testing.T) {
		body, ct, err := PrepareBody("", "", nil, "raw content", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(body) != "raw content" {
			t.Errorf("body = %q, want %q", body, "raw content")
		}
		if ct != "text/plain" {
			t.Errorf("content-type = %q, want %q", ct, "text/plain")
		}
	})

	t.Run("raw body custom content type", func(t *testing.T) {
		body, ct, err := PrepareBody("", "", nil, "<xml/>", "", "application/xml")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(body) != "<xml/>" {
			t.Errorf("body = %q, want %q", body, "<xml/>")
		}
		if ct != "application/xml" {
			t.Errorf("content-type = %q, want %q", ct, "application/xml")
		}
	})

	t.Run("raw file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "data.bin")
		os.WriteFile(path, []byte("file content"), 0644)

		body, ct, err := PrepareBody("", "", nil, "", path, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(body) != "file content" {
			t.Errorf("body = %q, want %q", body, "file content")
		}
		if ct != "text/plain" {
			t.Errorf("content-type = %q, want %q", ct, "text/plain")
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, _, err := PrepareBody("", "/nonexistent/file.json", nil, "", "", "")
		if err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

func TestExecuteRequest_Success200(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer server.Close()

	client := server.Client()
	result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "")

	if !result.OK {
		t.Errorf("expected OK=true, got false")
	}
	if result.StatusCode != 200 {
		t.Errorf("status = %d, want 200", result.StatusCode)
	}
	if result.Error != "" {
		t.Errorf("unexpected error: %s", result.Error)
	}
	if result.Elapsed <= 0 {
		t.Errorf("elapsed = %f, want > 0", result.Elapsed)
	}
}

func TestExecuteRequest_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := server.Client()
	result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "")

	if result.OK {
		t.Errorf("expected OK=false for 500 status")
	}
	if result.StatusCode != 500 {
		t.Errorf("status = %d, want 500", result.StatusCode)
	}
}

func TestExecuteRequest_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &http.Client{Timeout: 50 * time.Millisecond}
	result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "")

	if result.OK {
		t.Errorf("expected OK=false for timeout")
	}
	if result.Error == "" {
		t.Errorf("expected error message for timeout")
	}
	if result.ErrorKind != ErrorKindTimeout {
		t.Errorf("ErrorKind = %q, want %q", result.ErrorKind, ErrorKindTimeout)
	}
}

func TestExecuteRequest_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	client := server.Client()
	result := ExecuteRequest(ctx, client, "GET", server.URL, nil, nil, "", 0, "")

	if result.OK {
		t.Errorf("expected OK=false for cancelled context")
	}
	if result.Error == "" {
		t.Errorf("expected error message for cancelled context")
	}
	if result.ErrorKind != ErrorKindCancellation {
		t.Errorf("ErrorKind = %q, want %q", result.ErrorKind, ErrorKindCancellation)
	}
}

func TestExecuteRequest_HeadersAndBody(t *testing.T) {
	var receivedHeaders http.Header
	var receivedBody string
	var receivedMethod string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header
		receivedMethod = r.Method
		bodyBytes, _ := io.ReadAll(r.Body)
		receivedBody = string(bodyBytes)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	headers := map[string]string{
		"X-Custom":      "test-value",
		"Authorization": "Bearer abc",
	}
	body := []byte(`{"key":"value"}`)

	client := server.Client()
	result := ExecuteRequest(context.Background(), client, "POST", server.URL, headers, body, "application/json", 0, "")

	if !result.OK {
		t.Fatalf("expected OK=true, got error: %s", result.Error)
	}
	if receivedMethod != "POST" {
		t.Errorf("method = %q, want POST", receivedMethod)
	}
	if receivedHeaders.Get("X-Custom") != "test-value" {
		t.Errorf("X-Custom header = %q, want %q", receivedHeaders.Get("X-Custom"), "test-value")
	}
	if receivedHeaders.Get("Authorization") != "Bearer abc" {
		t.Errorf("Authorization header = %q, want %q", receivedHeaders.Get("Authorization"), "Bearer abc")
	}
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want %q", receivedHeaders.Get("Content-Type"), "application/json")
	}
	if receivedBody != `{"key":"value"}` {
		t.Errorf("body = %q, want %q", receivedBody, `{"key":"value"}`)
	}
}

func TestExecuteRequest_LargeResponseDrained(t *testing.T) {
	// Server returns a large response; verify it doesn't cause issues
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(strings.Repeat("x", 1024*512))) // 512KB
	}))
	defer server.Close()

	client := server.Client()
	result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "")

	if !result.OK {
		t.Errorf("expected OK=true, got error: %s", result.Error)
	}
}

func TestExecuteRequest_NoBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			bodyBytes, _ := io.ReadAll(r.Body)
			if len(bodyBytes) > 0 {
				t.Errorf("expected empty body, got %d bytes", len(bodyBytes))
			}
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := server.Client()
	result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "")

	if !result.OK {
		t.Errorf("expected OK=true, got error: %s", result.Error)
	}
}

func TestExecuteRequest_StatusCodeClassification(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantOK     bool
	}{
		{"200 OK", 200, true},
		{"201 Created", 201, true},
		{"204 No Content", 204, true},
		{"299 edge", 299, true},
		{"301 Redirect", 301, false},
		{"400 Bad Request", 400, false},
		{"404 Not Found", 404, false},
		{"500 Internal", 500, false},
		{"503 Unavailable", 503, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			}))
			defer server.Close()

			client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			}}
			result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "")

			if result.OK != tt.wantOK {
				t.Errorf("status %d: OK = %v, want %v", tt.statusCode, result.OK, tt.wantOK)
			}
			if result.StatusCode != tt.statusCode {
				t.Errorf("status = %d, want %d", result.StatusCode, tt.statusCode)
			}
		})
	}
}

func TestNormalizeError(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Get http://x: context deadline exceeded", "request timeout"},
		{"Post http://x: context canceled", "request cancelled"},
		{"dial tcp: connection refused", "connection refused"},
		{"dial tcp: lookup x: no such host", "DNS resolution failed"},
		{"read tcp: connection reset by peer", "connection reset"},
		{"Get http://x: EOF", "connection closed (EOF)"},
		{"tls: TLS handshake timeout", "TLS handshake failed"},
		{"short error", "short error"},
		{strings.Repeat("x", 100), strings.Repeat("x", 80) + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := normalizeError(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeError(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestExecuteRequest_ExpectStatusMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated) // 201
	}))
	defer server.Close()

	client := server.Client()

	// Expect 201, server returns 201 → should succeed
	result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 201, "")
	if !result.OK {
		t.Errorf("expected OK=true when expect-status matches, got error: %s", result.Error)
	}

	// Expect 200, server returns 201 → should fail
	result = ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 200, "")
	if result.OK {
		t.Error("expected OK=false when expect-status doesn't match")
	}
	if !strings.Contains(result.Error, "expected status 200") {
		t.Errorf("error should mention expected status, got: %s", result.Error)
	}
}

func TestExecuteRequest_ExpectBodyMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","data":"hello world"}`))
	}))
	defer server.Close()

	client := server.Client()

	// Body contains expected substring → success
	result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "hello world")
	if !result.OK {
		t.Errorf("expected OK=true when body matches, got error: %s", result.Error)
	}

	// Body doesn't contain expected substring → failure
	result = ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "not found text")
	if result.OK {
		t.Error("expected OK=false when body doesn't match")
	}
	if result.Error != "response body missing expected content" {
		t.Errorf("error = %q, want %q", result.Error, "response body missing expected content")
	}
}

func TestExecuteRequest_ResponseSize(t *testing.T) {
	body := strings.Repeat("x", 1024)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(body))
	}))
	defer server.Close()

	client := server.Client()
	result := ExecuteRequest(context.Background(), client, "GET", server.URL, nil, nil, "", 0, "")

	if result.ResponseSize != 1024 {
		t.Errorf("ResponseSize = %d, want 1024", result.ResponseSize)
	}
}

func TestPrepareBodyWithHeaders_ContentTypePrecedence(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		headers  map[string]string
		want     string
	}{
		{
			name:     "explicit overrides parsed header",
			explicit: "application/vnd.api+json",
			headers:  map[string]string{"Content-Type": "text/csv"},
			want:     "application/vnd.api+json",
		},
		{
			name:    "parsed header overrides inference",
			headers: map[string]string{"content-type": "text/csv"},
			want:    "text/csv",
		},
		{
			name: "body type is inferred",
			want: "application/json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, contentType, err := PrepareBodyWithHeaders(
				`{"key":"value"}`, "", nil, "", "", tt.explicit, tt.headers,
			)
			if err != nil {
				t.Fatalf("PrepareBodyWithHeaders() error = %v", err)
			}
			if contentType != tt.want {
				t.Errorf("content type = %q, want %q", contentType, tt.want)
			}
		})
	}
}

func TestExecuteRequestWithMatcher_MatchesAcrossReadBoundaries(t *testing.T) {
	matcher := PrepareBodyMatcher("needle")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(&chunkedReadCloser{chunks: [][]byte{
			[]byte("prefix-nee"),
			[]byte("dle-suffix"),
		}}), nil
	})}

	result := ExecuteRequestWithMatcher(
		context.Background(), client, "GET", "http://example.test", nil, nil, "", 0, matcher,
	)

	if !result.OK {
		t.Fatalf("expected cross-boundary match, got error: %s", result.Error)
	}
	if result.ResponseSize != int64(len("prefix-needle-suffix")) {
		t.Errorf("ResponseSize = %d, want %d", result.ResponseSize, len("prefix-needle-suffix"))
	}
}

func TestExecuteRequestWithMatcher_ConcurrentMatcherReuse(t *testing.T) {
	matcher := PrepareBodyMatcher("needle")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(&chunkedReadCloser{chunks: [][]byte{
			[]byte("prefix-nee"),
			[]byte("dle-suffix"),
		}}), nil
	})}

	const workers = 32
	var waitGroup sync.WaitGroup
	errors := make(chan string, workers)
	for range workers {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			result := ExecuteRequestWithMatcher(
				context.Background(), client, "GET", "http://example.test", nil, nil, "", 0, matcher,
			)
			if !result.OK {
				errors <- result.Error
			}
		}()
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		t.Errorf("concurrent request failed: %s", err)
	}
}

func TestExecuteRequestWithMatcher_DrainsFiveMiBAndReusesConnection(t *testing.T) {
	const responseSize = 5 << 20
	remoteAddresses := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteAddresses <- r.RemoteAddr
		chunk := strings.Repeat("x", 32<<10)
		for written := 0; written < responseSize; written += len(chunk) {
			if _, err := io.WriteString(w, chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	client := server.Client()
	matcher := PrepareBodyMatcher("xxx")
	first := ExecuteRequestWithMatcher(
		context.Background(), client, "GET", server.URL, nil, nil, "", 0, matcher,
	)
	second := ExecuteRequestWithMatcher(
		context.Background(), client, "GET", server.URL, nil, nil, "", 0, matcher,
	)

	for i, result := range []Result{first, second} {
		if !result.OK {
			t.Fatalf("request %d failed: %s", i+1, result.Error)
		}
		if result.ResponseSize != responseSize {
			t.Errorf("request %d ResponseSize = %d, want %d", i+1, result.ResponseSize, responseSize)
		}
	}
	firstAddress := <-remoteAddresses
	secondAddress := <-remoteAddresses
	if firstAddress != secondAddress {
		t.Errorf("connection was not reused: first=%q second=%q", firstAddress, secondAddress)
	}
}

func TestExecuteRequest_TimingIncludesDelayedBody(t *testing.T) {
	const headerDelay = 40 * time.Millisecond
	const bodyDelay = 80 * time.Millisecond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(headerDelay)
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		time.Sleep(bodyDelay)
		_, _ = io.WriteString(w, "done")
	}))
	defer server.Close()

	startedAt := time.Now()
	result := ExecuteRequest(context.Background(), server.Client(), "GET", server.URL, nil, nil, "", 0, "")
	finishedAt := time.Now()

	if !result.OK {
		t.Fatalf("request failed: %s", result.Error)
	}
	if result.TTFB < (headerDelay / 2).Seconds() {
		t.Errorf("TTFB = %f, expected delayed headers to affect TTFB", result.TTFB)
	}
	if result.Elapsed-result.TTFB < (bodyDelay / 2).Seconds() {
		t.Errorf("elapsed=%f TTFB=%f, expected delayed body to affect total", result.Elapsed, result.TTFB)
	}
	if result.CompletedAt.Before(startedAt) || result.CompletedAt.After(finishedAt) {
		t.Errorf("CompletedAt = %v, want within [%v, %v]", result.CompletedAt, startedAt, finishedAt)
	}
}

func TestExecuteRequest_TruncatedBodyRetainsPartialResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "20")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "partial")
	}))
	defer server.Close()

	result := ExecuteRequest(context.Background(), server.Client(), "GET", server.URL, nil, nil, "", 0, "")

	if result.OK {
		t.Fatal("expected truncated body to fail")
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if result.ResponseSize != int64(len("partial")) {
		t.Errorf("ResponseSize = %d, want %d", result.ResponseSize, len("partial"))
	}
	if result.ErrorKind != ErrorKindBodyRead {
		t.Errorf("ErrorKind = %q, want %q", result.ErrorKind, ErrorKindBodyRead)
	}
}

func TestExecuteRequest_BodyCloseError(t *testing.T) {
	closeErr := errors.New("close failed")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(&readCloserWithError{
			Reader:   strings.NewReader("partial"),
			closeErr: closeErr,
		}), nil
	})}

	result := ExecuteRequest(context.Background(), client, "GET", "http://example.test", nil, nil, "", 0, "")

	if result.OK {
		t.Fatal("expected close error to fail")
	}
	if result.StatusCode != http.StatusOK || result.ResponseSize != int64(len("partial")) {
		t.Errorf("partial result = status %d, size %d", result.StatusCode, result.ResponseSize)
	}
	if result.ErrorKind != ErrorKindBodyClose {
		t.Errorf("ErrorKind = %q, want %q", result.ErrorKind, ErrorKindBodyClose)
	}
}

func TestExecuteRequest_BodyReadErrorTakesPrecedenceOverCloseError(t *testing.T) {
	readErr := errors.New("read failed")
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return testResponse(&chunkedReadCloser{
			chunks:   [][]byte{[]byte("partial")},
			readErr:  readErr,
			closeErr: errors.New("close failed"),
		}), nil
	})}

	result := ExecuteRequest(context.Background(), client, "GET", "http://example.test", nil, nil, "", 0, "")

	if result.ErrorKind != ErrorKindBodyRead {
		t.Errorf("ErrorKind = %q, want %q", result.ErrorKind, ErrorKindBodyRead)
	}
	if result.Error != readErr.Error() {
		t.Errorf("Error = %q, want %q", result.Error, readErr)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

type chunkedReadCloser struct {
	chunks   [][]byte
	readErr  error
	closeErr error
}

func (r *chunkedReadCloser) Read(p []byte) (int, error) {
	if len(r.chunks) > 0 {
		chunk := r.chunks[0]
		r.chunks = r.chunks[1:]
		return copy(p, chunk), nil
	}
	if r.readErr != nil {
		return 0, r.readErr
	}
	return 0, io.EOF
}

func (r *chunkedReadCloser) Close() error {
	return r.closeErr
}

type readCloserWithError struct {
	io.Reader
	closeErr error
}

func (r *readCloserWithError) Close() error {
	return r.closeErr
}

func testResponse(body io.ReadCloser) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     fmt.Sprintf("%d %s", http.StatusOK, http.StatusText(http.StatusOK)),
		Header:     make(http.Header),
		Body:       body,
	}
}

func TestResponseBodyStreamingAllocations(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64<<10)
	reader := bytes.NewReader(nil)
	matcher := PrepareBodyMatcher("89abcdef0123")
	allocations := testing.AllocsPerRun(100, func() {
		reader.Reset(payload)
		state := matcher.newState()
		var size int64
		if err := observeResponseBody(reader, &size, &state); err != nil {
			panic(err)
		}
		if size != int64(len(payload)) || !state.found {
			panic("streaming observer invariant failed")
		}
	})
	if allocations > 1 {
		t.Fatalf("streaming allocations = %.2f, want <= 1", allocations)
	}
}

func BenchmarkResponseBodyStreaming(b *testing.B) {
	payload := bytes.Repeat([]byte("0123456789abcdef"), 64<<10)
	reader := bytes.NewReader(nil)
	matcher := PrepareBodyMatcher("89abcdef0123")
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		reader.Reset(payload)
		state := matcher.newState()
		var size int64
		if err := observeResponseBody(reader, &size, &state); err != nil {
			b.Fatal(err)
		}
	}
}
