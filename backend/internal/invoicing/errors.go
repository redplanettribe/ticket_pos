package invoicing

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// The invoicing module's domain errors: stable codes the operator surface
// keys its copy on. Each names one thing that went wrong, so that a wrong
// password is never mistaken for a bad file, nor either for a server that has
// no key to keep the certificate under (#453).

// ErrIssuerNotFound: the country's Issuer has not been recorded, and what was
// asked for (a certificate upload, a signing key) needs one to hang off.
func ErrIssuerNotFound() apperror.DomainError {
	return apperror.New("ISSUER_NOT_FOUND", "The Issuer has not been recorded yet. Save the Issuer details first.", nil)
}

// ErrCertificateKeyNotConfigured: the platform has no INVOICING_CERTIFICATE_KEY,
// so nothing can be kept in custody or opened from it. The page shows this
// plainly, so that a failed upload is not mistaken for a bad file.
func ErrCertificateKeyNotConfigured() apperror.DomainError {
	return apperror.New("CERTIFICATE_KEY_NOT_CONFIGURED", "The server has no certificate key configured. Set INVOICING_CERTIFICATE_KEY and try again.", nil)
}

// ErrCertificatePasswordIncorrect: the .p12 would not open with the password
// given. Nothing was stored.
func ErrCertificatePasswordIncorrect() apperror.DomainError {
	return apperror.New("CERTIFICATE_PASSWORD_INCORRECT", "The certificate could not be opened with that password.", nil)
}

// ErrCertificateNoRSAKey: the .p12 opened but carries no RSA private key, and
// the SRI's XAdES-BES signature needs one. Nothing was stored.
func ErrCertificateNoRSAKey() apperror.DomainError {
	return apperror.New("CERTIFICATE_NO_RSA_KEY", "The certificate file has no RSA private key. The SRI requires an RSA signing certificate.", nil)
}

// ErrCertificateFileInvalid: the bytes are not a PKCS#12 file at all, or one
// without a certificate in it. Nothing was stored.
func ErrCertificateFileInvalid() apperror.DomainError {
	return apperror.New("CERTIFICATE_FILE_INVALID", "That file is not a valid .p12 certificate.", nil)
}

// ErrCertificateNotUploaded: the Issuer has no certificate, so there is no
// signing key to open (#454 refuses to issue on it).
func ErrCertificateNotUploaded() apperror.DomainError {
	return apperror.New("CERTIFICATE_NOT_UPLOADED", "The Issuer has no signing certificate. Upload the .p12 first.", nil)
}

// ErrCertificateExpired: the certificate in custody is past its NotAfter on
// the platform's clock, and the SRI refuses a signature made with it. The
// Sale Invoice Drainer parks a document on it rather than consume a number
// (#474); the remedy is a re-upload.
func ErrCertificateExpired() apperror.DomainError {
	return apperror.New("CERTIFICATE_EXPIRED", "The Issuer's signing certificate has expired. Upload a current .p12.", nil)
}

// ErrCertificateUnreadable: the certificate on file will not open under the
// current key — the key was rotated, or the row was damaged. The remedy is a
// re-upload; the message says so.
func ErrCertificateUnreadable() apperror.DomainError {
	return apperror.New("CERTIFICATE_UNREADABLE", "The stored certificate cannot be opened under the current certificate key. Upload the .p12 again.", nil)
}

// ErrIssuerIncomplete: the Issuer is recorded but the authority's document
// cannot be built from it — a detail the platform accepts and the SRI's
// schema does not. The message says which. Refused before any number is
// consumed (#454).
func ErrIssuerIncomplete(reason string) apperror.DomainError {
	return apperror.New("ISSUER_INCOMPLETE", "The Issuer's details cannot go on a factura: "+reason+". Fix the Issuer first.", nil)
}

// ErrInvoiceInvalid: the Tax Invoice as entered cannot be built into the
// authority's document. The message is the builder's own. Refused before any
// number is consumed (#454).
func ErrInvoiceInvalid(reason string) apperror.DomainError {
	return apperror.New("INVOICE_INVALID", "The Tax Invoice cannot be built: "+reason+".", nil)
}

// ErrInvoiceNotFound: no Tax Invoice has that id.
func ErrInvoiceNotFound() apperror.DomainError {
	return apperror.New("INVOICE_NOT_FOUND", "No Tax Invoice has that id.", nil)
}

// ErrInvoiceAlreadyAuthorized: Check status and Resend are refused on an
// authorized Tax Invoice — it is a legal artifact and nothing about it is
// asked or sent again (#455, story 34).
func ErrInvoiceAlreadyAuthorized() apperror.DomainError {
	return apperror.New("INVOICE_ALREADY_AUTHORIZED", "The Tax Invoice is already authorized. Nothing can be checked or resent for it.", nil)
}

// ErrInvoiceNotIssued: Check status and Resend are refused on a document
// that is owed and not yet signed (#473) — there is no clave to ask about and
// nothing to send again. The Drainer is what issues it.
func ErrInvoiceNotIssued() apperror.DomainError {
	return apperror.New("INVOICE_NOT_ISSUED", "The document has not been issued yet: it is owed, and the Sale Invoice Drainer issues it.", nil)
}

// ErrInvoiceNotAnnullable: Mark annulled is allowed only on a document that
// is needs_attention or pending (#477) — one the operator could have annulled
// by hand at the authority's portal. An authorized one is credited, never
// annulled here; an annulled or withdrawn one has nothing left to mark.
func ErrInvoiceNotAnnullable() apperror.DomainError {
	return apperror.New("INVOICE_NOT_ANNULLABLE", "Only a document that is pending or needs attention can be marked annulled.", nil)
}

// ErrInvoiceAnnulled: Check status and Resend are refused on an annulled
// document (#477) — the operator recorded that the authority no longer holds
// it as valid, and nothing about it is asked or sent again.
func ErrInvoiceAnnulled() apperror.DomainError {
	return apperror.New("INVOICE_ANNULLED", "The document has been marked annulled. Nothing can be checked or resent for it.", nil)
}

// ErrInvoiceWithdrawn: Check status and Resend are refused on a withdrawn
// document (#476) — its Ticket Sale was reversed before it was ever sent,
// so there is nothing at the authority to ask about and nothing that will
// ever be sent.
func ErrInvoiceWithdrawn() apperror.DomainError {
	return apperror.New("INVOICE_WITHDRAWN", "The document was withdrawn: its Ticket Sale was reversed before it was sent, and nothing was ever sent to the Tax Authority.", nil)
}

// ErrInvoiceRefusedByNumber: Resend is refused on a document the authority
// refuses by NUMBER — the SRI's 45, "secuencial registrado" (#577, parent
// #575, ADR 0068). Resend re-signs and resubmits under the same clave and
// secuencial, as S1 §5.10 requires and as every other refusal wants, and
// that secuencial is the authority's whole objection: the send can only
// earn the same answer again, as it did on production's 001-001-000000025
// and 26 two days apart. The message teaches the rule rather than merely
// blocking, and says what is still open — Check status asks the authority
// what it holds and never sends, so it is untouched by this refusal.
func ErrInvoiceRefusedByNumber() apperror.DomainError {
	return apperror.New("INVOICE_REFUSED_BY_NUMBER", "The Tax Authority refuses this document's number. Resending it would submit the same secuencial the authority already rejects, and earn the same refusal. Check status still asks the authority what it holds.", nil)
}

// The Abandon's refusals (#578, parent #575, ADR 0068), one code each, so
// the operator's surface names what stood in the way rather than "cannot
// abandon". Abandoning declares that a document was never a legal document
// at all, so every fact it rests on is refused by its own name.

// ErrInvoiceAbandoned: Check status, Resend and Abandon are all refused on
// an abandoned document (#578) — the authority never took it and never
// will, so there is nothing left to ask about, nothing that would ever be
// sent, and nothing left to give up. A terminal state is terminal, and this
// is what makes it so.
func ErrInvoiceAbandoned() apperror.DomainError {
	return apperror.New("INVOICE_ABANDONED", "The document was abandoned: the Tax Authority never took it and never will. Nothing can be checked, resent or abandoned for it.", nil)
}

// ErrInvoiceNotAbandonable: Abandon is allowed only on a document parked
// needs_attention, rejected or not_authorized (#578) — the three states a
// refusal leaves a document in, the last two because a manual document's
// refusals are recorded as such rather than parked. A pending document is
// still with the authority and is checked, not given up on; an authorized
// one is a legal artifact and is never disowned this way; an owed one was
// never sent.
func ErrInvoiceNotAbandonable(status InvoiceStatus) apperror.DomainError {
	return apperror.New("INVOICE_NOT_ABANDONABLE", "Only a document the Tax Authority has refused can be abandoned; this one is "+string(status)+".", map[string]string{"status": string(status)})
}

// ErrInvoiceNotRefusedByNumber: Abandon is offered on a document the
// authority refuses by NUMBER and on no other (#578, ADR 0068). Whether a
// document parked for some other reason may ever be abandoned is
// deliberately undecided until a second case needs it, so this refusal
// names the fact rather than the policy: the authority did not refuse this
// document's number, and every refusal it did give has a real remedy.
func ErrInvoiceNotRefusedByNumber() apperror.DomainError {
	return apperror.New("INVOICE_NOT_REFUSED_BY_NUMBER", "Only a document the Tax Authority refuses for its number — error 45, secuencial registrado — can be abandoned. This document was refused for another reason, which has its own remedy.", nil)
}

// ErrInvoiceCheckNotFresh: Abandon was pressed without a fresh Check status
// immediately beforehand (#578, ADR 0068). Abandoning rests on the
// authority's own CURRENT answer, and the attempts ledger must carry that
// answer, timestamped, immediately before the act — the strongest record
// available if the authority later asks why a number was never declared.
// The remedy is one press: Check status, then Abandon.
func ErrInvoiceCheckNotFresh() apperror.DomainError {
	return apperror.New("INVOICE_CHECK_NOT_FRESH", "Check status first: a document is abandoned only on the Tax Authority's current answer, so a Check must be the last thing on its ledger and recent.", nil)
}

// ErrInvoiceAbandonInstead: Mark annulled is refused on a document that
// qualifies for Abandon (#578, ADR 0068), which narrows an existing action
// deliberately. Mark annulled records an annulment the operator performed
// BY HAND AT THE AUTHORITY'S PORTAL; for a number the authority never took,
// the portal shows nothing and there is nothing there to annul, so pressing
// it would write a true-looking record of an act that never happened. That
// was the operator's only escape from production's 001-001-000000025 and
// 26, and it was a falsehood. Abandon is the honest one, and the message
// says so.
func ErrInvoiceAbandonInstead() apperror.DomainError {
	return apperror.New("INVOICE_ABANDON_INSTEAD", "The Tax Authority refuses this document's number and never took it, so there is nothing at its portal to have been annulled. Abandon the document instead.", nil)
}

// ErrIssuerFieldFrozen: the Issuer detail named in details.field may no
// longer change — the RUC once any Tax Invoice exists (it is inside every
// clave de acceso), establecimiento and punto de emisión once a sequence has
// started under them (numbering must stay continuous). Every other detail
// still saves (#455, stories 4–6).
func ErrIssuerFieldFrozen(field string) apperror.DomainError {
	return apperror.New("ISSUER_FIELD_FROZEN", "The Issuer's "+field+" cannot change any more: documents have been issued under it.", map[string]string{"field": field})
}

// The Sale Invoice Reissue's refusals (#483, ADR 0061), one code each, so
// the operator's surface names what stood in the way rather than "cannot
// reissue". Each is a fact about the document or its Sale that no retry
// with the same body changes.

// ErrInvoiceManualNotReissuable: a manual Tax Invoice is not reissued — the
// operator issues another by hand.
func ErrInvoiceManualNotReissuable() apperror.DomainError {
	return apperror.New("INVOICE_MANUAL_NOT_REISSUABLE", "A manual Tax Invoice is not reissued. Issue another one by hand.", nil)
}

// ErrCreditNoteNotReissuable: a Credit Note is never itself credited.
func ErrCreditNoteNotReissuable() apperror.DomainError {
	return apperror.New("CREDIT_NOTE_NOT_REISSUABLE", "A Credit Note cannot be reissued: only a Sale Invoice can.", nil)
}

// ErrInvoiceNotAuthorized: only an authorized Sale Invoice is reissued. One
// still owed, pending or parked has Resend and Mark annulled as its path;
// one withdrawn or annulled no longer stands.
func ErrInvoiceNotAuthorized(status InvoiceStatus) apperror.DomainError {
	return apperror.New("INVOICE_NOT_AUTHORIZED", "Only an authorized Sale Invoice can be reissued; this one is "+string(status)+".", map[string]string{"status": string(status)})
}

// ErrInvoiceSaleReversed: the Sale no longer stands, so income that no
// longer stands is never re-declared.
func ErrInvoiceSaleReversed() apperror.DomainError {
	return apperror.New("INVOICE_SALE_REVERSED", "The Ticket Sale was reversed. Its Sale Invoice is credited by the reversal and cannot be reissued.", nil)
}

// ErrReissueInFlight: another reissue on the same Sale has not settled —
// its Credit Note or its corrected factura is not yet authorized — and a
// chain never forks.
func ErrReissueInFlight() apperror.DomainError {
	return apperror.New("REISSUE_IN_FLIGHT", "A reissue of this sale's Sale Invoice is already in progress. Wait until its Credit Note and corrected Sale Invoice are authorized.", nil)
}

// ErrInvoiceSuperseded: the factura is no longer the Sale's current one;
// the corrected factura is what a further reissue corrects. Only a LIVE
// successor earns this refusal (#579, ADR 0068): one that died — withdrawn,
// annulled or abandoned — supersedes nothing, and answering "reissue the
// current Sale Invoice instead" while pointing at a document the authority
// never authorized left the Sale unreachable (#480).
func ErrInvoiceSuperseded() apperror.DomainError {
	return apperror.New("INVOICE_SUPERSEDED", "This Sale Invoice was superseded by a reissue. Reissue the current Sale Invoice instead.", nil)
}

// ErrInvoiceAlreadyCredited: an authorized Credit Note already stands
// against the factura and no live successor exists — the Sale has no
// current factura (#480's gap), and a second Credit Note would credit it
// twice.
func ErrInvoiceAlreadyCredited() apperror.DomainError {
	return apperror.New("INVOICE_ALREADY_CREDITED", "This Sale Invoice is already credited by an authorized Credit Note and cannot be credited again.", nil)
}

// Issue again's refusals (#580, parent #575, ADR 0068), one code each on
// the same terms as the reissue's: the operator's surface names the fact
// that stood in the way, never "cannot issue again". Each is a fact about
// the document or its Sale that no retry with the same body changes.
//
// The Sale that no longer stands is ErrInvoiceSaleReversed, shared with the
// reissue deliberately: it is the same fact about the same Sale, and a
// surface that has already learned what it means must not have to learn a
// second word for it.

// ErrInvoiceManualNotIssuableAgain: a manual Tax Invoice is not re-owed —
// the operator types another by hand, as they always have (ADR 0068). The
// standing rule is not quietly reversed by the new act: re-owing a manual
// document would drag it into the owed → Drainer → delivery path it is
// deliberately excluded from in three separate places, to mend a case whose
// answer is to type it again.
func ErrInvoiceManualNotIssuableAgain() apperror.DomainError {
	return apperror.New("INVOICE_MANUAL_NOT_ISSUABLE_AGAIN", "A manual Tax Invoice is not issued again by the platform. Issue another one by hand.", nil)
}

// ErrCreditNoteNotIssuableAgain: a Credit Note is never itself re-owed. It
// exists to cancel a factura; a fresh one would cancel a document nothing
// has issued, and what a dead Credit Note leaves behind is dealt with where
// the chain is — its factura becomes uncredited and current again (#484).
func ErrCreditNoteNotIssuableAgain() apperror.DomainError {
	return apperror.New("CREDIT_NOTE_NOT_ISSUABLE_AGAIN", "A Credit Note cannot be issued again: only a Sale Invoice can.", nil)
}

// ErrInvoiceNotTerminallyDead: Issue again is offered on a Sale Invoice
// that is TERMINALLY DEAD and on no other (#580, ADR 0068) — `abandoned`,
// the authority never took it, or `annulled`, the operator disowned it by
// hand at the portal. Those are the two states in which the Sale provably
// has no factura and nothing will ever make this document one.
//
// `withdrawn` is deliberately not among them, though it is the third death:
// a document is withdrawn when its Sale was reversed or when the Credit
// Note it followed died, and in neither case is a fresh factura owed. An
// owed, pending, parked or refused document is still on its way somewhere,
// and an authorized one is the Sale's factura already; the reissue (ADR
// 0061) is what corrects that one.
func ErrInvoiceNotTerminallyDead(status InvoiceStatus) apperror.DomainError {
	return apperror.New("INVOICE_NOT_TERMINALLY_DEAD", "Only a Sale Invoice that is abandoned or annulled can be issued again; this one is "+string(status)+".", map[string]string{"status": string(status)})
}

// ErrInvoiceAlreadyReplaced: the dead document already has a LIVE
// replacement — a Sale Invoice that supersedes it and has not itself died —
// so a second Issue again would leave the Sale with two competing facturas,
// which is the one thing ADR 0061's chain exists to prevent.
//
// Live is read the way every other surface reads it since #579: a
// replacement that is itself withdrawn, annulled or abandoned supersedes
// nothing, so a Sale whose replacement ALSO died is issued again, and the
// chain grows another hop rather than stopping.
func ErrInvoiceAlreadyReplaced() apperror.DomainError {
	return apperror.New("INVOICE_ALREADY_REPLACED", "This Sale Invoice has already been issued again. Its replacement is the Sale's current Sale Invoice.", nil)
}

// ErrSaleInvoicingUnavailable: Sale Invoicing was asked for while
// SALE_INVOICING_ENABLED is closed (#471, ADR 0060) — a House designation or
// a Drainer run. "Not found." and a 404, on the terms the other feature flags
// answer on: while the flag is closed there is nothing here.
func ErrSaleInvoicingUnavailable() apperror.DomainError {
	return apperror.New("SALE_INVOICING_UNAVAILABLE", "Not found.", nil)
}
