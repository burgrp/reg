package rest

import (
	"context"
	"reflect"
	"time"

	"github.com/burgrp/reg/pkg/client"
)

// Consume implements client.Client.Consume
func (c *Client) Consume(ctx context.Context, name string) (<-chan client.ValueAndMetadata, chan<- any, error) {
	c.consumerMu.Lock()
	defer c.consumerMu.Unlock()

	// Create subscription with its own context
	subCtx, subCancel := context.WithCancel(context.Background())
	sub := &consumerSubscription{
		ctx:      subCtx,
		cancel:   subCancel,
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
	sub.wg.Add(1)
	go func() {
		defer sub.wg.Done()

		registers, err := c.consumerClient.GetRegisters(ctx, []string{name}, 0)
		if err == nil {
			if reg, exists := registers[name]; exists {
				// Deep copy metadata to avoid shared references
				var metadataCopy map[string]any
				if reg.Metadata != nil {
					metadataCopy = make(map[string]any, len(reg.Metadata))
					for k, v := range reg.Metadata {
						metadataCopy[k] = v
					}
				}

				// Track the value we're sending
				c.consumerMu.Lock()
				sub.lastValue = reg.Value
				sub.lastMetadata = metadataCopy
				c.consumerMu.Unlock()

				select {
				case sub.values <- client.ValueAndMetadata{Value: reg.Value, Metadata: metadataCopy}:
				case <-sub.ctx.Done():
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
		c.consumerMu.Unlock()

		// Cancel subscription context to signal senders to stop
		sub.cancel()

		// Wait for all active senders to finish before closing channels
		sub.wg.Wait()
		close(sub.values)
		close(sub.requests)

		// Stop batch poller if no more subscriptions
		c.consumerMu.Lock()
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

		// Long poll for changes
		pollInterval := c.ConsumerPollInterval
		if pollInterval == 0 {
			pollInterval = 5 * time.Second
		}
		registers, err := c.consumerClient.GetRegisters(ctx, names, pollInterval)
		if err != nil {
			time.Sleep(1 * time.Second) // Back off on error
			continue
		}

		// Distribute updates to subscribers
		c.consumerMu.Lock()
		for name, reg := range registers {
			if sub, exists := c.consumerSubs[name]; exists {
				// Only send if value or metadata actually changed
				valueChanged := !reflect.DeepEqual(sub.lastValue, reg.Value)
				metadataChanged := !reflect.DeepEqual(sub.lastMetadata, reg.Metadata)

				if valueChanged || metadataChanged {
					// Deep copy metadata to avoid shared references
					var metadataCopy map[string]any
					if reg.Metadata != nil {
						metadataCopy = make(map[string]any, len(reg.Metadata))
						for k, v := range reg.Metadata {
							metadataCopy[k] = v
						}
					}

					sub.lastValue = reg.Value
					sub.lastMetadata = metadataCopy
					sub.wg.Add(1)
					go func(s *consumerSubscription, val client.ValueAndMetadata) {
						defer s.wg.Done()
						select {
						case s.values <- val:
						case <-s.ctx.Done():
							// Subscription is being torn down, don't send
						default:
							// Channel full, skip
						}
					}(sub, client.ValueAndMetadata{Value: reg.Value, Metadata: metadataCopy})
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
