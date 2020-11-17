package logger

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/containerssh/log"

	"github.com/acarl005/stripansi"
	"github.com/fatih/color"
)

type LogFormatter struct {
}

func (l LogFormatter) Format(level log.Level, message string) []byte {
	var buf bytes.Buffer

	color.NoColor = false
	levelString, _ := level.String()

	line := fmt.Sprintf("%s\t%v\n", levelString, stripansi.Strip(message))
	switch level {
	case log.LevelDebug:
		_, _ = color.New(color.FgWhite).Fprint(&buf, line)
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

func (l LogFormatter) FormatData(level log.Level, data interface{}) []byte {
	return l.Format(level, fmt.Sprintf("%v", data))
}

type LogWriter struct {
	Prefix string
	Logger log.Logger
	Buf    bytes.Buffer
}

func (l LogWriter) Write(p []byte) (n int, err error) {
	prefix := l.Prefix
	if prefix == "" {
		prefix = "container:"
	}
	for _, b := range p {
		if bytes.Equal([]byte{b}, []byte("\n")) {
			l.Logger.Debug(fmt.Sprintf("%s\t%s", prefix, strings.TrimSpace(l.Buf.String())))
			l.Buf.Reset()
		} else {
			l.Buf.Write([]byte{b})
		}
	}
	if l.Buf.Len() > 0 {
		l.Logger.Debug(fmt.Sprintf("%s\t%s", prefix, strings.TrimSpace(l.Buf.String())))
	}
	return len(p), nil
}
