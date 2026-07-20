// Package service implements sales business rules and orchestrates transactions.
// Sales covers online, in-person, and import sales plus capacity logic.
package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// ActorContext is the acting Member for sales operations.
type ActorContext struct {
	MemberID       string
	OrganizationID string
}

// ImportSaleInput is one Direct Sale row to record.
type ImportSaleInput struct {
	CustomerEmail string
	CustomerName  string
	TicketTypeID  string
	Quantity      int
	PaymentMethod string
	SoldAt        time.Time
	// AmountCents overrides the catalog unit price snapshot; nil uses the catalog price.
	AmountCents *int
}

// CommitImportInput is a Direct Sale Import to record as one all-or-nothing batch.
type CommitImportInput struct {
	Source         string
	IdempotencyKey string
	Sales          []ImportSaleInput
}

// ImportResult is the outcome of a committed (or replayed) Sale Import.
type ImportResult struct {
	BatchID   string `json:"batch_id"`
	SaleCount int    `json:"sale_count"`
	Status    string `json:"status"`
	Replayed  bool   `json:"replayed"`
}

// Service implements sales business rules.
type Service struct {
	repo  *repository.Repository
	email platform.EmailSender
	now   func() time.Time
}

// New returns a sales service.
func New(repo *repository.Repository, email platform.EmailSender) *Service {
	return &Service{repo: repo, email: email, now: time.Now}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// CommitImport records a Direct Sale Import for an Event: each row becomes a
// Ticket Sale with one Line, capacity decrements atomically, and each customer is
// emailed a Sale Confirmation. The batch is all-or-nothing and idempotent.
func (s *Service) CommitImport(ctx context.Context, actor ActorContext, eventID string, input CommitImportInput) (*ImportResult, error) {
	eventName, ok, err := s.repo.GetEventName(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	commitSales := make([]repository.CommitSale, 0, len(input.Sales))
	for _, row := range input.Sales {
		ref, err := generateConfirmationRef()
		if err != nil {
			return nil, err
		}
		commitSales = append(commitSales, repository.CommitSale{
			CustomerEmail:   row.CustomerEmail,
			CustomerName:    row.CustomerName,
			PaymentMethod:   row.PaymentMethod,
			SoldAt:          row.SoldAt,
			ConfirmationRef: ref,
			Line: repository.CommitLine{
				TicketTypeID:   row.TicketTypeID,
				Quantity:       row.Quantity,
				UnitPriceCents: row.AmountCents,
			},
		})
	}

	batch, err := s.repo.CommitImport(ctx, repository.CommitInput{
		EventID:           eventID,
		OrganizationID:    actor.OrganizationID,
		Source:            input.Source,
		CreatedByMemberID: actor.MemberID,
		IdempotencyKey:    input.IdempotencyKey,
		Sales:             commitSales,
		Now:               s.now(),
	})
	if err != nil {
		return nil, mapCommitError(err)
	}

	result := &ImportResult{
		BatchID:   batch.ID,
		SaleCount: batch.SaleCount,
		Status:    batch.Status,
		Replayed:  batch.Replayed,
	}

	// A replay records nothing new, so it must not re-send confirmations.
	if !batch.Replayed {
		for _, cs := range commitSales {
			_ = s.email.SendSaleConfirmation(ctx, platform.SaleConfirmation{
				To:           cs.CustomerEmail,
				CustomerName: cs.CustomerName,
				EventName:    eventName,
				Reference:    cs.ConfirmationRef,
			})
		}
	}

	return result, nil
}

func mapCommitError(err error) error {
	var capErr *repository.CapacityError
	if errors.As(err, &capErr) {
		return sales.ErrImportBatchFailed(capErr.Row, "CAPACITY_EXCEEDED", map[string]any{
			"ticket_type_id": capErr.TicketTypeID,
			"requested":      capErr.Requested,
			"available":      capErr.Available,
		})
	}
	var unknownErr *repository.UnknownTicketTypeError
	if errors.As(err, &unknownErr) {
		return sales.ErrTicketTypeNotFound(unknownErr.TicketTypeID)
	}
	return err
}

// generateConfirmationRef returns a short, human-readable, collision-resistant
// reference such as "TP-J7K2QX9M".
func generateConfirmationRef() (string, error) {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "TP-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}
