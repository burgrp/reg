package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/burgrp/reg/pkg/wire/rest"
)

func TestClient_Provide_InitialValue(t *testing.T) {
	receivedInitial := make(chan bool, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/provider" {
			var req rest.ProviderPutRequest
			json.NewDecoder(r.Body).Decode(&req)
			if req.Registers["temp"].Value == 25.5 {
				receivedInitial <- true
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// GET request
		response := rest.ProviderGetResponse{
			Registers: map[string]rest.ProviderGetRegister{},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	metadata := map[string]any{"unit": "C"}
	_, _, err := client.Provide(ctx, "temp", 25.5, metadata, 10*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Verify server received initial value
	select {
	case <-receivedInitial:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Error("Server did not receive initial value")
	}
}

func TestClient_Provide_RejectsDuplicateNameAndInvalidTTL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		json.NewEncoder(w).Encode(rest.ProviderGetResponse{Registers: map[string]rest.ProviderGetRegister{}})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, _, err := client.Provide(ctx, "temp", 1, nil, time.Second); err != nil {
		t.Fatalf("Unexpected first provider error: %v", err)
	}
	if _, _, err := client.Provide(ctx, "temp", 2, nil, time.Second); err == nil {
		t.Fatal("Expected duplicate provider to be rejected")
	}
	if _, _, err := client.Provide(ctx, "other", 1, nil, 0); err == nil {
		t.Fatal("Expected non-positive TTL to be rejected")
	}
	if _, _, err := client.Provide(ctx, "tiny", 1, nil, time.Nanosecond); err == nil {
		t.Fatal("Expected TTL with zero refresh interval to be rejected")
	}
}

func TestClient_Provide_SerializesUpdatesAndRefreshes(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	var putCount atomic.Int32
	blocked := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			json.NewEncoder(w).Encode(rest.ProviderGetResponse{Registers: map[string]rest.ProviderGetRegister{}})
			return
		}
		count := putCount.Add(1)
		current := active.Add(1)
		for {
			previous := maximum.Load()
			if current <= previous || maximum.CompareAndSwap(previous, current) {
				break
			}
		}
		defer active.Add(-1)
		if count > 1 {
			select {
			case <-blocked:
			case <-r.Context().Done():
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	updates, _, err := client.Provide(ctx, "temp", 1, nil, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("Unexpected provider error: %v", err)
	}
	updates <- 2
	deadline := time.After(500 * time.Millisecond)
	for putCount.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("Timed out waiting for update PUT")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(40 * time.Millisecond)
	if got := maximum.Load(); got != 1 {
		t.Fatalf("Expected one active provider write, got %d", got)
	}
	cancel()
	close(blocked)
}

func TestClient_Provide_Updates(t *testing.T) {
	receivedUpdate := make(chan float64, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var req rest.ProviderPutRequest
			json.NewDecoder(r.Body).Decode(&req)
			if val, ok := req.Registers["temp"].Value.(float64); ok && val == 26.0 {
				receivedUpdate <- val
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// GET request
		response := rest.ProviderGetResponse{
			Registers: map[string]rest.ProviderGetRegister{},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	updates, _, err := client.Provide(ctx, "temp", 25.5, nil, 10*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Send update
	updates <- 26.0

	// Verify server received it
	select {
	case val := <-receivedUpdate:
		if val != 26.0 {
			t.Errorf("Expected update value 26.0, got %v", val)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Server did not receive update")
	}
}

func TestClient_Provide_ChangeRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// GET request - return change request
		response := rest.ProviderGetResponse{
			Registers: map[string]rest.ProviderGetRegister{
				"temp": {Value: 30.0},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, changeRequests, err := client.Provide(ctx, "temp", 25.5, nil, 10*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should receive change request
	select {
	case val := <-changeRequests:
		if val != 30.0 {
			t.Errorf("Expected change request 30.0, got %v", val)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for change request")
	}
}

func TestClient_Provide_TTLRefreshUsesCurrentValue(t *testing.T) {
	type putRequest struct {
		value any
		time  time.Time
	}
	putRequests := make(chan putRequest, 10)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var req rest.ProviderPutRequest
			json.NewDecoder(r.Body).Decode(&req)
			if reg, exists := req.Registers["temp"]; exists {
				putRequests <- putRequest{value: reg.Value, time: time.Now()}
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// GET request
		response := rest.ProviderGetResponse{
			Registers: map[string]rest.ProviderGetRegister{},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Provide with 2 second TTL (refresh at 1 second)
	updates, _, err := client.Provide(ctx, "temp", 25.5, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Get initial PUT
	initialReq := <-putRequests
	if initialReq.value != 25.5 {
		t.Errorf("Expected initial value 25.5, got %v", initialReq.value)
	}

	// Send update to 30.0
	updates <- 30.0

	// Get update PUT
	updateReq := <-putRequests
	if updateReq.value != 30.0 {
		t.Errorf("Expected update value 30.0, got %v", updateReq.value)
	}

	// Wait for TTL refresh (should happen at ~1 second)
	// The refresh should use the current value (30.0), not initial value (25.5)
	select {
	case refreshReq := <-putRequests:
		if refreshReq.value != 30.0 {
			t.Errorf("TTL refresh should use current value 30.0, but got %v (initial value was 25.5)", refreshReq.value)
		}
		// Verify this happened around the refresh interval
		timeSinceUpdate := refreshReq.time.Sub(updateReq.time)
		if timeSinceUpdate < 900*time.Millisecond || timeSinceUpdate > 1200*time.Millisecond {
			t.Logf("Warning: TTL refresh timing was %v, expected ~1s", timeSinceUpdate)
		}
	case <-time.After(2 * time.Second):
		t.Error("Timeout waiting for TTL refresh")
	}
}

func TestClient_Provide_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		response := rest.ProviderGetResponse{
			Registers: map[string]rest.ProviderGetRegister{},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	_, changeRequests, err := client.Provide(ctx, "temp", 25.5, nil, 10*time.Second)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Cancel context
	cancel()

	// ChangeRequests channel should close
	select {
	case _, ok := <-changeRequests:
		if ok {
			t.Error("ChangeRequests channel should be closed")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("ChangeRequests channel did not close")
	}

	// For send-only updates channel, we can't directly check if it's closed,
	// but the goroutine should exit cleanly on context cancel
}
