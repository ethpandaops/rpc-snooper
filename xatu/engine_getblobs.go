package xatu

import (
	"context"
	"strings"
	"sync"
	"time"

	xatuProto "github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// PendingGetBlobsCall stores request data awaiting response correlation.
type PendingGetBlobsCall struct {
	CallID           uint64
	RequestTimestamp time.Time
	VersionedHashes  []string
	MethodVersion    string
}

// EngineGetBlobsHandler handles engine_getBlobs* events.
type EngineGetBlobsHandler struct {
	publisher Publisher
	log       logrus.FieldLogger

	pending map[uint64]*PendingGetBlobsCall
	mu      sync.Mutex
}

// NewEngineGetBlobsHandler creates a new engine_getBlobs handler.
func NewEngineGetBlobsHandler(publisher Publisher, log logrus.FieldLogger) *EngineGetBlobsHandler {
	return &EngineGetBlobsHandler{
		publisher: publisher,
		log:       log.WithField("handler", "engine_getBlobs"),
		pending:   make(map[uint64]*PendingGetBlobsCall, 100),
	}
}

// Name returns the handler name.
func (h *EngineGetBlobsHandler) Name() string {
	return "engine_getBlobs"
}

// MethodMatcher returns a function that checks if a method matches engine_getBlobs*.
func (h *EngineGetBlobsHandler) MethodMatcher() func(method string) bool {
	return func(method string) bool {
		return strings.HasPrefix(method, "engine_getBlobs")
	}
}

// HandleRequest processes the request and stores pending data.
func (h *EngineGetBlobsHandler) HandleRequest(event *RequestEvent) bool {
	hashes := extractVersionedHashes(event.Params)
	version := extractMethodVersion(event.Method)

	h.mu.Lock()
	h.pending[event.CallID] = &PendingGetBlobsCall{
		CallID:           event.CallID,
		RequestTimestamp: event.Timestamp,
		VersionedHashes:  hashes,
		MethodVersion:    version,
	}
	h.mu.Unlock()

	h.log.WithFields(logrus.Fields{
		"call_id":         event.CallID,
		"requested_count": len(hashes),
		"method_version":  version,
	}).Debug("captured engine_getBlobs request")

	return true // Process response
}

// HandleResponse processes the response, correlates with request, and publishes the event.
func (h *EngineGetBlobsHandler) HandleResponse(event *ResponseEvent) {
	h.mu.Lock()

	pending, ok := h.pending[event.CallID]
	if !ok {
		h.mu.Unlock()
		h.log.WithField("call_id", event.CallID).Warn("no pending request found for response")

		return
	}

	delete(h.pending, event.CallID)

	h.mu.Unlock()

	// Build and publish event
	decoratedEvent := h.buildDecoratedEvent(pending, event)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := h.publisher.Publish(ctx, decoratedEvent); err != nil {
		h.log.WithError(err).Error("failed to publish engine_getBlobs event")

		return
	}

	h.log.WithFields(logrus.Fields{
		"call_id":         event.CallID,
		"duration_ms":     event.Duration.Milliseconds(),
		"requested_count": len(pending.VersionedHashes),
	}).Debug("published engine_getBlobs event")
}

func (h *EngineGetBlobsHandler) buildDecoratedEvent(
	pending *PendingGetBlobsCall,
	resp *ResponseEvent,
) *xatuProto.DecoratedEvent {
	returnedCount, status, errorMsg := extractGetBlobsResponseData(resp)

	durationMs := resp.Duration.Milliseconds()
	if durationMs < 0 {
		durationMs = 0
	}

	//nolint:gosec // Safe: slice length cannot exceed uint32 in practice
	requestedCount := uint32(len(pending.VersionedHashes))

	data := &xatuProto.ConsensusEngineAPIGetBlobs{
		RequestedAt:     timestamppb.New(pending.RequestTimestamp),
		DurationMs:      wrapperspb.UInt64(uint64(durationMs)), //nolint:gosec // duration is non-negative after check
		RequestedCount:  wrapperspb.UInt32(requestedCount),
		VersionedHashes: pending.VersionedHashes,
		ReturnedCount:   wrapperspb.UInt32(returnedCount),
		Status:          status,
		ErrorMessage:    errorMsg,
		MethodVersion:   pending.MethodVersion,
	}

	return &xatuProto.DecoratedEvent{
		Event: &xatuProto.Event{
			Name:     xatuProto.Event_CONSENSUS_ENGINE_API_GET_BLOBS,
			DateTime: timestamppb.New(resp.Timestamp),
			Id:       uuid.New().String(),
		},
		Meta: &xatuProto.Meta{
			Client: h.publisher.ClientMeta(),
		},
		Data: &xatuProto.DecoratedEvent_ConsensusEngineApiGetBlobs{
			ConsensusEngineApiGetBlobs: data,
		},
	}
}

// extractVersionedHashes extracts versioned hashes from the request params.
// params[0] should be an array of versioned hash strings.
func extractVersionedHashes(params []any) []string {
	if len(params) == 0 {
		return nil
	}

	hashList, ok := params[0].([]any)
	if !ok {
		return nil
	}

	hashes := make([]string, 0, len(hashList))

	for _, h := range hashList {
		if hash, ok := h.(string); ok {
			hashes = append(hashes, hash)
		}
	}

	return hashes
}

// extractMethodVersion extracts the version suffix from the method name.
// e.g., "engine_getBlobsV1" -> "V1"
func extractMethodVersion(method string) string {
	if strings.HasPrefix(method, "engine_getBlobs") {
		version := strings.TrimPrefix(method, "engine_getBlobs")
		if version != "" {
			return version
		}
	}

	return ""
}

// extractGetBlobsResponseData extracts the returned count, status, and error message from the response.
func extractGetBlobsResponseData(resp *ResponseEvent) (returnedCount uint32, status, errorMsg string) {
	// Handle error response
	if resp.Error != nil {
		return 0, "ERROR", resp.Error.Message
	}

	// Handle null result (unsupported)
	if resp.Result == nil {
		return 0, "UNSUPPORTED", ""
	}

	// Handle array result
	resultList, ok := resp.Result.([]any)
	if !ok {
		return 0, "UNSUPPORTED", ""
	}

	// Count non-null blobs
	var nonNullCount uint32

	for _, blob := range resultList {
		if blob != nil {
			nonNullCount++
		}
	}

	// Determine status
	resultLen := uint32(len(resultList)) //nolint:gosec // Safe: slice length cannot exceed uint32 in practice

	switch {
	case nonNullCount == 0:
		status = "EMPTY"
	case nonNullCount < resultLen:
		status = "PARTIAL"
	default:
		status = "SUCCESS"
	}

	return nonNullCount, status, ""
}
