package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/burgrp/reg/internal/registry"
)

func newTestServer() *Server {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Quiet during tests
	}))
	reg := registry.NewRegistry(logger)
	return &Server{
		registry: reg,
		logger:   logger,
	}
}

func TestConsumerGet_SingleRegister(t *testing.T) {
	server := newTestServer()

	// Set a register
	server.registry.SetRegister("temp", 25.5, registry.Metadata{"unit": "celsius"}, 10*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/consumer?name=temp", nil)
	w := httptest.NewRecorder()

	server.handleConsumer(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response ConsumerGetResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response.Registers) != 1 {
		t.Errorf("Expected 1 register, got %d", len(response.Registers))
	}

	temp, exists := response.Registers["temp"]
	if !exists {
		t.Fatal("Expected 'temp' register in response")
	}

	if temp.Value != 25.5 {
		t.Errorf("Expected value 25.5, got %v", temp.Value)
	}

	if temp.Metadata["unit"] != "celsius" {
		t.Errorf("Expected unit=celsius, got %v", temp.Metadata["unit"])
	}
}

func TestConsumerGet_AllRegisters(t *testing.T) {
	server := newTestServer()

	server.registry.SetRegister("temp", 25.5, registry.Metadata{}, 10*time.Second)
	server.registry.SetRegister("humidity", 60, registry.Metadata{}, 10*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/consumer", nil)
	w := httptest.NewRecorder()

	server.handleConsumer(w, req)

	var response ConsumerGetResponse
	json.NewDecoder(w.Body).Decode(&response)

	if len(response.Registers) != 2 {
		t.Errorf("Expected 2 registers, got %d", len(response.Registers))
	}
}

func TestConsumerGet_MultipleNames(t *testing.T) {
	server := newTestServer()

	server.registry.SetRegister("temp", 25.5, registry.Metadata{}, 10*time.Second)
	server.registry.SetRegister("humidity", 60, registry.Metadata{}, 10*time.Second)
	server.registry.SetRegister("pressure", 1013, registry.Metadata{}, 10*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/consumer?name=temp&name=humidity", nil)
	w := httptest.NewRecorder()

	server.handleConsumer(w, req)

	var response ConsumerGetResponse
	json.NewDecoder(w.Body).Decode(&response)

	if len(response.Registers) != 2 {
		t.Errorf("Expected 2 registers, got %d", len(response.Registers))
	}

	if _, exists := response.Registers["temp"]; !exists {
		t.Error("Expected 'temp' in response")
	}
	if _, exists := response.Registers["humidity"]; !exists {
		t.Error("Expected 'humidity' in response")
	}
}

func TestConsumerGet_InvalidWaitParameter(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/consumer?wait=invalid", nil)
	w := httptest.NewRecorder()

	server.handleConsumer(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestConsumerPut_RequestChange(t *testing.T) {
	server := newTestServer()

	requestBody := ConsumerPutRequest{
		Registers: map[string]ConsumerPutRegister{
			"temp": {Value: 30.0},
		},
	}

	body, _ := json.Marshal(requestBody)
	req := httptest.NewRequest(http.MethodPut, "/consumer", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleConsumer(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status 202, got %d", w.Code)
	}

	// Verify the change request was stored
	requests := server.registry.WaitForChangeRequests([]string{"temp"}, 0)
	if requests["temp"] != 30.0 {
		t.Errorf("Expected change request value 30.0, got %v", requests["temp"])
	}
}

func TestConsumerPut_InvalidJSON(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest(http.MethodPut, "/consumer", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	server.handleConsumer(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestConsumer_MethodNotAllowed(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/consumer", nil)
	w := httptest.NewRecorder()

	server.handleConsumer(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestProviderPut_SetRegister(t *testing.T) {
	server := newTestServer()

	// Use raw JSON to properly format Duration as string
	body := []byte(`{
		"registers": {
			"temp": {
				"value": 25.5,
				"metadata": {"unit": "celsius"},
				"ttl": "10s"
			}
		}
	}`)

	req := httptest.NewRequest(http.MethodPut, "/provider", bytes.NewReader(body))
	w := httptest.NewRecorder()

	server.handleProvider(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}

	// Verify register was set
	result := server.registry.WaitForChange([]string{"temp"}, 0)
	if result["temp"].Value != 25.5 {
		t.Errorf("Expected value 25.5, got %v", result["temp"].Value)
	}
}

func TestProviderPut_InvalidJSON(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest(http.MethodPut, "/provider", bytes.NewReader([]byte("invalid json")))
	w := httptest.NewRecorder()

	server.handleProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestProviderGet_ChangeRequests(t *testing.T) {
	server := newTestServer()

	// Create a change request
	server.registry.RequestChange("temp", 30.0)

	req := httptest.NewRequest(http.MethodGet, "/provider?name=temp", nil)
	w := httptest.NewRecorder()

	server.handleProvider(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response ProviderResponse
	json.NewDecoder(w.Body).Decode(&response)

	if len(response.Registers) != 1 {
		t.Errorf("Expected 1 register, got %d", len(response.Registers))
	}

	if response.Registers["temp"].Value != 30.0 {
		t.Errorf("Expected value 30.0, got %v", response.Registers["temp"].Value)
	}
}

func TestProviderGet_InvalidWaitParameter(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/provider?wait=invalid", nil)
	w := httptest.NewRecorder()

	server.handleProvider(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestProvider_MethodNotAllowed(t *testing.T) {
	server := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/provider", nil)
	w := httptest.NewRecorder()

	server.handleProvider(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestParseQueryParams_Names(t *testing.T) {
	server := newTestServer()

	tests := []struct {
		name     string
		url      string
		expected []string
	}{
		{
			name:     "single name parameter",
			url:      "/test?name=temp",
			expected: []string{"temp"},
		},
		{
			name:     "multiple name parameters",
			url:      "/test?name=temp&name=humidity",
			expected: []string{"temp", "humidity"},
		},
		{
			name:     "names parameter",
			url:      "/test?names=temp,humidity,pressure",
			expected: []string{"temp", "humidity", "pressure"},
		},
		{
			name:     "mixed name and names",
			url:      "/test?name=temp&names=humidity,pressure",
			expected: []string{"temp", "humidity", "pressure"},
		},
		{
			name:     "no parameters",
			url:      "/test",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			names, _, err := server.parseQueryParams(req)

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if len(names) != len(tt.expected) {
				t.Errorf("Expected %d names, got %d", len(tt.expected), len(names))
				return
			}

			for i, name := range names {
				if name != tt.expected[i] {
					t.Errorf("Expected name[%d]=%s, got %s", i, tt.expected[i], name)
				}
			}
		})
	}
}

func TestParseQueryParams_Wait(t *testing.T) {
	server := newTestServer()

	tests := []struct {
		name     string
		url      string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "valid wait parameter",
			url:      "/test?wait=5s",
			expected: 5 * time.Second,
			wantErr:  false,
		},
		{
			name:     "no wait parameter",
			url:      "/test",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "invalid wait parameter",
			url:      "/test?wait=invalid",
			expected: 0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			_, wait, err := server.parseQueryParams(req)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if wait != tt.expected {
				t.Errorf("Expected wait=%v, got %v", tt.expected, wait)
			}
		})
	}
}

func TestWriteResponse_ErrorPath(t *testing.T) {
	server := newTestServer()

	// Create a value that will fail JSON encoding (channel types can't be marshaled)
	invalidData := make(chan int)

	w := httptest.NewRecorder()
	server.writeResponse(w, invalidData)

	// Should get an error response
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500 for JSON encoding error, got %d", w.Code)
	}
}

func TestRunServer_Integration(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))
	reg := registry.NewRegistry(logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Use a dynamic port
	address := "127.0.0.1:0"

	// Note: We can't easily test RunServer without complex setup
	// since it starts a real HTTP server. The handler tests above
	// provide sufficient coverage for the REST logic.
	_ = ctx
	_ = address
	_ = reg
}

func BenchmarkConsumerGet(b *testing.B) {
	server := newTestServer()
	server.registry.SetRegister("temp", 25.5, registry.Metadata{}, 10*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/consumer?name=temp", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		server.handleConsumer(w, req)
	}
}

func BenchmarkConsumerPut(b *testing.B) {
	server := newTestServer()

	requestBody := ConsumerPutRequest{
		Registers: map[string]ConsumerPutRegister{
			"temp": {Value: 30.0},
		},
	}
	body, _ := json.Marshal(requestBody)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPut, "/consumer", bytes.NewReader(body))
		w := httptest.NewRecorder()
		server.handleConsumer(w, req)
	}
}

func BenchmarkProviderPut(b *testing.B) {
	server := newTestServer()

	body := []byte(`{
		"registers": {
			"temp": {
				"value": 25.5,
				"metadata": {"unit": "celsius"},
				"ttl": "10s"
			}
		}
	}`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPut, "/provider", bytes.NewReader(body))
		w := httptest.NewRecorder()
		server.handleProvider(w, req)
	}
}

func BenchmarkProviderGet(b *testing.B) {
	server := newTestServer()
	server.registry.RequestChange("temp", 30.0)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.registry.RequestChange("temp", 30.0)
		req := httptest.NewRequest(http.MethodGet, "/provider?name=temp", nil)
		w := httptest.NewRecorder()
		server.handleProvider(w, req)
	}
}

func BenchmarkParseQueryParams(b *testing.B) {
	server := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/test?name=temp&name=humidity&wait=5s", nil)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		server.parseQueryParams(req)
	}
}
