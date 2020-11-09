package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/containerssh/log"

	"github.com/acarl005/stripansi"
	"github.com/fatih/color"
)

type logFormatter struct {
}

func (l logFormatter) Format(level log.Level, message string) []byte {
	var buf bytes.Buffer

	color.NoColor = false
	levelString, _ := level.String()

	line := fmt.Sprintf("%s\t%v\n", levelString, stripansi.Strip(message))
	switch level {
	case log.LevelDebug:
		_, _ = color.New(color.FgWhite).Fprint(color.Output, line)
	case log.LevelInfo:
		_, _ = color.New(color.FgCyan).Fprint(&buf, line)
	case log.LevelNotice:
		_, _ = color.New(color.FgGreen).Fprint(&buf, line)
	case log.LevelWarning:
		_, _ = color.New(color.FgYellow).Fprint(&buf, line)
	default:
		_, _ = color.New(color.FgRed).Fprint(&buf, line)
	}
	return buf.Bytes()
}

func (l logFormatter) FormatData(level log.Level, data interface{}) []byte {
	return l.Format(level, fmt.Sprintf("%v", data))
}

type logWriter struct {
	logger log.Logger
	buf    bytes.Buffer
}

func (l logWriter) Write(p []byte) (n int, err error) {
	for _, b := range p {
		if bytes.Equal([]byte{b}, []byte("\n")) {
			l.logger.Debug(strings.TrimSpace(l.buf.String()))
			l.buf.Reset()
		} else {
			l.buf.Write([]byte{b})
		}
	}
	if l.buf.Len() > 0 {
		l.logger.Debug(strings.TrimSpace(l.buf.String()))
	}
	return len(p), nil
}
