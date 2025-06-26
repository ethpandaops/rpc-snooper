package builtin

import (
	"fmt"
	"sync"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/protocol"
	"github.com/ethpandaops/rpc-snooper/modules/types"
)

type ResponseTracer struct {
	Id           uint64
	ConnMgr      types.ConnectionManager
	RequestTimes map[string]time.Time
	Mu           sync.RWMutex
}

func (rt *ResponseTracer) ID() uint64 {
	return rt.Id
}

func (rt *ResponseTracer) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	rt.Mu.Lock()
	if rt.RequestTimes == nil {
		rt.RequestTimes = make(map[string]time.Time)
	}
	rt.RequestTimes[ctx.ID] = ctx.Timestamp
	rt.Mu.Unlock()

	return ctx, nil
}

func (rt *ResponseTracer) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	rt.Mu.RLock()
	startTime, exists := rt.RequestTimes[ctx.ID]
	rt.Mu.RUnlock()

	if !exists {
		return ctx, nil
	}

	rt.Mu.Lock()
	delete(rt.RequestTimes, ctx.ID)
	rt.Mu.Unlock()

	duration := ctx.Timestamp.Sub(startTime)

	tracerEvent := protocol.TracerEvent{
		ModuleID:     rt.Id,
		RequestID:    ctx.ID,
		Duration:     duration.Milliseconds(),
		RequestSize:  rt.calculateSize(ctx.Body),
		ResponseSize: rt.calculateSize(ctx.Body),
		StatusCode:   ctx.StatusCode,
	}

	msg := &protocol.WSMessage{
		ModuleID:  rt.Id,
		Method:    "tracer_event",
		Data:      tracerEvent,
		Timestamp: time.Now().UnixNano(),
	}

	if err := rt.ConnMgr.SendMessage(msg); err != nil {
		// Log error but don't fail the response processing
		return ctx, fmt.Errorf("failed to send tracer event: %w", err)
	}
	return ctx, nil
}

func (rt *ResponseTracer) Configure(config map[string]interface{}) error {
	return nil
}

func (rt *ResponseTracer) Close() error {
	rt.Mu.Lock()
	defer rt.Mu.Unlock()
	rt.RequestTimes = nil
	return nil
}

func (rt *ResponseTracer) calculateSize(data interface{}) int64 {
	if data == nil {
		return 0
	}

	switch v := data.(type) {
	case []byte:
		return int64(len(v))
	case string:
		return int64(len(v))
	default:
		return 0
	}
}
