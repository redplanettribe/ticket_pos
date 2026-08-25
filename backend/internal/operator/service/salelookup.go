package service

import "context"

// SaleLookup is what an operator gets back from pasting a Sale Confirmation
// reference: the Ticket Sale as sales knows it, and the Organization it belongs
// to as identity knows it.
//
// The Organization is a separate object rather than three fields on the sale
// because it is a separate module's answer. It is also the fact the reference
// alone hides: a support thread quotes a code, and which Organization's money
// this is turns out to be the first thing the operator needs.
type SaleLookup struct {
	Sale         Sale         `json:"sale"`
	Organization Organization `json:"organization"`
	// ReAddressing is the Sale Re-addressing block (#420, ADR 0058): the
	// pending record, or null, and the accepted history. It rides the lookup
	// rather than its own read because it is the second lever on this Sale
	// beside Reverse, and the page that offers both reads once. It carries no
	// token and no link, ever.
	ReAddressing ReAddressingBlock `json:"re_addressing"`
}

// LookUpSale finds one Ticket Sale by its Sale Confirmation reference, across
// every Organization on the platform.
//
// Crossing Organizations is the point of the operation, not a widening of an
// existing one: the flow always starts from a support thread that carries a
// reference and nothing else, so there is no Organization to scope by and no
// operator sales browser to arrive from (#123). Authority is the operator
// allowlist on the namespace this is reached through (ADR 0015).
//
// It is read-only. The Operator Reversal that will hang off this door is a
// separate operation, and nothing here writes, calls a Payment Provider, or
// depends on the Reversal Window having passed.
func (s *Service) LookUpSale(ctx context.Context, confirmationRef string) (*SaleLookup, error) {
	sale, err := s.money.SaleByConfirmationRef(ctx, confirmationRef)
	if err != nil {
		return nil, err
	}
	// The Organization comes from the module that owns Organizations, by the id
	// the sale carries — the same read the rest of this surface uses, so an
	// operator sees one description of an Organization wherever they meet it.
	org, err := s.organizations.GetOrganizationForOperator(ctx, sale.OrganizationID)
	if err != nil {
		return nil, err
	}
	reAddressing, err := s.money.SaleReAddressings(ctx, sale)
	if err != nil {
		return nil, err
	}
	return &SaleLookup{Sale: *sale, Organization: *org, ReAddressing: *reAddressing}, nil
}
