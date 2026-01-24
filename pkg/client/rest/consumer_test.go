package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestConsumerClient_GetRegisters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		if r.URL.Path != "/consumer" {
			t.Errorf("Expected path /consumer, got %s", r.URL.Path)
		}

		response := ConsumerGetResponse{
			Registers: map[string]ConsumerGetRegister{
				"temp": {
					Value:    25.5,
					Metadata: map[string]any{"unit": "celsius"},
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	registers, err := client.GetRegisters(ctx, nil, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(registers) != 1 {
		t.Errorf("Expected 1 register, got %d", len(registers))
	}

	temp, exists := registers["temp"]
	if !exists {
		t.Fatal("Expected 'temp' register")
	}

	if temp.Value != 25.5 {
		t.Errorf("Expected value 25.5, got %v", temp.Value)
	}

	if temp.Metadata["unit"] != "celsius" {
		t.Errorf("Expected unit=celsius, got %v", temp.Metadata["unit"])
	}
}

func TestConsumerClient_GetRegisters_WithNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		names := r.URL.Query()["name"]
		if len(names) != 2 {
			t.Errorf("Expected 2 name parameters, got %d", len(names))
		}

		response := ConsumerGetResponse{
			Registers: map[string]ConsumerGetRegister{
				"temp":     {Value: 25.5},
				"humidity": {Value: 60.0},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	registers, err := client.GetRegisters(ctx, []string{"temp", "humidity"}, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(registers) != 2 {
		t.Errorf("Expected 2 registers, got %d", len(registers))
	}
}

func TestConsumerClient_GetRegisters_WithWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wait := r.URL.Query().Get("wait")
		if wait != "5s" {
			t.Errorf("Expected wait=5s, got %s", wait)
		}

		response := ConsumerGetResponse{
			Registers: map[string]ConsumerGetRegister{
				"temp": {Value: 25.5},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	_, err := client.GetRegisters(ctx, []string{"temp"}, 5*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestConsumerClient_GetRegister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ConsumerGetResponse{
			Registers: map[string]ConsumerGetRegister{
				"temp": {Value: 25.5},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	reg, err := client.GetRegister(ctx, "temp", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if reg == nil {
		t.Fatal("Expected register, got nil")
	}

	if reg.Value != 25.5 {
		t.Errorf("Expected value 25.5, got %v", reg.Value)
	}
}

func TestConsumerClient_GetRegister_NotExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ConsumerGetResponse{
			Registers: map[string]ConsumerGetRegister{
				"nonexistent": {Value: nil},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	reg, err := client.GetRegister(ctx, "other", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if reg != nil {
		t.Error("Expected nil for non-existent register")
	}
}

func TestConsumerClient_RequestChanges(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("Expected PUT request, got %s", r.Method)
		}

		if r.URL.Path != "/consumer" {
			t.Errorf("Expected path /consumer, got %s", r.URL.Path)
		}

		var request ConsumerPutRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if len(request.Registers) != 2 {
			t.Errorf("Expected 2 registers, got %d", len(request.Registers))
		}

		if request.Registers["temp"].Value != 30.0 {
			t.Errorf("Expected temp=30.0, got %v", request.Registers["temp"].Value)
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	changes := map[string]any{
		"temp":     30.0,
		"humidity": 70.0,
	}

	err := client.RequestChanges(ctx, changes)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestConsumerClient_RequestChange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ConsumerPutRequest
		json.NewDecoder(r.Body).Decode(&request)

		if len(request.Registers) != 1 {
			t.Errorf("Expected 1 register, got %d", len(request.Registers))
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	err := client.RequestChange(ctx, "temp", 30.0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestConsumerClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	_, err := client.GetRegisters(ctx, nil, 0)
	if err == nil {
		t.Error("Expected error for 500 status code")
	}
}

func TestConsumerClient_BaseURLTrimming(t *testing.T) {
	client := NewConsumerClient("http://example.com/")
	if client.baseURL != "http://example.com" {
		t.Errorf("Expected trailing slash to be trimmed, got %s", client.baseURL)
	}
}

func BenchmarkConsumerClient_GetRegisters(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ConsumerGetResponse{
			Registers: map[string]ConsumerGetRegister{
				"temp": {Value: 25.5},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.GetRegisters(ctx, []string{"temp"}, 0)
	}
}

func BenchmarkConsumerClient_RequestChange(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := NewConsumerClient(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.RequestChange(ctx, "temp", 30.0)
	}
}
