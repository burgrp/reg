package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/burgrp/reg/pkg/client"
	wirest "github.com/burgrp/reg/pkg/wire/rest"
)

func TestClient_ConsumeAll_InitialValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(wirest.ConsumerGetResponse{
			Registers: map[string]wirest.ConsumerGetRegister{
				"temp":     {Value: 21.5, Metadata: map[string]any{"unit": "C"}},
				"humidity": {Value: 55.0},
			},
		})
	}))
	defer server.Close()

	c := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	updates, _, err := c.ConsumeAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case u := <-updates:
			seen[u.Name] = true
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timeout waiting for initial values")
		}
	}

	if !seen["temp"] || !seen["humidity"] {
		t.Errorf("did not receive updates for all registers: %v", seen)
	}
}

func TestClient_ConsumeAll_RequestChange(t *testing.T) {
	putBodies := make(chan wirest.ConsumerPutRequest, 1)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body wirest.ConsumerPutRequest
			json.NewDecoder(r.Body).Decode(&body)
			putBodies <- body
			w.WriteHeader(http.StatusAccepted)
			return
		}
		// GET — return empty registers so initial fetch completes quickly
		json.NewEncoder(w).Encode(wirest.ConsumerGetResponse{Registers: map[string]wirest.ConsumerGetRegister{}})
	}))
	defer server.Close()

	c := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	_, requests, err := c.ConsumeAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	requests <- client.RegisterChangeRequest{Name: "temp", Value: 30.0}

	select {
	case body := <-putBodies:
		reg, ok := body.Registers["temp"]
		if !ok {
			t.Fatal("expected 'temp' in PUT body")
		}
		if reg.Value != 30.0 {
			t.Errorf("expected value 30.0, got %v", reg.Value)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for PUT request")
	}
}

func TestClient_ConsumeAll_RemovalUpdate(t *testing.T) {
	var getCount int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			count := atomic.AddInt32(&getCount, 1)
			if count == 1 {
				json.NewEncoder(w).Encode(wirest.ConsumerGetResponse{
					Registers: map[string]wirest.ConsumerGetRegister{
						"temp": {Value: 21.5},
					},
				})
				return
			}

			json.NewEncoder(w).Encode(wirest.ConsumerGetResponse{Registers: map[string]wirest.ConsumerGetRegister{}})
			return
		}

		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	c := NewClient(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	updates, _, err := c.ConsumeAll(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Initial register
	select {
	case u := <-updates:
		if u.Name != "temp" || u.Removed {
			t.Fatalf("expected initial temp update, got %+v", u)
		}
	case <-time.After(700 * time.Millisecond):
		t.Fatal("timeout waiting for initial update")
	}

	// Tombstone/removal update
	select {
	case u := <-updates:
		if u.Name != "temp" || !u.Removed {
			t.Fatalf("expected removal update for temp, got %+v", u)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("timeout waiting for removal update")
	}
}
