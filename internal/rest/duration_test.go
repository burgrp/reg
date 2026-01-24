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
}

func BenchmarkDuration_UnmarshalJSON(b *testing.B) {
	input := []byte(`"5s"`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var d Duration
		json.Unmarshal(input, &d)
	}
}

func BenchmarkDuration_UnmarshalJSON_Complex(b *testing.B) {
	input := []byte(`"1h30m45s"`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var d Duration
		json.Unmarshal(input, &d)
	}
}
