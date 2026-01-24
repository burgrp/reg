package registry

import (
	"log/slog"
	"slices"
	"sync"
	"time"
)

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

	changeListeners Listeners[string]

	logger *slog.Logger
}

func NewRegistry(logger *slog.Logger) *Registry {
	r := &Registry{
		registers:       make(map[string]*Register),
		changeListeners: *NewListeners[string](),
		logger:          logger,
	}
	go r.cleanupExpiredRegisters()
	return r
}

func (r *Registry) cleanupExpiredRegisters() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		r.registersMu.Lock()
		for name, reg := range r.registers {
			if !reg.ttl.IsZero() && now.After(reg.ttl) {
				delete(r.registers, name)
				r.changeListeners.Notify(name)
				r.logger.Info("register expired and removed", "name", name)
			}
		}
		r.registersMu.Unlock()
	}
}

// WaitForChange waits for changes on the specified registers or until the duration elapses.
// It's ok to call with nil names, which will wait for any change.
func (r *Registry) WaitForChange(names []string, duration time.Duration) map[string]Register {

	if duration > 0 {
		changed := make(chan struct{}, 1)
		listenerID := r.changeListeners.Add(func(name string) {
			if names == nil || slices.Contains(names, name) {
				select {
				case changed <- struct{}{}:
				default:
				}
			}
		})
		defer r.changeListeners.Remove(listenerID)

		timeoutTimer := time.NewTimer(duration)
		defer timeoutTimer.Stop()
		select {
		case <-changed:
		case <-timeoutTimer.C:
		}
	}

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
			} else {
				registers[name] = Register{
					Metadata: make(Metadata),
				}
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
		reg = &Register{
			Metadata: metadata,
		}
		r.registers[name] = reg
	}

	if ttl <= 0 {
		ttl = defaultTTL
	}
	reg.ttl = time.Now().Add(ttl)

	if reg.Value != value {
		reg.Value = value
		r.logger.Debug("register value updated", "name", name, "ttl", ttl)
		r.changeListeners.Notify(name)
	}
}
