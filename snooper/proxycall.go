package snooper

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ProxyCallContext struct {
	callIndex    uint64
	context      context.Context
	cancelFn     context.CancelFunc
	cancelled    bool
	deadline     time.Time
	updateChan   chan time.Duration
	reqSentChan  chan struct{}
	streamReader io.ReadCloser
	data         map[string]interface{}
}

func (s *Snooper) newProxyCallContext(parent context.Context, timeout time.Duration) *ProxyCallContext {
	s.callIndexMutex.Lock()
	s.callIndexCounter++
	callIndex := s.callIndexCounter
	s.callIndexMutex.Unlock()

	callCtx := &ProxyCallContext{
		callIndex:   callIndex,
		deadline:    time.Now().Add(timeout),
		updateChan:  make(chan time.Duration, 5),
		reqSentChan: make(chan struct{}),
		data:        make(map[string]interface{}),
	}
	callCtx.context, callCtx.cancelFn = context.WithCancel(parent)

	go callCtx.processCallContext()

	return callCtx
}

func (callContext *ProxyCallContext) processCallContext() {
ctxLoop:
	for {
		timeout := time.Until(callContext.deadline)
		select {
		case newTimeout := <-callContext.updateChan:
			callContext.deadline = time.Now().Add(newTimeout)
		case <-callContext.context.Done():
			break ctxLoop
		case <-time.After(timeout):
			callContext.cancelFn()
			callContext.cancelled = true
			time.Sleep(10 * time.Millisecond)
		}
	}

	callContext.cancelled = true

	if callContext.streamReader != nil {
		callContext.streamReader.Close()
	}
}

func (callContext *ProxyCallContext) Context() context.Context {
	return callContext.context
}

func (callContext *ProxyCallContext) ID() uint64 {
	return callContext.callIndex
}

func (callContext *ProxyCallContext) SetData(moduleID uint64, key string, value interface{}) {
	callContext.data[fmt.Sprintf("%d:%s", moduleID, key)] = value
}

func (callContext *ProxyCallContext) GetData(moduleID uint64, key string) interface{} {
	return callContext.data[fmt.Sprintf("%d:%s", moduleID, key)]
}

func (s *Snooper) processProxyCall(w http.ResponseWriter, r *http.Request) error {
	// Check if flow is enabled
	s.flowMutex.RLock()
	flowEnabled := s.flowEnabled
	s.flowMutex.RUnlock()

	if !flowEnabled {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)

		response := map[string]interface{}{
			"status":  "error",
			"message": "Proxy flow is currently disabled",
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			s.logger.Errorf("failed writing flow disabled response: %v", err)
		}
		return nil
	}

	callContext := s.newProxyCallContext(r.Context(), s.CallTimeout)
	defer callContext.cancelFn()

	// pass all headers
	hh := http.Header{}

	for hk, hvs := range r.Header {
		for _, hv := range hvs {
			hh.Add(hk, hv)
		}
	}

	proxyIPChain := []string{}

	if forwaredFor := r.Header.Get("X-Forwarded-For"); forwaredFor != "" {
		proxyIPChain = strings.Split(forwaredFor, ", ")
	}

	proxyIPChain = append(proxyIPChain, r.RemoteAddr)
	hh.Set("X-Forwarded-For", strings.Join(proxyIPChain, ", "))

	// build proxy url
	queryArgs := ""
	if r.URL.RawQuery != "" {
		queryArgs = fmt.Sprintf("?%s", r.URL.RawQuery)
	}

	proxyURL, err := url.Parse(fmt.Sprintf("%s%s%s", s.target, r.URL.EscapedPath(), queryArgs))
	if err != nil {
		return fmt.Errorf("error parsing proxy url: %w", err)
	}

	// Create body reader with module processing and logging
	bodyReader := s.createRequestProcessingStream(callContext, r, r.Body)
	defer bodyReader.Close()

	// construct request to send to origin server
	req := &http.Request{
		Method:        r.Method,
		URL:           proxyURL,
		Header:        hh,
		Body:          bodyReader,
		ContentLength: r.ContentLength,
		Close:         r.Close,
	}
	client := &http.Client{Timeout: 0}
	req = req.WithContext(callContext.context)

	callStart := time.Now()
	resp, err := client.Do(req)

	if err != nil {
		return fmt.Errorf("proxy request error: %w", err)
	}

	callDuration := time.Since(callStart)

	if callContext.cancelled {
		resp.Body.Close()
		return fmt.Errorf("proxy context cancelled")
	}

	callContext.streamReader = resp.Body

	respContentType := resp.Header.Get("Content-Type")
	isEventStream := respContentType == "text/event-stream" || strings.HasPrefix(r.URL.EscapedPath(), "/eth/v1/events")

	// For event streams, we can't modify the response through modules (streaming requirement)
	if isEventStream {
		// passthru response headers
		respH := w.Header()

		for hk, hvs := range resp.Header {
			for _, hv := range hvs {
				respH.Add(hk, hv)
			}
		}

		respH.Set("X-Accel-Buffering", "no")
		w.WriteHeader(resp.StatusCode)
	}

	if isEventStream && resp.StatusCode == 200 {
		callContext.updateChan <- s.CallTimeout

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		_, err := s.processEventStreamResponse(callContext, r, w, resp)
		if err != nil {
			s.logger.WithField("callidx", callContext.callIndex).Warnf("event stream error: %v", err)
		}
	} else {
		// passthru response headers (modules may modify them during streaming)
		respH := w.Header()

		for hk, hvs := range resp.Header {
			for _, hv := range hvs {
				respH.Add(hk, hv)
			}
		}

		w.WriteHeader(resp.StatusCode)

		// Create response body reader with module processing and logging
		responseBodyReader := s.createResponseProcessingStream(callContext, r, resp, callDuration)
		defer responseBodyReader.Close()

		_, err = io.Copy(w, responseBodyReader)
		if err != nil {
			return fmt.Errorf("proxy response stream error: %w", err)
		}
	}

	return nil
}

func (s *Snooper) processEventStreamResponse(callContext *ProxyCallContext, r *http.Request, w http.ResponseWriter, rsp *http.Response) (int64, error) {
	rd := bufio.NewReader(rsp.Body)
	written := int64(0)

	for {
		lineBuf := []byte{}

		for {
			evt, err := rd.ReadSlice('\n')
			if err != nil {
				return written, err
			}

			wb, err := w.Write(evt)
			if err != nil {
				return written, err
			}

			written += int64(wb)

			if wb == 1 {
				break
			}

			lineBuf = append(lineBuf, evt...)
			lineBuf = append(lineBuf, '\n')
		}

		if len(lineBuf) > 2 {
			s.logEventResponse(callContext, r, rsp, lineBuf)
		}

		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}

		if callContext.cancelled {
			return written, nil
		}

		callContext.updateChan <- s.CallTimeout
	}
}

// createRequestProcessingStream creates a streaming reader that processes request data through logging
func (s *Snooper) createRequestProcessingStream(callCtx *ProxyCallContext, r *http.Request, stream io.ReadCloser) io.ReadCloser {
	// Create tee stream for logging (module processing now happens in log stream)
	loggedStream := s.createTeeLogStream(stream, func(reader io.ReadCloser) {
		s.logRequest(callCtx, r, reader)
		close(callCtx.reqSentChan)
	})

	return loggedStream
}

// createResponseProcessingStream creates a streaming reader for response processing
func (s *Snooper) createResponseProcessingStream(callCtx *ProxyCallContext, r *http.Request, resp *http.Response, callDuration time.Duration) io.ReadCloser {
	// Create tee stream for logging (module processing now happens in log stream)
	loggedStream := s.createTeeLogStream(resp.Body, func(reader io.ReadCloser) {
		<-callCtx.reqSentChan
		s.logResponse(callCtx, r, resp, reader, callDuration)
	})

	return loggedStream
}
