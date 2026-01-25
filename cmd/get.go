package cmd

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/burgrp/reg/pkg/client"
	clientfactory "github.com/burgrp/reg/pkg/client/factory"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	var withMetadata bool
	var stay bool

	cmd := &cobra.Command{
		Use:   "get <name> [name...]",
		Short: "Get register values from the registry",
		Long: `Get one or more register values from the registry.

Outputs register values to stdout. For a single register, outputs just the value.
For multiple registers, outputs in "name=value" format.

With -m flag, includes metadata in the output.
With --stay flag, continues running and outputs updates as they arrive.

Examples:
  reg get temp
  reg get temp humidity pressure
  reg get temp -m
  reg get temp --stay`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(args, withMetadata, stay)
		},
	}

	cmd.Flags().BoolVarP(&withMetadata, "metadata", "m", false, "include metadata in output")
	cmd.Flags().BoolVar(&stay, "stay", false, "stay running and output updates")
	return cmd
}

func runGet(names []string, withMetadata bool, stay bool) error {
	// Create client from environment
	c, err := clientfactory.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx := context.Background()
	multipleRegisters := len(names) > 1

	// Subscribe to all registers
	channels := make(map[string]<-chan client.ValueAndMetadata)
	for _, name := range names {
		values, _, err := c.Consume(ctx, name)
		if err != nil {
			return fmt.Errorf("failed to consume register %q: %w", name, err)
		}
		channels[name] = values
	}

	// If not staying, just get initial values
	if !stay {
		for _, name := range names {
			select {
			case v := <-channels[name]:
				outputValue(name, v, withMetadata, multipleRegisters)
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}

	// Stay mode: continuously output updates
	// We need to multiplex all channels
	type update struct {
		name  string
		value client.ValueAndMetadata
	}
	updatesChan := make(chan update)

	for name, ch := range channels {
		go func(name string, ch <-chan client.ValueAndMetadata) {
			for v := range ch {
				updatesChan <- update{name: name, value: v}
			}
		}(name, ch)
	}

	for u := range updatesChan {
		outputValue(u.name, u.value, withMetadata, multipleRegisters)
	}

	return nil
}

func outputValue(name string, v client.ValueAndMetadata, withMetadata bool, multipleRegisters bool) {
	if withMetadata {
		// Output as JSON object with value and metadata
		output := map[string]any{
			"value":    v.Value,
			"metadata": v.Metadata,
		}
		if multipleRegisters {
			// Include name in output for multiple registers
			output["name"] = name
		}
		data, _ := json.Marshal(output)
		fmt.Println(string(data))
	} else {
		// Output just the value
		data, _ := json.Marshal(v.Value)
		if multipleRegisters {
			fmt.Printf("%s=%s\n", name, string(data))
		} else {
			fmt.Println(string(data))
		}
	}
}
