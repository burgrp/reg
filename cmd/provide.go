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
			return runProvide(args, ttl)
		},
	}

	cmd.Flags().DurationVarP(&ttl, "ttl", "t", 5*time.Second, "Time to live for the register")

	return cmd
}

func runProvide(args []string, ttl time.Duration) error {
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

	fmt.Fprintf(os.Stderr, "Providing register '%s' with value %v, metadata %v, TTL %v\n", name, value, metadata, ttl)
	fmt.Fprintln(os.Stderr, "Reading new values from stdin (one JSON value per line)...")
	fmt.Fprintln(os.Stderr, "Writing change requests to stdout...")

	// Handle change requests (write to stdout)
	go func() {
		for req := range changeRequests {
			jsonBytes, err := json.Marshal(req)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error marshaling change request: %v\n", err)
				continue
			}
			fmt.Println(string(jsonBytes))
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
		fmt.Fprintf(os.Stderr, "Updated register '%s' to: %v\n", name, newValue)
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading stdin: %w", err)
	}

	return nil
}
