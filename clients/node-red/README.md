# @burgrp/node-red-contrib-reg

Node-RED node library for the reg IoT registry.

## Included Nodes

- reg-config: shared registry URL and polling settings
- reg-consume: subscribe to one register and request changes for it
- reg-consume-all: subscribe to all registers and request changes by topic
- reg-provide: provide a register value and receive change requests
- reg-request: send a consumer change request

## Install

```bash
cd clients/node-red
npm install
```

For local development in a Node-RED user directory:

```bash
npm link
cd ~/.node-red
npm link @burgrp/node-red-contrib-reg
```

## Message Contracts

### reg-consume output

- msg.topic: register name
- msg.payload: register value
- msg.metadata: metadata object

### reg-consume input

- msg.payload: requested value for the configured register

### reg-consume-all output

- msg.topic: register name
- msg.payload: register value
- msg.metadata: metadata object

### reg-consume-all input

- msg.topic: register name to request
- msg.payload: requested value

### reg-provide output

- msg.topic: register name
- msg.payload: consumer-requested value

### reg-provide input

- msg.payload: new provided value

### reg-request input/output

- Input register name: configured `name` or msg.topic
- Input requested value: msg.payload
- Output includes msg.requested=true on successful request

## Node Settings

reg-config supports:

- registryUrl (default: http://localhost:8080)
- consumerPollInterval in milliseconds (default: 5000)
- providerPollInterval in milliseconds (default: 30000)

If registryUrl is empty, REGISTRY environment variable is used.

## Quick Import Flow

Import the JSON below in Node-RED (Menu -> Import -> Clipboard). It creates:

- one reg-config node for http://localhost:8080
- one reg-provide node for register temperature
- one reg-consume node for the same register
- one reg-request node to request value changes
- inject and debug nodes to drive and observe the flow

```json
[
	{
		"id": "f1a2b3c4d5e6f001",
		"type": "tab",
		"label": "reg e2e demo",
		"disabled": false,
		"info": ""
	},
	{
		"id": "a1b2c3d4e5f60111",
		"type": "reg-config",
		"name": "local-reg",
		"registryUrl": "http://localhost:8080",
		"consumerPollInterval": "5000",
		"providerPollInterval": "30000"
	},
	{
		"id": "b1c2d3e4f5a60122",
		"type": "inject",
		"z": "f1a2b3c4d5e6f001",
		"name": "set temperature 21.5",
		"props": [
			{
				"p": "payload"
			}
		],
		"repeat": "",
		"crontab": "",
		"once": true,
		"onceDelay": "0.5",
		"topic": "",
		"payload": "21.5",
		"payloadType": "num",
		"x": 190,
		"y": 120,
		"wires": [
			[
				"c1d2e3f4a5b60133"
			]
		]
	},
	{
		"id": "c1d2e3f4a5b60133",
		"type": "reg-provide",
		"z": "f1a2b3c4d5e6f001",
		"name": "temperature",
		"server": "a1b2c3d4e5f60111",
		"initialValue": "21.5",
		"metadata": "{\"unit\":\"celsius\"}",
		"ttl": "5s",
		"x": 440,
		"y": 120,
		"wires": [
			[
				"d1e2f3a4b5c60144",
				"e1f2a3b4c5d60155",
				"c1d2e3f4a5b60133"
			]
		]
	},
	{
		"id": "d1e2f3a4b5c60144",
		"type": "debug",
		"z": "f1a2b3c4d5e6f001",
		"name": "provider change request",
		"active": true,
		"tosidebar": true,
		"console": false,
		"tostatus": false,
		"complete": "true",
		"targetType": "full",
		"x": 770,
		"y": 80,
		"wires": []
	},
	{
		"id": "e1f2a3b4c5d60155",
		"type": "debug",
		"z": "f1a2b3c4d5e6f001",
		"name": "consumer update",
		"active": true,
		"tosidebar": true,
		"console": false,
		"tostatus": false,
		"complete": "true",
		"targetType": "full",
		"x": 760,
		"y": 200,
		"wires": []
	},
	{
		"id": "f2a3b4c5d6e60166",
		"type": "reg-consume",
		"z": "f1a2b3c4d5e6f001",
		"name": "temperature",
		"server": "a1b2c3d4e5f60111",
		"x": 440,
		"y": 200,
		"wires": [
			[
				"e1f2a3b4c5d60155"
			]
		]
	},
	{
		"id": "a2b3c4d5e6f70177",
		"type": "inject",
		"z": "f1a2b3c4d5e6f001",
		"name": "request temperature 24",
		"props": [
			{
				"p": "payload"
			}
		],
		"repeat": "",
		"crontab": "",
		"once": false,
		"onceDelay": "0.1",
		"topic": "",
		"payload": "24",
		"payloadType": "num",
		"x": 190,
		"y": 280,
		"wires": [
			[
				"b2c3d4e5f6a70188"
			]
		]
	},
	{
		"id": "b2c3d4e5f6a70188",
		"type": "reg-request",
		"z": "f1a2b3c4d5e6f001",
		"name": "temperature",
		"server": "a1b2c3d4e5f60111",
		"x": 440,
		"y": 280,
		"wires": [
			[
				"c2d3e4f5a6b70199"
			]
		]
	},
	{
		"id": "c2d3e4f5a6b70199",
		"type": "debug",
		"z": "f1a2b3c4d5e6f001",
		"name": "request ack",
		"active": true,
		"tosidebar": true,
		"console": false,
		"tostatus": false,
		"complete": "true",
		"targetType": "full",
		"x": 760,
		"y": 280,
		"wires": []
	}
]
```

How to use it:

1. Start the registry server (`./reg serve`).
2. Import and deploy the flow.
3. Watch `consumer update` for initial and changed values.
4. Click `request temperature 24` to send a consumer change request and observe `provider change request` plus follow-up consumer updates.

## Test

```bash
cd clients/node-red
npm test
```
