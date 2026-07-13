package main

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
)

// slogAdapter satisfies shost.Logger on top of log/slog. In a real
// service use srog, which satisfies the interface directly.
type slogAdapter struct {
	l *slog.Logger
}

func newSlogAdapter() *slogAdapter {
	return &slogAdapter{l: slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))}
}

func (s *slogAdapter) Debug(template string, args ...any)       { s.l.Debug(render(template, args)) }
func (s *slogAdapter) Information(template string, args ...any) { s.l.Info(render(template, args)) }
func (s *slogAdapter) Warning(template string, args ...any)     { s.l.Warn(render(template, args)) }
func (s *slogAdapter) Error(err error, template string, args ...any) {
	s.l.Error(render(template, args), "error", err)
}

var placeholder = regexp.MustCompile(`\{[^{}]*\}`)

// render substitutes srog-style {Name} placeholders positionally.
func render(template string, args []any) string {
	i := 0
	return placeholder.ReplaceAllStringFunc(template, func(m string) string {
		if i >= len(args) {
			return m
		}
		v := fmt.Sprint(args[i])
		i++
		return v
	})
}
