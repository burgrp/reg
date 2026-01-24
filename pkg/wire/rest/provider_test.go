package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestProviderClient_SetRegisters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("Expected PUT request, got %s", r.Method)
		}

		if r.URL.Path != "/provider" {
			t.Errorf("Expected path /provider, got %s", r.URL.Path)
		}

		var request ProviderPutRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("Failed to decode request: %v", err)
		}

		if len(request.Registers) != 1 {
			t.Errorf("Expected 1 register, got %d", len(request.Registers))
		}

		temp := request.Registers["temp"]
		if temp.Value != 25.5 {
			t.Errorf("Expected value 25.5, got %v", temp.Value)
		}

		if temp.Metadata["unit"] != "celsius" {
			t.Errorf("Expected unit=celsius, got %v", temp.Metadata["unit"])
		}

		// Check TTL is properly formatted as duration string
		if time.Duration(temp.TTL) != 10*time.Second {
			t.Errorf("Expected TTL 10s, got %v", time.Duration(temp.TTL))
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	updates := map[string]RegisterUpdate{
		"temp": {
			Value:    25.5,
			Metadata: map[string]any{"unit": "celsius"},
			TTL:      10 * time.Second,
		},
	}

	err := client.SetRegisters(ctx, updates)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestProviderClient_SetRegister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request ProviderPutRequest
		json.NewDecoder(r.Body).Decode(&request)

		if len(request.Registers) != 1 {
			t.Errorf("Expected 1 register, got %d", len(request.Registers))
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	err := client.SetRegister(ctx, "temp", 25.5, map[string]any{"unit": "C"}, 10*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestProviderClient_GetChangeRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET request, got %s", r.Method)
		}

		if r.URL.Path != "/provider" {
			t.Errorf("Expected path /provider, got %s", r.URL.Path)
		}

		response := ProviderGetResponse{
			Registers: map[string]ProviderGetRegister{
				"temp":     {Value: 30.0},
				"humidity": {Value: 70.0},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	requests, err := client.GetChangeRequests(ctx, nil, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(requests) != 2 {
		t.Errorf("Expected 2 requests, got %d", len(requests))
	}

	if requests["temp"] != 30.0 {
		t.Errorf("Expected temp=30.0, got %v", requests["temp"])
	}

	// JSON numbers unmarshal as float64
	if requests["humidity"] != 70.0 {
		t.Errorf("Expected humidity=70.0, got %v", requests["humidity"])
	}
}

func TestProviderClient_GetChangeRequests_WithNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		names := r.URL.Query()["name"]
		if len(names) != 2 {
			t.Errorf("Expected 2 name parameters, got %d", len(names))
		}

		response := ProviderGetResponse{
			Registers: map[string]ProviderGetRegister{
				"temp":     {Value: 30.0},
				"humidity": {Value: 70.0},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	requests, err := client.GetChangeRequests(ctx, []string{"temp", "humidity"}, 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if len(requests) != 2 {
		t.Errorf("Expected 2 requests, got %d", len(requests))
	}
}

func TestProviderClient_GetChangeRequests_WithWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wait := r.URL.Query().Get("wait")
		if wait != "30s" {
			t.Errorf("Expected wait=30s, got %s", wait)
		}

		response := ProviderGetResponse{
			Registers: map[string]ProviderGetRegister{
				"temp": {Value: 30.0},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	_, err := client.GetChangeRequests(ctx, []string{"temp"}, 30*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestProviderClient_GetChangeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ProviderGetResponse{
			Registers: map[string]ProviderGetRegister{
				"temp": {Value: 30.0},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	value, err := client.GetChangeRequest(ctx, "temp", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if value == nil {
		t.Fatal("Expected value, got nil")
	}

	if value != 30.0 {
		t.Errorf("Expected value 30.0, got %v", value)
	}
}

func TestProviderClient_GetChangeRequest_NotExists(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ProviderGetResponse{
			Registers: map[string]ProviderGetRegister{},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	value, err := client.GetChangeRequest(ctx, "temp", 0)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if value != nil {
		t.Error("Expected nil for non-existent request")
	}
}

func TestProviderClient_ErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	_, err := client.GetChangeRequests(ctx, nil, 0)
	if err == nil {
		t.Error("Expected error for 500 status code")
	}

	err = client.SetRegister(ctx, "temp", 25.5, nil, 10*time.Second)
	if err == nil {
		t.Error("Expected error for 500 status code")
	}
}

func TestProviderClient_BaseURLTrimming(t *testing.T) {
	client := NewProviderClient("http://example.com/")
	if client.baseURL != "http://example.com" {
		t.Errorf("Expected trailing slash to be trimmed, got %s", client.baseURL)
	}
}

func BenchmarkProviderClient_SetRegister(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.SetRegister(ctx, "temp", 25.5, map[string]any{"unit": "C"}, 10*time.Second)
	}
}

func BenchmarkProviderClient_GetChangeRequests(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := ProviderGetResponse{
			Registers: map[string]ProviderGetRegister{
				"temp": {Value: 30.0},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewProviderClient(server.URL)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		client.GetChangeRequests(ctx, []string{"temp"}, 0)
	}
}
