package rest

import (
	"encoding/json"
	"net/http"
)

// ConsumerGetRegister represents a register in consumer GET responses
type ConsumerGetRegister struct {
	Value    any            `json:"value,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ConsumerGetResponse is the response format for consumer GET requests
type ConsumerGetResponse struct {
	Registers map[string]ConsumerGetRegister `json:"registers"`
}

// ConsumerPutRegister represents a register update request from a consumer
type ConsumerPutRegister struct {
	Value any `json:"value,omitempty"`
}

// ConsumerPutRequest is the request format for consumer PUT requests
type ConsumerPutRequest struct {
	Registers map[string]ConsumerPutRegister `json:"registers"`
}

// handleConsumer handles consumer endpoints: GET for reading registers with long polling,
// PUT for requesting value changes (returns 202 Accepted)
func (s *Server) handleConsumer(w http.ResponseWriter, r *http.Request) {

	if r.Method == http.MethodGet {
		names, wait, err := s.parseQueryParams(r)
		if err != nil {
			s.logger.Error("invalid wait parameter", "error", err)
			http.Error(w, "invalid wait parameter", http.StatusBadRequest)
			return
		}

		registers := s.registry.WaitForChangeWithContext(r.Context(), names, wait)

		response := ConsumerGetResponse{
			Registers: make(map[string]ConsumerGetRegister, len(registers)),
		}

		for name, reg := range registers {
			response.Registers[name] = ConsumerGetRegister{
				Value:    reg.Value,
				Metadata: reg.Metadata,
			}
		}

		s.writeResponse(w, response)

		return
	}

	if r.Method == http.MethodPut {
		var req ConsumerPutRequest
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
