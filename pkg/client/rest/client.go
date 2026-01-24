package rest

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/burgrp/reg/pkg/client"
	"github.com/burgrp/reg/pkg/wire/rest"
)

// Client implements the client.Client interface using REST protocol
type Client struct {
	consumerClient *rest.ConsumerClient
	providerClient *rest.ProviderClient

	// Consumer batching
	consumerMu       sync.Mutex
	consumerSubs     map[string]*consumerSubscription
	consumerBatchCtx context.Context
	consumerBatchCxl context.CancelFunc

	// Provider batching
	providerMu       sync.Mutex
	providerSubs     map[string]*providerSubscription
	providerBatchCtx context.Context
	providerBatchCxl context.CancelFunc
}

type consumerSubscription struct {
	values   chan client.ValueAndMetadata
	requests chan any
}

type providerSubscription struct {
	name         string
	currentValue any
	metadata     map[string]any
	ttl          time.Duration
	updates      chan any
	changeRequests chan any
}

// NewClient creates a new REST-based registry client with a default HTTP client
func NewClient(baseURL string) *Client {
	httpClient := &http.Client{}
	return NewClientWithHTTPClient(baseURL, httpClient)
}

// NewClientWithHTTPClient creates a new REST-based registry client with a custom HTTP client
func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		consumerClient: rest.NewConsumerClientWithHTTPClient(baseURL, httpClient),
		providerClient: rest.NewProviderClientWithHTTPClient(baseURL, httpClient),
		consumerSubs:   make(map[string]*consumerSubscription),
		providerSubs:   make(map[string]*providerSubscription),
	}
}
