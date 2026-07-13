package shost

// Logger is the minimal logging surface the host needs for lifecycle
// events. The method set is signature-compatible with srog, so an
// *srog.Logger can be passed to Builder.WithLogger directly; any other
// logging library can be adapted with a small wrapper.
//
// Templates use srog-style named placeholders: "service {Service} started".
type Logger interface {
	Debug(template string, args ...any)
	Information(template string, args ...any)
	Warning(template string, args ...any)
	Error(err error, template string, args ...any)
}

// nopLogger is the default when no logger is configured.
type nopLogger struct{}

func (nopLogger) Debug(string, ...any)        {}
func (nopLogger) Information(string, ...any)  {}
func (nopLogger) Warning(string, ...any)      {}
func (nopLogger) Error(error, string, ...any) {}
