# Examples

Practical examples demonstrating a complete smart heating system using the Registry high-level client API.

## System Overview

The examples show three components working together:

1. **DHT Sensor** (Provider) - Publishes temperature and humidity readings
2. **Heating Valve** (Provider) - Controls heating, responds to open/close commands
3. **Thermostat Controller** (Consumer) - Reads sensor data and controls the valve

## DHT Temperature Sensor

A temperature and humidity sensor that publishes readings every 2 seconds.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "math/rand"
    "time"

    "github.com/burgrp/reg/pkg/client/rest"
)

func main() {
    // Create client
    client := rest.NewClient("http://localhost:8080")
    ctx := context.Background()

    // Sensor readings
    temperature := 18.0
    humidity := 45.0

    // Metadata about the sensor
    metadata := map[string]any{
        "location": "living-room",
        "sensor":   "DHT22",
    }

    // Provide temperature register
    tempUpdates, _, err := client.Provide(ctx,
        "living-room-temp", temperature, metadata, 10*time.Second)
    if err != nil {
        log.Fatal(err)
    }

    // Provide humidity register
    humidityUpdates, _, err := client.Provide(ctx,
        "living-room-humidity", humidity, metadata, 10*time.Second)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("DHT22 sensor started")

    // Simulate sensor readings every 2 seconds
    ticker := time.NewTicker(2 * time.Second)
    defer ticker.Stop()

    for range ticker.C {
        // Simulate temperature changes (gradually warming/cooling)
        temperature += (rand.Float64() - 0.5) * 0.3

        // Simulate humidity changes
        humidity += (rand.Float64() - 0.5) * 2.0
        if humidity < 30 {
            humidity = 30
        }
        if humidity > 70 {
            humidity = 70
        }

        // Publish updates
        tempUpdates <- temperature
        humidityUpdates <- humidity

        fmt.Printf("Published - Temp: %.1f°C, Humidity: %.1f%%\n",
            temperature, humidity)
    }
}
```

## Heating Valve

A heating valve that responds to open/close commands from the thermostat.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/burgrp/reg/pkg/client/rest"
)

type ValveState struct {
    Position int    `json:"position"` // 0-100 (percentage open)
    Status   string `json:"status"`   // "open", "closed", "opening", "closing"
}

func main() {
    // Create client
    client := rest.NewClient("http://localhost:8080")
    ctx := context.Background()

    // Initial valve state
    state := ValveState{
        Position: 0,
        Status:   "closed",
    }

    metadata := map[string]any{
        "location": "living-room",
        "type":     "heating-valve",
    }

    // Provide valve state register
    updates, commands, err := client.Provide(ctx,
        "living-room-valve", state, metadata, 10*time.Second)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println("Heating valve ready")

    // Handle commands from thermostat
    for command := range commands {
        if cmdMap, ok := command.(map[string]any); ok {
            targetPosition := int(cmdMap["position"].(float64))

            fmt.Printf("Received command: move to %d%%\n", targetPosition)

            // Simulate gradual valve movement
            if targetPosition > state.Position {
                state.Status = "opening"
                for state.Position < targetPosition {
                    state.Position += 10
                    if state.Position > 100 {
                        state.Position = 100
                    }
                    updates <- state
                    time.Sleep(200 * time.Millisecond)
                }
                state.Status = "open"
            } else if targetPosition < state.Position {
                state.Status = "closing"
                for state.Position > targetPosition {
                    state.Position -= 10
                    if state.Position < 0 {
                        state.Position = 0
                    }
                    updates <- state
                    time.Sleep(200 * time.Millisecond)
                }
                state.Status = "closed"
            }

            // Final update with stable status
            updates <- state
            fmt.Printf("Valve now at %d%% (%s)\n", state.Position, state.Status)
        }
    }
}
```

## Thermostat Controller

The main controller that reads temperature from the DHT sensor and controls the heating valve to maintain target temperature.

```go
package main

import (
    "context"
    "fmt"
    "log"
    "time"

    "github.com/burgrp/reg/pkg/client/rest"
)

func main() {
    // Create client
    client := rest.NewClient("http://localhost:8080")
    ctx := context.Background()

    // Target temperature
    targetTemp := 21.0
    currentTemp := 18.0

    fmt.Printf("Thermostat started - Target: %.1f°C\n", targetTemp)

    // Subscribe to temperature sensor
    tempValues, _, err := client.Consume(ctx, "living-room-temp")
    if err != nil {
        log.Fatal(err)
    }

    // Subscribe to valve state
    valveValues, valveCommands, err := client.Consume(ctx, "living-room-valve")
    if err != nil {
        log.Fatal(err)
    }

    // Track valve state
    valvePosition := 0

    // Control loop ticker
    controlTicker := time.NewTicker(5 * time.Second)
    defer controlTicker.Stop()

    for {
        select {
        case value, ok := <-tempValues:
            if !ok {
                log.Println("Temperature sensor disconnected")
                return
            }

            // Update current temperature
            if temp, ok := value.Value.(float64); ok {
                currentTemp = temp
                fmt.Printf("Temperature: %.1f°C (target: %.1f°C)\n",
                    currentTemp, targetTemp)
            }

        case value, ok := <-valveValues:
            if !ok {
                log.Println("Valve disconnected")
                return
            }

            // Update valve state
            if state, ok := value.Value.(map[string]any); ok {
                valvePosition = int(state["position"].(float64))
                status := state["status"].(string)
                fmt.Printf("Valve: %d%% (%s)\n", valvePosition, status)
            }

        case <-controlTicker.C:
            // Control logic: adjust valve based on temperature error
            tempError := targetTemp - currentTemp
            desiredPosition := 0

            if tempError > 2.0 {
                // Much too cold - open valve fully
                desiredPosition = 100
            } else if tempError > 1.0 {
                // Slightly cold - open valve partially
                desiredPosition = 60
            } else if tempError > 0.5 {
                // Almost at target - minimal heating
                desiredPosition = 30
            } else if tempError < -0.5 {
                // Too warm - close valve
                desiredPosition = 0
            } else {
                // At target - maintain current position
                desiredPosition = valvePosition
            }

            // Send command if position change needed
            if desiredPosition != valvePosition {
                fmt.Printf("Adjusting valve: %d%% -> %d%% (error: %.1f°C)\n",
                    valvePosition, desiredPosition, tempError)

                valveCommands <- map[string]any{
                    "position": desiredPosition,
                }
            }
        }
    }
}
```

## Running the System

### Terminal 1: Start Registry Server

```bash
./reg serve
```

### Terminal 2: Start DHT Sensor

```bash
go run examples/dht-sensor.go
```

Output:
```
DHT22 sensor started
Published - Temp: 18.2°C, Humidity: 45.3%
Published - Temp: 18.4°C, Humidity: 46.1%
...
```

### Terminal 3: Start Heating Valve

```bash
go run examples/heating-valve.go
```

Output:
```
Heating valve ready
Received command: move to 100%
Valve now at 100% (open)
...
```

### Terminal 4: Start Thermostat Controller

```bash
go run examples/thermostat.go
```

Output:
```
Thermostat started - Target: 21.0°C
Temperature: 18.2°C (target: 21.0°C)
Valve: 0% (closed)
Adjusting valve: 0% -> 100% (error: 2.8°C)
Valve: 100% (open)
Temperature: 18.7°C (target: 21.0°C)
Temperature: 19.4°C (target: 21.0°C)
Adjusting valve: 100% -> 60% (error: 1.6°C)
Temperature: 20.1°C (target: 21.0°C)
Adjusting valve: 60% -> 30% (error: 0.9°C)
Temperature: 20.6°C (target: 21.0°C)
Temperature: 21.0°C (target: 21.0°C)
Adjusting valve: 30% -> 0% (error: 0.0°C)
```

## System Behavior

### Heating Cycle

1. DHT sensor publishes temperature (18°C)
2. Thermostat reads temperature, calculates error (3°C below target)
3. Thermostat sends command to open valve 100%
4. Valve gradually opens and reports position
5. Temperature rises as heating activates
6. When temperature approaches target, thermostat reduces valve opening
7. At target temperature, valve closes
8. Cycle repeats to maintain target

### Component Communication

```
DHT Sensor                 Registry                Thermostat
    |                         |                         |
    |--temp:18.2°C----------->|                         |
    |                         |<---read temp------------|
    |                         |---temp:18.2°C---------->|
    |                         |                         |
    |                         |                         | (calculate: need heat)
    |                         |                         |
    |                         |<---valve:open 100%------|
    |                         |                         |
Valve                        |                         |
    |<---open 100%------------|                         |
    |---position:100%-------->|                         |
    |                         |---position:100%-------->|
```

### Advantages of This Architecture

1. **Decoupled Components** - Each component is independent
2. **Hot-swappable** - Components can be restarted without affecting others
3. **Multiple Controllers** - Multiple thermostats can monitor same sensor
4. **Automatic Cleanup** - TTL ensures stale data is removed
5. **Simple Protocol** - Just HTTP/JSON, no complex message brokers

## Monitoring the System

Use the CLI to observe registers:

```bash
# List all active registers
./reg list

# Monitor specific register
watch -n 1 './reg get living-room-temp'

# Interactive browser
./reg browse
```

## Extending the System

### Add Multiple Rooms

Run multiple sensor/valve pairs with different names:

```go
// Bedroom sensor
client.Provide(ctx, "bedroom-temp", temperature, metadata, 10*time.Second)

// Bedroom valve
client.Provide(ctx, "bedroom-valve", state, metadata, 10*time.Second)

// Bedroom thermostat
client.Consume(ctx, "bedroom-temp")
client.Consume(ctx, "bedroom-valve")
```

### Add Remote Control

Create a simple web interface or mobile app that:

```go
// Read all room temperatures
tempValues, _, _ := client.Consume(ctx, "living-room-temp")
tempValues2, _, _ := client.Consume(ctx, "bedroom-temp")

// Change target temperature
thermostatReqs <- map[string]any{
    "target": 22.0,
}
```

### Add Scheduling

Extend thermostat with time-based target temperature:

```go
// Get current hour
hour := time.Now().Hour()

// Set target based on schedule
if hour >= 6 && hour < 8 {
    targetTemp = 22.0 // Morning warmth
} else if hour >= 22 || hour < 6 {
    targetTemp = 18.0 // Night economy
} else {
    targetTemp = 20.0 // Day comfort
}
```

These examples demonstrate a complete, working smart heating system using the Registry high-level API.
