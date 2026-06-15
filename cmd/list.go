package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	clientfactory "github.com/burgrp/reg/pkg/client/factory"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var withMetadata bool
	var stay bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registers from the registry",
		Long: `List all registers from the registry.

Without --stay flag, lists all registers once and exits.
With --stay flag, continuously outputs register updates as they occur.

Output format is "name=value" per line (one register per line).
With --metadata flag, outputs JSON with name, value, and metadata.

Requires REGISTRY environment variable to be set (e.g., http://localhost:8080).

Examples:
  export REGISTRY=http://localhost:8080
  reg list
  reg list --metadata
  reg list --stay
  reg list --stay --metadata`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(withMetadata, stay)
		},
	}

	cmd.Flags().BoolVarP(&withMetadata, "metadata", "m", false, "include metadata in output")
	cmd.Flags().BoolVarP(&stay, "stay", "s", false, "stay running and output updates")
	return cmd
}

func runList(withMetadata bool, stay bool) error {
	// Create client from environment
	c, err := clientfactory.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx := context.Background()

	if !stay {
		// Single list mode
		updates, _, err := c.ConsumeAll(ctx)
		if err != nil {
			return fmt.Errorf("failed to consume registers: %w", err)
		}

		// Collect all initial values (they come immediately)
		// We need to read until we've gotten all the initial values
		// The channel won't close, so we use a timeout
		type register struct {
			name     string
			value    any
			metadata map[string]any
		}
		registers := make(map[string]register)

		// Read initial batch (with a small timeout to detect when done)
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()

	readLoop:
		for {
			select {
			case update, ok := <-updates:
				if !ok {
					break readLoop
				}
				if update.Removed {
					delete(registers, update.Name)
					continue
				}
				registers[update.Name] = register{
					name:     update.Name,
					value:    update.Value,
					metadata: update.Metadata,
				}
				// Reset timer after each update
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(100 * time.Millisecond)
			case <-timer.C:
				// No more updates for 100ms, assume we got them all
				break readLoop
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Output all registers
		for _, reg := range registers {
			outputRegister(reg.name, reg.value, reg.metadata, withMetadata)
		}

		return nil
	}

	// Stay mode: continuously output updates
	updates, _, err := c.ConsumeAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to consume registers: %w", err)
	}

	for update := range updates {
		if update.Removed {
			continue
		}
		outputRegister(update.Name, update.Value, update.Metadata, withMetadata)
	}

	return nil
}

func outputRegister(name string, value any, metadata map[string]any, withMetadata bool) {
	if withMetadata {
		// Output as JSON object with name, value, and metadata
		output := map[string]any{
			"name":     name,
			"value":    value,
			"metadata": metadata,
		}
		data, _ := json.Marshal(output)
		fmt.Println(string(data))
	} else {
		// Output as name=value
		data, _ := json.Marshal(value)
		fmt.Printf("%s=%s\n", name, string(data))
	}
}
