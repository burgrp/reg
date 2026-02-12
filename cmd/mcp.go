package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	clientfactory "github.com/burgrp/reg/pkg/client/factory"
	"github.com/burgrp/reg/pkg/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
)

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Start MCP (Model Context Protocol) stdio server",
		Long: `Start an MCP server that communicates over stdio using JSON-RPC.

The MCP server exposes tools for interacting with the registry:
- get_register: Get a register's value and metadata
- set_register: Set a register's value (provider operation)
- list_registers: List all registers with their values
- request_change: Request a value change (consumer operation)

Requires REGISTRY environment variable to be set (e.g., http://localhost:8080).

This command is designed to be used by MCP clients (like Claude Desktop) via stdio.

Example:
  export REGISTRY=http://localhost:8080
  reg mcp`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runMCP()
		},
	}

	return cmd
}

func runMCP() error {
	// Create client from environment
	regClient, err := clientfactory.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	ctx := context.Background()

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "reg-registry",
		Version: "1.0.0",
	}, nil)

	// Register tools
	registerGetTool(server, regClient)
	registerSetTool(server, regClient)
	registerListTool(server, regClient)
	registerRequestChangeTool(server, regClient)

	// Start stdio server
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("MCP server error: %w", err)
	}

	return nil
}

// Input structs for tools

type getRegisterArgs struct {
	Name string `json:"name" jsonschema:"The name of the register to get"`
}

type setRegisterArgs struct {
	Name     string         `json:"name" jsonschema:"The name of the register to set"`
	Value    any            `json:"value" jsonschema:"The value to set (can be any JSON type)"`
	Metadata map[string]any `json:"metadata,omitempty" jsonschema:"Optional metadata for the register"`
	TTL      string         `json:"ttl,omitempty" jsonschema:"Optional TTL duration (e.g. '5s' '10m'). Default is 5s"`
}

type requestChangeArgs struct {
	Name  string `json:"name" jsonschema:"The name of the register to change"`
	Value any    `json:"value" jsonschema:"The requested new value (can be any JSON type)"`
}

type listRegistersArgs struct {
}

// Tool registration functions

func registerGetTool(server *mcp.Server, regClient client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_register",
		Description: "Get the current value and metadata of a register",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getRegisterArgs) (*mcp.CallToolResult, any, error) {
		// Use Consume to get the register value
		values, _, err := regClient.Consume(ctx, args.Name)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Error: %v", err)},
				},
				IsError: true,
			}, nil, nil
		}

		// Wait for the first value with a timeout
		select {
		case value, ok := <-values:
			if !ok {
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Register not found or channel closed"},
					},
					IsError: true,
				}, nil, nil
			}

			result := fmt.Sprintf("Register: %s\nValue: %v", args.Name, value.Value)
			if len(value.Metadata) > 0 {
				result += fmt.Sprintf("\nMetadata: %v", value.Metadata)
			}

			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: result},
				},
			}, nil, nil

		case <-time.After(100 * time.Millisecond):
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Register not found (timeout)"},
				},
				IsError: true,
			}, nil, nil

		case <-ctx.Done():
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "Context cancelled"},
				},
				IsError: true,
			}, nil, nil
		}
	})
}

func registerSetTool(server *mcp.Server, regClient client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "set_register",
		Description: "Set a register's value and metadata (provider operation)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args setRegisterArgs) (*mcp.CallToolResult, any, error) {
		// Parse TTL
		ttlStr := args.TTL
		if ttlStr == "" {
			ttlStr = "5s"
		}
		ttl, err := time.ParseDuration(ttlStr)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Invalid TTL: %v", err)},
				},
				IsError: true,
			}, nil, nil
		}

		metadata := args.Metadata
		if metadata == nil {
			metadata = make(map[string]any)
		}

		// Use Provide to set the register
		updates, _, err := regClient.Provide(ctx, args.Name, args.Value, metadata, ttl)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Failed to provide: %v", err)},
				},
				IsError: true,
			}, nil, nil
		}

		// Send the initial value
		updates <- args.Value

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Register '%s' set to: %v", args.Name, args.Value)},
			},
		}, nil, nil
	})
}

func registerListTool(server *mcp.Server, regClient client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_registers",
		Description: "List all registers with their current values and metadata",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listRegistersArgs) (*mcp.CallToolResult, any, error) {
		// Use ConsumeAll to get all registers
		updates, err := regClient.ConsumeAll(ctx)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Failed to consume: %v", err)},
				},
				IsError: true,
			}, nil, nil
		}

		type registerInfo struct {
			Value    any
			Metadata map[string]any
		}
		registers := make(map[string]registerInfo)

		// Collect all registers with timeout
		timer := time.NewTimer(100 * time.Millisecond)
		defer timer.Stop()

	readLoop:
		for {
			select {
			case update, ok := <-updates:
				if !ok {
					break readLoop
				}
				registers[update.Name] = registerInfo{
					Value:    update.Value,
					Metadata: update.Metadata,
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
				break readLoop
			case <-ctx.Done():
				return &mcp.CallToolResult{
					Content: []mcp.Content{
						&mcp.TextContent{Text: "Context cancelled"},
					},
					IsError: true,
				}, nil, nil
			}
		}

		// Build response text
		result := fmt.Sprintf("Found %d registers:\n\n", len(registers))
		for name, reg := range registers {
			result += fmt.Sprintf("• %s = %v\n", name, reg.Value)
			if len(reg.Metadata) > 0 {
				result += fmt.Sprintf("  Metadata: %v\n", reg.Metadata)
			}
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: result},
			},
		}, nil, nil
	})
}

func registerRequestChangeTool(server *mcp.Server, regClient client.Client) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "request_change",
		Description: "Request a change to a register's value (consumer operation)",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args requestChangeArgs) (*mcp.CallToolResult, any, error) {
		// Use Consume to request change
		_, requests, err := regClient.Consume(ctx, args.Name)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: fmt.Sprintf("Failed to consume: %v", err)},
				},
				IsError: true,
			}, nil, nil
		}

		// Send change request
		requests <- args.Value

		// Note: The actual change is asynchronous and depends on the provider
		slog.Info("Change request sent", "register", args.Name, "value", args.Value)

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: fmt.Sprintf("Change request sent for register '%s' to value: %v\nNote: The actual change depends on the provider's response.", args.Name, args.Value)},
			},
		}, nil, nil
	})
}
