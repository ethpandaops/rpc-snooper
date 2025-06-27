package snooper

import (
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
	reader  io.Reader
	closers []io.Closer
}

func (s *Snooper) createTeeLogStream(stream io.ReadCloser, logfn func(reader io.ReadCloser)) io.ReadCloser {
	logReader, logWriter := io.Pipe()
	resStream := io.TeeReader(stream, logWriter)

	go func() {
		defer func() {
			if err := recover(); err != nil {
				if err2, ok := err.(error); ok {
					s.logger.WithError(err2).Errorf("uncaught panic in log reader: %v, stack: %v", err, string(debug.Stack()))
				} else {
					s.logger.Errorf("uncaught panic in log reader: %v, stack: %v", err, string(debug.Stack()))
				}
			}
		}()
		defer logReader.Close()
		logfn(logReader)
	}()

	return &logReadCloser{
		reader:  resStream,
		closers: []io.Closer{stream, logReader, logWriter},
	}
}

func (r *logReadCloser) Read(p []byte) (n int, err error) {
	return r.reader.Read(p)
}

func (r *logReadCloser) Close() error {
	var resErr error

	for _, closer := range r.closers {
		err := closer.Close()
		if err != nil && resErr == nil {
			resErr = err
		}
	}

	return resErr
}

func (s *Snooper) logRequest(ctx *ProxyCallContext, req *http.Request, body io.ReadCloser) {
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

	s.logger.WithFields(logFields).Infof("REQUEST #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())

	ctx.SetData(0, "request_size", len(bodyData))

	// Extract and store jrpc_method for metrics collection if metrics are enabled
	if s.metricsEnabled && parsedData != nil {
		if jrpcMethod, ok := parsedData.(map[string]interface{}); ok {
			ctx.SetData(0, "jrpc_method", jrpcMethod["method"])
		}
	}

	// Process through modules using the already parsed/decoded data
	s.processRequestModules(ctx, req, bodyData, parsedData, contentType)
}

func (s *Snooper) logResponse(ctx *ProxyCallContext, req *http.Request, rsp *http.Response, body io.ReadCloser, callDuration time.Duration) {
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

	s.logger.WithFields(logFields).Infof("RESPONSE #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())

	// Process through modules using the already parsed/decoded data
	s.processResponseModules(ctx, req, rsp, bodyData, parsedData, contentType, callDuration)
}

func (s *Snooper) logEventResponse(ctx *ProxyCallContext, req *http.Request, rsp *http.Response, body []byte) {
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

	// Process through modules using the already parsed event data
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
func (s *Snooper) processResponseModules(ctx *ProxyCallContext, req *http.Request, rsp *http.Response, bodyData []byte, parsedData interface{}, contentType string, callDuration time.Duration) {
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
		Duration:    callDuration,
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
