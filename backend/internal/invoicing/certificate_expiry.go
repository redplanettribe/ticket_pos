package invoicing

import (
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// CertificateExpiryState is where the signing certificate stands against its
// NotAfter, as the Certificate Expiry Warning states it (ADR 0063, CONTEXT.md
// "Certificate Expiry Warning").
type CertificateExpiryState string

const (
	// CertificateExpiryNone: there is no certificate to expire. Not a warning
	// (ADR 0063 §6) — absent and expired are different facts with different
	// remedies, and the Issuer page already says "none".
	CertificateExpiryNone CertificateExpiryState = "none"
	// CertificateExpiryValid: more than CertificateExpiryWarningDays away.
	CertificateExpiryValid CertificateExpiryState = "valid"
	// CertificateExpiryExpiring: within the warning window, the day of expiry
	// included — the certificate still signs today.
	CertificateExpiryExpiring CertificateExpiryState = "expiring"
	// CertificateExpiryExpired: NotAfter's Ecuadorian date is behind today's.
	CertificateExpiryExpired CertificateExpiryState = "expired"
)

// CertificateExpiryWarningDays is how far ahead, in Ecuadorian calendar days,
// the warning begins: the ladder's first rung.
const CertificateExpiryWarningDays = 30

// CertificateExpiryThresholdDays are the ladder's rungs (ADR 0063 §2), highest
// first, in the days-before count CertificateExpiryAt yields. The ladder that
// climbs them — which rung a tick fires, and the ledger that says once — is
// the invoicing service's (#503); this is only the list.
var CertificateExpiryThresholdDays = [...]int{CertificateExpiryWarningDays, 7, 1, 0}

// CertificateExpiry is the warning's state as every surface reads it — the
// Issuer read's `certificate_expiry` block, the banners it feeds and the
// Drainer's ladder — computed once, here, so no surface re-derives days from a
// date (ADR 0063 §5). NotAfter and DaysBefore are nil exactly when State is
// CertificateExpiryNone.
type CertificateExpiry struct {
	State CertificateExpiryState
	// NotAfter is the certificate's own, as an instant.
	NotAfter *time.Time
	// DaysBefore is the count of Ecuadorian calendar days from today's
	// Ecuador date to the Ecuador date NotAfter falls on — never elapsed
	// hours. Zero on the day of expiry, negative once past.
	DaysBefore *int
}

// CertificateExpiryAt derives the warning's state at `now` for the
// certificate in custody, nil meaning none is.
//
// The count is in Ecuadorian calendar days, in the way StartOfEcuadorDay
// counts them: both instants are taken to the Ecuador date they fall on
// before they are compared, so a NotAfter at 03:00 UTC belongs to the
// previous Ecuadorian day, and a certificate expiring at 14:00 UTC on the 1st
// reads "1 day" for the whole of the 30th in Ecuador. America/Guayaquil has
// no DST, so the two midnights are an exact number of days apart.
func CertificateExpiryAt(now time.Time, certificate *CertificateMetadata) CertificateExpiry {
	if certificate == nil {
		return CertificateExpiry{State: CertificateExpiryNone}
	}
	today := platform.StartOfEcuadorDay(now)
	expiryDay := platform.StartOfEcuadorDay(certificate.NotAfter)
	days := int(expiryDay.Sub(today) / (24 * time.Hour))

	state := CertificateExpiryValid
	switch {
	case days < 0:
		state = CertificateExpiryExpired
	case days <= CertificateExpiryWarningDays:
		state = CertificateExpiryExpiring
	}
	notAfter := certificate.NotAfter
	return CertificateExpiry{State: state, NotAfter: &notAfter, DaysBefore: &days}
}
