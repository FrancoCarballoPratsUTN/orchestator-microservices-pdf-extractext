package consul

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type AgentClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func NewAgentClient(baseURL, token string, client *http.Client) *AgentClient {
	if client == nil {
		client = &http.Client{Timeout: requestTimeout}
	}
	return &AgentClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   strings.TrimSpace(token),
		client:  client,
	}
}

func NewAgentClientWithCA(baseURL, token, caFile string) (*AgentClient, error) {
	client, err := newHTTPClientWithCA(caFile)
	if err != nil {
		return nil, err
	}
	return NewAgentClient(baseURL, token, client), nil
}

type ServiceRegistration struct {
	ID      string
	Name    string
	Address string
	Port    int
	Check   string
	Tags    []string
}

func (c *AgentClient) RegisterService(registration ServiceRegistration) error {
	if registration.ID == "" || registration.Name == "" || registration.Address == "" ||
		registration.Port < 1 || registration.Check == "" {
		return fmt.Errorf("invalid Consul service registration")
	}
	payload := map[string]any{
		"ID":      registration.ID,
		"Name":    registration.Name,
		"Address": registration.Address,
		"Port":    registration.Port,
		"Tags":    registration.Tags,
		"Check": map[string]any{
			"HTTP":                           registration.Check,
			"Interval":                       "10s",
			"Timeout":                        "3s",
			"DeregisterCriticalServiceAfter": "1m",
		},
	}
	_, err := c.agentRequest(http.MethodPut, "/v1/agent/service/register", payload)
	return err
}

func (c *AgentClient) DeregisterService(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("Consul service ID must not be empty")
	}
	status, err := c.agentRequest(http.MethodPut, "/v1/agent/service/deregister/"+url.PathEscape(id), nil)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

func (c *AgentClient) agentRequest(method, path string, payload any) (int, error) {
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("encode Consul service request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return 0, fmt.Errorf("create Consul service request: %w", err)
	}
	if c.token != "" {
		request.Header.Set("X-Consul-Token", c.token)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("Consul service request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode/100 != 2 {
		return response.StatusCode, fmt.Errorf("Consul service request: unexpected status %s", response.Status)
	}
	return response.StatusCode, nil
}
