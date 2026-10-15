package httpclient

import (
	"context"
	"errors"
	"math"
	"math/rand/v2"
	"net/http"
	"time"
)

// RetryPolicy describes how a Client retries a failed East-West call. It is a
// value type so callers can build per-service policies without shared state.
type RetryPolicy struct {
	// Attempts is the total number of tries, including the first one. Values
	// below 1 are treated as 1 (no retry).
	Attempts int
	// AllowNonIdempotent permits retrying methods such as POST. Idempotent
	// methods (GET, HEAD, PUT, DELETE) are always eligible for retry.
	AllowNonIdempotent bool
	// BackoffBase and BackoffMax bound the exponential backoff with jitter
	// between attempts. A non-positive BackoffBase disables the delay.
	BackoffBase time.Duration
	BackoffMax  time.Duration
}

// DefaultRetryPolicy performs a single attempt: no retry. Callers opt into more
// attempts explicitly.
var DefaultRetryPolicy = RetryPolicy{Attempts: 1}

func (p RetryPolicy) attempts() int {
	if p.Attempts < 1 {
		return 1
	}
	return p.Attempts
}

// allowsMethod reports whether the policy may retry the given HTTP method.
func (p RetryPolicy) allowsMethod(method string) bool {
	if p.AllowNonIdempotent {
		return true
	}
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete:
		return true
	default:
		return false
	}
}

// backoff returns the delay before the given retry (1-based), applying full
// jitter so concurrent callers do not retry in lockstep.
func (p RetryPolicy) backoff(retry int) time.Duration {
	if p.BackoffBase <= 0 {
		return 0
	}
	delay := float64(p.BackoffBase) * math.Pow(2, float64(retry-1))
	if p.BackoffMax > 0 && delay > float64(p.BackoffMax) {
		delay = float64(p.BackoffMax)
	}
	return time.Duration(rand.Int64N(int64(delay) + 1))
}

// retryableStatus reports whether an upstream status is worth retrying. Only
// gateway-class failures are transient; 4xx and plain 500 are not.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// transportError marks a failure to reach the upstream (as opposed to an error
// status). It exists solely to classify retryability without conflating
// transport failures with decode or status errors.
type transportError struct {
	err error
}

func (e transportError) Error() string { return e.err.Error() }
func (e transportError) Unwrap() error { return e.err }

// isRetryable reports whether err is worth another attempt: a transport failure
// or a gateway-class upstream status.
func isRetryable(err error) bool {
	var transport transportError
	if errors.As(err, &transport) {
		return true
	}
	if code, ok := StatusCode(err); ok {
		return retryableStatus(code)
	}
	return false
}

// sleepCtx waits for d or until ctx is done, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
