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

// ConsumerClient provides methods for consumers to interact with the registry
type ConsumerClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewConsumerClient creates a new consumer client
func NewConsumerClient(baseURL string) *ConsumerClient {
	return &ConsumerClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{},
	}
}

// NewConsumerClientWithHTTPClient creates a new consumer client with a custom HTTP client
func NewConsumerClientWithHTTPClient(baseURL string, httpClient *http.Client) *ConsumerClient {
	return &ConsumerClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		httpClient: httpClient,
	}
}

// GetRegisters retrieves register values with optional long polling.
// If names is nil or empty, all registers are returned.
// If wait is greater than 0, the request will long-poll until a change occurs or timeout.
func (c *ConsumerClient) GetRegisters(ctx context.Context, names []string, wait time.Duration) (map[string]ConsumerGetRegister, error) {
	u, err := url.Parse(c.baseURL + "/consumer")
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

	var response ConsumerGetResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return response.Registers, nil
}

// GetRegister retrieves a single register value with optional long polling.
// If wait is greater than 0, the request will long-poll until a change occurs or timeout.
func (c *ConsumerClient) GetRegister(ctx context.Context, name string, wait time.Duration) (*ConsumerGetRegister, error) {
	registers, err := c.GetRegisters(ctx, []string{name}, wait)
	if err != nil {
		return nil, err
	}

	reg, exists := registers[name]
	if !exists {
		return nil, nil
	}

	return &reg, nil
}

// RequestChanges requests changes to register values.
// The server will accept the request (202 Accepted) and providers can poll for these requests.
func (c *ConsumerClient) RequestChanges(ctx context.Context, changes map[string]any) error {
	request := ConsumerPutRequest{
		Registers: make(map[string]ConsumerPutRegister, len(changes)),
	}

	for name, value := range changes {
		request.Registers[name] = ConsumerPutRegister{Value: value}
	}

	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/consumer", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

// RequestChange requests a change to a single register value.
func (c *ConsumerClient) RequestChange(ctx context.Context, name string, value any) error {
	return c.RequestChanges(ctx, map[string]any{name: value})
}
