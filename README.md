# rpc-snooper

`rpc-snooper` is a lightweight RPC proxy tool designed for debugging and monitoring RPC calls between Ethereum clients. It works by acting as a man-in-the-middle, logging all request details including JSON bodies. `rpc-snooper` is particularly useful for connections between beacon nodes and execution nodes, as well as beacon nodes and validator clients.

## Features

- **Request Forwarding:** Forwards all RPC requests to the specified target while logging the request and response details.
- **Flow Control API:** Start/stop proxy forwarding via REST API endpoints.
- **WebSocket Module System:** Real-time module management for advanced monitoring and filtering.
- **Metrics Integration:** Prometheus metrics endpoint for monitoring proxy performance.
- **CLI Support:** Includes several command-line options for customizing the proxy's behavior.

## Installation

### Prerequisites

Ensure you have `git` and `make` installed on your system to build `rpc-snooper`.

### Building from Source

To build `rpc-snooper` from source, follow these steps:

```bash
git clone https://github.com/ethpandaops/rpc-snooper.git
cd rpc-snooper
make
```

### Docker

Docker images for `rpc-snooper` are also available. Use the following command to pull and run the Docker image:

```bash
docker run [docker-run-options-placeholder]
```

## Usage

To start using `rpc-snooper`, run the following command:

```bash
./bin/snooper [options] <target>
```

Where `<target>` is the URL of the underlying RPC host to which requests should be forwarded.

### CLI Options

Here's an overview of the command-line options available:

```
Usage:
./snooper [options] <target>

Options:
  -b, --bind-address string   Address to bind to and listen for incoming requests. (default "127.0.0.1")
  -h, --help                  Run with verbose output
      --no-api                Do not provide management REST api
      --no-color              Do not use terminal colors in output
  -p, --port int              Port to listen for incoming requests. (default 3000)
      --api-port int          Optional separate port for the snooper API endpoints
      --api-auth string       Optional authentication for API endpoints (format: user:pass,user2:pass2,...)
      --metrics-port int      Optional port for Prometheus metrics endpoint
  -v, --verbose               Run with verbose output
  -V, --version               Print version information
```

## API Reference

The snooper exposes several API endpoints under the `/_snooper/` prefix for controlling and monitoring the proxy.

### Flow Control API

Control the proxy forwarding behavior:

#### GET `/_snooper/status`
Get the current flow control status.

**Response:**
```json
{
  "status": "success",
  "enabled": true,
  "message": "Flow is enabled"
}
```

#### POST `/_snooper/start`
Enable proxy forwarding (allows requests to be forwarded to target).

**Response:**
```json
{
  "status": "success", 
  "message": "Flow started",
  "enabled": true
}
```

#### POST `/_snooper/stop`
Disable proxy forwarding (blocks requests with 503 Service Unavailable).

**Response:**
```json
{
  "status": "success",
  "message": "Flow stopped", 
  "enabled": false
}
```

**Example Usage:**
```bash
# Check current status
curl http://localhost:3000/_snooper/status

# Stop forwarding requests
curl -X POST http://localhost:3000/_snooper/stop

# Resume forwarding requests  
curl -X POST http://localhost:3000/_snooper/start
```

### WebSocket Module API

Advanced real-time monitoring via WebSocket connection at `/_snooper/control`.

**Supported Module Types:**
- `request_snooper` - Monitor incoming requests
- `response_snooper` - Monitor outgoing responses  
- `request_counter` - Count and track requests
- `response_tracer` - Trace response patterns

**Module Registration:**
```json
{
  "method": "register_module",
  "data": {
    "type": "request_snooper",
    "name": "my-snooper",
    "config": {
      "request_filter": {
        "methods": ["POST"],
        "content_types": ["application/json"],
        "json_query": "$.method"
      }
    }
  }
}
```

### Metrics API

Prometheus metrics available at `/metrics` when metrics server is enabled:

```bash
# Start with metrics on port 9090
./snooper --metrics-port 9090 http://localhost:8545

# Access metrics
curl http://localhost:9090/metrics
```

## Common Usage Scenarios

### Basic Proxy with Flow Control
```bash
# Start the snooper proxy
./snooper -p 3000 http://localhost:8545

# Your RPC client connects to localhost:3000
# All requests are forwarded to localhost:8545

# Temporarily stop forwarding (useful for maintenance)
curl -X POST http://localhost:3000/_snooper/stop

# Resume forwarding
curl -X POST http://localhost:3000/_snooper/start
```

### Authenticated API Access
```bash
# Start with API authentication
./snooper --api-auth admin:secret123 --api-port 3001 http://localhost:8545

# Access API with authentication
curl -u admin:secret123 http://localhost:3001/_snooper/status
curl -u admin:secret123 -X POST http://localhost:3001/_snooper/stop
```

### Monitoring and Metrics
```bash
# Start with separate API and metrics servers
./snooper -p 3000 --api-port 3001 --metrics-port 9090 http://localhost:8545

# Main proxy: localhost:3000
# API endpoints: localhost:3001/_snooper/*  
# Metrics: localhost:9090/metrics
```

### Error Responses

When flow is disabled, all proxy requests return:
```json
{
  "status": "error",
  "message": "Proxy flow is currently disabled"
}
```
**HTTP Status:** `503 Service Unavailable`

### Authentication Required Response
```json
{
  "status": "error", 
  "message": "Unauthorized"
}
```
**HTTP Status:** `401 Unauthorized`

## Contributing

Contributions to `rpc-snooper` are welcome! Here are some ways you can contribute:

- Submitting patches and enhancements
- Reporting bugs
- Adding documentation

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct, and the process for submitting pull requests to us.

## License

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.