# Registry (reg)

A lightweight, IoT-focused service for managing named registers with dynamic values and automatic lifecycle management.

## Overview

A **register** is a named container that holds a current value, optional metadata, and a time-to-live (TTL). Think of it like a variable in memory that automatically expires if not refreshed. Each register has:
- **Name** - Unique identifier (e.g., "living-room-temperature")
- **Value** - Current data (any JSON type: number, string, object, array)
- **Metadata** - Optional key-value pairs with additional information
- **TTL** - How long the register stays valid without refresh

Registry provides a simplified Provider/Consumer model where:
- **Providers** publish registers and keep them alive by refreshing before TTL expires
- **Consumers** read register values and can request changes
- **Automatic cleanup** removes expired registers when TTL elapses

## Architecture

```
┌─────────────┐                    ┌─────────────┐
│  Provider   │                    │  Consumer   │
│  (Sensor)   │                    │ (Controller)│
└──────┬──────┘                    └──────┬──────┘
       │                                  │
       │ PUT /provider                    │ GET /consumer
       │ (set value)                      │ (read value)
       │                                  │
       └──────────┐          ┌────────────┘
                  │          │
                  v          v
            ┌─────────────────────┐
            │   Registry Server   │
            │                     │
            │  - In-memory store  │
            │  - TTL management   │
            │  - Change requests  │
            └─────────────────────┘
                  ^          │
       ┌──────────┘          └────────────┐
       │                                  │
       │ GET /provider                    │ PUT /consumer
       │ (poll for changes)               │ (request change)
       │                                  │
┌──────┴──────┐                    ┌──────┴──────┐
│  Provider   │                    │  Consumer   │
└─────────────┘                    └─────────────┘
```

## Quick Start

### Installation

```bash
go install github.com/burgrp/reg@latest
```

Or build from source:

```bash
git clone https://github.com/burgrp/reg.git
cd reg
go build -o reg
```

### Running the Server

```bash
# Start server on default port (8080)
./reg serve

# Start server on custom port
./reg serve --rest :9000
```

### Using the CLI

#### Provide a Register

```bash
# Set environment variable
export REGISTRY=http://localhost:8080

# Provide a temperature register with initial value
./reg provide temperature 22.5

# Provide with metadata
./reg provide temperature 22.5 '{"unit":"celsius","location":"room1"}'

# Provide with custom TTL (default is 5 seconds)
./reg provide temperature 22.5 '{"unit":"celsius"}' --ttl 10s
```

#### Consume a Register

```bash
# Read current value
./reg get temperature

# List all registers
./reg list

# Interactive browsing (TUI)
./reg browse
```

#### MCP Server (Model Context Protocol)

```bash
# Start MCP stdio server for AI assistant integration
export REGISTRY=http://localhost:8080
./reg mcp

# The MCP server exposes tools for AI assistants:
# - get_register: Get register value and metadata
# - set_register: Set register value (provider operation)
# - list_registers: List all registers
# - request_change: Request value change (consumer operation)
```

Use with MCP clients like Claude Desktop, Cline, or other MCP-compatible tools.

### Using the API

#### Provide a Register (Provider)

```bash
curl -X PUT http://localhost:8080/provider \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "temperature": {
        "value": 22.5,
        "metadata": {"unit": "celsius"},
        "ttl": "10s"
      }
    }
  }'
```

#### Read a Register (Consumer)

```bash
curl http://localhost:8080/consumer?name=temperature
```

#### Request a Change (Consumer)

```bash
curl -X PUT http://localhost:8080/consumer \
  -H "Content-Type: application/json" \
  -d '{
    "registers": {
      "temperature": {
        "value": 25.0
      }
    }
  }'
```

## Documentation

### Core Concepts
- [Architecture Overview](docs/architecture.md) - System design and components
- [Lifecycle Management](docs/lifecycle.md) - How registers are created, updated, and expired

### API Reference
- [REST API](docs/rest-api.md) - Complete HTTP API reference
- [Long Polling](docs/long-polling.md) - Real-time updates with long polling

### Client Libraries
- [Go Client Library](docs/go-client.md) - Using the Go client library
- [Client Examples](docs/examples.md) - Code examples for common use cases

### Development
- [Building and Testing](docs/development.md) - Development guide
- [Contributing](docs/contributing.md) - How to contribute

## Features

- **Protocol-agnostic core** - Clean separation between business logic and transport
- **REST API** - Simple HTTP interface with JSON
- **MCP Server** - Model Context Protocol stdio server for AI assistant integration
- **Long polling** - Efficient real-time updates without WebSockets
- **Automatic TTL management** - Registers expire automatically if not refreshed
- **Change requests** - Consumers can request value changes from providers
- **Batched operations** - Client library batches multiple subscriptions efficiently
- **Graceful shutdown** - Clean shutdown with signal handling
- **No dependencies** - Minimal external dependencies, easy to deploy

## Use Cases

- **IoT device management** - Sensors publish values, controllers consume and request changes
- **Configuration management** - Dynamic configuration with TTL-based expiration
- **Distributed state** - Lightweight state sharing between services
- **Command and control** - Send commands to devices via change requests

## License

MIT License - see LICENSE file for details

## Version

Run `./reg version` to see the current version.
