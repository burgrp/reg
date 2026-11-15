package cmd

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/burgrp/reg/internal/metrics"
	"github.com/burgrp/reg/internal/registry"
	"github.com/burgrp/reg/internal/rest"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

func newServeCmd() *cobra.Command {
	var addr string
	var metricsAddr string

	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the registry",
		Long: `Start a registry server with REST API and optional Prometheus metrics.

LOG_LEVEL controls diagnostic logging (default: info).
Accepts debug, info, warn, error (case-insensitive) and numeric offsets.

Example:
  LOG_LEVEL=debug reg serve --metrics :9090`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(addr, metricsAddr)
		},
	}

	cmd.Flags().StringVarP(&addr, "rest", "r", ":8080", "Address to listen on for REST protocol")
	cmd.Flags().StringVarP(&metricsAddr, "metrics", "m", "", "Address to listen on for Prometheus metrics (disabled if empty)")

	return cmd
}

func runServe(addr string, metricsAddr string) error {
	logger := slog.Default()

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

	// Start Prometheus metrics server if address is configured
	if metricsAddr != "" {
		if err := metrics.RunServer(ctx, metricsAddr, reg, logger, &errGroup); err != nil {
			return err
		}
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
