package platform

import "log/slog"

// Logger abstracts structured logging so services and repositories can be tested
// with a no-op or capture implementation.
type Logger interface {
	Info(msg string, args ...any)
	// Warn reports something that is working but should not be relied on — a
	// development fallback standing in for configuration, for instance.
	Warn(msg string, args ...any)
	Error(msg string, args ...any)
}

// SlogLogger adapts slog.Logger to the Logger interface.
type SlogLogger struct {
	inner *slog.Logger
}

// NewSlogLogger returns a Logger backed by the given slog.Logger.
func NewSlogLogger(l *slog.Logger) Logger {
	return &SlogLogger{inner: l}
}

func (l *SlogLogger) Info(msg string, args ...any) {
	l.inner.Info(msg, args...)
}

func (l *SlogLogger) Warn(msg string, args ...any) {
	l.inner.Warn(msg, args...)
}

func (l *SlogLogger) Error(msg string, args ...any) {
	l.inner.Error(msg, args...)
}
