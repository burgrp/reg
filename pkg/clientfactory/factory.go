package clientfactory

import (
	"fmt"
	"net/url"
	"os"

	"github.com/burgrp/reg/pkg/client"
	"github.com/burgrp/reg/pkg/client/rest"
)

// NewClient creates a client.Client based on the URL scheme.
// Currently supports:
// - http:// and https:// - Creates REST client
// Returns error for unsupported schemes.
func NewClient(registryURL string) (client.Client, error) {
	u, err := url.Parse(registryURL)
	if err != nil {
		return nil, fmt.Errorf("invalid registry URL: %w", err)
	}

	switch u.Scheme {
	case "http", "https":
		return rest.NewClient(registryURL), nil
	case "":
		return nil, fmt.Errorf("missing URL scheme (expected http:// or https://)")
	default:
		return nil, fmt.Errorf("unsupported URL scheme: %s (supported: http, https)", u.Scheme)
	}
}

// NewClientFromEnv creates a client.Client using the REGISTRY environment variable.
// Returns error if REGISTRY is not set or contains invalid URL.
func NewClientFromEnv() (client.Client, error) {
	registryURL := os.Getenv("REGISTRY")
	if registryURL == "" {
		return nil, fmt.Errorf("REGISTRY environment variable not set")
	}

	return NewClient(registryURL)
}
