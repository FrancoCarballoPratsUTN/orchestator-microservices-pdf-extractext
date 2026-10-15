package httpclient

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/reqctx"
)

func TestDoRetriesOnServiceUnavailableThenSucceeds(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	client := NewWithPolicy(server.URL, 5*time.Second, "", RetryPolicy{Attempts: 3}, nil)

	var got pingResponse
	err := client.Do(context.Background(), http.MethodGet, "/ping", "", nil, &got)

	if err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}
	if got.Status != "ok" {
		t.Errorf("Status = %q, want %q", got.Status, "ok")
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("upstream calls = %d, want 3", n)
	}
}

func TestDoStopsAfterConfiguredAttempts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	client := NewWithPolicy(server.URL, 5*time.Second, "", RetryPolicy{Attempts: 3}, nil)

	err := client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error when all attempts fail")
	}
	if n := calls.Load(); n != 3 {
		t.Errorf("upstream calls = %d, want 3", n)
	}
}

func TestDoDoesNotRetryClientErrors(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"title":"Bad Request","status":400}`))
	})
	client := NewWithPolicy(server.URL, 5*time.Second, "", RetryPolicy{Attempts: 3}, nil)

	err := client.Do(context.Background(), http.MethodGet, "/extract", "", nil, nil)

	if err == nil {
		t.Fatal("Do() expected an error for a 400 response")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("upstream calls = %d, want 1 (4xx is not retryable)", n)
	}
}

func TestDoDoesNotRetryNonIdempotentMethodByDefault(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	client := NewWithPolicy(server.URL, 5*time.Second, "", RetryPolicy{Attempts: 3}, nil)

	err := client.Do(context.Background(), http.MethodPost, "/extract", "application/pdf", []byte("%PDF-"), nil)

	if err == nil {
		t.Fatal("Do() expected an error for a 503 response")
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("upstream calls = %d, want 1 (POST is not retried by default)", n)
	}
}

func TestDoRetriesNonIdempotentMethodWhenAllowed(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	policy := RetryPolicy{Attempts: 3, AllowNonIdempotent: true}
	client := NewWithPolicy(server.URL, 5*time.Second, "", policy, nil)

	err := client.Do(context.Background(), http.MethodPost, "/extract", "application/pdf", []byte("%PDF-"), nil)

	if err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}
	if n := calls.Load(); n != 2 {
		t.Errorf("upstream calls = %d, want 2", n)
	}
}

func TestDoHonoursGlobalBudgetAcrossAttempts(t *testing.T) {
	t.Parallel()

	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Millisecond):
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	// 10 attempts inside a 100ms budget: a per-attempt timeout would allow
	// ~10×100ms, so an elapsed time near the budget proves the budget is global.
	client := NewWithPolicy(server.URL, 100*time.Millisecond, "", RetryPolicy{Attempts: 10}, nil)

	start := time.Now()
	err := client.Do(context.Background(), http.MethodGet, "/slow", "", nil, nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Do() expected an error when the budget is exhausted")
	}
	if elapsed > 400*time.Millisecond {
		t.Errorf("Do() took %v, want it bounded by the global budget (~100ms)", elapsed)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Do() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestDoSendsRequestIDHeaderFromContext(t *testing.T) {
	t.Parallel()

	var seen atomic.Value
	server := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.Header.Get("X-Request-ID"))
		w.WriteHeader(http.StatusOK)
	})
	client := New(server.URL, 5*time.Second, "")

	ctx := reqctx.WithRequestID(context.Background(), "req-retry")
	if err := client.Do(ctx, http.MethodGet, "/ping", "", nil, nil); err != nil {
		t.Fatalf("Do() unexpected error: %v", err)
	}

	if got, _ := seen.Load().(string); got != "req-retry" {
		t.Errorf("X-Request-ID = %q, want %q", got, "req-retry")
	}
}

func TestRetryPolicyAllowsMethod(t *testing.T) {
	t.Parallel()

	idempotent := RetryPolicy{Attempts: 2}
	nonIdempotent := RetryPolicy{Attempts: 2, AllowNonIdempotent: true}

	cases := []struct {
		name   string
		policy RetryPolicy
		method string
		want   bool
	}{
		{"GET allowed without flag", idempotent, http.MethodGet, true},
		{"PUT allowed without flag", idempotent, http.MethodPut, true},
		{"DELETE allowed without flag", idempotent, http.MethodDelete, true},
		{"POST blocked without flag", idempotent, http.MethodPost, false},
		{"POST allowed with flag", nonIdempotent, http.MethodPost, true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.policy.allowsMethod(tc.method); got != tc.want {
				t.Errorf("allowsMethod(%q) = %v, want %v", tc.method, got, tc.want)
			}
		})
	}
}

func TestRetryableStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		code int
		want bool
	}{
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, true},
		{http.StatusGatewayTimeout, true},
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusNotFound, false},
		{http.StatusConflict, false},
		{http.StatusInternalServerError, false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(http.StatusText(tc.code), func(t *testing.T) {
			t.Parallel()
			if got := retryableStatus(tc.code); got != tc.want {
				t.Errorf("retryableStatus(%d) = %v, want %v", tc.code, got, tc.want)
			}
		})
	}
}
