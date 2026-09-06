package registry

import (
	"context"
	"log/slog"
	"reflect"
	"slices"
	"sync"
	"time"
)

// Metadata contains static configuration data for a register
type Metadata map[string]any

const defaultTTL = 10 * time.Second

// Register represents an IoT entity with a name, value, and metadata
type Register struct {
	Value    any
	Metadata Metadata
	ttl      time.Time
}

// Registry is the protocol-agnostic registry implementation
type Registry struct {
	registers   map[string]*Register
	registersMu sync.RWMutex

	pendingRequests   map[string]any
	pendingRequestsMu sync.RWMutex

	valueChangeListeners   Listeners[string]
	changeRequestListeners Listeners[string]

	logger *slog.Logger
}

// NewRegistry creates a new registry instance with the given logger
func NewRegistry(logger *slog.Logger) *Registry {
	r := &Registry{
		registers:              make(map[string]*Register),
		pendingRequests:        make(map[string]any),
		valueChangeListeners:   *NewListeners[string](),
		changeRequestListeners: *NewListeners[string](),
		logger:                 logger,
	}
	return r
}

// Start starts background tasks (cleanup goroutine)
func (r *Registry) Start(stopChan <-chan struct{}) {
	go r.cleanupExpiredRegisters(stopChan)
}

// cleanupExpiredRegisters runs a background goroutine that checks for expired registers every second
// and removes them, notifying listeners. It stops when stopChan is closed.
func (r *Registry) cleanupExpiredRegisters(stopChan <-chan struct{}) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stopChan:
			r.logger.Debug("stopping cleanup goroutine")
			return
		case <-ticker.C:
			now := time.Now()
			r.registersMu.Lock()
			for name, reg := range r.registers {
				if !reg.ttl.IsZero() && now.After(reg.ttl) {
					delete(r.registers, name)
					r.valueChangeListeners.Notify(name)
					r.logger.Warn("register expired and removed", "name", name)
				}
			}
			r.registersMu.Unlock()
		}
	}
}

// waitForNotification waits for a notification on the given listeners matching the specified names.
// If names is nil, it waits for any notification. Returns when a notification arrives or duration elapses.
func (r *Registry) waitForNotification(listeners *Listeners[string], names []string, duration time.Duration) {
	r.waitForNotificationWithContext(context.Background(), listeners, names, duration)
}

// waitForNotificationWithContext waits for a matching listener notification, timeout, or context cancellation.
func (r *Registry) waitForNotificationWithContext(ctx context.Context, listeners *Listeners[string], names []string, duration time.Duration) {
	if duration <= 0 {
		return
	}

	if ctx == nil {
		ctx = context.Background()
	}

	changed := make(chan struct{}, 1)
	listenerID := listeners.Add(func(name string) {
		if names == nil || slices.Contains(names, name) {
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	})
	defer listeners.Remove(listenerID)

	timeoutTimer := time.NewTimer(duration)
	defer timeoutTimer.Stop()
	select {
	case <-ctx.Done():
	case <-changed:
	case <-timeoutTimer.C:
	}
}

// WaitForChange waits for changes on the specified registers or until the duration elapses.
// It's ok to call with nil names, which will wait for any change.
func (r *Registry) WaitForChange(names []string, duration time.Duration) map[string]Register {
	return r.WaitForChangeWithContext(context.Background(), names, duration)
}

// WaitForChangeWithContext waits for register changes until a change arrives, timeout elapses, or ctx is canceled.
func (r *Registry) WaitForChangeWithContext(ctx context.Context, names []string, duration time.Duration) map[string]Register {
	r.waitForNotificationWithContext(ctx, &r.valueChangeListeners, names, duration)

	if names == nil {

		// Return all registers
		r.registersMu.RLock()
		defer r.registersMu.RUnlock()

		registers := make(map[string]Register, len(r.registers))
		for name, reg := range r.registers {
			registers[name] = *reg
		}
		return registers

	} else {

		// Return specified registers
		registers := make(map[string]Register, len(names))

		r.registersMu.RLock()
		defer r.registersMu.RUnlock()

		for _, name := range names {
			if reg, exists := r.registers[name]; exists {
				registers[name] = *reg
			}
		}
		return registers
	}
}

// SetRegister sets the value and metadata of a register - called by providers
func (r *Registry) SetRegister(name string, value any, metadata Metadata, ttl time.Duration) {
	r.registersMu.Lock()
	defer r.registersMu.Unlock()

	reg, exists := r.registers[name]
	if !exists {
		reg = &Register{}
		r.registers[name] = reg
	}

	if ttl <= 0 {
		ttl = defaultTTL
	}
	reg.ttl = time.Now().Add(ttl)

	changed := !exists || !reflect.DeepEqual(reg.Value, value) || !reflect.DeepEqual(reg.Metadata, metadata)
	reg.Value = value
	reg.Metadata = metadata
	if changed {
		r.logger.Debug("register changed", "name", name, "value", value, "ttl", ttl)
		r.valueChangeListeners.Notify(name)
	}
}

// RequestChange stores a consumer's request to change a register value
func (r *Registry) RequestChange(name string, value any) {
	r.pendingRequestsMu.Lock()
	defer r.pendingRequestsMu.Unlock()

	r.pendingRequests[name] = value
	r.logger.Debug("register change requested", "name", name, "value", value)
	r.changeRequestListeners.Notify(name)
}

// WaitForChangeRequests waits for change requests on specified registers or until duration elapses
// Returns map of register names to requested values, consuming the requests from the queue
func (r *Registry) WaitForChangeRequests(names []string, duration time.Duration) map[string]any {
	return r.WaitForChangeRequestsWithContext(context.Background(), names, duration)
}

// WaitForChangeRequestsWithContext waits for change requests until notification, timeout, or context cancellation.
func (r *Registry) WaitForChangeRequestsWithContext(ctx context.Context, names []string, duration time.Duration) map[string]any {
	if ctx == nil {
		ctx = context.Background()
	}

	requests := r.takePendingRequestsWithContext(ctx, names)
	if len(requests) > 0 || duration <= 0 {
		return requests
	}

	changed := make(chan struct{}, 1)
	listenerID := r.changeRequestListeners.Add(func(name string) {
		if names == nil || slices.Contains(names, name) {
			select {
			case changed <- struct{}{}:
			default:
			}
		}
	})
	defer r.changeRequestListeners.Remove(listenerID)

	// A request may have arrived between the first queue check and listener registration.
	requests = r.takePendingRequestsWithContext(ctx, names)
	if len(requests) > 0 {
		return requests
	}

	timeoutTimer := time.NewTimer(duration)
	defer timeoutTimer.Stop()
	select {
	case <-ctx.Done():
		return map[string]any{}
	case <-changed:
	case <-timeoutTimer.C:
	}

	return r.takePendingRequestsWithContext(ctx, names)
}

func (r *Registry) takePendingRequests(names []string) map[string]any {
	return r.takePendingRequestsWithContext(context.Background(), names)
}

func (r *Registry) takePendingRequestsWithContext(ctx context.Context, names []string) map[string]any {
	r.pendingRequestsMu.Lock()
	defer r.pendingRequestsMu.Unlock()

	requests := make(map[string]any)
	if ctx.Err() != nil {
		return requests
	}

	if names == nil {
		// Return all pending requests
		for name, value := range r.pendingRequests {
			requests[name] = value
			delete(r.pendingRequests, name)
		}
	} else {
		// Return only specified registers that have pending requests
		for _, name := range names {
			if value, exists := r.pendingRequests[name]; exists {
				requests[name] = value
				delete(r.pendingRequests, name)
			}
		}
	}

	return requests
}
