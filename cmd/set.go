package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"

	clientfactory "github.com/burgrp/reg/pkg/client/factory"
	"github.com/spf13/cobra"
)

func newSetCmd() *cobra.Command {
	var withMetadata bool
	var stay bool
	var timeout time.Duration

	cmd := &cobra.Command{
		Use:   "set <name> [value]",
		Short: "Request a change to a register value",
		Long: `Request a change to a register value in the registry.

Without --stay flag, requests a single change and waits for it to be applied.
With --stay flag, continuously reads values from stdin (one JSON value per line),
requests changes, and outputs the actual register values to stdout.

Value must be valid JSON. If not provided, defaults to null.

Requires REGISTRY environment variable to be set (e.g., http://localhost:8080).

Examples:
  export REGISTRY=http://localhost:8080
  reg set temp 25.5
  reg set temp null
  reg set temp 30.5 --timeout 5s
  echo '30.0' | reg set temp --stay
  reg set temp --stay --metadata`,
		Args: func(cmd *cobra.Command, args []string) error {
			if stay {
				// In --stay mode, only name is allowed (no value)
				if len(args) != 1 {
					return fmt.Errorf("in --stay mode, only register name is allowed (values are read from stdin)")
				}
			} else {
				// Without --stay, name and optional value
				if len(args) < 1 || len(args) > 2 {
					return fmt.Errorf("requires register name and optional value")
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSet(args, withMetadata, stay, timeout)
		},
	}

	cmd.Flags().BoolVarP(&withMetadata, "metadata", "m", false, "include metadata in output (stay mode only)")
	cmd.Flags().BoolVarP(&stay, "stay", "s", false, "stay running, read values from stdin and output register values")
	cmd.Flags().DurationVarP(&timeout, "timeout", "t", 2*time.Second, "timeout for waiting for value to be set (non-stay mode)")
	return cmd
}

func runSet(args []string, withMetadata bool, stay bool, timeout time.Duration) error {
	name := args[0]

	// Create client from environment
	c, err := clientfactory.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx := context.Background()

	if !stay {
		// Single request mode with timeout
		var desiredValue any
		if len(args) >= 2 {
			if err := json.Unmarshal([]byte(args[1]), &desiredValue); err != nil {
				return fmt.Errorf("invalid value JSON: %w", err)
			}
		}

		// Subscribe to get both values and requests channels
		values, requests, err := c.Consume(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to consume register: %w", err)
		}

		// Get initial value
		var initialValue any
		select {
		case v := <-values:
			initialValue = v.Value
		case <-time.After(timeout):
			return fmt.Errorf("timeout waiting for initial value")
		}

		// Check if the value is already what we want (edge case)
		if reflect.DeepEqual(initialValue, desiredValue) {
			return nil // Already set, nothing to do
		}

		// Send the change request
		select {
		case requests <- desiredValue:
			// Request queued successfully
		case <-ctx.Done():
			return ctx.Err()
		}

		// Wait for the value to match what we requested
		timeoutTimer := time.NewTimer(timeout)
		defer timeoutTimer.Stop()

		for {
			select {
			case v := <-values:
				if reflect.DeepEqual(v.Value, desiredValue) {
					return nil // Successfully set
				}
				// Value changed but not to what we want, keep waiting
			case <-timeoutTimer.C:
				return fmt.Errorf("timeout waiting for value to be set")
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}

	// Stay mode: continuously request changes and output values
	values, requests, err := c.Consume(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to consume register: %w", err)
	}

	// Goroutine to read values and output to stdout
	go func() {
		for v := range values {
			outputValue(name, v, withMetadata, false)
		}
	}()

	// Read from stdin and send change requests
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()

		var newValue any
		if line != "" {
			if err := json.Unmarshal([]byte(line), &newValue); err != nil {
				fmt.Fprintf(os.Stderr, "Invalid JSON input: %v\n", err)
				continue
			}
		}

		requests <- newValue
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stdin: %w", err)
	}

	return nil
}
