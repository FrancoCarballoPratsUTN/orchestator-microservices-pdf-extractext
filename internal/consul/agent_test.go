package consul

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRegisterAndDeregisterService(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Consul-Token") != "service-token" {
			t.Fatalf("missing Consul token")
		}
		if r.URL.Path != "/v1/agent/service/register" && r.URL.Path != "/v1/agent/service/deregister/orchestrator-1" {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewAgentClient(server.URL, "service-token", server.Client())
	if err := client.RegisterService(ServiceRegistration{
		ID: "orchestrator-1", Name: "orchestrator", Address: "orchestrator", Port: 8080,
		Check: "http://orchestrator:8080/readyz",
	}); err != nil {
		t.Fatalf("RegisterService() unexpected error: %v", err)
	}
	if err := client.DeregisterService("orchestrator-1"); err != nil {
		t.Fatalf("DeregisterService() unexpected error: %v", err)
	}
}

type agentRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper agentRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func TestDeregisterServiceEscapesIDAndAcceptsNotFound(t *testing.T) {
	t.Parallel()

	client := NewAgentClient("http://consul", "", &http.Client{Transport: agentRoundTripper(func(request *http.Request) (*http.Response, error) {
		if got, want := request.URL.EscapedPath(), "/v1/agent/service/deregister/service%2Fwith%20space"; got != want {
			t.Errorf("escaped path = %q, want %q", got, want)
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Status:     "404 Not Found",
			Body:       http.NoBody,
			Header:     make(http.Header),
			Request:    request,
		}, nil
	})})

	if err := client.DeregisterService("service/with space"); err != nil {
		t.Fatalf("DeregisterService() returned error for missing service: %v", err)
	}
}
