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

func TestNewClientWithHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := rest.ConsumerGetResponse{
			Registers: map[string]rest.ConsumerGetRegister{
				"temp": {Value: 25.5},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create custom HTTP client with timeout
	customHTTPClient := &http.Client{
		Timeout: 5 * time.Second,
	}

	client := NewClientWithHTTPClient(server.URL, customHTTPClient)
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
	case <-time.After(500 * time.Millisecond):
		t.Error("Timeout waiting for initial value")
	}
}

func TestSharedHTTPClient(t *testing.T) {
	var consumerRequests atomic.Int32
	var providerRequests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/consumer" {
			consumerRequests.Add(1)
			response := rest.ConsumerGetResponse{
				Registers: map[string]rest.ConsumerGetRegister{
					"temp": {Value: 25.5},
				},
			}
			json.NewEncoder(w).Encode(response)
		} else if r.URL.Path == "/provider" {
			providerRequests.Add(1)
			if r.Method == http.MethodPut {
				w.WriteHeader(http.StatusNoContent)
			} else {
				response := rest.ProviderGetResponse{
					Registers: map[string]rest.ProviderGetRegister{},
				}
				json.NewEncoder(w).Encode(response)
			}
		}
	}))
	defer server.Close()

	// Create client with shared HTTP client
	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// Use both consumer and provider
	_, _, err1 := client.Consume(ctx, "temp")
	_, _, err2 := client.Provide(ctx, "temp", 25.5, nil, 10*time.Second)

	if err1 != nil || err2 != nil {
		t.Fatalf("Unexpected errors: %v, %v", err1, err2)
	}

	// Give time for requests
	time.Sleep(100 * time.Millisecond)

	// Both should have made requests (verifies shared client works)
	if consumerRequests.Load() == 0 {
		t.Error("Expected consumer requests")
	}
	if providerRequests.Load() == 0 {
		t.Error("Expected provider requests")
	}
}
