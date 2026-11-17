package platform

import (
	"context"
	"log/slog"
)

// Logger abstracts structured logging so services and repositories can be tested
// with a no-op or capture implementation.
type Logger interface {
	Info(msg string, args ...any)
	// Warn reports something that is working but should not be relied on — a
	// development fallback standing in for configuration, for instance.
	Warn(msg string, args ...any)
	// Error reports something that failed, and is reported to the Reporter as
	// well as logged (ADR 0079): promoting a line to Error, or demoting one,
	// is an alerting decision.
	//
	// It alone takes the context, because the report is placed by it: a call
	// inside a request carries that request's id, and the request's other
	// context as it gains more, and counts as the request's report, so a 5xx
	// that follows is not reported a second time. Outside a request, pass the
	// job's or the goroutine's own context.
	Error(ctx context.Context, msg string, args ...any)
}

// SlogLogger adapts slog.Logger to the Logger interface, reporting every Error
// line to its Reporter.
type SlogLogger struct {
	inner    *slog.Logger
	reporter Reporter
}

// NewSlogLogger returns a Logger backed by the given slog.Logger that reports
// its Error lines to reporter. Pass NoopReporter to report nothing; the
// AsyncReporter's own logger must, so a failing reporter never reports to
// itself.
func NewSlogLogger(l *slog.Logger, reporter Reporter) Logger {
	return &SlogLogger{inner: l, reporter: reporter}
}

func (l *SlogLogger) Info(msg string, args ...any) {
	l.inner.Info(msg, args...)
}

func (l *SlogLogger) Warn(msg string, args ...any) {
	l.inner.Warn(msg, args...)
}

// Error logs the line and reports it as one error: the message, wrapping the
// first error among args or, failing one, the value logged under "panic". No
// other argument is reported; they may name a person, and the log is where
// they belong (ADR 0079). The report's stack is the caller's or, logged while
// recovering a panic, the frame that panicked.
func (l *SlogLogger) Error(ctx context.Context, msg string, args ...any) {
	l.inner.ErrorContext(ctx, msg, args...)
	reportInRequest(ctx, l.reporter, ErrorReport{
		Err:       loggedError{msg: msg, cause: reportedCause(args)},
		Stack:     stackOrPanicStack(1),
		RequestID: RequestID(ctx),
	})
}

// loggedError is an Error line as a reported error.
type loggedError struct {
	msg   string
	cause error
}

func (e loggedError) Error() string {
	if e.cause == nil {
		return e.msg
	}
	return e.msg + ": " + e.cause.Error()
}

func (e loggedError) Unwrap() error { return e.cause }

// reportedCause is the first error value among a log line's arguments or,
// with none, a recovered panic's value logged under "panic"; nil otherwise.
func reportedCause(args []any) error {
	if err := firstError(args); err != nil {
		return err
	}
	for i := 0; i+1 < len(args); i += 2 {
		if args[i] == "panic" {
			return recoveredPanic{value: args[i+1]}
		}
	}
	return nil
}

// firstError is the first error value among a log line's arguments, nil when
// there is none.
func firstError(args []any) error {
	for _, arg := range args {
		switch v := arg.(type) {
		case error:
			return v
		case slog.Attr:
			if err, ok := v.Value.Any().(error); ok {
				return err
			}
		}
	}
	return nil
}
