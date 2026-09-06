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

func TestProviderBatchPolling(t *testing.T) {
	requestCount := atomic.Int32{}
	lastRequestNames := make(chan []string, 10)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// GET request for change requests
		requestCount.Add(1)
		names := r.URL.Query()["name"]
		if len(names) > 0 {
			namesCopy := make([]string, len(names))
			copy(namesCopy, names)
			select {
			case lastRequestNames <- namesCopy:
			default:
			}
		}

		response := rest.ProviderGetResponse{
			Registers: map[string]rest.ProviderGetRegister{
				"temp":     {Value: 30.0},
				"humidity": {Value: 70.0},
				"pressure": {Value: 1020.0},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.ProviderPollInterval = 500 * time.Millisecond // Fast polling for tests
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Provide three registers
	_, changeReqs1, err1 := client.Provide(ctx, "temp", 25.5, nil, 60*time.Second)
	_, changeReqs2, err2 := client.Provide(ctx, "humidity", 60.0, nil, 60*time.Second)
	_, _, err3 := client.Provide(ctx, "pressure", 1013.0, nil, 60*time.Second)

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("Unexpected errors: %v, %v, %v", err1, err2, err3)
	}

	// Clear any initial requests
	time.Sleep(500 * time.Millisecond)
	for len(lastRequestNames) > 0 {
		<-lastRequestNames
	}

	initialCount := requestCount.Load()

	// Wait for batch poller to run
	time.Sleep(1 * time.Second)

	// Should have made at least one more request
	finalCount := requestCount.Load()
	if finalCount <= initialCount {
		t.Errorf("Expected at least one batched request, got %d initial, %d final", initialCount, finalCount)
	}

	// Check that we got batched requests with multiple names
	select {
	case names := <-lastRequestNames:
		if len(names) < 2 {
			t.Errorf("Expected batched request with at least 2 names, got %d: %v", len(names), names)
		}
		t.Logf("Batched provider request contained %d names: %v", len(names), names)
	case <-time.After(100 * time.Millisecond):
		// May not have caught the exact batch poll
	}

	// Verify change requests were distributed
	select {
	case val := <-changeReqs1:
		if val != 30.0 {
			t.Errorf("Expected temp change request 30.0, got %v", val)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for temp change request")
	}

	select {
	case val := <-changeReqs2:
		if val != 70.0 {
			t.Errorf("Expected humidity change request 70.0, got %v", val)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for humidity change request")
	}
}

func TestProviderBatchPolling_DynamicProviders(t *testing.T) {
	batchedRequests := atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		names := r.URL.Query()["name"]
		if len(names) >= 2 {
			batchedRequests.Add(1)
		}

		response := rest.ProviderGetResponse{
			Registers: map[string]rest.ProviderGetRegister{
				"temp":     {Value: 30.0},
				"humidity": {Value: 70.0},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.ProviderPollInterval = 500 * time.Millisecond // Fast polling for tests
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	// Provide two registers
	_, _, _ = client.Provide(ctx1, "temp", 25.5, nil, 60*time.Second)
	_, changeReqs2, _ := client.Provide(ctx2, "humidity", 60.0, nil, 60*time.Second)

	// Wait for batch poller to run
	time.Sleep(1 * time.Second)

	initialBatched := batchedRequests.Load()
	if initialBatched == 0 {
		t.Error("Expected at least one batched request with 2 providers")
	}

	// Cancel first provider
	cancel1()
	time.Sleep(100 * time.Millisecond)

	// Continue with only one provider
	time.Sleep(1 * time.Second)

	// Verify second provider still receives change requests
	select {
	case val := <-changeReqs2:
		if val != 70.0 {
			t.Errorf("Expected humidity change request, got %v", val)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for remaining provider change request")
	}
}
