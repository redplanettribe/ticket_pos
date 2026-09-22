package platform

import (
	"net/http/httptest"
	"testing"

	"github.com/peter/ticket_pos/backend/internal/platform/apperror"
)

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
