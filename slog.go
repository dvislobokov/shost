package shost

import (
	"fmt"
	"log/slog"
	"strings"
)

// SlogLogger adapts a *slog.Logger to the shost Logger interface for
// applications not using srog. srog-style message templates are rendered
// into the log message, and each named placeholder additionally becomes a
// slog attribute:
//
//	// "service api started" with attribute Service=api
//	log.Information("service {Service} started", "api")
func SlogLogger(l *slog.Logger) Logger {
	if l == nil {
		panic("shost: SlogLogger called with nil logger")
	}
	return &slogLogger{l: l}
}

type slogLogger struct {
	l *slog.Logger
}

func (s *slogLogger) Debug(template string, args ...any) {
	msg, attrs := renderTemplate(template, args)
	s.l.Debug(msg, attrs...)
}

func (s *slogLogger) Information(template string, args ...any) {
	msg, attrs := renderTemplate(template, args)
	s.l.Info(msg, attrs...)
}

func (s *slogLogger) Warning(template string, args ...any) {
	msg, attrs := renderTemplate(template, args)
	s.l.Warn(msg, attrs...)
}

func (s *slogLogger) Error(err error, template string, args ...any) {
	msg, attrs := renderTemplate(template, args)
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	s.l.Error(msg, attrs...)
}

// renderTemplate substitutes srog-style {Name} placeholders with the
// positional args and returns the rendered message plus one slog attribute
// per matched placeholder. Args beyond the placeholder count are ignored;
// placeholders beyond the arg count are left verbatim.
func renderTemplate(template string, args []any) (string, []any) {
	if !strings.ContainsRune(template, '{') {
		return template, nil
	}
	var (
		msg   strings.Builder
		attrs []any
		rest  = template
		next  = 0
	)
	for {
		open := strings.IndexByte(rest, '{')
		if open < 0 {
			msg.WriteString(rest)
			break
		}
		clos := strings.IndexByte(rest[open:], '}')
		if clos < 0 {
			msg.WriteString(rest)
			break
		}
		name := rest[open+1 : open+clos]
		if name == "" || next >= len(args) {
			msg.WriteString(rest[:open+clos+1])
		} else {
			msg.WriteString(rest[:open])
			fmt.Fprintf(&msg, "%v", args[next])
			attrs = append(attrs, slog.Any(name, args[next]))
			next++
		}
		rest = rest[open+clos+1:]
	}
	return msg.String(), attrs
}
