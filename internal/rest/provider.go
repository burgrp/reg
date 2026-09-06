package rest

import (
	"encoding/json"
	"net/http"
	"time"
)

// ProviderPutRegister represents a register update from a provider
type ProviderPutRegister struct {
	Value        any            `json:"value"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	TTL          Duration       `json:"ttl,omitempty"`
	valuePresent bool
}

func (r *ProviderPutRegister) UnmarshalJSON(data []byte) error {
	type registerAlias ProviderPutRegister
	var value registerAlias
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*r = ProviderPutRegister(value)
	_, r.valuePresent = fields["value"]
	return nil
}

// ProviderPutRequest is the request format for provider PUT requests
type ProviderPutRequest struct {
	Registers map[string]ProviderPutRegister `json:"registers"`
}

// ProviderGetRegister represents a change request in provider GET responses
type ProviderGetRegister struct {
	Value any `json:"value"`
}

// ProviderResponse is the response format for provider GET requests
type ProviderResponse struct {
	Registers map[string]ProviderGetRegister `json:"registers"`
}

// handleProvider handles provider endpoints: PUT for setting/updating registers,
// GET for polling consumer change requests with long polling support
func (s *Server) handleProvider(w http.ResponseWriter, r *http.Request) {

	if r.Method == http.MethodGet {
		names, wait, err := s.parseQueryParams(r)
		if err != nil {
			s.logger.Error("invalid wait parameter", "error", err)
			http.Error(w, "invalid wait parameter", http.StatusBadRequest)
			return
		}

		requests := s.registry.WaitForChangeRequestsWithContext(r.Context(), names, wait)

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
		if req.Registers == nil {
			http.Error(w, "registers is required", http.StatusBadRequest)
			return
		}
		for _, reg := range req.Registers {
			if !reg.valuePresent {
				http.Error(w, "register value is required", http.StatusBadRequest)
				return
			}
		}

		for name, reg := range req.Registers {
			s.registry.SetRegister(name, reg.Value, reg.Metadata, time.Duration(reg.TTL))
		}

		w.WriteHeader(http.StatusNoContent)
		return
	}

	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}
