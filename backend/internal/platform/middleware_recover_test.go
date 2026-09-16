package platform

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// A handler that has already committed a streamed response and cannot finish
// it panics http.ErrAbortHandler so the server drops the connection and the
// client sees a failed download. RecoverMiddleware must let that abort through
// untouched: an envelope written after the handler's bytes would turn a
// truncated body into one that ends "cleanly".
func TestRecoverMiddleware_RepanicsErrAbortHandlerWithoutWritingAnEnvelope(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := RecoverMiddleware(logger, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("PK partial"))
		panic(http.ErrAbortHandler)
	}))
	rec := httptest.NewRecorder()

	var recovered any
	func() {
		defer func() { recovered = recover() }()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/archive", nil))
	}()

	if recovered != http.ErrAbortHandler {
		t.Fatalf("recovered %v, want http.ErrAbortHandler re-panicked", recovered)
	}
	if got := rec.Body.String(); got != "PK partial" {
		t.Fatalf("body = %q, want only the handler's own bytes", got)
	}
}

func TestRecoverMiddleware_WritesInternalErrorEnvelopeForOtherPanics(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := RecoverMiddleware(logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not an envelope: %v (%q)", err, rec.Body.String())
	}
	if env.Error == nil || env.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("envelope error = %+v, want INTERNAL_ERROR", env.Error)
	}
}
