package rest

import (
	"context"
	"time"
)

// Provide implements client.Client.Provide
func (c *Client) Provide(ctx context.Context, name string, value any, metadata map[string]any, ttl time.Duration) (chan<- any, <-chan any, error) {
	c.providerMu.Lock()
	defer c.providerMu.Unlock()

	// Create subscription
	sub := &providerSubscription{
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

	// Handle updates
	go c.handleProviderUpdates(ctx, name, metadata, ttl, sub.updates)

	// Handle TTL refresh
	go c.handleTTLRefresh(ctx, name, metadata, ttl)

	// Handle cleanup on context cancel
	go func() {
		<-ctx.Done()
		c.providerMu.Lock()
		delete(c.providerSubs, name)
		c.providerMu.Unlock()

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
				sub.wg.Add(1)
				go func(s *providerSubscription, val any) {
					defer s.wg.Done()
					select {
					case s.changeRequests <- val:
					default:
						// Channel full, skip
					}
				}(sub, value)
			}
		}
		c.providerMu.Unlock()
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
			// Update current value in subscription
			c.providerMu.Lock()
			if sub, exists := c.providerSubs[name]; exists {
				sub.currentValue = value
			}
			c.providerMu.Unlock()

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
			if !exists {
				c.providerMu.Unlock()
				return
			}
			currentValue := sub.currentValue
			c.providerMu.Unlock()

			// Refresh with current value
			c.providerClient.SetRegister(ctx, name, currentValue, metadata, ttl)
		}
	}
}
