package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
)

// The two downloads (#456). Neither rebuilds anything: the signed XML is the
// bytes the authority received, the authorization XML the bytes it answered
// with, and both leave exactly as they were stored.

// SignedXML returns the invoice's signed document, in every status a signed
// document has; an owed one has none yet (#473).
func (s *Service) SignedXML(ctx context.Context, id string) (*invoicing.Document, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	if !row.Invoice.Signed() || row.Ecuador == nil {
		return nil, invoicing.ErrSignedXMLNotFound()
	}
	return &invoicing.Document{
		Filename:    row.Ecuador.AccessKey + ".xml",
		ContentType: invoicing.ContentTypeXML,
		Body:        row.Invoice.SignedXML,
	}, nil
}

// AuthorizationXML returns the authority's authorization document, which
// only an authorized invoice has.
func (s *Service) AuthorizationXML(ctx context.Context, id string) (*invoicing.Document, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	if row.Invoice.Status != invoicing.InvoiceStatusAuthorized || len(row.Invoice.AuthorizationXML) == 0 || row.Ecuador == nil {
		return nil, invoicing.ErrAuthorizationXMLNotFound()
	}
	return &invoicing.Document{
		Filename:    row.Ecuador.AccessKey + "-autorizacion.xml",
		ContentType: invoicing.ContentTypeXML,
		Body:        row.Invoice.AuthorizationXML,
	}, nil
}
