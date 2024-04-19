package utils

import (
	"github.com/fatih/color"
	"github.com/sirupsen/logrus"
)

type SnooperFormatter struct {
	Formatter logrus.TextFormatter
}

func (f *SnooperFormatter) DisableColors() {
	color.NoColor = true
	f.Formatter.DisableColors = true
}

func (f *SnooperFormatter) EnableColors() {
	color.NoColor = false
	f.Formatter.DisableColors = false
	f.Formatter.ForceColors = true
}

func (f *SnooperFormatter) Format(entry *logrus.Entry) ([]byte, error) {
	data := (map[string]interface{})(entry.Data)

	var colorPrint *color.Color
	if fgColor, isColor := data["color"].(color.Attribute); isColor {
		colorPrint = color.New(fgColor)
		delete(entry.Data, "color")
	} else {
		colorPrint = color.New()
	}

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

	lineBuf, err := f.Formatter.Format(entry)
	if err != nil {
		return nil, err
	}

	if isBytes {
		coloredBody := colorPrint.Sprint(string(body))

		lineBuf = append(lineBuf, coloredBody...)
	}
	return lineBuf, nil
}
