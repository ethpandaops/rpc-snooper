package builtin

import (
	"fmt"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/protocol"
	"github.com/ethpandaops/rpc-snooper/types"
)

type RequestSnooper struct {
	id      uint64
	connMgr types.ConnectionManager
}

func NewRequestSnooper(id uint64, connMgr types.ConnectionManager) *RequestSnooper {
	return &RequestSnooper{
		id:      id,
		connMgr: connMgr,
	}
}

func (rs *RequestSnooper) ID() uint64 {
	return rs.id
}

func (rs *RequestSnooper) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	hookEvent := protocol.HookEvent{
		ModuleID:    rs.id,
		HookType:    "request",
		RequestID:   ctx.CallCtx.ID(),
		ContentType: ctx.ContentType,
	}

	msg := &protocol.WSMessage{
		ModuleID:  rs.id,
		Method:    "hook_event",
		Data:      hookEvent,
		Timestamp: time.Now().UnixNano(),
	}

	if err := rs.connMgr.SendMessageWithBinary(msg, ctx.BodyBytes); err != nil {
		// Log error but don't fail the request processing
		return ctx, fmt.Errorf("failed to send hook event: %w", err)
	}

	return ctx, nil
}

func (rs *RequestSnooper) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	return ctx, nil
}

func (rs *RequestSnooper) Configure(_ map[string]interface{}) error {
	return nil
}

func (rs *RequestSnooper) Close() error {
	return nil
}

type ResponseSnooper struct {
	id      uint64
	connMgr types.ConnectionManager
}

func NewResponseSnooper(id uint64, connMgr types.ConnectionManager) *ResponseSnooper {
	return &ResponseSnooper{
		id:      id,
		connMgr: connMgr,
	}
}

func (rs *ResponseSnooper) ID() uint64 {
	return rs.id
}

func (rs *ResponseSnooper) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	ctx.CallCtx.SetData(rs.id, "wants_response", true)
	return ctx, nil
}

func (rs *ResponseSnooper) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	hookEvent := protocol.HookEvent{
		ModuleID:    rs.id,
		HookType:    "response",
		RequestID:   ctx.CallCtx.ID(),
		ContentType: ctx.ContentType,
	}

	msg := &protocol.WSMessage{
		ModuleID:  rs.id,
		Method:    "hook_event",
		Data:      hookEvent,
		Timestamp: time.Now().UnixNano(),
	}

	if err := rs.connMgr.SendMessageWithBinary(msg, ctx.BodyBytes); err != nil {
		// Log error but don't fail the response processing
		return ctx, fmt.Errorf("failed to send hook event: %w", err)
	}

	return ctx, nil
}

func (rs *ResponseSnooper) Configure(_ map[string]interface{}) error {
	return nil
}

func (rs *ResponseSnooper) Close() error {
	return nil
}
