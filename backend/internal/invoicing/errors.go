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

// ErrIssuerFieldFrozen: the Issuer detail named in details.field may no
// longer change — the RUC once any Tax Invoice exists (it is inside every
// clave de acceso), establecimiento and punto de emisión once a sequence has
// started under them (numbering must stay continuous). Every other detail
// still saves (#455, stories 4–6).
func ErrIssuerFieldFrozen(field string) apperror.DomainError {
	return apperror.New("ISSUER_FIELD_FROZEN", "The Issuer's "+field+" cannot change any more: documents have been issued under it.", map[string]string{"field": field})
}
