package rest

import (
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
