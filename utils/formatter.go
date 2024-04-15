package utils

import (
	"github.com/sirupsen/logrus"
)

type SnooperFormatter struct {
	f logrus.TextFormatter
}

func (f *SnooperFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	data := (map[string]interface{})(entry.Data)
	body, isBytes := data["body"].([]byte)
	if isBytes {
		delete(entry.Data, "body")
	} else {
		bodyStr, isString := data["body"].(string)
		if isString {
			isBytes = true
			delete(entry.Data, "body")
			body = []byte(bodyStr)
		}
	}

	lineBuf, err := f.f.Format(entry)
	if err != nil {
		return nil, err
	}

	if isBytes {
		lineBuf = append(lineBuf, body...)
	}
	return lineBuf, nil
}
