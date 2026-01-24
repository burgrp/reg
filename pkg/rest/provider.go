package rest

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type ProviderPutRegister struct {
	Value    any            `json:"value,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	TTL      Duration       `json:"ttl,omitempty"`
}

type ProviderPutRequest struct {
	Registers map[string]ProviderPutRegister `json:"registers"`
}

type ProviderGetRegister struct {
	Value any `json:"value,omitempty"`
}

type ProviderResponse struct {
	Registers map[string]ProviderGetRegister `json:"registers"`
}

func (s *Server) handleProvider(w http.ResponseWriter, r *http.Request) {

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

		requests := s.registry.WaitForChangeRequests(names, wait)

		response := ProviderResponse{
			Registers: make(map[string]ProviderGetRegister),
		}

		for name, value := range requests {
			response.Registers[name] = ProviderGetRegister{
				Value: value,
			}
		}

		s.writeResponse(w, response)
		return
	}

	if r.Method == http.MethodPut {
		var req ProviderPutRequest
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

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
