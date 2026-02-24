package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/burgrp/reg/internal/registry"
	"github.com/burgrp/reg/internal/rest"
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

	cmd.Flags().StringVarP(&addr, "rest", "r", ":8080", "Address to listen on for REST protocol")

	return cmd
}

func runServe(addr string) error {
	// Setup logger with tint
	logger := slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      slog.LevelDebug,
		TimeFormat: time.TimeOnly,
	}))

	// Create context that cancels on shutdown signal
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Create registry
	reg := registry.NewRegistry(logger)

	// Create stop channel for registry cleanup goroutine
	stopChan := make(chan struct{})
	reg.Start(stopChan)

	var errGroup errgroup.Group

	// Start REST server
	if err := rest.RunServer(ctx, addr, reg, logger, &errGroup); err != nil {
		return err
	}

	// Handle shutdown signal
	errGroup.Go(func() error {
		sig := <-sigChan
		logger.Info("received shutdown signal", "signal", sig)
		cancel()
		close(stopChan)
		return nil
	})

	return errGroup.Wait()
}
