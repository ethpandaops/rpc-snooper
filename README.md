# rpc-snooper

`rpc-snooper` is a lightweight RPC proxy tool designed for debugging and monitoring RPC calls between Ethereum clients. It works by acting as a man-in-the-middle, logging all request details including JSON bodies. `rpc-snooper` is particularly useful for connections between beacon nodes and execution nodes, as well as beacon nodes and validator clients.

## Features

- **Request Forwarding:** Forwards all RPC requests to the specified target while logging the request and response details.
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
  -b, --bind-address string   Address to bind to and listen for incoming requests. (default "127.0.0.1")
  -h, --help                  Run with verbose output
      --no-api                Do not provide management REST api
      --no-color              Do not use terminal colors in output
  -p, --port int              Port to listen for incoming requests. (default 3000)
  -v, --verbose               Run with verbose output
  -V, --version               Print version information
```

## Contributing

Contributions to `rpc-snooper` are welcome! Here are some ways you can contribute:

- Submitting patches and enhancements
- Reporting bugs
- Adding documentation

Please read [CONTRIBUTING.md](CONTRIBUTING.md) for details on our code of conduct, and the process for submitting pull requests to us.

## License

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.