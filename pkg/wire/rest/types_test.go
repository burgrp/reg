package rest

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDuration_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{
			name:     "seconds",
			input:    `"5s"`,
			expected: 5 * time.Second,
			wantErr:  false,
		},
		{
			name:     "minutes",
			input:    `"10m"`,
			expected: 10 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "hours",
			input:    `"2h"`,
			expected: 2 * time.Hour,
			wantErr:  false,
		},
		{
			name:     "combined",
			input:    `"1h30m"`,
			expected: 90 * time.Minute,
			wantErr:  false,
		},
		{
			name:     "milliseconds",
			input:    `"100ms"`,
			expected: 100 * time.Millisecond,
			wantErr:  false,
		},
		{
			name:    "invalid format",
			input:   `"invalid"`,
			wantErr: true,
		},
		{
			name:    "not a string",
			input:   `123`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d Duration
			err := json.Unmarshal([]byte(tt.input), &d)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if time.Duration(d) != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, time.Duration(d))
			}
		})
	}
}

func TestDuration_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		expected string
	}{
		{
			name:     "seconds",
			duration: 5 * time.Second,
			expected: `"5s"`,
		},
		{
			name:     "minutes",
			duration: 10 * time.Minute,
			expected: `"10m0s"`,
		},
		{
			name:     "hours",
			duration: 2 * time.Hour,
			expected: `"2h0m0s"`,
		},
		{
			name:     "combined",
			duration: 90 * time.Minute,
			expected: `"1h30m0s"`,
		},
		{
			name:     "milliseconds",
			duration: 100 * time.Millisecond,
			expected: `"100ms"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Duration(tt.duration)
			data, err := json.Marshal(d)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if string(data) != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, string(data))
			}
		})
	}
}

func TestDuration_RoundTrip(t *testing.T) {
	original := Duration(5 * time.Second)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded Duration
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded != original {
		t.Errorf("Round trip failed: expected %v, got %v", original, decoded)
	}
}

func TestDuration_InStruct(t *testing.T) {
	type TestStruct struct {
		TTL Duration `json:"ttl"`
	}

	input := `{"ttl":"5s"}`

	var ts TestStruct
	err := json.Unmarshal([]byte(input), &ts)

	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if time.Duration(ts.TTL) != 5*time.Second {
		t.Errorf("Expected 5s, got %v", time.Duration(ts.TTL))
	}

	// Test marshal
	output, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Parse back and verify
	var ts2 TestStruct
	if err := json.Unmarshal(output, &ts2); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if ts2.TTL != ts.TTL {
		t.Errorf("Marshal/Unmarshal mismatch: expected %v, got %v", ts.TTL, ts2.TTL)
	}
}

func TestPutRegisters_MarshalExplicitNullValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{
			name: "consumer",
			value: ConsumerPutRequest{Registers: map[string]ConsumerPutRegister{
				"temp": {Value: nil},
			}},
		},
		{
			name: "provider",
			value: ProviderPutRequest{Registers: map[string]ProviderPutRegister{
				"temp": {Value: nil},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatalf("Failed to marshal request: %v", err)
			}
			if string(body) != `{"registers":{"temp":{"value":null}}}` {
				t.Fatalf("Expected explicit null value, got %s", body)
			}
		})
	}
}

func BenchmarkDuration_UnmarshalJSON(b *testing.B) {
	input := []byte(`"5s"`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var d Duration
		json.Unmarshal(input, &d)
	}
}

func BenchmarkDuration_MarshalJSON(b *testing.B) {
	d := Duration(5 * time.Second)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		json.Marshal(d)
	}
}
