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
	CallCtx     ProxyCallContext
	Method      string
	URL         *url.URL
	Headers     http.Header
	Body        interface{}
	BodyBytes   []byte
	ContentType string
	Timestamp   time.Time
}

type ResponseContext struct {
	CallCtx     ProxyCallContext
	StatusCode  int
	Headers     http.Header
	Body        interface{}
	BodyBytes   []byte
	ContentType string
	Timestamp   time.Time
	Duration    time.Duration
}

type ConnectionManager interface {
	SendMessage(msg *protocol.WSMessage) error
	SendMessageWithBinary(msg *protocol.WSMessage, binaryData []byte) error
	WaitForResponse(requestID uint64) (*protocol.WSMessageWithBinary, error)
	GenerateRequestID() uint64
}
type FilterConfig struct {
	RequestFilter  *Filter `json:"request_filter,omitempty"`
	ResponseFilter *Filter `json:"response_filter,omitempty"`
}

type Filter struct {
	ContentTypes []string    `json:"content_types,omitempty"`
	JSONQuery    string      `json:"json_query,omitempty"`
	Methods      []string    `json:"methods,omitempty"`      // HTTP methods to filter on (for requests)
	StatusCodes  []int       `json:"status_codes,omitempty"` // Response status codes to filter on (for responses)
	compiled     interface{} // gojq.Query - using interface{} to avoid import cycle
}

// GetCompiled returns the compiled gojq query
func (f *Filter) GetCompiled() interface{} {
	return f.compiled
}

// SetCompiled sets the compiled gojq query
func (f *Filter) SetCompiled(compiled interface{}) {
	f.compiled = compiled
}

type ProxyCallContext interface {
	Context() context.Context
	ID() uint64
	SetData(moduleId uint64, key string, value interface{})
	GetData(moduleId uint64, key string) interface{}
}
