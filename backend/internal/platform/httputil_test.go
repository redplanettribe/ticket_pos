package platform

import (
	"net/http/httptest"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

// A download of somebody's personal or tax data is marked so that no cache on
// the way to the reader keeps a copy.
func TestNoStoreForbidsEveryCache(t *testing.T) {
	rec := httptest.NewRecorder()
	rec.Header().Set("Cache-Control", "public, max-age=60")

	NoStore(rec)

	if got := rec.Header().Values("Cache-Control"); len(got) != 1 || got[0] != "no-store" {
		t.Fatalf("Cache-Control = %q, want exactly [no-store]", got)
	}
}

// A refusal about capacity says when to try again, from the mapper and not
// from any one handler; every other refusal says nothing about retrying.
func TestWriteDomainErrorSetsRetryAfterOnlyOnCapacityRefusals(t *testing.T) {
	for _, tc := range []struct {
		code string
		want string
	}{
		{"HOLDER_EXPORT_BUSY", "5"},
		{"EVENT_NOT_FOUND", ""},
	} {
		rec := httptest.NewRecorder()
		if err := WriteDomainError(rec, "req-1", apperror.New(tc.code, "refused", nil)); err != nil {
			t.Fatalf("%s: write: %v", tc.code, err)
		}
		if got := rec.Header().Get("Retry-After"); got != tc.want {
			t.Errorf("%s: Retry-After = %q, want %q", tc.code, got, tc.want)
		}
	}
}
