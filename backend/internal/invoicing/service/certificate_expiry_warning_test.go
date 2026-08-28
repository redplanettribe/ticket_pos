package service

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Certificate Expiry Warning's pure half (#503, ADR 0063 §2–§3): which
// rung of the 30/7/1/0 ladder one tick fires, given the day count and the
// rungs the ledger already holds. The fan-out, the ledger write and the
// flag's order are the integration harness's to prove
// (integration/certificate_expiry_warning_test.go).

func expiryWith(days int) invoicing.CertificateExpiry {
	state := invoicing.CertificateExpiryValid
	switch {
	case days < 0:
		state = invoicing.CertificateExpiryExpired
	case days <= invoicing.CertificateExpiryWarningDays:
		state = invoicing.CertificateExpiryExpiring
	}
	notAfter := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	return invoicing.CertificateExpiry{State: state, NotAfter: &notAfter, DaysBefore: &days}
}

func TestCertificateExpiryLadderFiresTheHighestUnfiredRungReached(t *testing.T) {
	cases := []struct {
		name      string
		expiry    invoicing.CertificateExpiry
		fired     []int
		want      int
		wantFires bool
	}{
		{"none fires nothing", invoicing.CertificateExpiry{State: invoicing.CertificateExpiryNone}, nil, 0, false},
		{"valid, 31 days left, fires nothing", expiryWith(31), nil, 0, false},
		{"30 days left fires 30", expiryWith(30), nil, 30, true},
		{"29 days left fires 30", expiryWith(29), nil, 30, true},
		{"uploaded with 5 days left fires 30 only", expiryWith(5), nil, 30, true},
		{"5 days left with only 30 fired (a paused scheduler) fires 7", expiryWith(5), []int{30}, 7, true},
		{"5 days left with 30 and 7 fired fires nothing", expiryWith(5), []int{30, 7}, 0, false},
		{"7 days left with 30 fired fires 7", expiryWith(7), []int{30}, 7, true},
		{"8 days left with 30 fired fires nothing", expiryWith(8), []int{30}, 0, false},
		{"1 day left with 30 and 7 fired fires 1", expiryWith(1), []int{30, 7}, 1, true},
		{"the day itself with 30, 7 and 1 fired fires 0", expiryWith(0), []int{30, 7, 1}, 0, true},
		{"uploaded already expired fires 0 only", expiryWith(-3), nil, 0, true},
		{"expired with every rung fired fires nothing", expiryWith(-3), []int{30, 7, 1, 0}, 0, false},
		{"expired with only 30 fired fires 0, never a stale countdown", expiryWith(-3), []int{30}, 0, true},
		{"expired with 0 fired fires nothing more, whatever was missed", expiryWith(-3), []int{0}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, fires := certificateExpiryThresholdDue(tc.expiry, tc.fired)
			if fires != tc.wantFires || (fires && got != tc.want) {
				t.Fatalf("threshold due = (%d, %v), want (%d, %v)", got, fires, tc.want, tc.wantFires)
			}
		})
	}
}

// TestCertificateExpiryRungsReachedCoverTheOnesBelowTheFiredOne: what one
// fan-out writes to the ledger — every rung the day count has reached, so a
// certificate uploaded with five days left is mailed once and not again five
// minutes later for the 7 rung it had also reached.
func TestCertificateExpiryRungsReachedCoverTheOnesBelowTheFiredOne(t *testing.T) {
	cases := []struct {
		name   string
		expiry invoicing.CertificateExpiry
		want   []int
	}{
		{"none", invoicing.CertificateExpiry{State: invoicing.CertificateExpiryNone}, nil},
		{"valid", expiryWith(31), nil},
		{"30 days", expiryWith(30), []int{30}},
		{"5 days", expiryWith(5), []int{30, 7}},
		{"1 day", expiryWith(1), []int{30, 7, 1}},
		{"the day", expiryWith(0), []int{30, 7, 1, 0}},
		{"expired", expiryWith(-3), []int{30, 7, 1, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := certificateExpiryRungsReached(tc.expiry); !slices.Equal(got, tc.want) {
				t.Fatalf("rungs reached = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCertificateExpiryLadderIsHighestFirst pins the rung order the decision
// walks: a reordered list would fire 0 before 30.
func TestCertificateExpiryLadderIsHighestFirst(t *testing.T) {
	rungs := invoicing.CertificateExpiryThresholdDays
	for i := 1; i < len(rungs); i++ {
		if rungs[i] >= rungs[i-1] {
			t.Fatalf("thresholds %v are not strictly descending", rungs)
		}
	}
	if rungs[0] != 30 || rungs[len(rungs)-1] != 0 {
		t.Fatalf("thresholds %v, want 30 first and 0 last", rungs)
	}
}

// TestCertificateExpiryWarningWithoutAnOperatorReaderTellsNobody: a service
// nobody handed the allowlist to mails nobody, logs, and never errors — and
// never reaches the repository, which is what makes the case provable here
// without a database.
func TestCertificateExpiryWarningWithoutAnOperatorReaderTellsNobody(t *testing.T) {
	email := &platform.CaptureEmailSender{}
	svc := New(nil, nil, platform.NewSlogLogger(slog.New(slog.DiscardHandler))).WithEmailSender(email)
	svc.warnOfCertificateExpiry(context.Background())
	if n := len(email.CertificateExpiryWarningsSent()); n != 0 {
		t.Fatalf("%d warnings sent with no operator reader; want none", n)
	}
}
