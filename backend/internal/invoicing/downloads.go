package invoicing

import "github.com/peter/ticket_pos/backend/internal/platform/apperror"

// Handing over the documents (#456). The signed XML exists from the moment a
// number is consumed and is handed over in every status; the authority's own
// document exists only once it authorized, and is the legal artifact.

// Document is one file handed over to the operator: the bytes exactly as
// stored, the filename built from the authority's reference.
type Document struct {
	Filename    string
	ContentType string
	Body        []byte
}

// ContentTypeXML is what both downloads are served as.
const ContentTypeXML = "application/xml; charset=utf-8"

// ErrAuthorizationXMLNotFound: the invoice exists but the authority has not
// authorized it, so there is no authorization document to hand over — and
// nothing is served in its place, since a RIDE or an XML without the
// authorization has no validity.
func ErrAuthorizationXMLNotFound() apperror.DomainError {
	return apperror.New("AUTHORIZATION_XML_NOT_FOUND", "The invoice has no authorization XML: the authority has not authorized it.", nil)
}

// ErrSignedXMLNotFound: the document is owed and not yet signed (#473), so
// there are no bytes to hand over. The Drainer produces them; until then the
// operator sees an owed document and nothing to download.
func ErrSignedXMLNotFound() apperror.DomainError {
	return apperror.New("SIGNED_XML_NOT_FOUND", "The document has not been signed yet: it is owed and nothing has been sent to the authority.", nil)
}
