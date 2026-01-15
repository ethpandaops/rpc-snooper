package xatu

import (
	"encoding/json"
	"strings"

	"github.com/sirupsen/logrus"
)

// MetadataUpdateFunc is a callback function for updating execution metadata.
type MetadataUpdateFunc func(versions []ClientVersionV1)

// EngineClientVersionHandler handles engine_getClientVersionV1 events.
// This handler does not publish events to xatu - it only observes responses
// and updates the cached execution metadata for use in other events.
type EngineClientVersionHandler struct {
	log          logrus.FieldLogger
	updateMetada MetadataUpdateFunc
}

// NewEngineClientVersionHandler creates a new engine_getClientVersionV1 handler.
func NewEngineClientVersionHandler(log logrus.FieldLogger, updateFn MetadataUpdateFunc) *EngineClientVersionHandler {
	return &EngineClientVersionHandler{
		log:          log.WithField("handler", "engine_getClientVersion"),
		updateMetada: updateFn,
	}
}

// Name returns the handler name.
func (h *EngineClientVersionHandler) Name() string {
	return "engine_getClientVersion"
}

// MethodMatcher returns a function that checks if a method matches engine_getClientVersionV*.
func (h *EngineClientVersionHandler) MethodMatcher() func(method string) bool {
	return func(method string) bool {
		return strings.HasPrefix(method, "engine_getClientVersion")
	}
}

// HandleRequest processes the request. We don't need to store anything from the request.
func (h *EngineClientVersionHandler) HandleRequest(_ *RequestEvent) bool {
	// We want to process the response to extract client version info
	return true
}

// HandleResponse processes the response and updates the cached execution metadata.
func (h *EngineClientVersionHandler) HandleResponse(event *ResponseEvent) {
	// Ignore error responses
	if event.Error != nil {
		h.log.WithFields(logrus.Fields{
			"error_code":    event.Error.Code,
			"error_message": event.Error.Message,
		}).Debug("engine_getClientVersion returned error")

		return
	}

	// Parse the result as []ClientVersionV1
	versions, err := parseClientVersionResponse(event.Result)
	if err != nil {
		h.log.WithError(err).Debug("failed to parse engine_getClientVersion response")

		return
	}

	if len(versions) == 0 {
		h.log.Debug("engine_getClientVersion returned empty result")

		return
	}

	// Update the cached metadata
	h.updateMetada(versions)

	h.log.WithFields(logrus.Fields{
		"client_count":   len(versions),
		"implementation": versions[0].Name,
		"version":        versions[0].Version,
	}).Debug("updated execution metadata from observed engine_getClientVersion response")
}

// parseClientVersionResponse parses the JSON-RPC result as []ClientVersionV1.
func parseClientVersionResponse(result any) ([]ClientVersionV1, error) {
	// Result could be []any or already []ClientVersionV1 depending on unmarshaling
	switch v := result.(type) {
	case []any:
		// Marshal back to JSON and unmarshal into proper struct
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}

		var versions []ClientVersionV1

		if err := json.Unmarshal(data, &versions); err != nil {
			return nil, err
		}

		return versions, nil

	case []ClientVersionV1:
		return v, nil

	default:
		// Try direct marshal/unmarshal
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}

		var versions []ClientVersionV1

		if err := json.Unmarshal(data, &versions); err != nil {
			return nil, err
		}

		return versions, nil
	}
}
