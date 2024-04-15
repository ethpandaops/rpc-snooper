package snooper

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/sirupsen/logrus"
)

func (s *Snooper) beautifyJson(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	obj := map[string]any{}
	err := json.Unmarshal(data, &obj)
	if err != nil {
		s.logger.Warnf("failed unmarshaling data: %v", err)
		return data
	}

	res, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		s.logger.Warnf("failed marshaling data: %v", err)
		return data
	}

	return res
}

func (s *Snooper) logRequest(ctx *proxyCallContext, req *http.Request, body []byte) {
	s.logMutex.Lock()
	defer s.logMutex.Unlock()

	contentType := req.Header.Get("Content-Type")
	contentLen := len(body)

	logFields := logrus.Fields{
		"length": contentLen,
	}

	if strings.Contains(contentType, "application/octet-stream") {
		hexBody := make([]byte, len(body)*2)
		hex.Encode(hexBody, body)
		body = hexBody
		logFields["type"] = "ssz"
	} else {
		body = s.beautifyJson(body)
		logFields["type"] = "json"
	}

	s.logger.WithFields(logFields).Infof("REQUEST #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())
	if contentLen > 0 {
		fmt.Printf("%v\n\n", string(body))
	}
}

func (s *Snooper) logResponse(ctx *proxyCallContext, req *http.Request, rsp *http.Response, body []byte) {
	s.logMutex.Lock()
	defer s.logMutex.Unlock()

	contentType := rsp.Header.Get("Content-Type")
	contentLen := len(body)

	logFields := logrus.Fields{
		"status": rsp.StatusCode,
		"length": contentLen,
	}

	if strings.Contains(contentType, "application/octet-stream") {
		hexBody := make([]byte, len(body)*2)
		hex.Encode(hexBody, body)
		body = hexBody
		logFields["type"] = "ssz"
	} else {
		body = s.beautifyJson(body)
		logFields["type"] = "json"
	}

	s.logger.WithFields(logFields).Infof("RESPONSE #%v: %v %v", ctx.callIndex, req.Method, req.URL.String())
	fmt.Printf("%v\n\n", string(body))
}

func (s *Snooper) logEventResponse(ctx *proxyCallContext, req *http.Request, rsp *http.Response, body []byte) {
	s.logMutex.Lock()
	defer s.logMutex.Unlock()

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

	s.logger.Infof("RESPONSE-EVENT %v %v (status: %v, body: %v)", req.Method, req.URL.EscapedPath(), rsp.StatusCode, len(body))

	if len(evt) >= 2 {
		bodyJson, err := json.Marshal(evt)
		if err != nil {
			s.logger.Warnf("failed parsing event data: %v", err)
		} else {
			body = s.beautifyJson(bodyJson)
		}
	}

	fmt.Printf("%v\n\n", string(body))
}
