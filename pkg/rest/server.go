package rest

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/burgrp/reg/pkg/registry"
	"golang.org/x/sync/errgroup"
)

// Server provides REST API for the registry
type Server struct {
	registry *registry.Registry
	logger   *slog.Logger
}

func RunServer(address string, registry *registry.Registry, logger *slog.Logger, eg *errgroup.Group) error {
	server := &Server{
		registry: registry,
		logger:   logger,
	}

	server.logger.Info("starting REST server", "addr", address)

	mux := http.NewServeMux()

	mux.HandleFunc("/consumer", server.handleConsumer)
	mux.HandleFunc("/provider", server.handleProvider)

	return http.ListenAndServe(address, mux)
}

func (s *Server) writeResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode response", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}
