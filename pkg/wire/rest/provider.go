package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ProviderClient provides methods for providers to interact with the registry
type ProviderClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewProviderClient creates a new provider client
func NewProviderClient(baseURL string) *ProviderClient {
	return &ProviderClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// NewProviderClientWithHTTPClient creates a new provider client with a custom HTTP client
func NewProviderClientWithHTTPClient(baseURL string, httpClient *http.Client) *ProviderClient {
	return &ProviderClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: httpClient,
	}
}

// RegisterUpdate represents a register update to be sent to the server
type RegisterUpdate struct {
	Value    any
	Metadata map[string]any
	TTL      time.Duration
}

// SetRegisters sets or updates register values with metadata and TTL.
func (c *ProviderClient) SetRegisters(ctx context.Context, updates map[string]RegisterUpdate) error {
	request := ProviderPutRequest{
		Registers: make(map[string]ProviderPutRegister, len(updates)),
	}

	for name, update := range updates {
		request.Registers[name] = ProviderPutRegister{
			Value:    update.Value,
			Metadata: update.Metadata,
			TTL:      Duration(update.TTL),
		}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/provider", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// SetRegister sets or updates a single register value with metadata and TTL.
func (c *ProviderClient) SetRegister(ctx context.Context, name string, value any, metadata map[string]any, ttl time.Duration) error {
	return c.SetRegisters(ctx, map[string]RegisterUpdate{
		name: {
			Value:    value,
			Metadata: metadata,
			TTL:      ttl,
		},
	})
}

// GetChangeRequests polls for consumer change requests with optional long polling.
// If names is nil or empty, all change requests are returned.
// If wait is greater than 0, the request will long-poll until a request arrives or timeout.
// Note: This call consumes the change requests - they won't be returned again.
func (c *ProviderClient) GetChangeRequests(ctx context.Context, names []string, wait time.Duration) (map[string]any, error) {
	u, err := url.Parse(c.baseURL + "/provider")
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	query := u.Query()
	for _, name := range names {
		query.Add("name", name)
	}
	if wait > 0 {
		query.Set("wait", wait.String())
	}
	u.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var response ProviderGetResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Convert to simple map
	result := make(map[string]any, len(response.Registers))
	for name, reg := range response.Registers {
		result[name] = reg.Value
	}

	return result, nil
}

// GetChangeRequest polls for a single consumer change request with optional long polling.
// If wait is greater than 0, the request will long-poll until a request arrives or timeout.
// Returns nil if no change request exists for the given register.
// Note: This call consumes the change request - it won't be returned again.
func (c *ProviderClient) GetChangeRequest(ctx context.Context, name string, wait time.Duration) (any, error) {
	requests, err := c.GetChangeRequests(ctx, []string{name}, wait)
	if err != nil {
		return nil, err
	}

	value, exists := requests[name]
	if !exists {
		return nil, nil
	}

	return value, nil
}
