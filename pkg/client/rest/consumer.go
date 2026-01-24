package rest

import (
	"context"
	"time"

	"github.com/burgrp/reg/pkg/client"
)

// Consume implements client.Client.Consume
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
