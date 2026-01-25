package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	clientfactory "github.com/burgrp/reg/pkg/client/factory"
	"github.com/spf13/cobra"
)

func newProvideCmd() *cobra.Command {
	var ttl time.Duration
	var readOnly bool

	cmd := &cobra.Command{
		Use:   "provide <name> [value] [metadata]",
		Short: "Provide a register to the registry",
		Long: `Provide a register with initial value and metadata.
Reads new values from stdin (one JSON value per line) and writes change requests to stdout.

Value and metadata must be valid JSON. If not provided, defaults to null and {}.

Examples:
  reg provide temp
  reg provide temp 25.5
  reg provide temp 25.5 '{"unit":"celsius"}'
  echo '30.0' | reg provide temp 25.5 '{"unit":"celsius"}'`,
		Args: cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProvide(args, ttl, readOnly)
		},
	}

	cmd.Flags().DurationVarP(&ttl, "ttl", "t", 5*time.Second, "Time to live for the register")
	cmd.Flags().BoolVarP(&readOnly, "read-only", "r", false, "Run in read-only register")
	return cmd
}

func runProvide(args []string, ttl time.Duration, readOnly bool) error {
	name := args[0]

	// Parse initial value (default: null)
	var value any
	if len(args) >= 2 {
		if err := json.Unmarshal([]byte(args[1]), &value); err != nil {
			return fmt.Errorf("invalid value JSON: %w", err)
		}
	}

	// Parse metadata (default: empty map)
	metadata := make(map[string]any)
	if len(args) >= 3 {
		if err := json.Unmarshal([]byte(args[2]), &metadata); err != nil {
			return fmt.Errorf("invalid metadata JSON: %w", err)
		}
	}

	// Create client from environment
	client, err := clientfactory.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx := context.Background()

	// Provide the register
	updates, changeRequests, err := client.Provide(ctx, name, value, metadata, ttl)
	if err != nil {
		return fmt.Errorf("failed to provide register: %w", err)
	}

	// Handle change requests (write to stdout)
	go func() {
		for req := range changeRequests {
			if !readOnly {
				updates <- req
			}
		}
	}()

	// Read values from stdin (send to updates channel)
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var newValue any
		if err := json.Unmarshal([]byte(line), &newValue); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid JSON input: %v\n", err)
			continue
		}

		updates <- newValue
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stdin: %w", err)
	}

	return nil
}
