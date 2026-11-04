package server

import (
	"testing"
	"time"

	"validationmicroservices-pdf-extractext/internal/config"
)

func TestNewConfiguresRequestTimeoutsForSequentialUpstreamCalls(t *testing.T) {
	httpTimeout := 35 * time.Second
	server := New(config.Config{Port: "8080", HTTPTimeout: httpTimeout}, discardLogger(), nil, nil, nil, nil)

	minimumTimeout := 2*httpTimeout + 10*time.Second
	if server.ReadTimeout < minimumTimeout {
		t.Errorf("ReadTimeout = %s, want at least %s", server.ReadTimeout, minimumTimeout)
	}
	if server.WriteTimeout < minimumTimeout {
		t.Errorf("WriteTimeout = %s, want at least %s", server.WriteTimeout, minimumTimeout)
	}
	if server.IdleTimeout == 0 {
		t.Error("IdleTimeout is not configured")
	}
	if server.MaxHeaderBytes != 0 {
		t.Errorf("MaxHeaderBytes = %d, want net/http default", server.MaxHeaderBytes)
	}
}

func TestRequestTimeoutComputesBudgetWithSlack(t *testing.T) {
	t.Parallel()

	got := requestTimeout(35 * time.Second)
	want := 80 * time.Second
	if got != want {
		t.Errorf("requestTimeout(35s) = %v, want %v", got, want)
	}
}
