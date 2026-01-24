package rest

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/burgrp/reg/pkg/registry"
	"golang.org/x/sync/errgroup"
)

// Server provides REST API for the registry
type Server struct {
	registry *registry.Registry
	logger   *slog.Logger
}

func RunServer(ctx context.Context, address string, registry *registry.Registry, logger *slog.Logger, eg *errgroup.Group) error {
	server := &Server{
		registry: registry,
		logger:   logger,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/consumer", server.handleConsumer)
	mux.HandleFunc("/provider", server.handleProvider)

	httpServer := &http.Server{
		Addr:    address,
		Handler: mux,
	}

	// Start HTTP server in goroutine
	eg.Go(func() error {
		server.logger.Info("starting REST server", "addr", address)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})

	// Wait for context cancellation (shutdown signal)
	eg.Go(func() error {
		<-ctx.Done()
		server.logger.Info("shutting down REST server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			server.logger.Error("error during server shutdown", "error", err)
			return err
		}

		server.logger.Info("REST server stopped")
		return nil
	})

	return nil
}

// parseQueryParams extracts wait duration and register names from query parameters
func (s *Server) parseQueryParams(r *http.Request) (names []string, wait time.Duration, err error) {
	waitStr := r.URL.Query().Get("wait")
	if waitStr != "" {
		wait, err = time.ParseDuration(waitStr)
		if err != nil {
			return nil, 0, err
		}
	}

	names = r.URL.Query()["name"]
	if r.URL.Query().Has("names") {
		nameStr := r.URL.Query().Get("names")
		if nameStr != "" {
			names = append(names, strings.Split(nameStr, ",")...)
		}
	}

	return names, wait, nil
}

func (s *Server) writeResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode response", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}
