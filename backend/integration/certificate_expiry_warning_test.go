package integration

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Certificate Expiry Warning's mail (#503, parent #490, ADR 0063): the
// Sale Invoice Drainer's tick reads the certificate in custody, works the
// 30/7/1/0 ladder above the Sale Invoicing flag, and mails every Platform
// Operator once per rung per certificate. Everything is asserted through the
// drain endpoint, the captured mail and the ledger table — the pure decision
// of WHICH rung is the service's unit test
// (invoicing/service/certificate_expiry_warning_test.go).
//
// Certificates are uploaded through the shared app and the drain is driven
// through the SRI app, over one database, exactly as the Drainer's own tests
// do; the invoicing clock is the SRI app's. The suite's fixed clock is
// 2026-07-07 12:00 UTC, 07:00 in Guayaquil, so a NotAfter on the 12th at
// noon UTC is five Ecuadorian days out.

// certificateExpiryNotice is one ledger row, as a test reads it.
type certificateExpiryNotice struct {
	Fingerprint    string
	ThresholdDays  int
	RecipientCount int
}

// Read by SQL because the ledger has no API: it is the Drainer's private
// memory of which rungs it has fired (ADR 0063 §3), never shown to an
// operator, and the only other evidence of it — the mail — cannot say which
// rungs one mail covered.
func certificateExpiryNotices(t *testing.T, env *testEnv) []certificateExpiryNotice {
	t.Helper()
	rows, err := env.db.Query(`SELECT fingerprint_sha256, threshold_days, recipient_count FROM certificate_expiry_notices ORDER BY sent_at, threshold_days DESC`)
	if err != nil {
		t.Fatalf("read certificate_expiry_notices: %v", err)
	}
	defer rows.Close()
	var out []certificateExpiryNotice
	for rows.Next() {
		var n certificateExpiryNotice
		if err := rows.Scan(&n.Fingerprint, &n.ThresholdDays, &n.RecipientCount); err != nil {
			t.Fatalf("scan certificate_expiry_notices: %v", err)
		}
		out = append(out, n)
	}
	return out
}

// issuerWithCertificateExpiring records the Issuer and uploads a throwaway
// certificate whose NotAfter is the given instant, returning the operator
// session and the certificate's fingerprint.
func issuerWithCertificateExpiring(t *testing.T, env *testEnv, operatorEmail string, notAfter time.Time) (sessionID, fingerprint string, p12 []byte) {
	t.Helper()
	sessionID = operatorSession(t, env, operatorEmail)
	putEcuadorIssuer(t, env, sessionID, validEcuadorIssuerBody())
	p12 = throwawayP12ValidUntil(t, rsaKey(t), "1790012345001", "s3cret", notAfter)
	uploaded := uploadCertificate(t, env, sessionID, p12, "s3cret")
	if uploaded.Certificate == nil {
		t.Fatal("upload answered without certificate metadata")
	}
	return sessionID, uploaded.Certificate.FingerprintSHA256, p12
}

// warningsTo groups the captured warnings by address, so a test can say
// "one per operator" and read each in its own language.
func warningsTo(t *testing.T) map[string][]platform.CertificateExpiryWarning {
	t.Helper()
	out := map[string][]platform.CertificateExpiryWarning{}
	for _, w := range sharedEmail.CertificateExpiryWarningsSent() {
		out[w.To] = append(out[w.To], w)
	}
	return out
}

func wantOneWarning(t *testing.T, sent map[string][]platform.CertificateExpiryWarning, to string, threshold int, locale platform.Locale) platform.CertificateExpiryWarning {
	t.Helper()
	got := sent[to]
	if len(got) != 1 {
		t.Fatalf("%s received %d warnings, want exactly one: %+v", to, len(got), got)
	}
	if got[0].Threshold != threshold || got[0].Locale != locale {
		t.Fatalf("%s's warning = threshold %d locale %q, want threshold %d locale %q", to, got[0].Threshold, got[0].Locale, threshold, locale)
	}
	return got[0]
}

// TestCertificateExpiryWarningMailsEveryOperatorInTheirLocaleOnce: a
// certificate uploaded with five days left fires the 30-day rung — and only
// that one — to every address on the allowlist, each in its own Staff
// Locale, with the date, the RUC and the Issuer-page link; the ledger holds
// a row for the rung mailed and one for the 7 rung it covered, each with the
// count of accepted sends; and a second tick the same day sends nothing
// more. Before any certificate is uploaded, and while it is "none", the tick
// mails nobody.
func TestCertificateExpiryWarningMailsEveryOperatorInTheirLocaleOnce(t *testing.T) {
	env := setupTest(t)
	englishSession := operatorSession(t, env, "operator@example.com")
	spanishSession := operatorSession(t, env, "operador@example.com")
	setStaffLocale(t, env, spanishSession, "es")

	// No Issuer, then an Issuer with no certificate: nobody is told.
	drainSaleInvoices(t)
	putEcuadorIssuer(t, env, englishSession, validEcuadorIssuerBody())
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 0 {
		t.Fatalf("%d warnings sent with no certificate in custody; want none", n)
	}
	if rows := certificateExpiryNotices(t, env); len(rows) != 0 {
		t.Fatalf("ledger rows with no certificate = %+v; want none", rows)
	}

	notAfter := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	p12 := throwawayP12ValidUntil(t, rsaKey(t), "1790012345001", "s3cret", notAfter)
	uploaded := uploadCertificate(t, env, englishSession, p12, "s3cret")

	drainSaleInvoices(t)
	sent := warningsTo(t)
	if len(sent) != 2 {
		t.Fatalf("warnings went to %d addresses, want the two on the allowlist: %v", len(sent), sent)
	}
	english := wantOneWarning(t, sent, "operator@example.com", 30, platform.LocaleEN)
	wantOneWarning(t, sent, "operador@example.com", 30, platform.LocaleES)
	if !english.NotAfter.Equal(notAfter) {
		t.Fatalf("warning NotAfter = %s, want %s", english.NotAfter, notAfter)
	}
	if english.RUC != "1790012345001" {
		t.Fatalf("warning RUC = %q, want the Issuer's", english.RUC)
	}
	if english.IssuerURL != "http://staff.example/operator/invoicing/issuer" {
		t.Fatalf("warning IssuerURL = %q, want the Issuer page under the staff base URL", english.IssuerURL)
	}
	// Five days out has reached 30 and 7; the one mail covers both, and the
	// ledger says so, or the next tick would send "seven days" as well.
	rows := certificateExpiryNotices(t, env)
	if len(rows) != 2 {
		t.Fatalf("ledger = %+v, want two rows (30 mailed, 7 covered)", rows)
	}
	for i, want := range []int{30, 7} {
		if rows[i].Fingerprint != uploaded.Certificate.FingerprintSHA256 || rows[i].ThresholdDays != want || rows[i].RecipientCount != 2 {
			t.Fatalf("ledger row %d = %+v, want (this fingerprint, %d, 2 recipients)", i, rows[i], want)
		}
	}

	// The same tick again, the same day: nothing more.
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 2 {
		t.Fatalf("%d warnings after a second tick, want still 2", n)
	}
	if rows := certificateExpiryNotices(t, env); len(rows) != 2 {
		t.Fatalf("ledger after a second tick = %+v, want still two rows", rows)
	}
}

// thirtyDaysOut is a NotAfter exactly 30 Ecuadorian days from the fixed
// clock: the first rung, and only the first, reached.
var thirtyDaysOut = time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

// TestCertificateExpiryWarningClimbsTheLadderOneRungPerDay follows one
// certificate from 30 days out to past its date: 30, then 7, then 1, then 0,
// each once, and nothing on the days between or after.
func TestCertificateExpiryWarningClimbsTheLadderOneRungPerDay(t *testing.T) {
	env := setupTest(t)
	notAfter := thirtyDaysOut
	_, fingerprint, _ := issuerWithCertificateExpiring(t, env, "operator@example.com", notAfter)

	steps := []struct {
		name  string
		at    time.Time
		fires int // -1 for nothing
	}{
		{"30 days out", fixedClock, 30},
		{"29 days out", fixedClock.Add(24 * time.Hour), -1},
		{"8 days out", time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC), -1},
		{"7 days out", time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC), 7},
		{"6 days out", time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC), -1},
		{"the day before, at 03:00 UTC still the 4th in Guayaquil", time.Date(2026, 8, 5, 3, 0, 0, 0, time.UTC), -1},
		{"the day before", time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC), 1},
		{"the day itself", time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC), 0},
		{"the day itself, later", time.Date(2026, 8, 6, 20, 0, 0, 0, time.UTC), -1},
		{"expired", time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC), -1},
		{"long expired", time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC), -1},
	}
	var fired []int
	for _, step := range steps {
		atInvoicingClock(t, step.at)
		before := len(sharedEmail.CertificateExpiryWarningsSent())
		drainSaleInvoices(t)
		sent := sharedEmail.CertificateExpiryWarningsSent()[before:]
		if step.fires < 0 {
			if len(sent) != 0 {
				t.Fatalf("%s: fired %+v, want nothing", step.name, sent)
			}
			continue
		}
		if len(sent) != 1 || sent[0].Threshold != step.fires {
			t.Fatalf("%s: fired %+v, want one warning at %d", step.name, sent, step.fires)
		}
		fired = append(fired, step.fires)
	}
	rows := certificateExpiryNotices(t, env)
	if len(rows) != 4 {
		t.Fatalf("ledger = %+v, want four rows", rows)
	}
	for i, want := range []int{30, 7, 1, 0} {
		if rows[i].Fingerprint != fingerprint || rows[i].ThresholdDays != want {
			t.Fatalf("ledger row %d = %+v, want (%s, %d)", i, rows[i], fingerprint, want)
		}
	}
	if len(fired) != 4 {
		t.Fatalf("fired %v, want all four rungs", fired)
	}
}

// TestCertificateExpiryWarningAlreadyExpiredFiresTheExpiredRungOnly: a
// certificate uploaded past its date says "has expired" once, never a
// countdown, and never again.
func TestCertificateExpiryWarningAlreadyExpiredFiresTheExpiredRungOnly(t *testing.T) {
	env := setupTest(t)
	_, fingerprint, _ := issuerWithCertificateExpiring(t, env, "operator@example.com", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC))

	drainSaleInvoices(t)
	wantOneWarning(t, warningsTo(t), "operator@example.com", 0, platform.LocaleEN)
	// Every rung is reached past the date; the one mail covers them all.
	rows := certificateExpiryNotices(t, env)
	if len(rows) != 4 {
		t.Fatalf("ledger = %+v, want all four rungs covered by the one mail", rows)
	}
	for i, want := range []int{30, 7, 1, 0} {
		if rows[i].Fingerprint != fingerprint || rows[i].ThresholdDays != want {
			t.Fatalf("ledger row %d = %+v, want (%s, %d)", i, rows[i], fingerprint, want)
		}
	}
	atInvoicingClock(t, fixedClock.Add(24*time.Hour))
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 1 {
		t.Fatalf("%d warnings after expiry, want the one and no daily nag", n)
	}
}

// TestCertificateExpiryWarningRunsWithTheFlagClosed: the deployment this
// warning exists for. SALE_INVOICING_ENABLED closed, the drain still answers
// 404 SALE_INVOICING_UNAVAILABLE and signs nothing — and the ladder has run,
// the operators are told, and the ledger row is there.
func TestCertificateExpiryWarningRunsWithTheFlagClosed(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	closed := startAppWithSaleInvoicingClosed(t)
	_, fingerprint, _ := issuerWithCertificateExpiring(t, env, "operator@example.com", thirtyDaysOut)

	resp, body := closed.post(t, saleInvoiceDrainPath, nil, nil)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
		t.Fatalf("drain while closed status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", resp.StatusCode, body.Error)
	}
	wantOneWarning(t, warningsTo(t), "operator@example.com", 30, platform.LocaleEN)
	rows := certificateExpiryNotices(t, env)
	if len(rows) != 1 || rows[0].Fingerprint != fingerprint || rows[0].ThresholdDays != 30 {
		t.Fatalf("ledger = %+v, want one row at 30", rows)
	}
	if n := sriStub.receptionCount(); n != 0 {
		t.Fatalf("the SRI heard %d receptions with the flag closed; want none", n)
	}
	// And the 404 is the same on a tick with nothing left to fire.
	resp, body = closed.post(t, saleInvoiceDrainPath, nil, nil)
	if resp.StatusCode != http.StatusNotFound || body.Error == nil || body.Error.Code != "SALE_INVOICING_UNAVAILABLE" {
		t.Fatalf("second drain while closed status=%d error=%+v; want 404 SALE_INVOICING_UNAVAILABLE", resp.StatusCode, body.Error)
	}
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 1 {
		t.Fatalf("%d warnings after a second closed tick, want still 1", n)
	}
}

// TestCertificateExpiryWarningRetriesTheRungWhenNobodyWasReached: a
// provider outage on the tick that reaches a rung leaves no ledger row, the
// tick itself succeeds, and the next tick sends the same rung.
func TestCertificateExpiryWarningRetriesTheRungWhenNobodyWasReached(t *testing.T) {
	env := setupTest(t)
	_, fingerprint, _ := issuerWithCertificateExpiring(t, env, "operator@example.com", thirtyDaysOut)

	sharedEmail.FailWith(errors.New("resend: 503"))
	t.Cleanup(func() { sharedEmail.FailWith(nil) })
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 0 {
		t.Fatalf("%d warnings recorded under a failing sender, want none", n)
	}
	if rows := certificateExpiryNotices(t, env); len(rows) != 0 {
		t.Fatalf("ledger after a total failure = %+v, want no row", rows)
	}

	sharedEmail.FailWith(nil)
	drainSaleInvoices(t)
	wantOneWarning(t, warningsTo(t), "operator@example.com", 30, platform.LocaleEN)
	rows := certificateExpiryNotices(t, env)
	if len(rows) != 1 || rows[0].Fingerprint != fingerprint || rows[0].ThresholdDays != 30 || rows[0].RecipientCount != 1 {
		t.Fatalf("ledger after the retry = %+v, want one row (30, 1 recipient)", rows)
	}
}

// oneAddressRefused is the shared capture sender with one address the
// provider refuses: the partial failure as the Drainer would meet it.
type oneAddressRefused struct {
	platform.EmailSender
	refused string
}

func (s oneAddressRefused) SendCertificateExpiryWarning(ctx context.Context, w platform.CertificateExpiryWarning) error {
	if w.To == s.refused {
		return errors.New("resend: mailbox unavailable")
	}
	return s.EmailSender.SendCertificateExpiryWarning(ctx, w)
}

// TestCertificateExpiryWarningOneBadInboxDoesNotSilenceTheRest: a send that
// fails for one operator neither fails the tick nor the other operators; the
// rung is fired — the row counts who accepted — and is not sent again.
func TestCertificateExpiryWarningOneBadInboxDoesNotSilenceTheRest(t *testing.T) {
	env := setupTest(t)
	_, fingerprint, _ := issuerWithCertificateExpiring(t, env, "operator@example.com", thirtyDaysOut)
	operatorSession(t, env, "second@example.com")
	sriApp.InvoicingService.WithEmailSender(oneAddressRefused{EmailSender: sharedEmail, refused: "second@example.com"})
	t.Cleanup(func() { sriApp.InvoicingService.WithEmailSender(sharedEmail) })

	drainSaleInvoices(t)
	sent := warningsTo(t)
	wantOneWarning(t, sent, "operator@example.com", 30, platform.LocaleEN)
	if len(sent["second@example.com"]) != 0 {
		t.Fatalf("the refused address received %+v", sent["second@example.com"])
	}
	rows := certificateExpiryNotices(t, env)
	if len(rows) != 1 || rows[0].Fingerprint != fingerprint || rows[0].ThresholdDays != 30 || rows[0].RecipientCount != 1 {
		t.Fatalf("ledger after a partial failure = %+v, want one row (30, 1 recipient)", rows)
	}
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 1 {
		t.Fatalf("%d warnings after a second tick, want still 1: a fired rung is not retried for the address that refused", n)
	}
}

// TestCertificateExpiryWarningRestartsForANewCertificateAndNotForTheSameOne:
// the ledger is keyed on the fingerprint. The same .p12 uploaded again is the
// same fingerprint and re-fires nothing; a different certificate has no rows
// and is warned about on its own terms.
func TestCertificateExpiryWarningRestartsForANewCertificateAndNotForTheSameOne(t *testing.T) {
	env := setupTest(t)
	notAfter := thirtyDaysOut
	sessionID, first, p12 := issuerWithCertificateExpiring(t, env, "operator@example.com", notAfter)
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 1 {
		t.Fatalf("%d warnings for the first certificate, want 1", n)
	}

	// The same file again: same fingerprint, nothing to say.
	reuploaded := uploadCertificate(t, env, sessionID, p12, "s3cret")
	if reuploaded.Certificate.FingerprintSHA256 != first {
		t.Fatalf("re-upload changed the fingerprint: %s -> %s", first, reuploaded.Certificate.FingerprintSHA256)
	}
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 1 {
		t.Fatalf("%d warnings after re-uploading the same certificate, want still 1", n)
	}

	// A different certificate, even to the same date: the ladder starts over.
	renewed := uploadCertificate(t, env, sessionID, throwawayP12ValidUntil(t, rsaKey(t), "1790012345001", "s3cret", notAfter), "s3cret")
	second := renewed.Certificate.FingerprintSHA256
	if second == first {
		t.Fatal("a fresh certificate has the first one's fingerprint")
	}
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 2 {
		t.Fatalf("%d warnings after a new certificate, want 2", n)
	}
	rows := certificateExpiryNotices(t, env)
	if len(rows) != 2 || rows[0].Fingerprint != first || rows[1].Fingerprint != second || rows[0].ThresholdDays != 30 || rows[1].ThresholdDays != 30 {
		t.Fatalf("ledger = %+v, want (first, 30) then (second, 30)", rows)
	}
}

// TestCertificateExpiryWarningWithAnEmptyAllowlistTellsNobody: an allowlist
// with nobody on it is logged and never an error, and fires no rung — the
// operator added tomorrow is still told.
func TestCertificateExpiryWarningWithAnEmptyAllowlistTellsNobody(t *testing.T) {
	env := setupTest(t)
	_, fingerprint, _ := issuerWithCertificateExpiring(t, env, "operator@example.com", thirtyDaysOut)
	// Emptied by SQL because the allowlist has no remove endpoint: operators
	// are seeded by the harness and by the SQL seeding rule (ADR 0015), and
	// the fixed harness always has at least one.
	if _, err := env.db.Exec(`DELETE FROM platform_operators`); err != nil {
		t.Fatalf("empty the allowlist: %v", err)
	}
	drainSaleInvoices(t)
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 0 {
		t.Fatalf("%d warnings with an empty allowlist, want none", n)
	}
	if rows := certificateExpiryNotices(t, env); len(rows) != 0 {
		t.Fatalf("ledger with an empty allowlist = %+v, want no row", rows)
	}

	seedPlatformOperator(t, env, "late@example.com")
	drainSaleInvoices(t)
	wantOneWarning(t, warningsTo(t), "late@example.com", 30, platform.LocaleEN)
	if rows := certificateExpiryNotices(t, env); len(rows) != 1 || rows[0].Fingerprint != fingerprint {
		t.Fatalf("ledger once an operator exists = %+v, want one row", rows)
	}
}

// TestCertificateExpiryWarningIsNotRunByTheCheckoutKick: the ladder is a
// fact about a date, not about a sale, and has no business on the checkout's
// latency (ADR 0063 §1). With the post-commit kick on and a certificate in
// custody at its first rung, a paid House checkout is worked to "authorized"
// in the background and nobody is warned; the scheduled tick that follows
// is the one that warns.
func TestCertificateExpiryWarningIsNotRunByTheCheckoutKick(t *testing.T) {
	env := setupTest(t)
	sriStub.reset()
	adminSessionID := orgAdminSession(t, env)
	orgID := operatorOrgIDBySlug(t, env, "test-org")
	operatorSessionID, _, _ := issuerWithCertificateExpiring(t, env, "operator@example.com", thirtyDaysOut)
	designateHouseOrganization(t, env, operatorSessionID, orgID)
	_, gaID := publishCheckoutEvent(t, env, adminSessionID, "House Fest", "house-fest", 1000, 10)
	payphoneApp.InvoicingService.WithSaleInvoiceKick(true)
	t.Cleanup(func() { payphoneApp.InvoicingService.WithSaleInvoiceKick(false) })

	paidCheckoutApproved(t, "house-fest", checkoutBody("guest@example.com", "Ana", "Lopez", cartLine(gaID, 1)))
	// The kicked round has run to its end once the document is authorized:
	// had it worked the ladder, the mail would have gone out before the
	// first claim.
	list := getSaleInvoiceList(t, operatorSessionID)
	if len(list.Data) != 1 {
		t.Fatalf("%d Sale Invoices after one House checkout, want 1", len(list.Data))
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		detail := getDrainedInvoice(t, operatorSessionID, list.Data[0].ID)
		if detail.Status == "authorized" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the kicked document never authorized: %s with %d attempts", detail.Status, len(detail.AttemptRows))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if n := len(sharedEmail.CertificateExpiryWarningsSent()); n != 0 {
		t.Fatalf("%d warnings sent by the checkout kick, want none: the ladder is the scheduled tick's", n)
	}
	if rows := certificateExpiryNotices(t, env); len(rows) != 0 {
		t.Fatalf("ledger after the kick = %+v, want no row", rows)
	}

	drainSaleInvoices(t)
	wantOneWarning(t, warningsTo(t), "operator@example.com", 30, platform.LocaleEN)
}
