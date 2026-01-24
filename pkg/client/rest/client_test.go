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

func TestClient_Consume_InitialValue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := rest.ConsumerGetResponse{
			Registers: map[string]rest.ConsumerGetRegister{
				"temp": {Value: 25.5, Metadata: map[string]any{"unit": "C"}},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	values, _, err := client.Consume(ctx, "temp")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should receive initial value
	select {
	case v := <-values:
		if v.Value != 25.5 {
			t.Errorf("Expected value 25.5, got %v", v.Value)
		}
		if v.Metadata["unit"] != "C" {
			t.Errorf("Expected unit=C, got %v", v.Metadata["unit"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Timeout waiting for initial value")
	}
}

func TestClient_Consume_LongPolling(t *testing.T) {
	callCount := atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := callCount.Add(1)

		response := rest.ConsumerGetResponse{
			Registers: map[string]rest.ConsumerGetRegister{
				"temp": {Value: float64(20 + count), Metadata: map[string]any{}},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	values, _, err := client.Consume(ctx, "temp")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Should receive initial value
	v1 := <-values
	if v1.Value != 21.0 {
		t.Errorf("Expected first value 21, got %v", v1.Value)
	}

	// Should receive more values from long polling
	v2 := <-values
	if v2.Value != 22.0 {
		t.Errorf("Expected second value 22, got %v", v2.Value)
	}
}

func TestClient_Consume_RequestChange(t *testing.T) {
	receivedRequest := make(chan float64, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var req rest.ConsumerPutRequest
			json.NewDecoder(r.Body).Decode(&req)
			if val, ok := req.Registers["temp"].Value.(float64); ok {
				receivedRequest <- val
			}
			w.WriteHeader(http.StatusAccepted)
			return
		}

		// GET request
		response := rest.ConsumerGetResponse{
			Registers: map[string]rest.ConsumerGetRegister{
				"temp": {Value: 25.5},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, requests, err := client.Consume(ctx, "temp")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Send change request
	requests <- 30.0

	// Verify server received it
	select {
	case val := <-receivedRequest:
		if val != 30.0 {
			t.Errorf("Expected request value 30.0, got %v", val)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Server did not receive change request")
	}
}

func TestClient_Consume_ContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := rest.ConsumerGetResponse{
			Registers: map[string]rest.ConsumerGetRegister{
				"temp": {Value: 25.5},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())

	values, _, err := client.Consume(ctx, "temp")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Cancel context
	cancel()

	// Values channel should close
	select {
	case _, ok := <-values:
		if ok {
			// Drain any pending values
			<-values
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Values channel did not close")
	}

	// For send-only channel, we can't directly check if it's closed,
	// but attempting to send after context cancel should be safe
	// (goroutine will exit, but channel write won't panic)
}

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

func TestClient_Consume_MultipleRegisters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := rest.ConsumerGetResponse{
			Registers: map[string]rest.ConsumerGetRegister{
				"temp":     {Value: 25.5},
				"humidity": {Value: 60.0},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Subscribe to two registers
	values1, _, err1 := client.Consume(ctx, "temp")
	values2, _, err2 := client.Consume(ctx, "humidity")

	if err1 != nil || err2 != nil {
		t.Fatalf("Unexpected errors: %v, %v", err1, err2)
	}

	// Both should receive initial values
	select {
	case v := <-values1:
		if v.Value != 25.5 {
			t.Errorf("Expected temp=25.5, got %v", v.Value)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Timeout waiting for temp value")
	}

	select {
	case v := <-values2:
		if v.Value != 60.0 {
			t.Errorf("Expected humidity=60.0, got %v", v.Value)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Timeout waiting for humidity value")
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
