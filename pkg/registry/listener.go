package registry

import "sync"

// Listeners manages a thread-safe collection of callback functions for notifications
type Listeners[T any] struct {
	listeners map[uint64]func(T)
	mu        sync.RWMutex
	next      uint64
}

// NewListeners creates a new Listeners instance
func NewListeners[T any]() *Listeners[T] {
	return &Listeners[T]{
		listeners: make(map[uint64]func(T)),
	}
}

// Add registers a new listener callback and returns a unique ID for later removal
func (l *Listeners[T]) Add(listener func(T)) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	id := l.next
	l.listeners[id] = listener
	return id
}

// Remove unregisters a listener by its ID
func (l *Listeners[T]) Remove(id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.listeners, id)
}

// Notify calls all registered listener callbacks with the given parameter
func (l *Listeners[T]) Notify(p T) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, listener := range l.listeners {
		listener(p)
	}
}
