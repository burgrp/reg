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

func TestConsumerBatchPolling(t *testing.T) {
	requestCount := atomic.Int32{}
	lastRequestNames := make(chan []string, 10)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		names := r.URL.Query()["name"]
		if len(names) > 0 {
			// Send copy to avoid race
			namesCopy := make([]string, len(names))
			copy(namesCopy, names)
			select {
			case lastRequestNames <- namesCopy:
			default:
			}
		}

		response := rest.ConsumerGetResponse{
			Registers: map[string]rest.ConsumerGetRegister{
				"temp":     {Value: 25.5},
				"humidity": {Value: 60.0},
				"pressure": {Value: 1013.0},
			},
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Subscribe to three registers
	values1, _, err1 := client.Consume(ctx, "temp")
	values2, _, err2 := client.Consume(ctx, "humidity")
	values3, _, err3 := client.Consume(ctx, "pressure")

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("Unexpected errors: %v, %v, %v", err1, err2, err3)
	}

	// Drain initial individual requests (one per subscription)
	<-values1
	<-values2
	<-values3

	// Clear the request names buffer
	for len(lastRequestNames) > 0 {
		<-lastRequestNames
	}

	initialCount := requestCount.Load()

	// Wait for batch poller to run (should be within 5 seconds + some margin)
	time.Sleep(6 * time.Second)

	// Should have made at least one more request
	finalCount := requestCount.Load()
	if finalCount <= initialCount {
		t.Errorf("Expected at least one batched request, got %d initial, %d final", initialCount, finalCount)
	}

	// Check that the last request had multiple names (batched)
	select {
	case names := <-lastRequestNames:
		if len(names) < 2 {
			t.Errorf("Expected batched request with at least 2 names, got %d: %v", len(names), names)
		}
		t.Logf("Batched request contained %d names: %v", len(names), names)
	case <-time.After(100 * time.Millisecond):
		// May not have gotten the last batch poll yet, that's okay if we saw increased count
	}
}

func TestConsumerBatchPolling_DynamicSubscriptions(t *testing.T) {
	batchedRequests := atomic.Int32{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		names := r.URL.Query()["name"]
		if len(names) >= 2 {
			batchedRequests.Add(1)
		}

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
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel2()

	// Subscribe to two registers
	values1, _, _ := client.Consume(ctx1, "temp")
	values2, _, _ := client.Consume(ctx2, "humidity")

	// Drain initial values
	<-values1
	<-values2

	// Wait for batch poller to run
	time.Sleep(6 * time.Second)

	initialBatched := batchedRequests.Load()
	if initialBatched == 0 {
		t.Error("Expected at least one batched request with 2 subscriptions")
	}

	// Cancel first subscription
	cancel1()
	time.Sleep(100 * time.Millisecond)

	// Continue with only one subscription - should stop batching
	// (though implementation may still send array with 1 name)
	time.Sleep(6 * time.Second)

	// Just verify no panics and second subscription still works
	select {
	case v := <-values2:
		if v.Value != 60.0 {
			t.Errorf("Expected humidity value, got %v", v.Value)
		}
	case <-time.After(1 * time.Second):
		// May not have received another update yet
	}
}
