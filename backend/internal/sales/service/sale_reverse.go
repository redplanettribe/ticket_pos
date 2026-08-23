package service

import (
	"context"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// ReverseSaleResult is the outcome of reversing one imported Ticket Sale.
type ReverseSaleResult struct {
	SaleID          string    `json:"sale_id"`
	ConfirmationRef string    `json:"confirmation_ref"`
	Status          string    `json:"status"`
	ReversedAt      time.Time `json:"reversed_at"`
	ReversedBy      string    `json:"reversed_by"`
}

// ReverseImportedSale reverses ONE imported Ticket Sale from the Sales list
// (#350, ADR 0050): the same staff Sale Reversal a Sale Import undo performs,
// scoped to a single row from any batch, however old. Its Tickets cease to
// stand, capacity, the Purchase Limit tally, Tickets Sold and Takings drop for
// that sale alone, and its batch is left exactly as it was.
//
// THE BUYER IS MAILED NOTHING, and there is no toggle for it. An imported buyer
// dealt with the Organization's sales rep and may not know this platform
// exists; the batch undo's notify toggle exists for the same reason and
// defaults the same way. The correction that follows (#351) offers a new Sale
// Confirmation instead, which is the mail that is actually useful.
//
// EVERY ACCEPTED HOLDER IS TOLD, unconditionally, through the one helper every
// reversal route shares (#327): they came here, proved an address and are
// expecting to attend. After commit, like the others.
func (s *Service) ReverseImportedSale(ctx context.Context, actor ActorContext, eventID, saleID string) (*ReverseSaleResult, error) {
	_, ok, err := s.repo.GetEventName(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	now := s.now()
	reversed, err := s.repo.ReverseImportedSale(ctx, repository.ReverseImportedSaleInput{
		EventID:        eventID,
		OrganizationID: actor.OrganizationID,
		SaleID:         saleID,
		Now:            now,
	})
	if err != nil {
		return nil, mapReverseSaleError(err)
	}

	s.tellDisplacedHolders(ctx, []string{reversed.ID})

	return &ReverseSaleResult{
		SaleID:          reversed.ID,
		ConfirmationRef: reversed.ConfirmationRef,
		Status:          saleStatusReversed,
		ReversedAt:      now,
		ReversedBy:      sales.ReversalActorStaff,
	}, nil
}

func mapReverseSaleError(err error) error {
	var notFound *repository.SaleNotFoundError
	if errors.As(err, &notFound) {
		return sales.ErrTicketSaleIDNotFound(notFound.SaleID)
	}
	var notImported *repository.SaleNotImportedError
	if errors.As(err, &notImported) {
		return sales.ErrSaleNotImported(notImported.Channel)
	}
	var already *repository.SaleAlreadyReversedError
	if errors.As(err, &already) {
		return sales.ErrSaleAlreadyReversed()
	}
	return err
}
