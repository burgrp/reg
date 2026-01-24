package rest

import (
	"encoding/json"
	"time"
)

// Duration is a custom type that marshals/unmarshals JSON duration strings like "5s", "10m", "1h"
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler to parse Go duration strings
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON implements json.Marshaler to format duration as string
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// ConsumerGetRegister represents a register in consumer GET responses
type ConsumerGetRegister struct {
	Value    any            `json:"value,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ConsumerGetResponse is the response format for consumer GET requests
type ConsumerGetResponse struct {
	Registers map[string]ConsumerGetRegister `json:"registers"`
}

// ConsumerPutRegister represents a register update request from a consumer
type ConsumerPutRegister struct {
	Value any `json:"value,omitempty"`
}

// ConsumerPutRequest is the request format for consumer PUT requests
type ConsumerPutRequest struct {
	Registers map[string]ConsumerPutRegister `json:"registers"`
}

// ProviderPutRegister represents a register update from a provider
type ProviderPutRegister struct {
	Value    any            `json:"value,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
	TTL      Duration       `json:"ttl,omitempty"`
}

// ProviderPutRequest is the request format for provider PUT requests
type ProviderPutRequest struct {
	Registers map[string]ProviderPutRegister `json:"registers"`
}

// ProviderGetRegister represents a change request in provider GET responses
type ProviderGetRegister struct {
	Value any `json:"value,omitempty"`
}

// ProviderGetResponse is the response format for provider GET requests
type ProviderGetResponse struct {
	Registers map[string]ProviderGetRegister `json:"registers"`
}
