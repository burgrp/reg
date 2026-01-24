package cmd

import (
	"log/slog"
	"os"
	"time"

	"github.com/burgrp/reg/pkg/registry"
	"github.com/burgrp/reg/pkg/rest"
	"github.com/lmittmann/tint"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func newServeCmd() *cobra.Command {
	var addr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the registry",
		Long:  `Starts the registry server with specified parameters.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(addr)
		},
	}

	cmd.Flags().StringVarP(&addr, "addr", "a", ":8080", "Address to listen on")

	return cmd
}

func runServe(addr string) error {
	// Setup logger with tint
	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.TimeOnly,
	}))

	// Create registry
	registry := registry.NewRegistry(logger)

	var errGroup errgroup.Group

	// Start REST server
	errGroup.Go(func() error {
		return rest.RunServer(addr, registry, logger, &errGroup)
	})

	return errGroup.Wait()
}
