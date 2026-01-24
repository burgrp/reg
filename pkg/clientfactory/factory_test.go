package clientfactory

import (
	"os"
	"testing"
)

func TestNewClient_HTTP(t *testing.T) {
	client, err := NewClient("http://localhost:8080")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client, got nil")
	}
}

func TestNewClient_HTTPS(t *testing.T) {
	client, err := NewClient("https://registry.example.com")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client, got nil")
	}
}

func TestNewClient_InvalidURL(t *testing.T) {
	_, err := NewClient("not a valid url ://")
	if err == nil {
		t.Error("Expected error for invalid URL")
	}
}

func TestNewClient_MissingScheme(t *testing.T) {
	_, err := NewClient("localhost:8080")
	if err == nil {
		t.Error("Expected error for missing scheme")
	}
}

func TestNewClient_UnsupportedScheme(t *testing.T) {
	tests := []string{
		"grpc://localhost:8080",
		"ws://localhost:8080",
		"ftp://localhost:8080",
	}

	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			_, err := NewClient(url)
			if err == nil {
				t.Errorf("Expected error for unsupported scheme: %s", url)
			}
		})
	}
}

func TestNewClientFromEnv_Success(t *testing.T) {
	// Set environment variable
	os.Setenv("REGISTRY", "http://localhost:8080")
	defer os.Unsetenv("REGISTRY")

	client, err := NewClientFromEnv()
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client, got nil")
	}
}

func TestNewClientFromEnv_NotSet(t *testing.T) {
	// Ensure REGISTRY is not set
	os.Unsetenv("REGISTRY")

	_, err := NewClientFromEnv()
	if err == nil {
		t.Error("Expected error when REGISTRY not set")
	}
}

func TestNewClientFromEnv_InvalidURL(t *testing.T) {
	// Set invalid URL
	os.Setenv("REGISTRY", "not a valid url")
	defer os.Unsetenv("REGISTRY")

	_, err := NewClientFromEnv()
	if err == nil {
		t.Error("Expected error for invalid URL in REGISTRY")
	}
}

func TestNewClientFromEnv_UnsupportedScheme(t *testing.T) {
	// Set URL with unsupported scheme
	os.Setenv("REGISTRY", "grpc://localhost:8080")
	defer os.Unsetenv("REGISTRY")

	_, err := NewClientFromEnv()
	if err == nil {
		t.Error("Expected error for unsupported scheme in REGISTRY")
	}
}

func TestNewClient_HTTPSWithPath(t *testing.T) {
	client, err := NewClient("https://registry.example.com/api/v1")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client, got nil")
	}
}

func TestNewClient_WithPort(t *testing.T) {
	client, err := NewClient("http://localhost:9090")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("Expected client, got nil")
	}
}
