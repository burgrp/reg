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

func TestClient_Consume_WaitsForInitialBeforePolling(t *testing.T) {
	initialStarted := make(chan struct{})
	initialRelease := make(chan struct{})
	pollStarted := make(chan struct{}, 1)
	pollResponse := make(chan float64, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") == "" {
			close(initialStarted)
			select {
			case <-initialRelease:
				json.NewEncoder(w).Encode(rest.ConsumerGetResponse{
					Registers: map[string]rest.ConsumerGetRegister{"temp": {Value: 1.0}},
				})
			case <-r.Context().Done():
			}
			return
		}
		select {
		case pollStarted <- struct{}{}:
		default:
		}
		select {
		case value := <-pollResponse:
			json.NewEncoder(w).Encode(rest.ConsumerGetResponse{
				Registers: map[string]rest.ConsumerGetRegister{"temp": {Value: value}},
			})
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	values, _, err := client.Consume(ctx, "temp")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	<-initialStarted

	select {
	case <-pollStarted:
		t.Fatal("poll started before the initial snapshot completed")
	case <-time.After(30 * time.Millisecond):
	}
	close(initialRelease)
	if value := <-values; value.Value != 1.0 {
		t.Fatalf("Expected initial value 1, got %v", value.Value)
	}
	select {
	case <-pollStarted:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("poll did not start after the initial snapshot")
	}
	pollResponse <- 2.0
	if value := <-values; value.Value != 2.0 {
		t.Fatalf("Expected polled value 2, got %v", value.Value)
	}
}

func TestClient_Consume_RejectsDuplicateName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(rest.ConsumerGetResponse{Registers: map[string]rest.ConsumerGetRegister{}})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, _, err := client.Consume(ctx, "temp"); err != nil {
		t.Fatalf("Unexpected first consumer error: %v", err)
	}
	if _, _, err := client.Consume(ctx, "temp"); err == nil {
		t.Fatal("Expected duplicate consumer to be rejected")
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

	// Should receive multiple values
	receivedValues := make(map[float64]bool)
	timeout := time.After(1 * time.Second)

	for i := 0; i < 2; i++ {
		select {
		case v := <-values:
			if val, ok := v.Value.(float64); ok {
				receivedValues[val] = true
			}
		case <-timeout:
			t.Fatal("Timeout waiting for values")
		}
	}

	// Should have received at least 2 different values
	if len(receivedValues) < 2 {
		t.Errorf("Expected at least 2 different values, got %d", len(receivedValues))
	}
}

func TestClient_Consume_RecreatedRegisterWithSameValue(t *testing.T) {
	pollResponses := make(chan rest.ConsumerGetResponse)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") == "" {
			json.NewEncoder(w).Encode(rest.ConsumerGetResponse{
				Registers: map[string]rest.ConsumerGetRegister{"temp": {Value: 20.0}},
			})
			return
		}
		select {
		case response := <-pollResponses:
			json.NewEncoder(w).Encode(response)
		case <-r.Context().Done():
			return
		}
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	values, _, err := client.Consume(ctx, "temp")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	select {
	case value := <-values:
		if value.Value != 20.0 {
			t.Fatalf("Expected initial value 20, got %v", value.Value)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for initial value")
	}

	pollResponses <- rest.ConsumerGetResponse{Registers: map[string]rest.ConsumerGetRegister{}}
	pollResponses <- rest.ConsumerGetResponse{
		Registers: map[string]rest.ConsumerGetRegister{"temp": {Value: 20.0}},
	}

	select {
	case value := <-values:
		if value.Value != 20.0 {
			t.Fatalf("Expected recreated value 20, got %v", value.Value)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Timeout waiting for recreated value")
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
