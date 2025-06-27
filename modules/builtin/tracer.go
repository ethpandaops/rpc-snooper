package builtin

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/protocol"
	"github.com/ethpandaops/rpc-snooper/types"
	"github.com/itchyny/gojq"
)

type ResponseTracer struct {
	id             uint64
	connMgr        types.ConnectionManager
	requestSelect  string
	responseSelect string
	requestQuery   *gojq.Query
	responseQuery  *gojq.Query
}

func NewResponseTracer(id uint64, connMgr types.ConnectionManager) *ResponseTracer {
	return &ResponseTracer{
		id:      id,
		connMgr: connMgr,
	}
}

func (rt *ResponseTracer) ID() uint64 {
	return rt.id
}

func (rt *ResponseTracer) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	ctx.CallCtx.SetData(rt.id, "wants_response", true)

	// Extract request data if query is configured
	if rt.requestQuery != nil && strings.Contains(ctx.ContentType, "json") {
		requestData := rt.extractData(rt.requestQuery, ctx.Body)
		if requestData != nil {
			ctx.CallCtx.SetData(rt.id, "request_extracted_data", requestData)
		}
	}

	return ctx, nil
}

func (rt *ResponseTracer) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	duration := ctx.Duration
	requestSize, _ := ctx.CallCtx.GetData(0, "request_size").(int)

	// Extract response data if query is configured
	var responseData any

	if rt.responseQuery != nil && strings.Contains(ctx.ContentType, "json") {
		responseData = rt.extractData(rt.responseQuery, ctx.Body)
	}

	// Get previously extracted request data
	requestData := ctx.CallCtx.GetData(rt.id, "request_extracted_data")

	tracerEvent := &protocol.TracerEvent{
		ModuleID:     rt.id,
		RequestID:    ctx.CallCtx.ID(),
		Duration:     duration.Milliseconds(),
		ResponseSize: int64(len(ctx.BodyBytes)),
		RequestSize:  int64(requestSize),
		StatusCode:   ctx.StatusCode,
		RequestData:  requestData,
		ResponseData: responseData,
	}

	msg := &protocol.WSMessage{
		ModuleID:  rt.id,
		Method:    "tracer_event",
		Data:      tracerEvent,
		Timestamp: time.Now().UnixNano(),
	}

	if err := rt.connMgr.SendMessage(msg); err != nil {
		// Log error but don't fail the response processing
		return ctx, fmt.Errorf("failed to send tracer event: %w", err)
	}

	return ctx, nil
}

func (rt *ResponseTracer) Configure(config map[string]interface{}) error {
	// Parse request_select if provided
	if requestSelect, ok := config["request_select"].(string); ok && requestSelect != "" {
		rt.requestSelect = requestSelect

		query, err := gojq.Parse(requestSelect)
		if err != nil {
			return fmt.Errorf("failed to parse request_select query: %w", err)
		}

		rt.requestQuery = query
	}

	// Parse response_select if provided
	if responseSelect, ok := config["response_select"].(string); ok && responseSelect != "" {
		rt.responseSelect = responseSelect

		query, err := gojq.Parse(responseSelect)
		if err != nil {
			return fmt.Errorf("failed to parse response_select query: %w", err)
		}

		rt.responseQuery = query
	}

	return nil
}

func (rt *ResponseTracer) Close() error {
	return nil
}

// extractData runs a gojq query against the provided data and returns the result
func (rt *ResponseTracer) extractData(query *gojq.Query, body any) any {
	// Convert body to JSON if it's not already
	var data any
	switch v := body.(type) {
	case []byte:
		if err := json.Unmarshal(v, &data); err != nil {
			return nil
		}
	case string:
		if err := json.Unmarshal([]byte(v), &data); err != nil {
			return nil
		}
	default:
		data = v
	}

	// Run the query and collect all results
	iter := query.Run(data)

	var results []any

	for {
		v, ok := iter.Next()
		if !ok {
			break
		}

		if _, ok := v.(error); ok {
			// Skip errors
			continue
		}

		results = append(results, v)
	}

	// Return based on number of results
	switch {
	case len(results) == 0:
		return nil
	case len(results) == 1:
		return results[0]
	default:
		return results
	}
}
