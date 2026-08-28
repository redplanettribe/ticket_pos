package service

import (
	"context"
	"slices"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The Certificate Expiry Warning's mail (#503, parent #490, ADR 0063): the
// Sale Invoice Drainer's scheduled tick reads the certificate in custody,
// works the 30/7/1/0 ladder, and tells every Platform Operator — once per
// rung per certificate — that the Issuer's .p12 is about to lapse, or has.
//
// IT RUNS ABOVE THE FLAG (ADR 0063 §1). DrainSaleInvoices calls this before
// it asks whether SALE_INVOICING_ENABLED is open, so the deployment this
// warning exists for — the certificate uploaded ahead of launch, the flag
// still closed, the Drainer's endpoint answering 404 — is the deployment in
// which it runs. The checkout-kicked round (KickSaleInvoiceDrainer) does not
// run it: the ladder is a fact about a date, not about a sale, and it has no
// business on the checkout's latency.
//
// HIGHEST UNFIRED RUNG REACHED, ONE PER TICK (ADR 0063 §2). A certificate
// uploaded with five days left fires 30 and nothing else; one uploaded
// already expired fires 0 alone. The pure decision is
// certificateExpiryThresholdDue; what is around it is the ledger read, the
// fan-out and the ledger write.
//
// THE LEDGER ROW COMES AFTER THE FAN-OUT, and only once at least one address
// accepted the mail (ADR 0063 §3, migration 103). A per-address failure is
// logged and skipped and never fails the tick; a total failure leaves no row,
// so the next tick tries the same rung again. At-least-once, because there
// is no second chance at "seven days".
//
// NOBODY TOLD IS NEVER AN ERROR. An unconfigured allowlist reader, an empty
// allowlist, a read that failed and a certificate that cannot be read are
// each logged and each leave the tick to go on to the round; this is the
// Payout Request notice's rule (sales/service/payoutrequestnotice.go), and
// the reason is the same — there is nothing a caller could do with the
// failure but lie about what happened to the documents.
//
// ONE LOG LINE PER TICK states the ladder's outcome, so a closed-flag
// deployment can be seen watching the certificate.

// issuerPagePath is where the renewed .p12 is uploaded on the staff
// application, under the staff base URL: the one link the mail carries.
const issuerPagePath = "/operator/invoicing/issuer"

// certificateExpiryThresholdDue is the ladder's pure decision: among the
// rungs the day count has reached (days_before <= T) that the ledger does
// not hold, the largest — or none. A certificate that is absent fires nothing
// (ADR 0063 §6).
//
// AN EXPIRED CERTIFICATE HAS ONE RUNG. Past NotAfter the only true sentence
// is "has expired", so the 30, 7 and 1 rungs are not candidates whether or
// not they ever fired: one uploaded already expired fires 0 alone (ADR 0063
// §2), and one whose earlier rungs were missed — a paused scheduler — is not
// sent three stale countdowns on the day the job wakes up. Before that day
// the rungs read as a countdown from the date the mail names, so the highest
// unfired one reached is the right one even when it overstates the days.
func certificateExpiryThresholdDue(expiry invoicing.CertificateExpiry, fired []int) (int, bool) {
	if expiry.State == invoicing.CertificateExpiryNone || expiry.DaysBefore == nil {
		return 0, false
	}
	for _, threshold := range invoicing.CertificateExpiryThresholdDays {
		if *expiry.DaysBefore > threshold || slices.Contains(fired, threshold) {
			continue
		}
		if *expiry.DaysBefore < 0 && threshold != 0 {
			continue
		}
		return threshold, true
	}
	return 0, false
}

// certificateExpiryRungsReached is every rung the day count has reached
// (days_before <= T), the fired one and those below it alike: a certificate
// uploaded with five days left has reached 30 and 7, is mailed about once —
// the 30 rung — and 7 is covered by that mail. Both are written to the ledger,
// or the next tick, five minutes later, would send "seven days" to the same
// people about the same date, which is the three-mails-at-once outcome ADR
// 0063 rejected, only spread out. A rung that becomes reached later — 1, then
// 0 — is a new fact, and fires on its own.
func certificateExpiryRungsReached(expiry invoicing.CertificateExpiry) []int {
	if expiry.State == invoicing.CertificateExpiryNone || expiry.DaysBefore == nil {
		return nil
	}
	var out []int
	for _, threshold := range invoicing.CertificateExpiryThresholdDays {
		if *expiry.DaysBefore <= threshold {
			out = append(out, threshold)
		}
	}
	return out
}

// warnOfCertificateExpiry works the ladder once, on the certificate in
// custody at this tick. It returns nothing: there is no outcome the Drainer
// could act on, and the log line is the outcome's record.
func (s *Service) warnOfCertificateExpiry(ctx context.Context) {
	if s.operators == nil {
		s.logger.Warn("invoicing: certificate expiry ladder skipped; no operator allowlist reader is wired, nobody can be warned")
		return
	}
	row, err := s.repo.GetEcuadorIssuer(ctx)
	if err != nil {
		s.logger.Error("invoicing: certificate expiry ladder skipped; the Issuer could not be read", "error", err)
		return
	}
	var certificate *invoicing.CertificateMetadata
	if row != nil {
		certificate = row.Issuer.Certificate
	}
	now := s.clock()
	expiry := invoicing.CertificateExpiryAt(now, certificate)
	if expiry.State == invoicing.CertificateExpiryNone {
		// Absent is the Issuer page's business, never a warning (ADR 0063 §6).
		s.logger.Info("invoicing: certificate expiry ladder", "state", expiry.State, "fired", "none")
		return
	}

	fired, err := s.repo.FiredCertificateExpiryThresholds(ctx, certificate.FingerprintSHA256)
	if err != nil {
		s.logger.Error("invoicing: certificate expiry ladder skipped; the ledger could not be read", "error", err)
		return
	}
	threshold, due := certificateExpiryThresholdDue(expiry, fired)
	if !due {
		s.logger.Info("invoicing: certificate expiry ladder", "state", expiry.State, "days_before", *expiry.DaysBefore, "fired", "none", "already_fired", fired)
		return
	}

	recipients, err := s.operators.PlatformOperatorEmails(ctx)
	if err != nil {
		s.logger.Error("invoicing: certificate expiry ladder skipped; the operator allowlist could not be read", "threshold_days", threshold, "error", err)
		return
	}
	if len(recipients) == 0 {
		s.logger.Warn("invoicing: certificate expiry ladder skipped; the operator allowlist is empty, nobody can be warned", "threshold_days", threshold)
		return
	}

	accepted := 0
	for _, to := range recipients {
		// Resolved per RECIPIENT, as the Payout Request notice does: an
		// operator who reads Spanish is written to in Spanish whether or not
		// the operator beside them does.
		err := s.email.SendCertificateExpiryWarning(ctx, platform.CertificateExpiryWarning{
			To:        to,
			Locale:    s.staffLocale(ctx, to),
			Threshold: threshold,
			NotAfter:  *expiry.NotAfter,
			RUC:       row.Details.RUC,
			IssuerURL: s.staffBaseURL + issuerPagePath,
		})
		if err != nil {
			// The address is not logged: the allowlist is identity's, and
			// the log counts recipients as the drain counts documents.
			s.logger.Error("invoicing: certificate expiry warning could not be sent to one operator; the others are still written to", "threshold_days", threshold, "error", err)
			continue
		}
		accepted++
	}
	if accepted == 0 {
		s.logger.Error("invoicing: certificate expiry warning reached nobody; the rung stays unfired and the next tick retries", "threshold_days", threshold, "recipients", len(recipients))
		return
	}
	if err := s.repo.RecordCertificateExpiryNotice(ctx, certificate.FingerprintSHA256, certificateExpiryRungsReached(expiry), s.clock(), accepted); err != nil {
		// The mail went; the next tick will send it again. Worth a line, and
		// the direction worth failing in (migration 103).
		s.logger.Error("invoicing: certificate expiry warning sent but its ledger row could not be written; the next tick will repeat it", "threshold_days", threshold, "error", err)
		return
	}
	s.logger.Info("invoicing: certificate expiry ladder", "state", expiry.State, "days_before", *expiry.DaysBefore, "fired", threshold, "recipients", len(recipients), "accepted", accepted)
}

// staffLocale is the language one warning is written in: the Staff Locale
// stored against the address it is going to, and English underneath (ADR
// 0041), on the Payout Request notice's terms — an unconfigured reader, a
// failed read and an address nobody stated a language for are one outcome,
// and none of them is worth withholding the warning over.
func (s *Service) staffLocale(ctx context.Context, email string) platform.Locale {
	if s.staffLocales == nil {
		return platform.DefaultLocale
	}
	stored, err := s.staffLocales.StaffLocale(ctx, email)
	if err != nil {
		s.logger.Error("invoicing: certificate expiry warning: read staff locale", "error", err)
		return platform.DefaultLocale
	}
	return platform.ResolveStaffLocale(stored)
}
