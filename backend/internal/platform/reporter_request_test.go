package platform_test

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
	"github.com/peter/ticket_pos/backend/internal/platform/reportertest"
)

// The cause a failing handler hides behind its response.
var errConnectionRefused = errors.New("connection refused")

// reportingServer serves routes through the request pipeline with the fake as
// its reporter, and returns the Logger a handler would be handed: the real one,
// reporting to the same fake, as NewApp wires them (ADR 0079).
func reportingServer(t *testing.T, routes func(mux *http.ServeMux, logger platform.Logger)) (*httptest.Server, *reportertest.Reporter) {
	t.Helper()
	fake := reportertest.New()
	mux := http.NewServeMux()
	routes(mux, platform.NewSlogLogger(discardLogger(), fake))
	server := httptest.NewServer(platform.RequestPipeline(discardLogger(), fake, mux))
	t.Cleanup(server.Close)
	return server, fake
}

// get requests path and returns the response's status and request id.
func get(t *testing.T, server *httptest.Server, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(server.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header.Get(platform.RequestIDHeader)
}

func TestANonDomain500IsReportedOnceWithTheOriginalError(t *testing.T) {
	server, fake := reportingServer(t, func(mux *http.ServeMux, _ platform.Logger) {
		mux.HandleFunc("GET /events/{id}", func(w http.ResponseWriter, r *http.Request) {
			_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), fmt.Errorf("load event: %w", errConnectionRefused))
		})
	})

	status, requestID := get(t, server, "/events/42")

	if status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", status)
	}
	reports := fake.Errors()
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want exactly 1: %+v", len(reports), reports)
	}
	if !errors.Is(reports[0].Err, errConnectionRefused) || reports[0].Err.Error() != "load event: connection refused" {
		t.Errorf("reported %q, want the original error", reports[0].Err)
	}
	if reports[0].RequestID != requestID || requestID == "" {
		t.Errorf("report request id = %q, want the response's %q", reports[0].RequestID, requestID)
	}
}

// logsAndAnswers is a handler's own frame, where an Error log's report must
// start.
func logsAndAnswers(logger platform.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Error(r.Context(), "receipt not sent", "email", "ana@example.com", "error", errConnectionRefused)
		w.WriteHeader(http.StatusNoContent)
	}
}

// THE REQUEST ID OF AN ERROR LOG. Logger.Error takes the context it is logged
// under, so an Error line inside a request is reported with that request's id
// (#765). Only the message and the error travel; the other arguments may name
// a person and stay in the log.
func TestAnErrorLogInsideARequestIsReportedOnceWithThatRequestsID(t *testing.T) {
	server, fake := reportingServer(t, func(mux *http.ServeMux, logger platform.Logger) {
		mux.Handle("GET /receipts", logsAndAnswers(logger))
	})

	status, requestID := get(t, server, "/receipts")

	if status != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
	reports := fake.Errors()
	if len(reports) != 1 {
		t.Fatalf("got %d reports, want exactly 1: %+v", len(reports), reports)
	}
	report := reports[0]
	if report.RequestID != requestID || requestID == "" {
		t.Errorf("report request id = %q, want the response's %q", report.RequestID, requestID)
	}
	if report.Err.Error() != "receipt not sent: connection refused" || !errors.Is(report.Err, errConnectionRefused) {
		t.Errorf("reported %q, want the message wrapping the logged error", report.Err)
	}
	if strings.Contains(fmt.Sprintf("%+v", report), "ana@example.com") {
		t.Errorf("the report carries a logged argument other than the error: %+v", report)
	}
	if len(report.Stack) == 0 || !strings.Contains(report.Stack[0].Function, "logsAndAnswers") {
		t.Errorf("stack = %+v, want it to start in the handler that logged", report.Stack)
	}
}

func TestAnErrorLogOutsideARequestIsReportedWithoutARequestID(t *testing.T) {
	fake := reportertest.New()

	platform.NewSlogLogger(discardLogger(), fake).Error(t.Context(), "nightly job could not start")

	reports := fake.Errors()
	if len(reports) != 1 || reports[0].Err.Error() != "nightly job could not start" || reports[0].RequestID != "" {
		t.Fatalf("reports = %+v, want one, with no request id", reports)
	}
}

// ONE FAILED REQUEST, ONE EVENT (ADR 0079). Whatever answers the 5xx, and
// whatever was logged at Error on the way, the request reports once: the first
// report stands, and the request line of the 5xx reports only when nothing
// inside did.
func TestARequestEndingInA5xxIsReportedOnce(t *testing.T) {
	cases := []struct {
		name string
		// handler answers GET /things/{id}.
		handler func(logger platform.Logger) http.HandlerFunc
		// want is what the one report says.
		want string
	}{
		{
			name: "a panic",
			handler: func(platform.Logger) http.HandlerFunc {
				return func(http.ResponseWriter, *http.Request) { panic("handler exploded") }
			},
			want: "panic: handler exploded",
		},
		{
			name: "a non-domain error",
			handler: func(platform.Logger) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), errConnectionRefused)
				}
			},
			want: "connection refused",
		},
		{
			name: "a domain error mapped to a 5xx",
			handler: func(platform.Logger) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					_ = platform.WriteDomainError(w, platform.RequestID(r.Context()),
						fmt.Errorf("open signing key: %w", apperror.New("CERTIFICATE_UNREADABLE", "The stored certificate cannot be opened", nil)))
				}
			},
			want: "open signing key: The stored certificate cannot be opened",
		},
		{
			name: "an Error log, then a non-domain error",
			handler: func(logger platform.Logger) http.HandlerFunc {
				return func(w http.ResponseWriter, r *http.Request) {
					logger.Error(r.Context(), "PAYMENT_APPROVED_WITHOUT_SALE: resolve by hand", "error", errConnectionRefused)
					_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), errConnectionRefused)
				}
			},
			want: "PAYMENT_APPROVED_WITHOUT_SALE: resolve by hand: connection refused",
		},
		{
			name: "an Error log, then a panic",
			handler: func(logger platform.Logger) http.HandlerFunc {
				return func(_ http.ResponseWriter, r *http.Request) {
					logger.Error(r.Context(), "about to fail")
					panic("handler exploded")
				}
			},
			want: "about to fail",
		},
		{
			name: "a 5xx written by hand",
			handler: func(platform.Logger) http.HandlerFunc {
				return func(w http.ResponseWriter, _ *http.Request) {
					http.Error(w, "unavailable", http.StatusBadGateway)
				}
			},
			// The route pattern, never the raw path with its id.
			want: "GET /things/{id} answered 502",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, fake := reportingServer(t, func(mux *http.ServeMux, logger platform.Logger) {
				mux.Handle("GET /things/{id}", tc.handler(logger))
			})

			status, requestID := get(t, server, "/things/0912345678")

			if status < 500 {
				t.Fatalf("status = %d, want a 5xx", status)
			}
			reports := fake.Errors()
			if len(reports) != 1 {
				t.Fatalf("got %d reports, want exactly 1: %+v", len(reports), reports)
			}
			if got := reports[0].Err.Error(); got != tc.want {
				t.Errorf("reported %q, want %q", got, tc.want)
			}
			if reports[0].RequestID != requestID || requestID == "" {
				t.Errorf("report request id = %q, want the response's %q", reports[0].RequestID, requestID)
			}
		})
	}
}

// The platform refusing as designed is not reported: a 4xx domain error, a
// Warn, and a capacity refusal, a 503 that carries a Retry-After (ADR 0079).
func TestARefusalAndAWarnAreNotReported(t *testing.T) {
	cases := []struct {
		name    string
		handler func(logger platform.Logger) http.HandlerFunc
	}{
		{"a 4xx domain error", func(platform.Logger) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), apperror.New("EVENT_NOT_FOUND", "Event not found", nil))
			}
		}},
		{"a Warn", func(logger platform.Logger) http.HandlerFunc {
			return func(w http.ResponseWriter, _ *http.Request) {
				logger.Warn("a fallback stands in", "error", errConnectionRefused)
				w.WriteHeader(http.StatusOK)
			}
		}},
		{"a capacity refusal", func(platform.Logger) http.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) {
				_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), apperror.New("HOLDER_EXPORT_BUSY", "Try again shortly", nil))
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, fake := reportingServer(t, func(mux *http.ServeMux, logger platform.Logger) {
				mux.Handle("GET /things", tc.handler(logger))
			})

			get(t, server, "/things")

			if reports := fake.Errors(); len(reports) != 0 {
				t.Fatalf("got %d reports, want none: %+v", len(reports), reports)
			}
		})
	}
}

// Work detached from a request with DetachFromRequest, such as the drainer
// kick, reports under the request's id but is not the request's report: the
// request's own 5xx is still reported (ADR 0079).
func TestAnErrorLogDetachedFromItsRequestDoesNotStandInForTheRequestsReport(t *testing.T) {
	server, fake := reportingServer(t, func(mux *http.ServeMux, logger platform.Logger) {
		mux.HandleFunc("GET /checkouts", func(w http.ResponseWriter, r *http.Request) {
			logger.Error(platform.DetachFromRequest(r.Context()), "the post-commit kick could not drain", "error", errConnectionRefused)
			_ = platform.WriteDomainError(w, platform.RequestID(r.Context()), errors.New("commit failed"))
		})
	})

	_, requestID := get(t, server, "/checkouts")

	reports := fake.Errors()
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want the detached line's and the request's: %+v", len(reports), reports)
	}
	if got := reports[1].Err.Error(); got != "commit failed" {
		t.Errorf("second report %q, want the request's own 500", got)
	}
	for _, report := range reports {
		if report.RequestID != requestID {
			t.Errorf("report %q has request id %q, want %q", report.Err, report.RequestID, requestID)
		}
	}
}
