# rpc-snooper

`rpc-snooper` is a lightweight RPC proxy tool designed for debugging and monitoring RPC calls between Ethereum clients. It works by acting as a man-in-the-middle, logging all request details including JSON bodies. `rpc-snooper` is particularly useful for connections between beacon nodes and execution nodes, as well as beacon nodes and validator clients.

## Features

- **Request Forwarding:** Forwards all RPC requests to the specified target while logging the request and response details.
- **Flow Control API:** Start/stop proxy forwarding via REST API endpoints.
- **Internal API:** Exposes an internal API for basic control of the proxy, such as temporarily stopping the forwarding of requests/responses.
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
  -b, --bind-address string   Address to bind to and listen for incoming requests (default "127.0.0.1")
  -h, --help                  Show help information
      --api-bind string       Address to bind for API endpoints (default "0.0.0.0")
      --api-port int          Optional separate port for API endpoints
      --api-auth string       Authentication for API endpoints (format: user:pass,user2:pass2,...)
      --metrics-bind string   Address to bind for metrics endpoint (default "127.0.0.1")
      --metrics-port int      Port for Prometheus metrics endpoint
      --no-api                Disable management REST API
      --no-color              Disable terminal colors in output
  -p, --port int              Port to listen for incoming requests (default 3000)
  -v, --verbose               Enable verbose output
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

### WebSocket Control API

WebSocket connection available at `/_snooper/control` for advanced module management and real-time monitoring.

### Metrics API

When `--metrics-port` is specified, Prometheus metrics are available at `/metrics`:

```bash
# Start with metrics enabled
./snooper --metrics-port 9090 http://localhost:8545

# Access metrics
curl http://localhost:9090/metrics
```

**Available Metrics:**
- Go runtime metrics (garbage collection, memory usage, etc.)
- HTTP request/response metrics (when processing requests)

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
# Start with API authentication on separate port
./snooper --api-auth admin:secret123 --api-port 3001 http://localhost:8545

# Access API with authentication
curl -u admin:secret123 http://localhost:3001/_snooper/status
curl -u admin:secret123 -X POST http://localhost:3001/_snooper/stop
```

### Multiple Services Setup
```bash
# Start with separate API, metrics, and main proxy ports
./snooper -p 3000 --api-port 3001 --metrics-port 9090 http://localhost:8545

# Main proxy: http://localhost:3000/
# API endpoints: http://localhost:3000/_snooper/* AND http://localhost:3001/_snooper/*
# Metrics: http://localhost:9090/metrics
```

### Disable API Completely
```bash
# Start without any management API
./snooper --no-api -p 3000 http://localhost:8545

# Only proxy functionality available, no /_snooper/ endpoints
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

## Xatu Integration

`rpc-snooper` can publish Engine API events to [Xatu](https://github.com/ethpandaops/xatu) for observability and analysis. Currently supported RPC methods:

- `engine_newPayload*` (V1, V2, V3, V4)
- `engine_getBlobs*` (V1)

### CLI Options

```
--xatu-enabled              Enable Xatu event publishing (env: SNOOPER_XATU_ENABLED)
--xatu-name                 Instance name for Xatu events (env: SNOOPER_XATU_NAME)
--xatu-output               Output sink, can be repeated (format: type:address) (env: SNOOPER_XATU_OUTPUTS)
--xatu-label                Custom label, can be repeated (format: key=value) (env: SNOOPER_XATU_LABELS)
--xatu-tls                  Enable TLS for xatu:// outputs (env: SNOOPER_XATU_TLS)
--xatu-header               Custom header, can be repeated (format: name=value) (env: SNOOPER_XATU_HEADERS)
```

Output types: `stdout`, `http`, `xatu` (gRPC), `kafka`

### Examples

**Output to stdout (for debugging):**
```bash
./snooper --xatu-enabled --xatu-name my-snooper --xatu-output stdout http://localhost:8551
```

**Output to Xatu server (gRPC):**
```bash
./snooper --xatu-enabled --xatu-name my-snooper \
  --xatu-output xatu:xatu.example.com:8080 \
  --xatu-tls \
  http://localhost:8551
```

**With custom labels:**
```bash
./snooper --xatu-enabled --xatu-name my-snooper \
  --xatu-output xatu:xatu.example.com:8080 \
  --xatu-label network=mainnet \
  --xatu-label client=geth \
  http://localhost:8551
```

**Using environment variables:**
```bash
export SNOOPER_XATU_ENABLED=true
export SNOOPER_XATU_NAME=my-snooper
export SNOOPER_XATU_OUTPUTS=xatu:xatu.example.com:8080
export SNOOPER_XATU_LABELS=network=mainnet,client=geth
export SNOOPER_XATU_TLS=true

./snooper http://localhost:8551
```

## Contributing

Contributions to `rpc-snooper` are welcome! Here are some ways you can contribute:

- Submitting patches and enhancements
- Reporting bugs
- Adding documentation

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct, and the process for submitting pull requests to us.

## License

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.