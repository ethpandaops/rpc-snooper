package xatu

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// Output type constants.
const (
	OutputTypeStdout = "stdout"
	OutputTypeHTTP   = "http"
	OutputTypeXatu   = "xatu"
	OutputTypeKafka  = "kafka"
)

// Config holds the Xatu integration configuration.
type Config struct {
	// Enabled controls whether Xatu event publishing is active.
	Enabled bool

	// Name identifies this rpc-snooper instance in events.
	Name string

	// NetworkName is the name of the Ethereum network (e.g., "mainnet", "sepolia").
	NetworkName string

	// NetworkID is the network ID of the Ethereum network.
	NetworkID uint64

	// Labels are custom key-value pairs added to event metadata.
	Labels map[string]string

	// Outputs defines where events are published.
	Outputs []OutputConfig

	// TLS enables TLS for xatu:// outputs.
	TLS bool

	// Headers are custom headers for HTTP/Xatu outputs.
	Headers map[string]string

	// MaxQueueSize is the maximum number of events to buffer before dropping.
	MaxQueueSize int

	// MaxExportBatchSize is the maximum number of events per batch export.
	MaxExportBatchSize int

	// Workers is the number of concurrent export workers.
	Workers int

	// BatchTimeout is how long to wait before exporting a partial batch.
	BatchTimeout time.Duration

	// ExportTimeout is the timeout for each export operation.
	ExportTimeout time.Duration

	// KeepAlive configures gRPC keepalive settings.
	KeepAlive KeepAliveConfig
}

// KeepAliveConfig holds gRPC keepalive settings.
type KeepAliveConfig struct {
	// Enabled controls whether keepalive is active.
	Enabled bool

	// Time is the duration after which a keepalive ping is sent.
	Time time.Duration

	// Timeout is the duration to wait for a keepalive response.
	Timeout time.Duration
}

// OutputConfig defines a single output sink configuration.
type OutputConfig struct {
	// Type is the output type: "stdout", "http", "xatu", "kafka".
	Type string

	// Address is the output address (URL, host:port, or brokers/topic).
	Address string
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.Name == "" {
		return errors.New("xatu name is required when enabled")
	}

	if c.NetworkName == "" {
		return errors.New("xatu network name is required when enabled")
	}

	if c.NetworkID == 0 {
		return errors.New("xatu network ID is required when enabled")
	}

	if len(c.Outputs) == 0 {
		return errors.New("at least one xatu output is required when enabled")
	}

	for i, out := range c.Outputs {
		if err := out.Validate(); err != nil {
			return fmt.Errorf("xatu output[%d]: %w", i, err)
		}
	}

	return nil
}

// Validate checks if the output configuration is valid.
func (o *OutputConfig) Validate() error {
	switch o.Type {
	case OutputTypeStdout:
		// stdout doesn't require an address
		return nil
	case OutputTypeHTTP, OutputTypeXatu, OutputTypeKafka:
		if o.Address == "" {
			return fmt.Errorf("address is required for output type %q", o.Type)
		}

		return nil
	default:
		return fmt.Errorf("unknown output type %q (valid: %s, %s, %s, %s)",
			o.Type, OutputTypeStdout, OutputTypeHTTP, OutputTypeXatu, OutputTypeKafka)
	}
}

// ParseOutputFlag parses an output flag value in "type:address" or "type" format.
// Examples:
//   - "stdout" -> {Type: "stdout", Address: ""}
//   - "http:https://example.com" -> {Type: "http", Address: "https://example.com"}
//   - "xatu:xatu.example.com:8080" -> {Type: "xatu", Address: "xatu.example.com:8080"}
//   - "kafka:broker1:9092,broker2:9092/topic" -> {Type: "kafka", Address: "broker1:9092,broker2:9092/topic"}
func ParseOutputFlag(s string) (OutputConfig, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return OutputConfig{}, errors.New("empty output flag")
	}

	// Handle "stdout" without address
	if s == OutputTypeStdout {
		return OutputConfig{Type: OutputTypeStdout}, nil
	}

	// Split on first colon
	idx := strings.Index(s, ":")
	if idx < 0 {
		return OutputConfig{}, fmt.Errorf("invalid output format %q (expected type:address or stdout)", s)
	}

	outputType := s[:idx]
	address := s[idx+1:]

	return OutputConfig{
		Type:    outputType,
		Address: address,
	}, nil
}

// ParseLabelFlag parses a label flag value in "key=value" format.
func ParseLabelFlag(s string) (key, value string, err error) {
	return parseKeyValueFlag(s, "label")
}

// ParseHeaderFlag parses a header flag value in "name=value" format.
func ParseHeaderFlag(s string) (name, value string, err error) {
	return parseKeyValueFlag(s, "header")
}

// parseKeyValueFlag parses a "key=value" formatted string.
func parseKeyValueFlag(s, flagType string) (key, value string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", fmt.Errorf("empty %s flag", flagType)
	}

	key, value, found := strings.Cut(s, "=")
	if !found {
		return "", "", fmt.Errorf("invalid %s format %q (expected key=value)", flagType, s)
	}

	if key == "" {
		return "", "", fmt.Errorf("%s key cannot be empty", flagType)
	}

	return key, value, nil
}
