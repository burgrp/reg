package registry

import (
	"sync"
	"testing"
	"time"
)

func TestListeners_Add(t *testing.T) {
	listeners := NewListeners[string]()

	id1 := listeners.Add(func(s string) {})
	id2 := listeners.Add(func(s string) {})

	if id1 == 0 {
		t.Error("Expected non-zero ID for first listener")
	}
	if id2 == 0 {
		t.Error("Expected non-zero ID for second listener")
	}
	if id1 == id2 {
		t.Error("Expected unique IDs for different listeners")
	}
}

func TestListeners_Remove(t *testing.T) {
	listeners := NewListeners[string]()

	called := false
	id := listeners.Add(func(s string) {
		called = true
	})

	listeners.Remove(id)
	listeners.Notify("test")

	if called {
		t.Error("Listener should not be called after removal")
	}
}

func TestListeners_Notify(t *testing.T) {
	listeners := NewListeners[string]()

	var received []string
	var mu sync.Mutex

	listeners.Add(func(s string) {
		mu.Lock()
		received = append(received, s)
		mu.Unlock()
	})

	listeners.Add(func(s string) {
		mu.Lock()
		received = append(received, s+"_2")
		mu.Unlock()
	})

	listeners.Notify("test")

	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Errorf("Expected 2 notifications, got %d", len(received))
	}
}

func TestListeners_Concurrent(t *testing.T) {
	listeners := NewListeners[int]()

	var wg sync.WaitGroup

	// Add listeners concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			listeners.Add(func(v int) {})
		}()
	}

	// Notify concurrently
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(val int) {
			defer wg.Done()
			listeners.Notify(val)
		}(i)
	}

	wg.Wait()
}

func BenchmarkListeners_Add(b *testing.B) {
	listeners := NewListeners[string]()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		listeners.Add(func(s string) {})
	}
}

func BenchmarkListeners_Notify(b *testing.B) {
	listeners := NewListeners[string]()

	for i := 0; i < 100; i++ {
		listeners.Add(func(s string) {})
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		listeners.Notify("test")
	}
}

func BenchmarkListeners_Concurrent(b *testing.B) {
	listeners := NewListeners[int]()

	for i := 0; i < 10; i++ {
		listeners.Add(func(v int) {})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			listeners.Notify(42)
		}
	})
}
