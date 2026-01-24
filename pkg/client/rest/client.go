package rest

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/burgrp/reg/pkg/client"
	"github.com/burgrp/reg/pkg/wire/rest"
)

// Client implements the Registry interface using REST protocol
type Client struct {
	consumerClient *rest.ConsumerClient
	providerClient *rest.ProviderClient

	// Consumer batching
	consumerMu       sync.Mutex
	consumerSubs     map[string]*consumerSubscription
	consumerBatchCtx context.Context
	consumerBatchCxl context.CancelFunc

	// Provider batching
	providerMu       sync.Mutex
	providerSubs     map[string]*providerSubscription
	providerBatchCtx context.Context
	providerBatchCxl context.CancelFunc
}

type consumerSubscription struct {
	values   chan client.ValueAndMetadata
	requests chan any
}

type providerSubscription struct {
	name           string
	initialValue   any
	metadata       map[string]any
	ttl            time.Duration
	updates        chan any
	changeRequests chan any
}

// NewClient creates a new REST-based registry client with a default HTTP client
func NewClient(baseURL string) *Client {
	httpClient := &http.Client{}
	return NewClientWithHTTPClient(baseURL, httpClient)
}

// NewClientWithHTTPClient creates a new REST-based registry client with a custom HTTP client
func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		consumerClient: rest.NewConsumerClientWithHTTPClient(baseURL, httpClient),
		providerClient: rest.NewProviderClientWithHTTPClient(baseURL, httpClient),
		consumerSubs:   make(map[string]*consumerSubscription),
		providerSubs:   make(map[string]*providerSubscription),
	}
}

// Consume implements Registry.Consume
func (c *Client) Consume(ctx context.Context, name string) (<-chan client.ValueAndMetadata, chan<- any, error) {
	c.consumerMu.Lock()
	defer c.consumerMu.Unlock()

	// Create subscription
	sub := &consumerSubscription{
		values:   make(chan client.ValueAndMetadata, 1),
		requests: make(chan any, 1),
	}
	c.consumerSubs[name] = sub

	// Start batch poller if not running
	if c.consumerBatchCtx == nil {
		c.consumerBatchCtx, c.consumerBatchCxl = context.WithCancel(context.Background())
		go c.consumerBatchPoller(c.consumerBatchCtx)
	}

	// Get initial value (no-wait)
	go func() {
		registers, err := c.consumerClient.GetRegisters(ctx, []string{name}, 0)
		if err == nil {
			if reg, exists := registers[name]; exists {
				select {
				case sub.values <- client.ValueAndMetadata{Value: reg.Value, Metadata: reg.Metadata}:
				case <-ctx.Done():
				}
			}
		}
	}()

	// Handle change requests
	go c.handleConsumerRequests(ctx, name, sub.requests)

	// Handle cleanup on context cancel
	go func() {
		<-ctx.Done()
		c.consumerMu.Lock()
		delete(c.consumerSubs, name)
		close(sub.values)
		close(sub.requests)

		// Stop batch poller if no more subscriptions
		if len(c.consumerSubs) == 0 && c.consumerBatchCxl != nil {
			c.consumerBatchCxl()
			c.consumerBatchCtx = nil
			c.consumerBatchCxl = nil
		}
		c.consumerMu.Unlock()
	}()

	return sub.values, sub.requests, nil
}

// Provide implements Registry.Provide
func (c *Client) Provide(ctx context.Context, name string, value any, metadata map[string]any, ttl time.Duration) (chan<- any, <-chan any, error) {
	c.providerMu.Lock()
	defer c.providerMu.Unlock()

	// Create subscription
	sub := &providerSubscription{
		name:           name,
		initialValue:   value,
		metadata:       metadata,
		ttl:            ttl,
		updates:        make(chan any, 1),
		changeRequests: make(chan any, 1),
	}
	c.providerSubs[name] = sub

	// Set initial value
	err := c.providerClient.SetRegister(ctx, name, value, metadata, ttl)
	if err != nil {
		return nil, nil, err
	}

	// Start batch poller if not running
	if c.providerBatchCtx == nil {
		c.providerBatchCtx, c.providerBatchCxl = context.WithCancel(context.Background())
		go c.providerBatchPoller(c.providerBatchCtx)
	}

	// Handle updates
	go c.handleProviderUpdates(ctx, name, metadata, ttl, sub.updates)

	// Handle TTL refresh
	go c.handleTTLRefresh(ctx, name, metadata, ttl)

	// Handle cleanup on context cancel
	go func() {
		<-ctx.Done()
		c.providerMu.Lock()
		delete(c.providerSubs, name)
		close(sub.updates)
		close(sub.changeRequests)

		// Stop batch poller if no more subscriptions
		if len(c.providerSubs) == 0 && c.providerBatchCxl != nil {
			c.providerBatchCxl()
			c.providerBatchCtx = nil
			c.providerBatchCxl = nil
		}
		c.providerMu.Unlock()
	}()

	return sub.updates, sub.changeRequests, nil
}

// consumerBatchPoller polls for all consumer subscriptions in a single request
func (c *Client) consumerBatchPoller(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.consumerMu.Lock()
		if len(c.consumerSubs) == 0 {
			c.consumerMu.Unlock()
			return
		}

		// Build list of names to poll
		names := make([]string, 0, len(c.consumerSubs))
		for name := range c.consumerSubs {
			names = append(names, name)
		}
		c.consumerMu.Unlock()

		// Long poll for changes (5 seconds)
		registers, err := c.consumerClient.GetRegisters(ctx, names, 5*time.Second)
		if err != nil {
			time.Sleep(1 * time.Second) // Back off on error
			continue
		}

		// Distribute updates to subscribers
		c.consumerMu.Lock()
		for name, reg := range registers {
			if sub, exists := c.consumerSubs[name]; exists {
				select {
				case sub.values <- client.ValueAndMetadata{Value: reg.Value, Metadata: reg.Metadata}:
				default:
					// Channel full, skip
				}
			}
		}
		c.consumerMu.Unlock()
	}
}

// providerBatchPoller polls for change requests for all provider subscriptions
func (c *Client) providerBatchPoller(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.providerMu.Lock()
		if len(c.providerSubs) == 0 {
			c.providerMu.Unlock()
			return
		}

		// Build list of names to poll
		names := make([]string, 0, len(c.providerSubs))
		for name := range c.providerSubs {
			names = append(names, name)
		}
		c.providerMu.Unlock()

		// Long poll for change requests (30 seconds)
		requests, err := c.providerClient.GetChangeRequests(ctx, names, 30*time.Second)
		if err != nil {
			time.Sleep(1 * time.Second) // Back off on error
			continue
		}

		// Distribute requests to subscribers
		c.providerMu.Lock()
		for name, value := range requests {
			if sub, exists := c.providerSubs[name]; exists {
				select {
				case sub.changeRequests <- value:
				default:
					// Channel full, skip
				}
			}
		}
		c.providerMu.Unlock()
	}
}

// handleConsumerRequests sends consumer change requests to the server
func (c *Client) handleConsumerRequests(ctx context.Context, name string, requests <-chan any) {
	for {
		select {
		case <-ctx.Done():
			return
		case value, ok := <-requests:
			if !ok {
				return
			}
			c.consumerClient.RequestChange(ctx, name, value)
		}
	}
}

// handleProviderUpdates sends provider value updates to the server
func (c *Client) handleProviderUpdates(ctx context.Context, name string, metadata map[string]any, ttl time.Duration, updates <-chan any) {
	for {
		select {
		case <-ctx.Done():
			return
		case value, ok := <-updates:
			if !ok {
				return
			}
			c.providerClient.SetRegister(ctx, name, value, metadata, ttl)
		}
	}
}

// handleTTLRefresh automatically refreshes the register TTL before expiration
func (c *Client) handleTTLRefresh(ctx context.Context, name string, metadata map[string]any, ttl time.Duration) {
	if ttl <= 0 {
		return // No TTL refresh needed
	}

	// Refresh at 50% of TTL
	refreshInterval := ttl / 2
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.providerMu.Lock()
			sub, exists := c.providerSubs[name]
			c.providerMu.Unlock()

			if !exists {
				return
			}

			// Get current value from subscription
			c.providerClient.SetRegister(ctx, name, sub.initialValue, metadata, ttl)
		}
	}
}
