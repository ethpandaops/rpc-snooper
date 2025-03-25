package snooper

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type proxyCallContext struct {
	callIndex    uint64
	context      context.Context
	cancelFn     context.CancelFunc
	cancelled    bool
	deadline     time.Time
	updateChan   chan time.Duration
	streamReader io.ReadCloser
}

func (s *Snooper) newProxyCallContext(parent context.Context, timeout time.Duration) *proxyCallContext {
	s.callIndexMutex.Lock()
	s.callIndexCounter++
	callIndex := s.callIndexCounter
	s.callIndexMutex.Unlock()

	callCtx := &proxyCallContext{
		callIndex:  callIndex,
		deadline:   time.Now().Add(timeout),
		updateChan: make(chan time.Duration, 5),
	}
	callCtx.context, callCtx.cancelFn = context.WithCancel(parent)

	go callCtx.processCallContext()

	return callCtx
}

func (callContext *proxyCallContext) processCallContext() {
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

func (s *Snooper) processProxyCall(w http.ResponseWriter, r *http.Request) error {
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

	// stream body
	bodyReader := s.createTeeLogStream(r.Body, func(reader io.ReadCloser) {
		s.logRequest(callContext, r, reader)
	})
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

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("proxy request error: %w", err)
	}

	if callContext.cancelled {
		resp.Body.Close()
		return fmt.Errorf("proxy context cancelled")
	}

	callContext.streamReader = resp.Body

	respContentType := resp.Header.Get("Content-Type")
	isEventStream := respContentType == "text/event-stream" || strings.HasPrefix(r.URL.EscapedPath(), "/eth/v1/events")

	// passthru response headers
	respH := w.Header()

	for hk, hvs := range resp.Header {
		for _, hv := range hvs {
			respH.Add(hk, hv)
		}
	}

	if isEventStream {
		respH.Set("X-Accel-Buffering", "no")
	}

	w.WriteHeader(resp.StatusCode)

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
		// stream response body
		bodyReader := s.createTeeLogStream(resp.Body, func(reader io.ReadCloser) {
			s.logResponse(callContext, r, resp, reader)
		})
		defer bodyReader.Close()

		_, err = io.Copy(w, bodyReader)
		if err != nil {
			return fmt.Errorf("proxy response stream error: %w", err)
		}
	}

	return nil
}

func (s *Snooper) processEventStreamResponse(callContext *proxyCallContext, r *http.Request, w http.ResponseWriter, rsp *http.Response) (int64, error) {
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
			s.logEventResponse(r, rsp, lineBuf)
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
