package snooper

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/ethpandaops/rpc-snooper/types"
	"github.com/ethpandaops/rpc-snooper/utils"
	"github.com/fatih/color"
	"github.com/sirupsen/logrus"
)

func (s *Snooper) beautifyJSON(body []byte) []byte {
	var obj any

	err := json.Unmarshal(body, &obj)
	if err != nil {
		s.logger.Warnf("failed unmarshaling data: %v", err)
		return nil
	}

	res, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		s.logger.Warnf("failed marshaling data: %v", err)
		return nil
	}

	return res
}

type logReadCloser struct {
	reader   io.Reader
	buf      *bytes.Buffer
	original io.ReadCloser
	logFn    func(data []byte)
	closed   bool
	logger   logrus.FieldLogger
}

func (s *Snooper) createTeeLogStream(stream io.ReadCloser, logfn func(reader io.ReadCloser)) io.ReadCloser {
	return s.createTeeLogStreamWithSizeHint(stream, 0, logfn)
}

// createTeeLogStreamWithSizeHint creates a tee log stream with an optional size hint for buffer pre-allocation.
// When sizeHint > 0, the buffer is pre-allocated to avoid reallocations during streaming.
// This provides ~2x throughput improvement for large payloads.
func (s *Snooper) createTeeLogStreamWithSizeHint(stream io.ReadCloser, sizeHint int64, logfn func(reader io.ReadCloser)) io.ReadCloser {
	var buf *bytes.Buffer
	if sizeHint > 0 {
		buf = bytes.NewBuffer(make([]byte, 0, sizeHint))
	} else {
		buf = new(bytes.Buffer)
	}

	teeReader := io.TeeReader(stream, buf)

	return &logReadCloser{
		reader:   teeReader,
		buf:      buf,
		original: stream,
		logFn: func(data []byte) {
			logfn(io.NopCloser(bytes.NewReader(data)))
		},
		logger: s.logger,
	}
}

func (r *logReadCloser) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}

func (r *logReadCloser) Close() error {
	if r.closed {
		return nil
	}

	r.closed = true

	// Drain any remaining data from the tee reader into the buffer
	_, _ = io.Copy(io.Discard, r.reader)

	// Close original stream
	err := r.original.Close()

	// Capture buffer data before spawning goroutine
	data := r.buf.Bytes()

	// Spawn goroutine for async logging (preserves current behavior)
	go func() {
		defer func() {
			if panicErr := recover(); panicErr != nil {
				if err2, ok := panicErr.(error); ok {
					r.logger.WithError(err2).Errorf("uncaught panic in log reader: %v, stack: %v", panicErr, string(debug.Stack()))
				} else {
					r.logger.Errorf("uncaught panic in log reader: %v, stack: %v", panicErr, string(debug.Stack()))
				}
			}
		}()

		r.logFn(data)
	}()

	return err
}

func (s *Snooper) logRequest(ctx *ProxyCallContext, req *http.Request, body io.ReadCloser) {
	// Generate sequence number for this request processing
	seq := s.orderedProcessor.GetNextSequence()

	defer s.orderedProcessor.CompleteSequence(seq)

	// Parse request body
	contentEncoding := req.Header.Get("Content-Encoding")
	contentType := req.Header.Get("Content-Type")

	switch contentEncoding {
	case "gzip":
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			s.logger.Warnf("failed unpacking gzip request body: %v", err)
			return
		}
		defer gzipReader.Close()

		body = gzipReader
	case "br":
		brotliReader := brotli.NewReader(body)
		body = io.NopCloser(brotliReader)
	}

	logFields := logrus.Fields{
		"color":  color.FgCyan,
		"length": req.ContentLength,
	}

	var bodyData []byte

	var parsedData any

	switch {
	case req.ContentLength == 0:
		logFields["body"] = []byte{}
		bodyData = []byte{}
	case strings.Contains(contentType, "application/octet-stream"):
		body = utils.NewHexEncoder(body)
		bodyData, _ = io.ReadAll(body)
		logFields["type"] = "ssz"
		logFields["body"] = fmt.Sprintf("%v\n\n", string(bodyData))
	default:
		bodyData, _ = io.ReadAll(body)

		if beautifiedJSON := s.beautifyJSON(bodyData); len(beautifiedJSON) > 0 {
			logFields["type"] = "json"
			logFields["body"] = fmt.Sprintf("%v\n\n", string(beautifiedJSON))

			// Store parsed JSON for module processing
			_ = json.Unmarshal(bodyData, &parsedData)
		} else {
			logFields["type"] = "unknown"
			bodyBuf := make([]byte, len(bodyData)*2)

			hex.Encode(bodyBuf, bodyData)

			logFields["body"] = bodyBuf
		}
	}

	ctx.SetData(0, "request_size", len(bodyData))

	// Extract and store jrpc_method for metrics collection if metrics are enabled
	if s.metricsEnabled && parsedData != nil {
		if jrpcMethod, ok := parsedData.(map[string]interface{}); ok {
			ctx.SetData(0, "jrpc_method", jrpcMethod["method"])
		}
	}

	// Wait for our turn in the processing sequence
	if !s.orderedProcessor.WaitForSequence(seq) {
		return // Context was cancelled
	}

	// Process modules in order
	s.processRequestModules(ctx, req, bodyData, parsedData, contentType)

	s.logger.WithFields(logFields).Infof("REQUEST #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())
}

func (s *Snooper) logResponse(ctx *ProxyCallContext, req *http.Request, rsp *http.Response, body io.ReadCloser) {
	// Generate sequence number for this response processing
	seq := s.orderedProcessor.GetNextSequence()

	defer s.orderedProcessor.CompleteSequence(seq)

	// Parse response body
	contentEncoding := rsp.Header.Get("Content-Encoding")
	contentType := rsp.Header.Get("Content-Type")

	switch contentEncoding {
	case "gzip":
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			s.logger.Warnf("failed unpacking gzip response body: %v", err)
			return
		}
		defer gzipReader.Close()

		body = gzipReader
	case "br":
		brotliReader := brotli.NewReader(body)
		body = io.NopCloser(brotliReader)
	}

	logFields := logrus.Fields{
		"status": rsp.StatusCode,
		"length": rsp.ContentLength,
	}

	if rsp.StatusCode >= 200 && rsp.StatusCode <= 299 {
		logFields["color"] = color.FgGreen
	} else {
		logFields["color"] = color.FgRed
	}

	var bodyData []byte

	var parsedData any

	switch {
	case rsp.ContentLength == 0:
		logFields["body"] = []byte{}
		bodyData = []byte{}
	case strings.Contains(contentType, "application/octet-stream"):
		body = utils.NewHexEncoder(body)
		bodyData, _ = io.ReadAll(body)
		logFields["type"] = "ssz"
		logFields["body"] = fmt.Sprintf("%v\n\n", string(bodyData))
	default:
		bodyData, _ = io.ReadAll(body)
		if beautifiedJSON := s.beautifyJSON(bodyData); len(beautifiedJSON) > 0 {
			logFields["type"] = "json"
			logFields["body"] = fmt.Sprintf("%v\n\n", string(beautifiedJSON))
			// Store parsed JSON for module processing
			_ = json.Unmarshal(bodyData, &parsedData)
		} else {
			logFields["type"] = "unknown"
			bodyBuf := make([]byte, len(bodyData)*2)
			hex.Encode(bodyBuf, bodyData)
			logFields["body"] = bodyBuf
		}
	}

	// Wait for our turn in the processing sequence
	if !s.orderedProcessor.WaitForSequence(seq) {
		return // Context was cancelled
	}

	// Process modules in order
	s.processResponseModules(ctx, req, rsp, bodyData, parsedData, contentType)

	s.logger.WithFields(logFields).Infof("RESPONSE #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())
}

func (s *Snooper) logEventResponse(ctx *ProxyCallContext, req *http.Request, rsp *http.Response, body []byte) {
	// Generate sequence number for this event processing
	seq := s.orderedProcessor.GetNextSequence()

	defer s.orderedProcessor.CompleteSequence(seq)

	// Parse event body
	logFields := logrus.Fields{
		"color": color.FgGreen,
	}

	evt := map[string]any{}

	for _, line := range strings.Split(string(body), "\n") {
		line = strings.Trim(line, "\r\n ")
		if line == "" {
			continue
		}

		sep := strings.Index(line, ":")
		if sep <= 0 {
			continue
		}

		switch line[0:sep] {
		case "data":
			data := map[string]any{}

			err := json.Unmarshal([]byte(line[sep+1:]), &data)
			if err != nil {
				s.logger.Warnf("failed parsing event data: %v", err)
			} else {
				evt[line[0:sep]] = data
			}
		default:
			evt[line[0:sep]] = line[sep+1:]
		}
	}

	logFields["body"] = body

	var parsedEventData interface{}

	if len(evt) >= 2 {
		bodyJSON, err := json.Marshal(evt)
		if err != nil {
			s.logger.Warnf("failed parsing event data: %v", err)
		} else {
			logFields["body"] = fmt.Sprintf("%v\n\n", string(s.beautifyJSON(bodyJSON)))
			parsedEventData = evt
		}
	}

	// Wait for our turn in the processing sequence
	if !s.orderedProcessor.WaitForSequence(seq) {
		return // Context was cancelled
	}

	// Process modules in order
	s.processEventModules(ctx, req, rsp, body, parsedEventData)

	s.logger.WithFields(logFields).Infof("RESPONSE-EVENT %v %v (status: %v, body: %v)", req.Method, req.URL.EscapedPath(), rsp.StatusCode, len(body))
}

// processRequestModules processes request data through modules using already parsed/decoded data
func (s *Snooper) processRequestModules(ctx *ProxyCallContext, req *http.Request, bodyData []byte, parsedData interface{}, contentType string) {
	if s.moduleManager == nil || !s.moduleManager.IsEnabled() {
		return
	}

	// Create request context for modules with the parsed data
	// Use parsed JSON data if available, otherwise use raw byte data
	var bodyForModules interface{}
	if parsedData != nil {
		bodyForModules = parsedData
	} else {
		bodyForModules = bodyData
	}

	reqCtx := &types.RequestContext{
		CallCtx:     ctx,
		Method:      req.Method,
		URL:         req.URL,
		Headers:     req.Header,
		Body:        bodyForModules,
		BodyBytes:   bodyData,
		ContentType: contentType,
		Timestamp:   time.Now(),
	}

	// Process through modules (non-modifying, observation only)
	_, err := s.moduleManager.ProcessRequest(reqCtx)
	if err != nil {
		s.logger.WithError(err).Warn("Module processing failed for request")
	}
}

// processResponseModules processes response data through modules using already parsed/decoded data
func (s *Snooper) processResponseModules(ctx *ProxyCallContext, req *http.Request, rsp *http.Response, bodyData []byte, parsedData interface{}, contentType string) {
	if s.moduleManager == nil || !s.moduleManager.IsEnabled() {
		return
	}

	// Create response context for modules with the parsed data
	// Use parsed JSON data if available, otherwise use raw byte data
	var bodyForModules interface{}
	if parsedData != nil {
		bodyForModules = parsedData
	} else {
		bodyForModules = bodyData
	}

	respCtx := &types.ResponseContext{
		CallCtx:     ctx,
		StatusCode:  rsp.StatusCode,
		Headers:     rsp.Header,
		Body:        bodyForModules,
		BodyBytes:   bodyData,
		ContentType: contentType,
		Timestamp:   time.Now(),
		Duration:    ctx.CallDuration(),
	}

	// Process through modules (non-modifying, observation only)
	_, err := s.moduleManager.ProcessResponse(respCtx)
	if err != nil {
		s.logger.WithError(err).Warn("Module processing failed for response")
	}

	// Collect metrics if enabled
	if s.metricsEnabled {
		s.collectMetrics(req, respCtx)
	}
}

// processEventModules processes event stream data through modules using already parsed event data
func (s *Snooper) processEventModules(ctx *ProxyCallContext, _ *http.Request, rsp *http.Response, bodyData []byte, parsedData interface{}) {
	if s.moduleManager == nil || !s.moduleManager.IsEnabled() {
		return
	}

	// Use parsed event data if available, otherwise use raw byte data
	var bodyForModules interface{}
	if parsedData != nil {
		bodyForModules = parsedData
	} else {
		bodyForModules = bodyData
	}

	// Create response context for event modules
	respCtx := &types.ResponseContext{
		CallCtx:     ctx,
		StatusCode:  rsp.StatusCode,
		Headers:     rsp.Header,
		Body:        bodyForModules,
		ContentType: "text/event-stream",
		Timestamp:   time.Now(),
	}

	// Process through modules (non-modifying, observation only)
	_, err := s.moduleManager.ProcessResponse(respCtx)
	if err != nil {
		s.logger.WithError(err).Warn("Module processing failed for event stream")
	}
}
