package rest

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type ConsumerGetRegister struct {
	Value    any            `json:"value,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

type ConsumerGetResponse struct {
	Registers map[string]ConsumerGetRegister `json:"registers"`
}

type ConsumerPutRegister struct {
	Value any `json:"value,omitempty"`
}

type ConsumerPutRequest struct {
	Registers map[string]ConsumerPutRegister `json:"registers"`
}

func (s *Server) handleConsumer(w http.ResponseWriter, r *http.Request) {

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
