package types

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/protocol"
)

type Module interface {
	ID() uint64
	OnRequest(ctx *RequestContext) (*RequestContext, error)
	OnResponse(ctx *ResponseContext) (*ResponseContext, error)
	Configure(config map[string]interface{}) error
	Close() error
}

type RequestContext struct {
	Context     context.Context
	ID          string
	Method      string
	URL         *url.URL
	Headers     http.Header
	Body        interface{}
	ContentType string
	Timestamp   time.Time
	Modified    bool
	ModifiedBy  uint64
}

type ResponseContext struct {
	Context     context.Context
	ID          string
	StatusCode  int
	Headers     http.Header
	Body        interface{}
	ContentType string
	Timestamp   time.Time
	Modified    bool
	ModifiedBy  uint64
}

type ConnectionManager interface {
	SendMessage(msg *protocol.WSMessage) error
	WaitForResponse(requestID uint64) (*protocol.WSMessage, error)
	GenerateRequestID() uint64
}

type FilterConfig struct {
	ContentTypes []string    `json:"content_types,omitempty"`
	JSONQuery    string      `json:"json_query,omitempty"`
	Methods      []string    `json:"methods,omitempty"`      // HTTP methods to filter on
	StatusCodes  []int       `json:"status_codes,omitempty"` // Response status codes to filter on
	compiled     interface{} // gojq.Query - using interface{} to avoid import cycle
}

// GetCompiled returns the compiled gojq query
func (f *FilterConfig) GetCompiled() interface{} {
	return f.compiled
}

// SetCompiled sets the compiled gojq query
func (f *FilterConfig) SetCompiled(compiled interface{}) {
	f.compiled = compiled
}
