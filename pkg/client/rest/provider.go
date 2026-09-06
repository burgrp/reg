package rest

import (
	"context"
	"fmt"
	"time"
)

// Provide implements client.Client.Provide
func (c *Client) Provide(ctx context.Context, name string, value any, metadata map[string]any, ttl time.Duration) (chan<- any, <-chan any, error) {
	c.providerMu.Lock()
	defer c.providerMu.Unlock()
	if ttl <= 0 || ttl/2 <= 0 {
		return nil, nil, fmt.Errorf("ttl must be positive")
	}
	if _, exists := c.providerSubs[name]; exists {
		return nil, nil, fmt.Errorf("register %q already has an active provider", name)
	}

	// Create subscription with its own context
	subCtx, subCancel := context.WithCancel(context.Background())
	sub := &providerSubscription{
		ctx:            subCtx,
		cancel:         subCancel,
		name:           name,
		currentValue:   value,
		metadata:       metadata,
		ttl:            ttl,
		updates:        make(chan any, 1),
		changeRequests: make(chan any, 1),
	}
	c.providerSubs[name] = sub

	// Set initial value
	c.providerClient.SetRegister(ctx, name, value, metadata, ttl) // Ignore error for initial set

	// Start batch poller if not running
	if c.providerBatchCtx == nil {
		c.providerBatchCtx, c.providerBatchCxl = context.WithCancel(context.Background())
		go c.providerBatchPoller(c.providerBatchCtx)
	}

	// Serialize updates and TTL refreshes so an older write can never complete last.
	sub.wg.Add(1)
	go c.handleProviderWrites(ctx, sub)

	// Handle cleanup on context cancel
	go func() {
		<-ctx.Done()
		c.providerMu.Lock()
		if c.providerSubs[name] == sub {
			delete(c.providerSubs, name)
		}
		c.providerMu.Unlock()

		// Cancel subscription context to signal senders to stop
		sub.cancel()

		// Wait for all active senders to finish before closing channels
		sub.wg.Wait()
		close(sub.updates)
		close(sub.changeRequests)

		// Stop batch poller if no more subscriptions
		c.providerMu.Lock()
		if len(c.providerSubs) == 0 && c.providerBatchCxl != nil {
			c.providerBatchCxl()
			c.providerBatchCtx = nil
			c.providerBatchCxl = nil
		}
		c.providerMu.Unlock()
	}()

	return sub.updates, sub.changeRequests, nil
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

		// Long poll for change requests
		pollInterval := c.ProviderPollInterval
		if pollInterval <= 0 {
			pollInterval = 30 * time.Second
		}
		requests, err := c.providerClient.GetChangeRequests(ctx, names, pollInterval)
		if err != nil {
			time.Sleep(1 * time.Second) // Back off on error
			continue
		}

		// Distribute requests to subscribers
		c.providerMu.Lock()
		for name, value := range requests {
			if sub, exists := c.providerSubs[name]; exists {
				sub.wg.Add(1)
				go func(s *providerSubscription, val any) {
					defer s.wg.Done()
					select {
					case s.changeRequests <- val:
					case <-s.ctx.Done():
						// Subscription is being torn down, don't send
					default:
						// Channel full, skip
					}
				}(sub, value)
			}
		}
		c.providerMu.Unlock()
	}
}

// handleProviderWrites serializes explicit updates and automatic TTL refreshes.
func (c *Client) handleProviderWrites(ctx context.Context, sub *providerSubscription) {
	defer sub.wg.Done()
	ticker := time.NewTicker(sub.ttl / 2)
	defer ticker.Stop()

	currentValue := sub.currentValue
	for {
		select {
		case <-ctx.Done():
			return
		case value, ok := <-sub.updates:
			if !ok {
				return
			}
			currentValue = value
			c.providerMu.Lock()
			if c.providerSubs[sub.name] == sub {
				sub.currentValue = value
			}
			c.providerMu.Unlock()
			c.providerClient.SetRegister(ctx, sub.name, currentValue, sub.metadata, sub.ttl)
		case <-ticker.C:
			c.providerClient.SetRegister(ctx, sub.name, currentValue, sub.metadata, sub.ttl)
		}
	}
}
