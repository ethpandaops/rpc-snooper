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

// beautifyJSONForLog beautifies JSON for log output, optionally
// truncating large hex values. Module processing and proxy behavior
// are completely unaffected — only console log display is changed.
func (s *Snooper) beautifyJSONForLog(body []byte) []byte {
	if !s.logTruncationEnabled {
		return s.beautifyJSON(body)
	}

	var obj any

	err := json.Unmarshal(body, &obj)
	if err != nil {
		// Unmarshal failed — beautifyJSON will also fail on the same
		// input, so log the warning directly and return nil.
		s.logger.Warnf("failed unmarshaling data: %v", err)

		return nil
	}

	obj = truncateHexInTree(obj)

	res, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		// MarshalIndent failed after a successful unmarshal. Fall back
		// to plain beautification (re-parse is cheap for valid JSON).
		return s.beautifyJSON(body)
	}

	return res
}

// formatHexBodyForLog formats a hex-encoded body (e.g. SSZ) for log
// output, optionally truncating large values when truncation is enabled.
// When truncation applies, only the first and last preview bytes are
// hex-encoded, avoiding a full 2× allocation for large payloads.
func (s *Snooper) formatHexBodyForLog(bodyData []byte) string {
	if s.logTruncationEnabled && len(bodyData) > hexTruncateThreshold/2 {
		// Only encode the preview bytes instead of the entire body.
		prefix := hex.EncodeToString(bodyData[:hexTruncatePreviewLen/2])
		suffix := hex.EncodeToString(bodyData[len(bodyData)-hexTruncatePreviewLen/2:])

		return fmt.Sprintf("0x%s...%s <%d bytes>", prefix, suffix, len(bodyData))
	}

	str := fmt.Sprintf("0x%s", hex.EncodeToString(bodyData))
	if s.logTruncationEnabled {
		str = truncateHexValue(str)
	}

	return str
}

type logReadCloser struct {
	reader   io.Reader
	buf      *bytes.Buffer
	original io.ReadCloser
	logFn    func(data []byte)
	closed   bool
	logger   logrus.FieldLogger
}

func (s *Snooper) createTeeLogStream(stream io.ReadCloser, logfn func(data []byte)) io.ReadCloser {
	return s.createTeeLogStreamWithSizeHint(stream, 0, logfn)
}

// createTeeLogStreamWithSizeHint creates a tee log stream with an optional size hint for buffer pre-allocation.
// When sizeHint > 0, the buffer is pre-allocated to avoid reallocations during streaming.
// This provides ~2x throughput improvement for large payloads.
func (s *Snooper) createTeeLogStreamWithSizeHint(stream io.ReadCloser, sizeHint int64, logfn func(data []byte)) io.ReadCloser {
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
		logFn:    logfn,
		logger:   s.logger,
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

	// Capture buffer data and callbacks before spawning goroutine.
	// Extracting logFn/logger avoids capturing r in the closure,
	// allowing GC to free the buffer, original stream, and tee reader
	// while the logging goroutine runs.
	data := r.buf.Bytes()
	logFn := r.logFn
	logger := r.logger

	go func() {
		defer func() {
			if panicErr := recover(); panicErr != nil {
				if err2, ok := panicErr.(error); ok {
					logger.WithError(err2).Errorf("uncaught panic in log reader: %v, stack: %v", panicErr, string(debug.Stack()))
				} else {
					logger.Errorf("uncaught panic in log reader: %v, stack: %v", panicErr, string(debug.Stack()))
				}
			}
		}()

		logFn(data)
	}()

	return err
}

func (s *Snooper) logRequest(ctx *ProxyCallContext, req *http.Request, bodyBytes []byte) {
	seq := s.orderedProcessor.GetNextSequence()
	defer s.orderedProcessor.CompleteSequence(seq)

	contentEncoding := req.Header.Get("Content-Encoding")

	bodyData, err := s.decompressBody(bodyBytes, contentEncoding)
	if err != nil {
		return
	}

	// Wait — only holding decompressed bodyData during the wait
	if !s.orderedProcessor.WaitForSequence(seq) {
		return
	}

	// All heavy allocations (beautifyJSON, Unmarshal, hex encoding) happen after the wait
	contentType := req.Header.Get("Content-Type")

	logFields := logrus.Fields{
		"color":  color.FgCyan,
		"length": req.ContentLength,
	}

	var parsedData any

	switch {
	case req.ContentLength == 0:
		bodyData = []byte{}
	case strings.Contains(contentType, "application/octet-stream"):
		if !s.hideBodies {
			logFields["type"] = "ssz"
			logFields["body"] = s.formatHexBodyForLog(bodyData)
		}

		hexEncoded := make([]byte, len(bodyData)*2)
		hex.Encode(hexEncoded, bodyData)
		bodyData = hexEncoded
	default:
		_ = json.Unmarshal(bodyData, &parsedData)

		if !s.hideBodies {
			if beautifiedJSON := s.beautifyJSONForLog(bodyData); len(beautifiedJSON) > 0 {
				logFields["type"] = "json"
				logFields["body"] = string(beautifiedJSON)
			} else {
				logFields["type"] = "unknown"
				logFields["body"] = s.formatHexBodyForLog(bodyData)
			}
		}
	}

	ctx.SetData(0, "request_size", len(bodyData))

	if s.metricsEnabled && parsedData != nil {
		if jrpcMethod, ok := parsedData.(map[string]interface{}); ok {
			ctx.SetData(0, "jrpc_method", jrpcMethod["method"])
		}
	}

	s.processRequestModules(ctx, req, bodyData, parsedData, contentType)
	s.logger.WithFields(logFields).Infof("REQUEST #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())
}

func (s *Snooper) decompressBody(data []byte, contentEncoding string) ([]byte, error) {
	switch contentEncoding {
	case "gzip":
		gzipReader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			s.logger.Warnf("failed unpacking gzip body: %v", err)
			return nil, err
		}
		defer gzipReader.Close()

		decompressed, _ := io.ReadAll(gzipReader)

		return decompressed, nil
	case "br":
		decompressed, _ := io.ReadAll(brotli.NewReader(bytes.NewReader(data)))

		return decompressed, nil
	default:
		return data, nil
	}
}

func (s *Snooper) logResponse(ctx *ProxyCallContext, req *http.Request, rsp *http.Response, bodyBytes []byte) {
	seq := s.orderedProcessor.GetNextSequence()
	defer s.orderedProcessor.CompleteSequence(seq)

	contentEncoding := rsp.Header.Get("Content-Encoding")

	bodyData, err := s.decompressBody(bodyBytes, contentEncoding)
	if err != nil {
		return
	}

	// Wait — only holding decompressed bodyData during the wait
	if !s.orderedProcessor.WaitForSequence(seq) {
		return
	}

	// All heavy allocations happen after the wait
	contentType := rsp.Header.Get("Content-Type")

	logFields := logrus.Fields{
		"status": rsp.StatusCode,
		"length": rsp.ContentLength,
	}

	if rsp.StatusCode >= 200 && rsp.StatusCode <= 299 {
		logFields["color"] = color.FgGreen
	} else {
		logFields["color"] = color.FgRed
	}

	var parsedData any

	switch {
	case rsp.ContentLength == 0:
		bodyData = []byte{}
	case strings.Contains(contentType, "application/octet-stream"):
		if !s.hideBodies {
			logFields["type"] = "ssz"
			logFields["body"] = s.formatHexBodyForLog(bodyData)
		}

		hexEncoded := make([]byte, len(bodyData)*2)
		hex.Encode(hexEncoded, bodyData)
		bodyData = hexEncoded
	default:
		_ = json.Unmarshal(bodyData, &parsedData)

		if !s.hideBodies {
			if beautifiedJSON := s.beautifyJSONForLog(bodyData); len(beautifiedJSON) > 0 {
				logFields["type"] = "json"
				logFields["body"] = string(beautifiedJSON)
			} else {
				logFields["type"] = "unknown"
				logFields["body"] = s.formatHexBodyForLog(bodyData)
			}
		}
	}

	s.processResponseModules(ctx, req, rsp, bodyData, parsedData, contentType)
	s.logger.WithFields(logFields).Infof("RESPONSE #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())
}

func (s *Snooper) logEventResponse(ctx *ProxyCallContext, req *http.Request, rsp *http.Response, body []byte) {
	// Generate sequence number for this event processing
	seq := s.orderedProcessor.GetNextSequence()

	defer s.orderedProcessor.CompleteSequence(seq)

	// Wait — only holding raw body bytes during the wait
	if !s.orderedProcessor.WaitForSequence(seq) {
		return // Context was cancelled
	}

	// All heavy allocations (parsing, beautify) happen after the wait
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

	var parsedEventData interface{}

	if len(evt) >= 2 {
		bodyJSON, err := json.Marshal(evt)
		if err != nil {
			s.logger.Warnf("failed parsing event data: %v", err)
		} else {
			if !s.hideBodies {
				logFields["body"] = string(s.beautifyJSONForLog(bodyJSON))
			}

			parsedEventData = evt
		}
	} else if !s.hideBodies {
		logFields["body"] = body
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
