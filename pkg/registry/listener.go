package registry

import "sync"

type Listeners[T any] struct {
	listeners map[uint64]func(T)
	mu        sync.RWMutex
	next      uint64
}

func NewListeners[T any]() *Listeners[T] {
	return &Listeners[T]{
		listeners: make(map[uint64]func(T)),
	}
}

func (l *Listeners[T]) Add(listener func(T)) uint64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.next++
	id := l.next
	l.listeners[id] = listener
	return id
}

func (l *Listeners[T]) Remove(id uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.listeners, id)
}

func (l *Listeners[T]) Notify(p T) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, listener := range l.listeners {
		listener(p)
	}
}
