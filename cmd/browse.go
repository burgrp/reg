package cmd

import (
	"fmt"

	"github.com/burgrp/reg/pkg/browser"
	clientfactory "github.com/burgrp/reg/pkg/client/factory"
	"github.com/spf13/cobra"
)

func newBrowseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "browse",
		Short: "Browse registers in an interactive TUI",
		Long: `Browse registers in an interactive text-based user interface.

Features:
- View all registers in real-time
- Toggle between flat and tree view (t key)
- Toggle metadata panel (m key)
- Navigate with arrow keys
- Edit register values (Enter key) - coming soon
- Quit with 'q'

Requires REGISTRY environment variable to be set (e.g., http://localhost:8080).

Examples:
  export REGISTRY=http://localhost:8080
  reg browse`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBrowse()
		},
	}

	return cmd
}

func runBrowse() error {
	// Create client from environment
	c, err := clientfactory.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}

	// Create and run browser
	b := browser.New(c)
	return b.Run()
}
