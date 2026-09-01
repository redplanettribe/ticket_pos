package invoicing

// The refusal by number (#576, parent #575, ADR 0068): the Tax Authority's
// word that it will not take this document because of the NUMBER it carries,
// not because of anything in it. In Ecuador that is the SRI's recepción error
// 45, "ERROR SECUENCIAL REGISTRADO" — the authority saying a comprobante with
// this secuencial is already registered under some other clave de acceso.
//
// It is the one refusal the platform's ordinary remedy cannot mend. Resend
// re-signs and resubmits under the same clave and secuencial, as S1 §5.10
// requires and as every other refusal wants; that is precisely what 45
// complains about, so a resend can only earn the same answer again. On
// 2026-08-30 two production facturas, 001-001-000000025 and 26, proved it
// stable over days. Why the authority refuses a number the operator cannot
// find at its own portal is #573's question, not this one.
//
// DERIVED, NEVER STORED (ADR 0068). The authority's messages are already
// persisted verbatim on the invoice row, so this is a question asked of them
// whenever a detail page is open — no column, no backfill. Two consequences
// decided it: the two production documents, written long before this code,
// answer true the moment it deploys; and if the detection is later found
// wrong, correcting the predicate re-answers every historical row at once.
// The Recipient Warning earns its column because the list filters and the
// dashboard counts it across rows; this is a single-row question.

// AuthorityMessageSequenceRegistered is the SRI's 45, "ERROR SECUENCIAL
// REGISTRADO". AuthorityMessageTypeError is the SRI's tipo for an error, as
// against the ADVERTENCIA the Recipient Warning reads.
const (
	AuthorityMessageSequenceRegistered = "45"
	AuthorityMessageTypeError          = "ERROR"
)

// RefusedByNumberIn reports whether a refusal's messages carry the SRI's 45
// as an error: what makes a document one the authority refuses by number.
// Exact identifier, error-typed — the same code carried as an advertencia,
// or with no tipo at all, is not the authority refusing the number and must
// not be read as one, because what hangs off this answer (#577's hard Resend
// refusal, #578's Abandon) may never be reached by accident.
func RefusedByNumberIn(messages []AuthorityMessage) bool {
	for _, m := range messages {
		if m.Type == AuthorityMessageTypeError && m.Identifier == AuthorityMessageSequenceRegistered {
			return true
		}
	}
	return false
}
