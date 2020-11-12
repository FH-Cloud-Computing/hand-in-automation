package main

import (
	"fmt"
	"html"

	"github.com/acarl005/stripansi"
	"github.com/containerssh/log"
)

type HTMLLogFormatter struct {
}

func (l HTMLLogFormatter) Format(level log.Level, message string) []byte {
	levelString, _ := level.String()
	line := fmt.Sprintf(
		"<tr class=\"%s\"><td class=\"loglevel\">%s</td><td class=\"logentry\">%v</td></tr>\n",
		levelString,
		levelString,
		html.EscapeString(stripansi.Strip(message)),
	)
	return []byte(line)
}

func (l HTMLLogFormatter) FormatData(level log.Level, data interface{}) []byte {
	return l.Format(level, fmt.Sprintf("%v", data))
}
