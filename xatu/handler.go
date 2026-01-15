package xatu

import (
	"time"
)

// Handler constants.
const (
	// DefaultPublishTimeout is the context timeout for publishing events.
	DefaultPublishTimeout = 5 * time.Second

	// DefaultPendingCapacity is the initial capacity for pending call maps.
	DefaultPendingCapacity = 100
)

// EventHandler defines the interface for handling specific JSON-RPC method events.
// Each handler is responsible for processing requests and responses for a specific
// set of methods and publishing corresponding Xatu events.
type EventHandler interface {
	// Name returns the handler name for logging and metrics.
	Name() string

	// MethodMatcher returns a function that checks if a JSON-RPC method matches this handler.
	MethodMatcher() func(method string) bool

	// HandleRequest processes the request and stores pending data.
	// Returns true if this handler should also process the corresponding response.
	HandleRequest(ctx *RequestEvent) bool

	// HandleResponse processes the response, correlates with the request data, and publishes the event.
	HandleResponse(ctx *ResponseEvent)
}

// RequestEvent contains data from an intercepted JSON-RPC request.
type RequestEvent struct {
	// CallID is the unique identifier for this request/response pair.
	CallID uint64

	// Timestamp is when the request was received.
	Timestamp time.Time

	// Method is the JSON-RPC method name (e.g., "engine_getBlobsV1").
	Method string

	// Params are the JSON-RPC parameters.
	Params []any

	// BodyBytes contains the raw request body bytes (useful for SSZ-encoded data).
	BodyBytes []byte
}

// ResponseEvent contains data from an intercepted JSON-RPC response.
type ResponseEvent struct {
	// CallID is the unique identifier for this request/response pair.
	CallID uint64

	// Timestamp is when the response was received.
	Timestamp time.Time

	// Duration is the time taken for the request to complete.
	Duration time.Duration

	// Result is the JSON-RPC result field (nil if there was an error).
	Result any

	// Error contains the JSON-RPC error if present.
	Error *RPCError

	// BodyBytes contains the raw response body bytes.
	BodyBytes []byte
}

// RPCError represents a JSON-RPC error response.
type RPCError struct {
	Code    int
	Message string
}
