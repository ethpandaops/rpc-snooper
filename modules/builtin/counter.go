package builtin

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/protocol"
	"github.com/ethpandaops/rpc-snooper/types"
)

type RequestCounter struct {
	Id      uint64
	ConnMgr types.ConnectionManager
	Count   int64
}

func (rc *RequestCounter) ID() uint64 {
	return rc.Id
}

func (rc *RequestCounter) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	count := atomic.AddInt64(&rc.Count, 1)

	counterEvent := protocol.CounterEvent{
		ModuleID:    rc.Id,
		Count:       count,
		RequestType: ctx.Method,
	}

	msg := &protocol.WSMessage{
		ModuleID:  rc.Id,
		Method:    "counter_event",
		Data:      counterEvent,
		Timestamp: time.Now().UnixNano(),
	}

	if err := rc.ConnMgr.SendMessage(msg); err != nil {
		// Log error but don't fail the request processing
		return ctx, fmt.Errorf("failed to send counter event: %w", err)
	}
	return ctx, nil
}

func (rc *RequestCounter) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	return ctx, nil
}

func (rc *RequestCounter) Configure(config map[string]interface{}) error {
	return nil
}

func (rc *RequestCounter) Close() error {
	return nil
}

func (rc *RequestCounter) GetCount() int64 {
	return atomic.LoadInt64(&rc.Count)
}
