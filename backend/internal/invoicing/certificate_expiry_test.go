package invoicing_test

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// The one derivation of the Certificate Expiry Warning's state (ADR 0063 §2,
// §5): the Issuer read, the banners and the Drainer's ladder all read this and
// none of them counts days on its own. Days are Ecuadorian calendar days —
// America/Guayaquil is UTC-5 with no DST, so Ecuadorian midnight is 05:00 UTC —
// never elapsed hours: a certificate expiring at 14:00 UTC on the 1st reads
// "1 day" for the whole of the 30th in Ecuador.
func TestCertificateExpiryAt(t *testing.T) {
	t.Parallel()

	// 2026-07-07 07:00 in Ecuador.
	now := mustParse(t, "2026-07-07T12:00:00Z")

	cases := []struct {
		name     string
		notAfter string
		state    invoicing.CertificateExpiryState
		days     int
	}{
		{"a year out is valid", "2027-07-07T12:00:00Z", invoicing.CertificateExpiryValid, 365},
		{"thirty-one days out is valid", "2026-08-07T12:00:00Z", invoicing.CertificateExpiryValid, 31},
		{"thirty days out is expiring", "2026-08-06T12:00:00Z", invoicing.CertificateExpiryExpiring, 30},
		{"03:00 UTC belongs to the previous Ecuadorian day", "2026-08-07T03:00:00Z", invoicing.CertificateExpiryExpiring, 30},
		{"Ecuadorian midnight belongs to the day it opens", "2026-08-07T05:00:00Z", invoicing.CertificateExpiryValid, 31},
		{"seven days out", "2026-07-14T12:00:00Z", invoicing.CertificateExpiryExpiring, 7},
		{"tomorrow at 14:00 UTC reads one day all of today", "2026-07-08T14:00:00Z", invoicing.CertificateExpiryExpiring, 1},
		{"later today is the day of expiry, not yet expired", "2026-07-07T23:00:00Z", invoicing.CertificateExpiryExpiring, 0},
		{"earlier today is still the day of expiry", "2026-07-07T10:00:00Z", invoicing.CertificateExpiryExpiring, 0},
		{"tonight UTC is still today in Ecuador", "2026-07-08T03:00:00Z", invoicing.CertificateExpiryExpiring, 0},
		{"yesterday is expired by one day", "2026-07-06T12:00:00Z", invoicing.CertificateExpiryExpired, -1},
		{"long gone", "2026-01-01T00:00:00Z", invoicing.CertificateExpiryExpired, -188},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			notAfter := mustParse(t, tc.notAfter)
			got := invoicing.CertificateExpiryAt(now, &invoicing.CertificateMetadata{NotAfter: notAfter})
			if got.State != tc.state {
				t.Fatalf("state = %q, want %q", got.State, tc.state)
			}
			if got.DaysBefore == nil || *got.DaysBefore != tc.days {
				t.Fatalf("days_before = %v, want %d", got.DaysBefore, tc.days)
			}
			if got.NotAfter == nil || !got.NotAfter.Equal(notAfter) {
				t.Fatalf("not_after = %v, want %s", got.NotAfter, tc.notAfter)
			}
		})
	}
}

// A missing certificate is not a warning (ADR 0063 §6): the state is `none`
// and there is no date and no count to show or to ladder on.
func TestCertificateExpiryAtWithoutCertificate(t *testing.T) {
	t.Parallel()

	got := invoicing.CertificateExpiryAt(mustParse(t, "2026-07-07T12:00:00Z"), nil)
	if got.State != invoicing.CertificateExpiryNone {
		t.Fatalf("state = %q, want none", got.State)
	}
	if got.NotAfter != nil || got.DaysBefore != nil {
		t.Fatalf("none carries a date or a count: %+v", got)
	}
}

// The ladder's rungs (ADR 0063 §2), highest first, so #503 reads them from
// here and the warning's own threshold is the first rung.
func TestCertificateExpiryThresholds(t *testing.T) {
	t.Parallel()

	want := []int{30, 7, 1, 0}
	if len(invoicing.CertificateExpiryThresholdDays) != len(want) {
		t.Fatalf("thresholds = %v, want %v", invoicing.CertificateExpiryThresholdDays, want)
	}
	for i, d := range want {
		if invoicing.CertificateExpiryThresholdDays[i] != d {
			t.Fatalf("thresholds = %v, want %v", invoicing.CertificateExpiryThresholdDays, want)
		}
	}
	if invoicing.CertificateExpiryWarningDays != 30 {
		t.Fatalf("warning window = %d, want 30", invoicing.CertificateExpiryWarningDays)
	}
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return parsed
}
