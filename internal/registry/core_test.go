package registry

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func newTestRegistry() *Registry {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelError, // Quiet during tests
	}))
	return NewRegistry(logger)
}

func TestSetRegister(t *testing.T) {
	reg := newTestRegistry()

	metadata := Metadata{"unit": "celsius"}
	reg.SetRegister("temp", 25.5, metadata, 10*time.Second)

	result := reg.WaitForChange([]string{"temp"}, 0)

	if len(result) != 1 {
		t.Fatalf("Expected 1 register, got %d", len(result))
	}

	temp, exists := result["temp"]
	if !exists {
		t.Fatal("Expected 'temp' register to exist")
	}

	if temp.Value != 25.5 {
		t.Errorf("Expected value 25.5, got %v", temp.Value)
	}

	if temp.Metadata["unit"] != "celsius" {
		t.Errorf("Expected metadata unit=celsius, got %v", temp.Metadata["unit"])
	}
}

func TestSetRegister_Update(t *testing.T) {
	reg := newTestRegistry()

	metadata := Metadata{"unit": "celsius"}
	reg.SetRegister("temp", 20.0, metadata, 10*time.Second)
	reg.SetRegister("temp", 25.0, metadata, 10*time.Second)

	result := reg.WaitForChange([]string{"temp"}, 0)

	if result["temp"].Value != 25.0 {
		t.Errorf("Expected updated value 25.0, got %v", result["temp"].Value)
	}
}

func TestSetRegister_MetadataOnlyUpdateNotifies(t *testing.T) {
	reg := newTestRegistry()
	reg.SetRegister("temp", 25.0, Metadata{"unit": "C"}, 10*time.Second)

	done := make(chan map[string]Register, 1)
	go func() {
		done <- reg.WaitForChange([]string{"temp"}, 500*time.Millisecond)
	}()
	time.Sleep(30 * time.Millisecond)
	reg.SetRegister("temp", 25.0, Metadata{"unit": "F"}, 10*time.Second)

	select {
	case registers := <-done:
		if got := registers["temp"].Metadata["unit"]; got != "F" {
			t.Fatalf("Expected updated metadata unit=F, got %v", got)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("metadata-only update did not notify waiting consumers")
	}
}

func TestSetRegister_NewNullValueNotifies(t *testing.T) {
	reg := newTestRegistry()
	done := make(chan map[string]Register, 1)
	go func() {
		done <- reg.WaitForChange([]string{"nullable"}, 500*time.Millisecond)
	}()
	time.Sleep(30 * time.Millisecond)
	reg.SetRegister("nullable", nil, nil, 10*time.Second)

	select {
	case registers := <-done:
		register, exists := registers["nullable"]
		if !exists || register.Value != nil {
			t.Fatalf("Expected newly created null register, got %v", registers)
		}
	case <-time.After(150 * time.Millisecond):
		t.Fatal("new null register did not notify waiting consumers")
	}
}

func TestWaitForChange_AllRegisters(t *testing.T) {
	reg := newTestRegistry()

	reg.SetRegister("temp", 25.5, Metadata{"unit": "C"}, 10*time.Second)
	reg.SetRegister("humidity", 60, Metadata{"unit": "%"}, 10*time.Second)

	result := reg.WaitForChange(nil, 0)

	if len(result) != 2 {
		t.Errorf("Expected 2 registers, got %d", len(result))
	}
}

func TestWaitForChange_SpecificRegisters(t *testing.T) {
	reg := newTestRegistry()

	reg.SetRegister("temp", 25.5, Metadata{}, 10*time.Second)
	reg.SetRegister("humidity", 60, Metadata{}, 10*time.Second)

	result := reg.WaitForChange([]string{"temp"}, 0)

	if len(result) != 1 {
		t.Errorf("Expected 1 register, got %d", len(result))
	}

	if _, exists := result["temp"]; !exists {
		t.Error("Expected 'temp' register to exist")
	}
}

func TestWaitForChange_NonExistent(t *testing.T) {
	reg := newTestRegistry()

	result := reg.WaitForChange([]string{"nonexistent"}, 0)

	if len(result) != 0 {
		t.Errorf("Expected no results, got %d", len(result))
	}
}

func TestWaitForChange_LongPolling(t *testing.T) {
	reg := newTestRegistry()

	done := make(chan bool)

	go func() {
		result := reg.WaitForChange([]string{"temp"}, 200*time.Millisecond)
		if result["temp"].Value == 25.5 {
			done <- true
		} else {
			done <- false
		}
	}()

	time.Sleep(50 * time.Millisecond)
	reg.SetRegister("temp", 25.5, Metadata{}, 10*time.Second)

	select {
	case success := <-done:
		if !success {
			t.Error("Expected to receive updated value")
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("Long polling timed out")
	}
}

func TestWaitForChange_Timeout(t *testing.T) {
	reg := newTestRegistry()

	start := time.Now()
	reg.WaitForChange([]string{"temp"}, 100*time.Millisecond)
	duration := time.Since(start)

	if duration < 100*time.Millisecond {
		t.Errorf("Expected wait of at least 100ms, got %v", duration)
	}

	if duration > 150*time.Millisecond {
		t.Errorf("Expected wait of around 100ms, got %v", duration)
	}
}

func TestWaitForChangeWithContext_Canceled(t *testing.T) {
	reg := newTestRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	reg.WaitForChangeWithContext(ctx, []string{"temp"}, 5*time.Second)
	duration := time.Since(start)

	if duration >= time.Second {
		t.Fatalf("expected cancellation to interrupt wait early, got %v", duration)
	}
}

func TestRequestChange(t *testing.T) {
	reg := newTestRegistry()

	reg.RequestChange("temp", 30.0)

	reg.pendingRequestsMu.RLock()
	value, exists := reg.pendingRequests["temp"]
	reg.pendingRequestsMu.RUnlock()

	if !exists {
		t.Fatal("Expected pending request for 'temp'")
	}

	if value != 30.0 {
		t.Errorf("Expected value 30.0, got %v", value)
	}
}

func TestRequestChange_OverwritesOld(t *testing.T) {
	reg := newTestRegistry()

	reg.RequestChange("temp", 20.0)
	reg.RequestChange("temp", 30.0)

	reg.pendingRequestsMu.RLock()
	value := reg.pendingRequests["temp"]
	reg.pendingRequestsMu.RUnlock()

	if value != 30.0 {
		t.Errorf("Expected latest value 30.0, got %v", value)
	}
}

func TestWaitForChangeRequests(t *testing.T) {
	reg := newTestRegistry()

	reg.RequestChange("temp", 30.0)
	reg.RequestChange("humidity", 70)

	requests := reg.WaitForChangeRequests(nil, 0)

	if len(requests) != 2 {
		t.Errorf("Expected 2 requests, got %d", len(requests))
	}

	if requests["temp"] != 30.0 {
		t.Errorf("Expected temp=30.0, got %v", requests["temp"])
	}
}

func TestWaitForChangeRequests_Consumes(t *testing.T) {
	reg := newTestRegistry()

	reg.RequestChange("temp", 30.0)

	// First call should return the request
	requests1 := reg.WaitForChangeRequests([]string{"temp"}, 0)
	if len(requests1) != 1 {
		t.Errorf("Expected 1 request, got %d", len(requests1))
	}

	// Second call should return empty (consumed)
	requests2 := reg.WaitForChangeRequests([]string{"temp"}, 0)
	if len(requests2) != 0 {
		t.Errorf("Expected 0 requests (consumed), got %d", len(requests2))
	}
}

func TestWaitForChangeRequests_AlreadyPendingReturnsImmediately(t *testing.T) {
	reg := newTestRegistry()
	reg.RequestChange("temp", 30.0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan map[string]any, 1)
	go func() {
		done <- reg.WaitForChangeRequestsWithContext(ctx, []string{"temp"}, time.Hour)
	}()

	select {
	case requests := <-done:
		if requests["temp"] != 30.0 {
			t.Fatalf("expected pending temp=30.0, got %v", requests)
		}
	case <-time.After(100 * time.Millisecond):
		cancel()
		<-done
		t.Fatal("an already pending request should return without long-polling")
	}
}

func TestWaitForChangeRequests_LongPolling(t *testing.T) {
	reg := newTestRegistry()

	done := make(chan bool)

	go func() {
		requests := reg.WaitForChangeRequests([]string{"temp"}, 200*time.Millisecond)
		if requests["temp"] == 30.0 {
			done <- true
		} else {
			done <- false
		}
	}()

	time.Sleep(50 * time.Millisecond)
	reg.RequestChange("temp", 30.0)

	select {
	case success := <-done:
		if !success {
			t.Error("Expected to receive change request")
		}
	case <-time.After(300 * time.Millisecond):
		t.Error("Long polling timed out")
	}
}

func TestWaitForChangeRequestsWithContext_Canceled(t *testing.T) {
	reg := newTestRegistry()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	requests := reg.WaitForChangeRequestsWithContext(ctx, []string{"temp"}, 5*time.Second)
	duration := time.Since(start)

	if len(requests) != 0 {
		t.Fatalf("expected no requests on cancellation, got %d", len(requests))
	}

	if duration >= time.Second {
		t.Fatalf("expected cancellation to interrupt wait early, got %v", duration)
	}
}

func TestWaitForChangeRequestsWithContext_CanceledPreservesPendingRequest(t *testing.T) {
	reg := newTestRegistry()
	reg.RequestChange("temp", 30.0)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	requests := reg.WaitForChangeRequestsWithContext(ctx, []string{"temp"}, time.Second)
	if len(requests) != 0 {
		t.Fatalf("expected canceled poll to return no requests, got %v", requests)
	}

	preserved := reg.WaitForChangeRequests([]string{"temp"}, 0)
	if preserved["temp"] != 30.0 {
		t.Fatalf("expected pending request to remain queued, got %v", preserved)
	}
}

func TestTTLExpiration(t *testing.T) {
	reg := newTestRegistry()

	stopChan := make(chan struct{})
	reg.Start(stopChan)
	defer close(stopChan)

	// Set register with very short TTL
	reg.SetRegister("temp", 25.5, Metadata{}, 100*time.Millisecond)

	// Verify it exists
	result1 := reg.WaitForChange([]string{"temp"}, 0)
	if result1["temp"].Value != 25.5 {
		t.Error("Expected register to exist initially")
	}

	// Wait for TTL to expire
	time.Sleep(1500 * time.Millisecond) // Wait for cleanup cycle

	// Verify it's gone
	result2 := reg.WaitForChange([]string{"temp"}, 0)
	if result2["temp"].Value != nil {
		t.Error("Expected register to be expired")
	}
}

func TestDefaultTTL(t *testing.T) {
	reg := newTestRegistry()

	reg.SetRegister("temp", 25.5, Metadata{}, 0)

	reg.registersMu.RLock()
	ttl := reg.registers["temp"].ttl
	reg.registersMu.RUnlock()

	expectedTTL := time.Now().Add(defaultTTL)

	if ttl.Before(expectedTTL.Add(-1*time.Second)) || ttl.After(expectedTTL.Add(1*time.Second)) {
		t.Errorf("Expected TTL around %v, got %v", expectedTTL, ttl)
	}
}

func TestConcurrentAccess(t *testing.T) {
	reg := newTestRegistry()

	done := make(chan bool)

	// Multiple writers
	for i := 0; i < 10; i++ {
		go func(val int) {
			for j := 0; j < 100; j++ {
				reg.SetRegister("temp", val, Metadata{}, 10*time.Second)
			}
			done <- true
		}(i)
	}

	// Multiple readers
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				reg.WaitForChange([]string{"temp"}, 0)
			}
			done <- true
		}()
	}

	// Multiple change requesters
	for i := 0; i < 10; i++ {
		go func(val int) {
			for j := 0; j < 100; j++ {
				reg.RequestChange("temp", val)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 30; i++ {
		<-done
	}
}

func BenchmarkSetRegister(b *testing.B) {
	reg := newTestRegistry()
	metadata := Metadata{"unit": "celsius"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.SetRegister("temp", 25.5, metadata, 10*time.Second)
	}
}

func BenchmarkWaitForChange_NoWait(b *testing.B) {
	reg := newTestRegistry()
	reg.SetRegister("temp", 25.5, Metadata{}, 10*time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.WaitForChange([]string{"temp"}, 0)
	}
}

func BenchmarkWaitForChange_AllRegisters(b *testing.B) {
	reg := newTestRegistry()

	for i := 0; i < 100; i++ {
		reg.SetRegister(string(rune(i)), i, Metadata{}, 10*time.Second)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.WaitForChange(nil, 0)
	}
}

func BenchmarkRequestChange(b *testing.B) {
	reg := newTestRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.RequestChange("temp", 30.0)
	}
}

func BenchmarkWaitForChangeRequests(b *testing.B) {
	reg := newTestRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reg.RequestChange("temp", 30.0)
		reg.WaitForChangeRequests([]string{"temp"}, 0)
	}
}

func BenchmarkConcurrentSetRegister(b *testing.B) {
	reg := newTestRegistry()
	metadata := Metadata{"unit": "celsius"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			reg.SetRegister("temp", 25.5, metadata, 10*time.Second)
		}
	})
}

func BenchmarkConcurrentWaitForChange(b *testing.B) {
	reg := newTestRegistry()
	reg.SetRegister("temp", 25.5, Metadata{}, 10*time.Second)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			reg.WaitForChange([]string{"temp"}, 0)
		}
	})
}
