package client

import (
	"context"
	"time"
)

// Client defines the protocol-agnostic interface for interacting with the registry.
// Implementations handle the underlying protocol details (REST, gRPC, etc.) and
// provide channel-based, reactive access to registers.
type Client interface {
	// Consume subscribes to a register and returns channels for reading values and requesting changes.
	// The values channel immediately receives the current value (with no-wait GET internally),
	// then continuously polls for updates using long-polling.
	// The requests channel allows sending change requests to providers.
	// Both channels are closed when the context is cancelled.
	Consume(ctx context.Context, name string) (<-chan ValueAndMetadata, chan<- any, error)

	// Provide publishes a register with initial value, metadata, and TTL, returning channels
	// for updates and change requests.
	// The updates channel allows sending new values (metadata and TTL remain from initial call).
	// The changeRequests channel receives consumer change requests via long-polling.
	// TTL is automatically refreshed in the background before expiration.
	// Both channels are closed when the context is cancelled.
	Provide(ctx context.Context, name string, value any, metadata map[string]any, ttl time.Duration) (chan<- any, <-chan any, error)
}

// ValueAndMetadata represents a register value with its associated metadata
type ValueAndMetadata struct {
	Value    any
	Metadata map[string]any
}
