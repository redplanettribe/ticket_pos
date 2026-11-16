package platform_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// panickingHandler is the frame a recovered panic's report must start at.
func panickingHandler(http.ResponseWriter, *http.Request) {
	panic("handler exploded")
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestARecoveredPanicIsReportedOnceWithItsStack(t *testing.T) {
	fake := reportertest.New()
	// Behind the AsyncReporter, as cmd/server wires the real one.
	reporter := platform.NewAsyncReporter(fake, platform.NewSlogLogger(discardLogger()))
	server := httptest.NewServer(platform.RequestPipeline(discardLogger(), reporter, http.HandlerFunc(panickingHandler)))
	defer server.Close()

	resp, err := http.Get(server.URL + "/orders")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if !reporter.Flush(time.Second) {
		t.Fatal("reports not flushed")
	}

	reports := fake.Errors()
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want exactly 1: %+v", len(reports), reports)
	}
	report := reports[0]
	if !strings.Contains(report.Err.Error(), "handler exploded") {
		t.Errorf("reported error = %q, want the panic value", report.Err)
	}
	if want := resp.Header.Get(platform.RequestIDHeader); report.RequestID != want || want == "" {
		t.Errorf("report request id = %q, want the response's %q", report.RequestID, want)
	}
	if len(report.Stack) == 0 {
		t.Fatal("report carries no stack")
	}
	if top := report.Stack[0].Function; !strings.HasSuffix(top, ".panickingHandler") {
		t.Errorf("stack starts at %q, want the frame that panicked, panickingHandler", top)
	}
}

// dereferencingHandler panics in the runtime, not with panic().
func dereferencingHandler(w http.ResponseWriter, _ *http.Request) {
	var missing *http.Request
	_, _ = w.Write([]byte(missing.Method))
}

func TestARuntimePanicsStackStartsAtTheFrameThatPanicked(t *testing.T) {
	fake := reportertest.New()
	server := httptest.NewServer(platform.RequestPipeline(discardLogger(), fake, http.HandlerFunc(dereferencingHandler)))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()

	reports := fake.Errors()
	if len(reports) != 1 || len(reports[0].Stack) == 0 {
		t.Fatalf("reports = %+v, want one with a stack", reports)
	}
	if top := reports[0].Stack[0].Function; !strings.HasSuffix(top, ".dereferencingHandler") {
		t.Errorf("stack starts at %q, want dereferencingHandler, not the runtime's frames", top)
	}
}

func TestAPanicValueThatIsAnErrorIsReportedAsThatError(t *testing.T) {
	cause := errors.New("nil map write")
	fake := reportertest.New()
	server := httptest.NewServer(platform.RequestPipeline(discardLogger(), fake, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(cause)
	})))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	_ = resp.Body.Close()

	reports := fake.Errors()
	if len(reports) != 1 || !errors.Is(reports[0].Err, cause) {
		t.Fatalf("reports = %+v, want one wrapping the panic's error", reports)
	}
}

func TestAnAbortedStreamIsNotReported(t *testing.T) {
	fake := reportertest.New()
	server := httptest.NewServer(platform.RequestPipeline(discardLogger(), fake, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("partial"))
		panic(http.ErrAbortHandler)
	})))
	defer server.Close()

	if resp, err := http.Get(server.URL); err == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	if reports := fake.Errors(); len(reports) != 0 {
		t.Fatalf("an abort was reported: %+v", reports)
	}
}

func TestGracefulShutdownFlushesTheReporterWithATwoSecondBound(t *testing.T) {
	fake := reportertest.New()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ctx, shutdown := context.WithCancel(context.Background())
	served := make(chan error, 1)
	go func() {
		served <- platform.Serve(ctx, &http.Server{Handler: http.NotFoundHandler()}, listener, fake, platform.NewSlogLogger(discardLogger()))
	}()

	shutdown()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("Serve after shutdown = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve did not return after shutdown")
	}
	if got := fake.Flushes(); len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("flushes = %v, want one bounded at 2s", got)
	}
}

func TestAStartupFailureIsReportedAndFlushed(t *testing.T) {
	fake := reportertest.New()
	failure := errors.New("database: connection refused")

	platform.ReportStartupFailure(fake, failure)

	if reports := fake.Errors(); len(reports) != 1 || !errors.Is(reports[0].Err, failure) {
		t.Fatalf("reports = %+v, want the startup failure", reports)
	}
	if got := fake.Flushes(); len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("flushes = %v, want one bounded at 2s", got)
	}
}

func TestReporterConfigIsReadFromTheEnvironment(t *testing.T) {
	t.Run("nothing set: no DSN, development, no release", func(t *testing.T) {
		t.Setenv("SENTRY_DSN", "")
		t.Setenv("SENTRY_RELEASE", "")
		t.Setenv("APP_ENV", "")
		got := platform.LoadReporterConfig()
		want := platform.ReporterConfig{Environment: "development"}
		if got != want {
			t.Fatalf("config = %+v, want %+v", got, want)
		}
	})
	t.Run("production with a DSN and the image's release", func(t *testing.T) {
		t.Setenv("SENTRY_DSN", " https://key@sentry.invalid/1 ")
		t.Setenv("SENTRY_RELEASE", "9b770900504e73676dac14c9f7131bb6bac5238c")
		t.Setenv("APP_ENV", "production")
		got := platform.LoadReporterConfig()
		want := platform.ReporterConfig{
			DSN:         "https://key@sentry.invalid/1",
			Environment: "production",
			Release:     "9b770900504e73676dac14c9f7131bb6bac5238c",
		}
		if got != want {
			t.Fatalf("config = %+v, want %+v", got, want)
		}
	})
}
