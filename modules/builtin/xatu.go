package builtin

import (
	"sync"

	"github.com/ethpandaops/rpc-snooper/types"
	"github.com/ethpandaops/rpc-snooper/xatu"
)

// XatuModule implements types.Module for Xatu event publishing.
type XatuModule struct {
	id     uint64
	router *xatu.Router

	// Track which handler matched for each call
	handlerMap map[uint64]xatu.EventHandler
	mu         sync.Mutex
}

// NewXatuModule creates a new XatuModule.
func NewXatuModule(id uint64, router *xatu.Router) *XatuModule {
	return &XatuModule{
		id:         id,
		router:     router,
		handlerMap: make(map[uint64]xatu.EventHandler, 100),
	}
}

// ID returns the module ID.
func (m *XatuModule) ID() uint64 {
	return m.id
}

// OnRequest processes the request through the Xatu router.
func (m *XatuModule) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	if m.router == nil {
		return ctx, nil
	}

	// Extract JSON-RPC method from parsed body
	method := extractMethod(ctx.Body)
	if method == "" {
		return ctx, nil
	}

	event := &xatu.RequestEvent{
		CallID:    ctx.CallCtx.ID(),
		Timestamp: ctx.Timestamp,
		Method:    method,
		Params:    extractParams(ctx.Body),
		BodyBytes: ctx.BodyBytes,
	}

	// Route to matching handler
	handler, matched := m.router.RouteRequest(event)
	if matched && handler != nil {
		m.mu.Lock()
		m.handlerMap[event.CallID] = handler
		m.mu.Unlock()
	}

	return ctx, nil
}

// OnResponse processes the response through the matched handler.
func (m *XatuModule) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	m.mu.Lock()

	handler, ok := m.handlerMap[ctx.CallCtx.ID()]
	if ok {
		delete(m.handlerMap, ctx.CallCtx.ID())
	}

	m.mu.Unlock()

	if !ok || handler == nil {
		return ctx, nil
	}

	event := &xatu.ResponseEvent{
		CallID:    ctx.CallCtx.ID(),
		Timestamp: ctx.Timestamp,
		Duration:  ctx.Duration,
		Result:    extractResult(ctx.Body),
		Error:     extractRPCError(ctx.Body),
		BodyBytes: ctx.BodyBytes,
	}

	handler.HandleResponse(event)

	return ctx, nil
}

// Configure is a no-op for XatuModule.
func (m *XatuModule) Configure(_ map[string]interface{}) error {
	return nil
}

// Close is a no-op for XatuModule.
func (m *XatuModule) Close() error {
	return nil
}

// extractMethod extracts the JSON-RPC method from the parsed body.
func extractMethod(body interface{}) string {
	bodyMap, ok := body.(map[string]interface{})
	if !ok {
		return ""
	}

	method, ok := bodyMap["method"].(string)
	if !ok {
		return ""
	}

	return method
}

// extractParams extracts the JSON-RPC params from the parsed body.
func extractParams(body interface{}) []any {
	bodyMap, ok := body.(map[string]interface{})
	if !ok {
		return nil
	}

	params, ok := bodyMap["params"].([]interface{})
	if !ok {
		return nil
	}

	return params
}

// extractResult extracts the JSON-RPC result from the parsed body.
func extractResult(body interface{}) interface{} {
	bodyMap, ok := body.(map[string]interface{})
	if !ok {
		return nil
	}

	return bodyMap["result"]
}

// extractRPCError extracts the JSON-RPC error from the parsed body.
func extractRPCError(body interface{}) *xatu.RPCError {
	bodyMap, ok := body.(map[string]interface{})
	if !ok {
		return nil
	}

	errObj, ok := bodyMap["error"].(map[string]interface{})
	if !ok {
		return nil
	}

	rpcErr := &xatu.RPCError{}

	if code, ok := errObj["code"].(float64); ok {
		rpcErr.Code = int(code)
	}

	if msg, ok := errObj["message"].(string); ok {
		rpcErr.Message = msg
	}

	return rpcErr
}
