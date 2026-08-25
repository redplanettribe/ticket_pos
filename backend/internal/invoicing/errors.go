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

// ErrCertificateUnreadable: the certificate on file will not open under the
// current key — the key was rotated, or the row was damaged. The remedy is a
// re-upload; the message says so.
func ErrCertificateUnreadable() apperror.DomainError {
	return apperror.New("CERTIFICATE_UNREADABLE", "The stored certificate cannot be opened under the current certificate key. Upload the .p12 again.", nil)
}
