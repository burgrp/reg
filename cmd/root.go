package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
)

func Execute() error {
	rootCmd := newRootCmd()
	return rootCmd.Execute()
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reg",
		Short: "Registry server and command-line client",
		Long: `Read, publish and request changes to named registers, or run a registry server.

Environment:
  REGISTRY   Registry URL for client commands (e.g. http://localhost:8080).
  LOG_LEVEL  Diagnostic logging level for all commands (default: info).
             Accepts debug, info, warn, error (case-insensitive) and numeric
             offsets such as INFO+2. Does not filter command output.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			level := slog.LevelInfo
			if value := os.Getenv("LOG_LEVEL"); value != "" {
				if err := level.UnmarshalText([]byte(value)); err != nil {
					return fmt.Errorf("invalid LOG_LEVEL %q: %w", value, err)
				}
			}

			slog.SetDefault(slog.New(tint.NewHandler(cmd.ErrOrStderr(), &tint.Options{
				Level:      level,
				TimeFormat: time.TimeOnly,
			})))
			return nil
		},
	}

	cmd.AddCommand(newServeCmd())
	cmd.AddCommand(newProvideCmd())
	cmd.AddCommand(newGetCmd())
	cmd.AddCommand(newSetCmd())
	cmd.AddCommand(newListCmd())
	cmd.AddCommand(newBrowseCmd())
	cmd.AddCommand(newVersionCmd())
	cmd.AddCommand(newMCPCmd())

	return cmd
}
