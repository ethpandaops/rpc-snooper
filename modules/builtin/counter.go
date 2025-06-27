package builtin

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ethpandaops/rpc-snooper/modules/protocol"
	"github.com/ethpandaops/rpc-snooper/types"
)

type RequestCounter struct {
	id      uint64
	connMgr types.ConnectionManager
	count   int64
}

func NewRequestCounter(id uint64, connMgr types.ConnectionManager) *RequestCounter {
	return &RequestCounter{
		id:      id,
		connMgr: connMgr,
	}
}

func (rc *RequestCounter) ID() uint64 {
	return rc.id
}

func (rc *RequestCounter) OnRequest(ctx *types.RequestContext) (*types.RequestContext, error) {
	count := atomic.AddInt64(&rc.count, 1)

	counterEvent := protocol.CounterEvent{
		ModuleID:    rc.id,
		Count:       count,
		RequestType: ctx.Method,
	}

	msg := &protocol.WSMessage{
		ModuleID:  rc.id,
		Method:    "counter_event",
		Data:      counterEvent,
		Timestamp: time.Now().UnixNano(),
	}

	if err := rc.connMgr.SendMessage(msg); err != nil {
		// Log error but don't fail the request processing
		return ctx, fmt.Errorf("failed to send counter event: %w", err)
	}

	return ctx, nil
}

func (rc *RequestCounter) OnResponse(ctx *types.ResponseContext) (*types.ResponseContext, error) {
	return ctx, nil
}

func (rc *RequestCounter) Configure(_ map[string]interface{}) error {
	return nil
}

func (rc *RequestCounter) Close() error {
	return nil
}

func (rc *RequestCounter) GetCount() int64 {
	return atomic.LoadInt64(&rc.count)
}
