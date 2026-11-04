package metrics

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/burgrp/reg/internal/registry"
)

func TestSanitizeMetricName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"accu:tank:temp:ref:hi", "accu:tank:temp:ref:hi"},
		{"_sensor:temperature2", "_sensor:temperature2"},
		{"0x00158d000483b274:temperature", "_0x00158d000483b274:temperature"},
		{"123", "_123"},
		{"living-room:temperature", "living_room:temperature"},
		{"room/one:temperature", "room_one:temperature"},
		{"room one:temperature", "room_one:temperature"},
		{"room\none:temperature", "room_one:temperature"},
		{"caf\u00e9:temperature", "caf_:temperature"},
		{"\u6e29\u5ea6", "__"},
		{"", "_"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeMetricName(tt.name); got != tt.want {
				t.Errorf("sanitizeMetricName(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestHandleMetricsNames(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		labels string
	}{
		{
			"accu.tank.temp.ref.hi",
			"accu:tank:temp:ref:hi",
			`n1="accu",n2="tank",n3="temp",n4="ref",n5="hi"`,
		},
		{
			"0x00158d000483b274.temperature",
			"_0x00158d000483b274:temperature",
			`n1="0x00158d000483b274",n2="temperature"`,
		},
		{
			"living-room.temperature",
			"living_room:temperature",
			`n1="living-room",n2="temperature"`,
		},
		{
			"room\"\\\n.temperature",
			"room___:temperature",
			`n1="room\"\\\n",n2="temperature"`,
		},
		{
			"",
			"_",
			`n1=""`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := registry.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
			reg.SetRegister(tt.name, 13.55, registry.Metadata{"device": "zigbee"}, time.Hour)
			response := httptest.NewRecorder()

			handleMetrics(response, reg)

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
				t.Errorf("Content-Type = %q", got)
			}
			want := fmt.Sprintf("# TYPE %s gauge\n%s{%s,device=\"zigbee\"} 13.55\n", tt.metric, tt.metric, tt.labels)
			if got := response.Body.String(); got != want {
				t.Errorf("metrics = %q, want %q", got, want)
			}
		})
	}
}

func TestHandleMetricsNormalizationCollisions(t *testing.T) {
	reg := registry.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
	registers := []struct {
		name   string
		value  float64
		sample string
	}{
		{"0x123.temperature", 1, `_0x123:temperature{n1="0x123",n2="temperature"} 1`},
		{"_0x123.temperature", 2, `_0x123:temperature{n1="_0x123",n2="temperature"} 2`},
		{"room-1.temperature", 3, `room_1:temperature{n1="room-1",n2="temperature"} 3`},
		{"room_1.temperature", 4, `room_1:temperature{n1="room_1",n2="temperature"} 4`},
	}
	for _, r := range registers {
		reg.SetRegister(r.name, r.value, nil, time.Hour)
	}
	response := httptest.NewRecorder()

	handleMetrics(response, reg)

	body := response.Body.String()
	for _, metric := range []string{"_0x123:temperature", "room_1:temperature"} {
		if got := strings.Count(body, "# TYPE "+metric+" gauge\n"); got != 1 {
			t.Errorf("TYPE declarations for %q = %d, want 1", metric, got)
		}
	}
	for _, r := range registers {
		if !strings.Contains(body, r.sample+"\n") {
			t.Errorf("missing sample %q in %q", r.sample, body)
		}
	}
}

func TestHandleMetricsValues(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{float64(13.55), "13.55"},
		{float32(1.5), "1.5"},
		{int(1), "1"},
		{int32(2), "2"},
		{int64(3), "3"},
		{uint(4), "4"},
		{uint32(5), "5"},
		{uint64(6), "6"},
		{true, "1"},
		{false, "0"},
		{"not numeric", ""},
		{nil, ""},
		{map[string]any{"value": 1}, ""},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%T/%v", tt.value, tt.value), func(t *testing.T) {
			reg := registry.NewRegistry(slog.New(slog.NewTextHandler(io.Discard, nil)))
			reg.SetRegister("sensor.value", tt.value, nil, time.Hour)
			response := httptest.NewRecorder()

			handleMetrics(response, reg)

			var want string
			if tt.want != "" {
				want = "# TYPE sensor:value gauge\nsensor:value{n1=\"sensor\",n2=\"value\"} " + tt.want + "\n"
			}
			if got := response.Body.String(); got != want {
				t.Errorf("metrics = %q, want %q", got, want)
			}
		})
	}
}
