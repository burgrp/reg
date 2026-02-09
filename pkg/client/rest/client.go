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

	// Polling intervals (configurable for testing)
	ConsumerPollInterval time.Duration // defaults to 5s
	ProviderPollInterval time.Duration // defaults to 30s
}

type consumerSubscription struct {
	ctx          context.Context
	cancel       context.CancelFunc
	values       chan client.ValueAndMetadata
	requests     chan any
	wg           sync.WaitGroup // tracks active senders to channels
	lastValue    any
	lastMetadata map[string]any
}

type providerSubscription struct {
	ctx            context.Context
	cancel         context.CancelFunc
	name           string
	currentValue   any
	metadata       map[string]any
	ttl            time.Duration
	updates        chan any
	changeRequests chan any
	wg             sync.WaitGroup // tracks active senders to channels
}

// NewClient creates a new REST-based registry client with a default HTTP client
func NewClient(baseURL string) *Client {
	httpClient := &http.Client{}
	return NewClientWithHTTPClient(baseURL, httpClient)
}

// NewClientWithHTTPClient creates a new REST-based registry client with a custom HTTP client
func NewClientWithHTTPClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		consumerClient:       rest.NewConsumerClientWithHTTPClient(baseURL, httpClient),
		providerClient:       rest.NewProviderClientWithHTTPClient(baseURL, httpClient),
		consumerSubs:         make(map[string]*consumerSubscription),
		providerSubs:         make(map[string]*providerSubscription),
		ConsumerPollInterval: 5 * time.Second,  // default
		ProviderPollInterval: 30 * time.Second, // default
	}
}
