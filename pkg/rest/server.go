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

	mux.HandleFunc("/registers", server.handleRegisters)
	mux.HandleFunc("/provider/requests", server.handleProviderRequests)

	return http.ListenAndServe(address, mux)
}

type RegistersGetRegister struct {
	Value    any            `json:"value,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type RegistersGetResponse struct {
	Registers map[string]RegistersGetRegister `json:"registers"`
}

type RegistersPutRegister struct {
	Value    any            `json:"value,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	TTL      Duration       `json:"ttl,omitempty"`
}

type RegistersPutRequest struct {
	Registers map[string]RegistersPutRegister `json:"registers"`
}

type RegistersPatchRegister struct {
	Value any `json:"value,omitempty"`
}

type RegistersPatchRequest struct {
	Registers map[string]RegistersPatchRegister `json:"registers"`
}

type ProviderRequestsGetRegister struct {
	Value any `json:"value,omitempty"`
}

type ProviderRequestsGetResponse struct {
	Registers map[string]ProviderRequestsGetRegister `json:"registers"`
}

func (s *Server) handleRegisters(w http.ResponseWriter, r *http.Request) {

	if r.Method == http.MethodGet {

		waitStr := r.URL.Query().Get("wait")
		var wait time.Duration
		if waitStr != "" {
			var err error
			wait, err = time.ParseDuration(waitStr)
			if err != nil {
				s.logger.Error("invalid wait parameter", "error", err)
				http.Error(w, "invalid wait parameter", http.StatusBadRequest)
				return
			}
		}

		names := r.URL.Query()["name"]
		if r.URL.Query().Has("names") {
			nameStr := r.URL.Query().Get("names")
			if nameStr != "" {
				names = append(names, strings.Split(nameStr, ",")...)
			}
		}

		registers := s.registry.WaitForChange(names, wait)

		response := RegistersGetResponse{
			Registers: make(map[string]RegistersGetRegister, len(registers)),
		}

		for name, reg := range registers {
			response.Registers[name] = RegistersGetRegister{
				Value:    reg.Value,
				Metadata: reg.Metadata,
			}
		}

		s.writeResponse(w, response)

		return
	}

	if r.Method == http.MethodPut {
		var req RegistersPutRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.logger.Error("failed to decode request", "error", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		for name, reg := range req.Registers {
			s.registry.SetRegister(name, reg.Value, reg.Metadata, time.Duration(reg.TTL))
		}

		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method == http.MethodPatch {
		var req RegistersPatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.logger.Error("failed to decode request", "error", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		for name, reg := range req.Registers {
			s.registry.RequestChange(name, reg.Value)
		}

		w.WriteHeader(http.StatusAccepted)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *Server) handleProviderRequests(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	waitStr := r.URL.Query().Get("wait")
	var wait time.Duration
	if waitStr != "" {
		var err error
		wait, err = time.ParseDuration(waitStr)
		if err != nil {
			s.logger.Error("invalid wait parameter", "error", err)
			http.Error(w, "invalid wait parameter", http.StatusBadRequest)
			return
		}
	}

	names := r.URL.Query()["name"]
	if r.URL.Query().Has("names") {
		nameStr := r.URL.Query().Get("names")
		if nameStr != "" {
			names = append(names, strings.Split(nameStr, ",")...)
		}
	}

	requests := s.registry.WaitForChangeRequests(names, wait)

	response := ProviderRequestsGetResponse{
		Registers: make(map[string]ProviderRequestsGetRegister),
	}

	for name, value := range requests {
		response.Registers[name] = ProviderRequestsGetRegister{
			Value: value,
		}
	}

	s.writeResponse(w, response)
}

func (s *Server) writeResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		s.logger.Error("failed to encode response", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
}
