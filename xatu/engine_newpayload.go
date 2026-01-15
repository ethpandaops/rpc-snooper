package xatu

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	xatuProto "github.com/ethpandaops/xatu/pkg/proto/xatu"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

const (
	// statusUnknown is the default status when the response cannot be parsed.
	statusUnknown = "UNKNOWN"
	// statusError is the status when the response contains an error.
	statusError = "ERROR"
)

// PendingNewPayloadCall stores request data awaiting response correlation.
type PendingNewPayloadCall struct {
	CallID           uint64
	RequestTimestamp time.Time
	MethodVersion    string

	// Execution payload fields
	BlockNumber uint64
	BlockHash   string
	ParentHash  string
	GasUsed     uint64
	GasLimit    uint64
	TxCount     uint32
	BlobCount   uint32
}

// EngineNewPayloadHandler handles engine_newPayload* events.
type EngineNewPayloadHandler struct {
	publisher Publisher
	log       logrus.FieldLogger

	pending map[uint64]*PendingNewPayloadCall
	mu      sync.Mutex
}

// NewEngineNewPayloadHandler creates a new engine_newPayload handler.
func NewEngineNewPayloadHandler(publisher Publisher, log logrus.FieldLogger) *EngineNewPayloadHandler {
	return &EngineNewPayloadHandler{
		publisher: publisher,
		log:       log.WithField("handler", "engine_newPayload"),
		pending:   make(map[uint64]*PendingNewPayloadCall, 100),
	}
}

// Name returns the handler name.
func (h *EngineNewPayloadHandler) Name() string {
	return "engine_newPayload"
}

// MethodMatcher returns a function that checks if a method matches engine_newPayload*.
func (h *EngineNewPayloadHandler) MethodMatcher() func(method string) bool {
	return func(method string) bool {
		return strings.HasPrefix(method, "engine_newPayload")
	}
}

// HandleRequest processes the request and stores pending data.
func (h *EngineNewPayloadHandler) HandleRequest(event *RequestEvent) bool {
	pending := h.extractPayloadData(event)

	h.mu.Lock()
	h.pending[event.CallID] = pending
	h.mu.Unlock()

	h.log.WithFields(logrus.Fields{
		"call_id":        event.CallID,
		"block_number":   pending.BlockNumber,
		"block_hash":     pending.BlockHash,
		"tx_count":       pending.TxCount,
		"blob_count":     pending.BlobCount,
		"method_version": pending.MethodVersion,
	}).Debug("captured engine_newPayload request")

	return true // Process response
}

// HandleResponse processes the response, correlates with request, and publishes the event.
func (h *EngineNewPayloadHandler) HandleResponse(event *ResponseEvent) {
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
		h.log.WithError(err).Error("failed to publish engine_newPayload event")

		return
	}

	h.log.WithFields(logrus.Fields{
		"call_id":      event.CallID,
		"duration_ms":  event.Duration.Milliseconds(),
		"block_number": pending.BlockNumber,
		"block_hash":   pending.BlockHash,
	}).Debug("published engine_newPayload event")
}

func (h *EngineNewPayloadHandler) extractPayloadData(event *RequestEvent) *PendingNewPayloadCall {
	pending := &PendingNewPayloadCall{
		CallID:           event.CallID,
		RequestTimestamp: event.Timestamp,
		MethodVersion:    extractNewPayloadMethodVersion(event.Method),
	}

	// params[0] is the ExecutionPayload
	if len(event.Params) == 0 {
		return pending
	}

	payload, ok := event.Params[0].(map[string]any)
	if !ok {
		return pending
	}

	// Extract block number (hex string -> uint64)
	if blockNumber, ok := payload["blockNumber"].(string); ok {
		pending.BlockNumber = hexToUint64(blockNumber)
	}

	// Extract block hash
	if blockHash, ok := payload["blockHash"].(string); ok {
		pending.BlockHash = blockHash
	}

	// Extract parent hash
	if parentHash, ok := payload["parentHash"].(string); ok {
		pending.ParentHash = parentHash
	}

	// Extract gas used (hex string -> uint64)
	if gasUsed, ok := payload["gasUsed"].(string); ok {
		pending.GasUsed = hexToUint64(gasUsed)
	}

	// Extract gas limit (hex string -> uint64)
	if gasLimit, ok := payload["gasLimit"].(string); ok {
		pending.GasLimit = hexToUint64(gasLimit)
	}

	// Extract transaction count
	if transactions, ok := payload["transactions"].([]any); ok {
		//nolint:gosec // Safe: transaction count cannot exceed uint32 in practice
		pending.TxCount = uint32(len(transactions))
	}

	// Extract blob count from blobGasUsed or versioned hashes
	// In V3+, we may have expectedBlobVersionedHashes in params[1]
	if len(event.Params) > 1 {
		if versionedHashes, ok := event.Params[1].([]any); ok {
			//nolint:gosec // Safe: blob count cannot exceed uint32 in practice
			pending.BlobCount = uint32(len(versionedHashes))
		}
	}

	return pending
}

func (h *EngineNewPayloadHandler) buildDecoratedEvent(
	pending *PendingNewPayloadCall,
	resp *ResponseEvent,
) *xatuProto.DecoratedEvent {
	status, latestValidHash, validationError := extractNewPayloadResponseData(resp)

	durationMs := resp.Duration.Milliseconds()
	if durationMs < 0 {
		durationMs = 0
	}

	data := &xatuProto.ExecutionEngineNewPayload{
		Source:        xatuProto.EngineSource_ENGINE_SOURCE_SNOOPER,
		RequestedAt:   timestamppb.New(pending.RequestTimestamp),
		DurationMs:    wrapperspb.UInt64(uint64(durationMs)), //nolint:gosec // duration is non-negative after check
		MethodVersion: pending.MethodVersion,

		// Execution payload details
		BlockNumber: wrapperspb.UInt64(pending.BlockNumber),
		BlockHash:   pending.BlockHash,
		ParentHash:  pending.ParentHash,
		GasUsed:     wrapperspb.UInt64(pending.GasUsed),
		GasLimit:    wrapperspb.UInt64(pending.GasLimit),
		TxCount:     wrapperspb.UInt32(pending.TxCount),
		BlobCount:   wrapperspb.UInt32(pending.BlobCount),

		// Response data
		Status:          status,
		LatestValidHash: latestValidHash,
		ValidationError: validationError,
	}

	return &xatuProto.DecoratedEvent{
		Event: &xatuProto.Event{
			Name:     xatuProto.Event_EXECUTION_ENGINE_NEW_PAYLOAD,
			DateTime: timestamppb.New(resp.Timestamp),
			Id:       uuid.New().String(),
		},
		Meta: &xatuProto.Meta{
			Client: h.publisher.ClientMeta(),
		},
		Data: &xatuProto.DecoratedEvent_ExecutionEngineNewPayload{
			ExecutionEngineNewPayload: data,
		},
	}
}

// extractNewPayloadMethodVersion extracts the version suffix from the method name.
// e.g., "engine_newPayloadV3" -> "V3"
func extractNewPayloadMethodVersion(method string) string {
	if strings.HasPrefix(method, "engine_newPayload") {
		version := strings.TrimPrefix(method, "engine_newPayload")
		if version != "" {
			return version
		}
	}

	return ""
}

// extractNewPayloadResponseData extracts the payload status from the response.
// Returns status, latestValidHash, and validationError.
func extractNewPayloadResponseData(resp *ResponseEvent) (status, latestValidHash, validationError string) {
	// Handle error response
	if resp.Error != nil {
		return statusError, "", resp.Error.Message
	}

	// Handle null result
	if resp.Result == nil {
		return statusUnknown, "", ""
	}

	// Result should be a PayloadStatusV1 object
	result, ok := resp.Result.(map[string]any)
	if !ok {
		return statusUnknown, "", ""
	}

	// Extract status (VALID, INVALID, SYNCING, ACCEPTED, INVALID_BLOCK_HASH)
	if s, ok := result["status"].(string); ok {
		status = s
	} else {
		status = statusUnknown
	}

	// Extract latestValidHash (present when status is INVALID)
	if lvh, ok := result["latestValidHash"].(string); ok {
		latestValidHash = lvh
	}

	// Extract validationError (present when validation fails)
	if ve, ok := result["validationError"].(string); ok {
		validationError = ve
	}

	return status, latestValidHash, validationError
}

// hexToUint64 converts a hex string (with or without 0x prefix) to uint64.
func hexToUint64(s string) uint64 {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimPrefix(s, "0X")

	if s == "" {
		return 0
	}

	val, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0
	}

	return val
}
