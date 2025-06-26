# snooper-hook - Testing Tool for RPC Snooper Modules

The `snooper-hook` command is a testing utility that allows you to connect to a running RPC Snooper instance and register various module types for testing purposes. It handles all module request types and logs incoming requests without making any modifications.

## Usage

```bash
./snooper-hook [options]
```

### Options

- `-url string`: WebSocket URL of the snooper control endpoint (default "ws://localhost:8080/control")
- `-type string`: Module type to register: request_snooper, response_snooper, counter, tracer (default "request_snooper")
- `-name string`: Module name (default "test-hook")
- `-config string`: Module configuration as JSON string (default "{}")
- `-verbose`: Enable verbose logging

### Supported Module Types

1. **request_snooper** - Logs all incoming requests (observing only)
2. **response_snooper** - Logs all outgoing responses (observing only)
3. **counter** - Receives and logs counter events 
4. **tracer** - Receives and logs performance tracer events

## Examples

### Basic Request Snooping
```bash
# Connect and log all requests
./snooper-hook -type request_snooper -name "debug-requests"
```

### Counter Testing
```bash
# Test counter events
./snooper-hook -type counter -name "test-counter" -verbose
```

### Custom Configuration
```bash
# Register with filtering configuration
./snooper-hook -type request_snooper -name "filtered-requests" \
  -config '{"content_types": ["application/json"], "methods": ["POST"]}'
```

### Different Snooper Instance
```bash
# Connect to a different snooper instance
./snooper-hook -url "ws://remote-snooper:8080/control" -type counter
```

## Output

The tool provides structured logging showing:

- Module registration success/failure
- Hook events received (request/response data)
- Binary stream data (when applicable)
- Counter events with request counts
- Tracer events with performance metrics

### Example Output

```
INFO[0000] Connecting to snooper control endpoint...    url="ws://localhost:8080/control"
INFO[0000] WebSocket connection established             
INFO[0001] Module registered successfully, listening for hooks... module_id=123 module_name=test-hook module_type=request_snooper
INFO[0005] Hook event received                          content_type="application/json" has_binary=true hook_type=request request_id="456"
INFO[0005] Received binary data                         binary_id=789 preview="{\"method\":\"eth_getBalance\"," size=256
INFO[0006] Hook event received                          content_type="application/json" has_binary=true hook_type=response request_id="456"
INFO[0007] Counter event received                       count=42 request_type="eth_getBalance"
INFO[0008] Tracer event received                        duration_ms=125 request_id="456" status_code=200
```

## Features

- **Pure Observation**: All modules are observing-only, no data modification
- **Binary Stream Support**: Handles binary streaming protocol for large payloads  
- **Observing Module Types**: Supports request_snooper, response_snooper, counter, tracer
- **Graceful Shutdown**: Handles SIGINT for clean shutdown
- **Verbose Logging**: Optional detailed logging for debugging
- **Configurable**: Supports module-specific configuration via JSON

## Use Cases

1. **Development Testing**: Test observing module hooks during development
2. **Protocol Debugging**: Debug WebSocket protocol communication
3. **Performance Testing**: Monitor observation performance and data flow
4. **Integration Testing**: Verify module registration and communication
5. **Binary Stream Testing**: Test large payload streaming functionality
6. **Metrics Testing**: Test counter and tracer event generation

## Building

```bash
go build -o bin/snooper-hook ./cmd/snooper-hook
```

The tool will be built as `bin/snooper-hook` and can be run directly.