package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/invoicing"
	"github.com/peter/ticket_pos/backend/internal/invoicing/ride"
)

// The two downloads (#456). Neither rebuilds anything: the signed XML is the
// bytes the authority received, the authorization XML the bytes it answered
// with, and both leave exactly as they were stored. The third (#494, ADR
// 0062) is the RIDE, rendered from that same row each time and stored
// nowhere.

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

// RIDE renders the document's RIDE from the row as stored, which only an
// authorized document has. The row is loaded exactly as AuthorizationXML
// loads it and the refusal is the renderer's own — RIDE_NOT_FOUND for any
// document that is not authorized — so the delivery and the Customer Area
// (#496, #497) get the same answer from the same seam.
func (s *Service) RIDE(ctx context.Context, id string) (*invoicing.Document, error) {
	row, err := s.repo.GetInvoice(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, invoicing.ErrInvoiceNotFound()
	}
	return ride.Render(&row.Invoice, row.Ecuador)
}
