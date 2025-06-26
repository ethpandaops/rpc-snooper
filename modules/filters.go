package modules

import (
	"encoding/json"
	"strings"

	"github.com/ethpandaops/rpc-snooper/modules/types"
	"github.com/itchyny/gojq"
	"github.com/sirupsen/logrus"
)

type FilterEngine struct {
	logger logrus.FieldLogger
}

func NewFilterEngine(logger logrus.FieldLogger) *FilterEngine {
	return &FilterEngine{
		logger: logger,
	}
}

// CompileFilter compiles the JSON query in a filter configuration
func (fe *FilterEngine) CompileFilter(filter *types.FilterConfig) error {
	if filter.JSONQuery == "" {
		return nil
	}

	query, err := gojq.Parse(filter.JSONQuery)
	if err != nil {
		return err
	}

	// Store the compiled query in the filter config
	// We use interface{} to avoid import cycles
	filter.SetCompiled(query)
	return nil
}

// ShouldProcessRequest determines if a request should be processed by a module based on filters
func (fe *FilterEngine) ShouldProcessRequest(filter *types.FilterConfig, ctx *types.RequestContext) bool {
	if filter == nil {
		return true
	}

	// Check HTTP method filter
	if len(filter.Methods) > 0 {
		matched := false
		for _, method := range filter.Methods {
			if strings.EqualFold(ctx.Method, method) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check content type filter
	if len(filter.ContentTypes) > 0 {
		matched := false
		for _, ct := range filter.ContentTypes {
			if strings.Contains(ctx.ContentType, ct) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check JSON query filter
	if filter.JSONQuery != "" && strings.Contains(ctx.ContentType, "json") {
		return fe.evaluateJSONQuery(filter, ctx.Body)
	}

	return true
}

// ShouldProcessResponse determines if a response should be processed by a module based on filters
func (fe *FilterEngine) ShouldProcessResponse(filter *types.FilterConfig, ctx *types.ResponseContext) bool {
	if filter == nil {
		return true
	}

	// Check status code filter
	if len(filter.StatusCodes) > 0 {
		matched := false
		for _, code := range filter.StatusCodes {
			if ctx.StatusCode == code {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check content type filter
	if len(filter.ContentTypes) > 0 {
		matched := false
		for _, ct := range filter.ContentTypes {
			if strings.Contains(ctx.ContentType, ct) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check JSON query filter
	if filter.JSONQuery != "" && strings.Contains(ctx.ContentType, "json") {
		return fe.evaluateJSONQuery(filter, ctx.Body)
	}

	return true
}

// evaluateJSONQuery evaluates a gojq query against the provided data
func (fe *FilterEngine) evaluateJSONQuery(filter *types.FilterConfig, body interface{}) bool {
	compiled := filter.GetCompiled()
	if compiled == nil {
		fe.logger.Warn("JSON query not compiled, skipping filter")
		return true
	}

	query, ok := compiled.(*gojq.Query)
	if !ok {
		fe.logger.Warn("Invalid compiled query type, skipping filter")
		return true
	}

	// Convert body to JSON if it's not already
	var data interface{}
	switch v := body.(type) {
	case []byte:
		if err := json.Unmarshal(v, &data); err != nil {
			fe.logger.WithError(err).Debug("Failed to unmarshal body for JSON query")
			return false
		}
	case string:
		if err := json.Unmarshal([]byte(v), &data); err != nil {
			fe.logger.WithError(err).Debug("Failed to unmarshal string body for JSON query")
			return false
		}
	default:
		data = v
	}

	// Run the query
	iter := query.Run(data)
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, ok := v.(error); ok {
			fe.logger.WithError(err).Debug("Error in JSON query evaluation")
			return false
		}
		// If we get any truthy result, the filter matches
		if result, ok := v.(bool); ok && result {
			return true
		}
		// Non-boolean results that are not nil/false are considered truthy
		if v != nil && v != false {
			return true
		}
	}

	return false
}

