package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// The Customer Dossier (#638, spec #635; CONTEXT.md "Customer Dossier"): the
// one read of everything an Event knows about one Customer.
//
// ONE CALL, ONE ASSEMBLY. GetCustomerDossier is the whole interface: the
// handler asks for a Dossier and gets a finished one. Each later part of the
// Dossier — a Sale's phone, Affiliate Link, invoices and re-addressing (#639),
// its Tickets and the Tickets held on other Sales (#640), their Answers (#641)
// — is a field on CustomerDossier or DossierSale, filled here, so no caller
// ever composes a Dossier out of pieces.
//
// SCOPED TO THE EVENT, NEVER TO THE PLATFORM-GLOBAL CUSTOMER. From the Customer
// record it takes the id and the email identity; every name and Tax ID comes
// off this Event's own Sales, beside the Sale it was given on. A later purchase
// at another Organization rewrites nothing shown here.
//
// NOTHING AT THIS EVENT IS NOT FOUND. A Customer with no Ticket Sale on this
// Event is answered exactly as an id that names nobody, so the Dossier cannot
// be used to learn whether somebody bought elsewhere. #640 widens "something at
// this Event" to a Ticket held on another buyer's Sale.

// Dossier Sale statuses. `corrected` is a reversed Sale that a Sale Correction
// replaced — the same reading the Sales list gives a reversed row naming its
// replacement.
const (
	DossierSaleActive    = "active"
	DossierSaleReversed  = "reversed"
	DossierSaleCorrected = "corrected"
)

// CustomerDossier is the Customer Dossier response.
type CustomerDossier struct {
	Customer DossierCustomerView `json:"customer"`
	// Sales are this Event's Ticket Sales to the Customer, reversed and
	// corrected ones included, newest first.
	Sales []DossierSaleView `json:"sales"`
}

// DossierCustomerView is who the Customer is, platform-wide: an id and an
// email, and deliberately nothing more.
type DossierCustomerView struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

// DossierSaleView is one of this Event's Ticket Sales on the Dossier, with the
// name and Tax ID given on it.
type DossierSaleView struct {
	ID              string `json:"id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Status is `active`, `reversed` or `corrected`.
	Status string `json:"status" enums:"active,reversed,corrected"`
	// ReversedAt is when a reversed or corrected Sale was reversed; null on an
	// active one.
	ReversedAt *time.Time `json:"reversed_at"`
	// ReplacedByConfirmationRef names the replacement of a corrected Sale.
	ReplacedByConfirmationRef *string   `json:"replaced_by_confirmation_ref"`
	SoldAt                    time.Time `json:"sold_at"`
	RecordedAt                time.Time `json:"recorded_at"`
	// Channel is the Sales Channel: `online`, `in_person` or `import`.
	Channel string `json:"channel"`
	// Source is the Sales list's `source` for the Sale.
	Source *string `json:"source"`
	// Origin is how the Sale reached the platform, derived exactly as the Sales
	// list derives it (ADR 0052).
	Origin            string                `json:"origin"`
	TicketTypes       []DossierSaleLineView `json:"ticket_types"`
	AmountCents       int                   `json:"amount_cents"`
	Currency          string                `json:"currency"`
	PaymentMethod     *string               `json:"payment_method"`
	CustomerFirstName string                `json:"customer_first_name"`
	CustomerLastName  string                `json:"customer_last_name"`
	TaxIDType         *string               `json:"tax_id_type"`
	TaxIDNumber       *string               `json:"tax_id_number"`

	// What surrounds the Sale (#639) — see customer_dossier_surroundings.go.

	// Phone is the number given on the checkout this Sale came from, read off
	// its Payment (ADR 0073) and never the Customer record's current phone.
	// Null for a Sale with no checkout behind it — In-Person, imported or
	// manually recorded — or a checkout that gave none.
	Phone *string `json:"phone"`
	// AffiliateLinkName is the display name of the Affiliate Link that
	// attributed the Sale, or null.
	AffiliateLinkName *string `json:"affiliate_link_name"`
	// TaxInvoices are the Tax Invoices about the Sale — its Sale Invoices and
	// any Credit Notes — as the operator Sale lookup
	// reads them, in the Sale's chain order; empty, never null.
	TaxInvoices []DossierTaxInvoiceView `json:"tax_invoices"`
	// ReAddressedAt is when the Sale was re-addressed (ADR 0058) — the
	// acceptance of its latest accepted Sale Re-addressing — or null. Neither
	// address and no token is ever carried.
	ReAddressedAt *time.Time `json:"re_addressed_at"`
}

// DossierSaleLineView is one Ticket Type bought on a Dossier Sale.
type DossierSaleLineView struct {
	TicketTypeID   string `json:"ticket_type_id"`
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
}

// GetCustomerDossier assembles the Customer Dossier of one Customer at one of
// the caller's Organization's Events.
func (s *Service) GetCustomerDossier(
	ctx context.Context, actor ActorContext, eventID, customerID string,
) (*CustomerDossier, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	saleRows, err := s.repo.ListDossierSales(ctx, actor.OrganizationID, eventID, customerID)
	if err != nil {
		return nil, err
	}
	if len(saleRows) == 0 {
		return nil, catalog.ErrCustomerNotFoundAtEvent()
	}
	customer, err := s.repo.GetDossierCustomer(ctx, customerID)
	if err != nil {
		return nil, err
	}
	if customer == nil {
		return nil, catalog.ErrCustomerNotFoundAtEvent()
	}

	dossier := &CustomerDossier{
		Customer: DossierCustomerView{ID: customer.ID, Email: customer.Email},
		Sales:    make([]DossierSaleView, 0, len(saleRows)),
	}
	for _, row := range saleRows {
		dossier.Sales = append(dossier.Sales, dossierSaleView(row))
	}
	if err := s.fillDossierSaleSurroundings(ctx, dossier.Sales); err != nil {
		return nil, err
	}
	return dossier, nil
}

// dossierSaleView shapes one Sale row for the Dossier.
func dossierSaleView(row repository.DossierSale) DossierSaleView {
	lines := make([]DossierSaleLineView, 0, len(row.TicketTypes))
	for _, line := range row.TicketTypes {
		lines = append(lines, DossierSaleLineView(line))
	}
	return DossierSaleView{
		ID:                        row.ID,
		ConfirmationRef:           row.ConfirmationRef,
		Status:                    dossierSaleStatus(row),
		ReversedAt:                row.ReversedAt,
		ReplacedByConfirmationRef: row.ReplacedByConfirmationRef,
		SoldAt:                    row.SoldAt,
		RecordedAt:                row.RecordedAt,
		Channel:                   row.Channel,
		Source:                    row.Source,
		Origin:                    sales.DeriveSaleOrigin(row.Channel, row.ImportBatchID, row.ReplacesSaleID),
		TicketTypes:               lines,
		AmountCents:               row.AmountCents,
		Currency:                  row.Currency,
		PaymentMethod:             row.PaymentMethod,
		CustomerFirstName:         row.CustomerFirstName,
		CustomerLastName:          row.CustomerLastName,
		TaxIDType:                 row.TaxIDType,
		TaxIDNumber:               row.TaxIDNumber,
	}
}

// dossierSaleStatus reads a Sale's status for the Dossier: a reversed Sale a
// correction replaced is `corrected`.
func dossierSaleStatus(row repository.DossierSale) string {
	if row.Status != DossierSaleReversed {
		return DossierSaleActive
	}
	if row.ReplacedBySaleID != nil {
		return DossierSaleCorrected
	}
	return DossierSaleReversed
}
