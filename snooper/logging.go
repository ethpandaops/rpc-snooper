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

	"github.com/ethpandaops/rpc-snooper/utils"
	"github.com/fatih/color"
	"github.com/sirupsen/logrus"
)

func (s *Snooper) beautifyJson(body []byte) []byte {
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
				s.logger.WithError(err.(error)).Errorf("uncaught panic in log reader: %v, stack: %v", err, string(debug.Stack()))
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

func (r *logReadCloser) Read(p []byte) (n int, err error) { return r.reader.Read(p) }
func (r *logReadCloser) Close() error {
	fmt.Printf("logReadCloser close")
	var resErr error
	for _, closer := range r.closers {
		err := closer.Close()
		if err != nil && resErr == nil {
			resErr = err
		}
	}
	return resErr
}

func (s *Snooper) logRequest(ctx *proxyCallContext, req *http.Request, body io.ReadCloser) {
	contentEncoding := req.Header.Get("Content-Encoding")
	contentType := req.Header.Get("Content-Type")

	if contentEncoding == "gzip" {
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			s.logger.Warnf("failed unpacking gzip request body: %v", err)
			return
		}
		defer gzipReader.Close()

		body = gzipReader
	}

	logFields := logrus.Fields{
		"color":  color.FgCyan,
		"length": req.ContentLength,
	}

	if req.ContentLength == 0 {
		logFields["body"] = []byte{}
	} else if strings.Contains(contentType, "application/octet-stream") {
		body = utils.NewHexEncoder(body)
		bodyData, _ := io.ReadAll(body)
		logFields["type"] = "ssz"
		logFields["body"] = fmt.Sprintf("%v\n\n", string(bodyData))
	} else {
		bodyData, _ := io.ReadAll(body)

		if bodyData = s.beautifyJson(bodyData); len(bodyData) > 0 {
			logFields["type"] = "json"
			logFields["body"] = fmt.Sprintf("%v\n\n", string(bodyData))
		} else {
			logFields["type"] = "unknown"
			bodyBuf := make([]byte, len(bodyData)*2)
			hex.Encode(bodyBuf, bodyData)
			logFields["body"] = bodyBuf
		}
	}

	s.logger.WithFields(logFields).Infof("REQUEST #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())
}

func (s *Snooper) logResponse(ctx *proxyCallContext, req *http.Request, rsp *http.Response, body io.ReadCloser) {
	contentEncoding := rsp.Header.Get("Content-Encoding")
	contentType := rsp.Header.Get("Content-Type")

	if contentEncoding == "gzip" {
		gzipReader, err := gzip.NewReader(body)
		if err != nil {
			s.logger.Warnf("failed unpacking gzip response body: %v", err)
			return
		}
		defer gzipReader.Close()

		body = gzipReader
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

	if rsp.ContentLength == 0 {
		logFields["body"] = []byte{}
	} else if strings.Contains(contentType, "application/octet-stream") {
		body = utils.NewHexEncoder(body)
		bodyData, _ := io.ReadAll(body)
		logFields["type"] = "ssz"
		logFields["body"] = fmt.Sprintf("%v\n\n", string(bodyData))
	} else {
		bodyData, _ := io.ReadAll(body)
		if bodyData = s.beautifyJson(bodyData); len(bodyData) > 0 {
			logFields["type"] = "json"
			logFields["body"] = fmt.Sprintf("%v\n\n", string(bodyData))
		} else {
			logFields["type"] = "unknown"
			bodyBuf := make([]byte, len(bodyData)*2)
			hex.Encode(bodyBuf, bodyData)
			logFields["body"] = bodyBuf
		}
	}

	s.logger.WithFields(logFields).Infof("RESPONSE #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())
}

func (s *Snooper) logEventResponse(ctx *proxyCallContext, req *http.Request, rsp *http.Response, body []byte) {
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
	if len(evt) >= 2 {
		bodyJson, err := json.Marshal(evt)
		if err != nil {
			s.logger.Warnf("failed parsing event data: %v", err)
		} else {
			logFields["body"] = fmt.Sprintf("%v\n\n", string(s.beautifyJson(bodyJson)))
		}
	}

	s.logger.WithFields(logFields).Infof("RESPONSE-EVENT %v %v (status: %v, body: %v)", req.Method, req.URL.EscapedPath(), rsp.StatusCode, len(body))
}
