package xatu

import (
	"errors"
	"fmt"
	"strings"
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

	// Labels are custom key-value pairs added to event metadata.
	Labels map[string]string

	// Outputs defines where events are published.
	Outputs []OutputConfig

	// TLS enables TLS for xatu:// outputs.
	TLS bool

	// Headers are custom headers for HTTP/Xatu outputs.
	Headers map[string]string
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
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("empty label flag")
	}

	var found bool

	key, value, found = strings.Cut(s, "=")
	if !found {
		return "", "", fmt.Errorf("invalid label format %q (expected key=value)", s)
	}

	if key == "" {
		return "", "", errors.New("label key cannot be empty")
	}

	return key, value, nil
}

// ParseHeaderFlag parses a header flag value in "name=value" format.
func ParseHeaderFlag(s string) (name, value string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", errors.New("empty header flag")
	}

	var found bool

	name, value, found = strings.Cut(s, "=")
	if !found {
		return "", "", fmt.Errorf("invalid header format %q (expected name=value)", s)
	}

	if name == "" {
		return "", "", errors.New("header name cannot be empty")
	}

	return name, value, nil
}
