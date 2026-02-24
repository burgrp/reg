package metrics

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/burgrp/reg/internal/registry"
	"golang.org/x/sync/errgroup"
)

// RunServer starts the Prometheus metrics HTTP server on the given address.
// It exposes all numeric registers as gauge metrics at /metrics.
func RunServer(ctx context.Context, address string, reg *registry.Registry, logger *slog.Logger, eg *errgroup.Group) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		handleMetrics(w, reg)
	})

	httpServer := &http.Server{
		Addr:    address,
		Handler: mux,
	}

	eg.Go(func() error {
		logger.Info("metrics server listening", "addr", address)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})

	eg.Go(func() error {
		<-ctx.Done()
		logger.Info("shutting down metrics server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			logger.Error("error during metrics server shutdown", "error", err)
			return err
		}

		logger.Info("metrics server stopped")
		return nil
	})

	return nil
}

func handleMetrics(w http.ResponseWriter, reg *registry.Registry) {
	registers := reg.WaitForChange(nil, 0)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

	for name, r := range registers {
		floatVal, ok := toFloat64(r.Value)
		if !ok {
			continue
		}

		metricName := strings.ReplaceAll(name, ".", ":")
		parts := strings.Split(metricName, ":")

		var labels []string
		for i, part := range parts {
			labels = append(labels, fmt.Sprintf("n%d=%s", i+1, quoteLabelValue(part)))
		}
		for k, v := range r.Metadata {
			labels = append(labels, fmt.Sprintf("%s=%s", sanitizeLabelName(k), quoteLabelValue(fmt.Sprintf("%v", v))))
		}

		labelStr := ""
		if len(labels) > 0 {
			labelStr = "{" + strings.Join(labels, ",") + "}"
		}

		fmt.Fprintf(w, "# TYPE %s gauge\n", metricName)
		fmt.Fprintf(w, "%s%s %g\n", metricName, labelStr, floatVal)
	}
}

// toFloat64 converts numeric register values to float64. Returns false for non-numeric types.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint:
		return float64(n), true
	case uint32:
		return float64(n), true
	case uint64:
		return float64(n), true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	default:
		return 0, false
	}
}

// sanitizeLabelName replaces characters not valid in Prometheus label names with underscores.
// Label names must match [a-zA-Z_][a-zA-Z0-9_]*.
func sanitizeLabelName(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case unicode.IsLetter(r) || r == '_':
			b.WriteRune(r)
		case unicode.IsDigit(r) && i > 0:
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// quoteLabelValue returns a Prometheus-quoted label value string.
// Escapes backslashes, double quotes, and newlines per the text exposition format.
func quoteLabelValue(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
