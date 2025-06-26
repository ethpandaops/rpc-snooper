package builtin

import (
	"fmt"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/protocol"
	"github.com/ethpandaops/rpc-snooper/modules/types"
)

type RequestSnooper struct {
	Id      uint64
	ConnMgr types.ConnectionManager
}

func (rs *RequestSnooper) ID() uint64 {
	return rs.Id
}

func (rs *RequestSnooper) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	hookEvent := protocol.HookEvent{
		ModuleID:    rs.Id,
		HookType:    "request",
		RequestID:   ctx.ID,
		Data:        ctx.Body,
		ContentType: ctx.ContentType,
	}

	msg := &protocol.WSMessage{
		ModuleID:  rs.Id,
		Method:    "hook_event",
		Data:      hookEvent,
		Timestamp: time.Now().UnixNano(),
	}

	if err := rs.ConnMgr.SendMessage(msg); err != nil {
		// Log error but don't fail the request processing
		return ctx, fmt.Errorf("failed to send hook event: %w", err)
	}
	return ctx, nil
}

func (rs *RequestSnooper) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	return ctx, nil
}

func (rs *RequestSnooper) Configure(config map[string]interface{}) error {
	return nil
}

func (rs *RequestSnooper) Close() error {
	return nil
}

type ResponseSnooper struct {
	Id      uint64
	ConnMgr types.ConnectionManager
}

func (rs *ResponseSnooper) ID() uint64 {
	return rs.Id
}

func (rs *ResponseSnooper) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	return ctx, nil
}

func (rs *ResponseSnooper) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	hookEvent := protocol.HookEvent{
		ModuleID:    rs.Id,
		HookType:    "response",
		RequestID:   ctx.ID,
		Data:        ctx.Body,
		ContentType: ctx.ContentType,
	}

	msg := &protocol.WSMessage{
		ModuleID:  rs.Id,
		Method:    "hook_event",
		Data:      hookEvent,
		Timestamp: time.Now().UnixNano(),
	}

	if err := rs.ConnMgr.SendMessage(msg); err != nil {
		// Log error but don't fail the response processing
		return ctx, fmt.Errorf("failed to send hook event: %w", err)
	}
	return ctx, nil
}

func (rs *ResponseSnooper) Configure(config map[string]interface{}) error {
	return nil
}

func (rs *ResponseSnooper) Close() error {
	return nil
}
