package cmd

import (
	"bytes"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/burgrp/reg/internal/registry"
	"github.com/spf13/cobra"
)

func preserveLogging(t *testing.T) {
	t.Helper()
	logger := slog.Default()
	writer := log.Writer()
	flags := log.Flags()
	t.Cleanup(func() {
		slog.SetDefault(logger)
		log.SetOutput(writer)
		log.SetFlags(flags)
	})
}

func TestRootCommandLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		value string
		unset bool
		level slog.Level
	}{
		{name: "unset", unset: true, level: slog.LevelInfo},
		{name: "empty", level: slog.LevelInfo},
		{name: "debug", value: "debug", level: slog.LevelDebug},
		{name: "info", value: "info", level: slog.LevelInfo},
		{name: "warn", value: "warn", level: slog.LevelWarn},
		{name: "error", value: "error", level: slog.LevelError},
		{name: "uppercase", value: "DEBUG", level: slog.LevelDebug},
		{name: "mixed case", value: "WaRn", level: slog.LevelWarn},
		{name: "positive offset", value: "INFO+2", level: slog.LevelInfo + 2},
		{name: "negative offset", value: "ERROR-8", level: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preserveLogging(t)
			t.Setenv("LOG_LEVEL", tt.value)
			if tt.unset {
				if err := os.Unsetenv("LOG_LEVEL"); err != nil {
					t.Fatal(err)
				}
			}

			var stdout, stderr bytes.Buffer
			root := newRootCmd()
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.AddCommand(&cobra.Command{
				Use: "logging-test",
				Run: func(cmd *cobra.Command, args []string) {
					logger := slog.Default()
					ctx := cmd.Context()
					if !logger.Enabled(ctx, tt.level) || logger.Enabled(ctx, tt.level-1) {
						t.Errorf("logger threshold does not match %v", tt.level)
					}
					for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
						slog.Log(ctx, level, "diagnostic-"+level.String())
					}
					reg := registry.NewRegistry(logger)
					reg.SetRegister("test.temperature", 21.5, nil, time.Minute)
					log.Print("standard-log-diagnostic")
					fmt.Fprintln(cmd.OutOrStdout(), "command result")
				},
			})
			root.SetArgs([]string{"logging-test"})
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}

			if got := stdout.String(); got != "command result\n" {
				t.Errorf("stdout = %q, want command result only", got)
			}
			for _, level := range []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError} {
				got := strings.Contains(stderr.String(), "diagnostic-"+level.String())
				if want := level >= tt.level; got != want {
					t.Errorf("log at %v present = %v, want %v", level, got, want)
				}
			}
			if got, want := strings.Contains(stderr.String(), "register changed"), slog.LevelDebug >= tt.level; got != want {
				t.Errorf("register change log present = %v, want %v", got, want)
			}
			if got, want := strings.Contains(stderr.String(), "standard-log-diagnostic"), slog.LevelInfo >= tt.level; got != want {
				t.Errorf("standard log present = %v, want %v", got, want)
			}
		})
	}
}

func TestRootCommandInvalidLogLevel(t *testing.T) {
	tests := []struct {
		name  string
		value string
		args  []string
	}{
		{name: "version", value: "verbose", args: []string{"version"}},
		{name: "serve", value: "trace", args: []string{"serve", "--rest", "invalid"}},
		{name: "mcp", value: "INFO+bad", args: []string{"mcp"}},
		{name: "whitespace", value: " ", args: []string{"version"}},
		{name: "overflow", value: "INFO+999999999999999999999999", args: []string{"version"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			preserveLogging(t)
			t.Setenv("LOG_LEVEL", tt.value)
			var stdout, stderr bytes.Buffer
			root := newRootCmd()
			root.SetOut(&stdout)
			root.SetErr(&stderr)
			root.SetArgs(tt.args)
			root.SilenceUsage = true
			err := root.Execute()
			if err == nil {
				t.Fatal("expected invalid LOG_LEVEL to prevent command execution")
			}
			want := fmt.Sprintf("invalid LOG_LEVEL %q", tt.value)
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error = %q, want %q", err, want)
			}
			if !strings.Contains(stderr.String(), "Error: "+want) {
				t.Errorf("stderr = %q, want explicit LOG_LEVEL error", stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("unexpected stdout: %q", stdout.String())
			}
		})
	}
}

func TestCommandHelpDescribesLogLevel(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "bare command", args: []string{}},
		{name: "root help", args: []string{"--help"}},
		{name: "serve help", args: []string{"serve", "--help"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", "invalid")
			var output bytes.Buffer
			root := newRootCmd()
			root.SetOut(&output)
			root.SetErr(&output)
			root.SetArgs(tt.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"LOG_LEVEL", "default: info", "debug", "info", "warn", "error"} {
				if !strings.Contains(output.String(), want) {
					t.Errorf("help does not contain %q", want)
				}
			}
			if strings.Contains(output.String(), "A CLI application built with Cobra") {
				t.Error("help still contains the generic Cobra description")
			}
		})
	}
}
