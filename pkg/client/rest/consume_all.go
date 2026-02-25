package rest

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/burgrp/reg/pkg/client"
)

// ConsumeAll implements client.Client.ConsumeAll
func (c *Client) ConsumeAll(ctx context.Context) (<-chan client.RegisterUpdate, chan<- client.RegisterChangeRequest, error) {
	updates := make(chan client.RegisterUpdate, 10)
	requests := make(chan client.RegisterChangeRequest, 10)

	// Track last seen values for each register to avoid duplicates
	lastValues := make(map[string]any)
	lastMetadata := make(map[string]map[string]any)
	var mu sync.Mutex

	go func() {
		defer close(updates)

		// Get initial values for all registers (no-wait)
		registers, err := c.consumerClient.GetRegisters(ctx, nil, 0)
		if err == nil {
			mu.Lock()
			for name, reg := range registers {
				// Deep copy metadata
				var metadataCopy map[string]any
				if reg.Metadata != nil {
					metadataCopy = make(map[string]any, len(reg.Metadata))
					for k, v := range reg.Metadata {
						metadataCopy[k] = v
					}
				}

				lastValues[name] = reg.Value
				lastMetadata[name] = metadataCopy

				select {
				case updates <- client.RegisterUpdate{
					Name:     name,
					Value:    reg.Value,
					Metadata: metadataCopy,
				}:
				case <-ctx.Done():
					mu.Unlock()
					return
				}
			}
			mu.Unlock()
		}

		// Continuous long polling for all registers
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			// Long poll for changes (5 seconds)
			registers, err := c.consumerClient.GetRegisters(ctx, nil, 5*time.Second)
			if err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(1 * time.Second):
					continue // Back off on error
				}
			}

			mu.Lock()
			for name, reg := range registers {
				// Check if value or metadata changed
				lastVal, hasLastVal := lastValues[name]
				lastMeta := lastMetadata[name]

				valueChanged := !hasLastVal || !reflect.DeepEqual(lastVal, reg.Value)
				metadataChanged := !reflect.DeepEqual(lastMeta, reg.Metadata)

				if valueChanged || metadataChanged {
					// Deep copy metadata
					var metadataCopy map[string]any
					if reg.Metadata != nil {
						metadataCopy = make(map[string]any, len(reg.Metadata))
						for k, v := range reg.Metadata {
							metadataCopy[k] = v
						}
					}

					lastValues[name] = reg.Value
					lastMetadata[name] = metadataCopy

					select {
					case updates <- client.RegisterUpdate{
						Name:     name,
						Value:    reg.Value,
						Metadata: metadataCopy,
					}:
					case <-ctx.Done():
						mu.Unlock()
						return
					}
				}
			}
			mu.Unlock()
		}
	}()

	// Handle change requests
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case req, ok := <-requests:
				if !ok {
					return
				}
				c.consumerClient.RequestChange(ctx, req.Name, req.Value)
			}
		}
	}()

	return updates, requests, nil
}
