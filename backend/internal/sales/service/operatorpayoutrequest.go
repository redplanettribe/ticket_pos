package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// The Payout Request as a Platform Operator reads it (#176, ADR 0026).
//
// Two shapes, and the difference between them is the whole of this file's
// reason to exist:
//
//   - OperatorPayoutRequest is a LIST row — the cross-Organization queue and the
//     history on an Organization's detail page. Its account number is masked and
//     it carries no Tax ID at all.
//   - PayoutRequest, the type the Organization's own surface already uses, is the
//     DETAIL. It carries the snapshot whole, because executing a transfer means
//     retyping an account number into a banking app.
//
// The split is the ADR's "masked in list contexts", applied at the edge of the
// API rather than in a renderer. The queue is the one screen showing every
// Organization's bank details at once and the one operators screenshot into
// support threads; a payload that carried fifty whole account numbers to a
// browser which drew dots over them would satisfy the screenshot and nothing
// else. Masking here is also the only version of the rule an integration test
// can hold, and this rule is otherwise enforced by review alone.

// OperatorPayoutRequestProfile is the bank detail a list row carries: enough for
// an operator to recognise an account, never enough to retype one.
//
// The Tax ID is absent rather than masked. It identifies the party being
// invoiced and is needed to raise the factura, which happens at the moment of
// transfer — on the detail view — and never while triaging a backlog.
type OperatorPayoutRequestProfile struct {
	BankName string `json:"bank_name"`
	// AccountType is 'ahorros' or 'corriente': the word the receiving bank's own
	// form uses, and no part of the account's identity.
	AccountType string `json:"account_type"`
	// AccountNumberMasked is "····4821". The field is named for what it holds,
	// so no future reader mistakes it for something they can pay to.
	AccountNumberMasked string `json:"account_number_masked"`
	AccountHolderName   string `json:"account_holder_name"`
}

// OperatorPayoutRequest is one ask as a list shows it.
//
// It carries no currency, for the reason the Organization's own view does not:
// a request is always in its Organization's currency, which travels beside it on
// the Organization object, and a second copy is a second place to be wrong.
type OperatorPayoutRequest struct {
	ID string `json:"id"`
	// OrganizationID is the join key the operator service resolves the
	// Organization by, and is not serialised: the Organization travels beside the
	// request as its own object, and one id in two places is one id too many.
	OrganizationID string  `json:"-"`
	AmountCents    int     `json:"amount_cents"`
	Note           *string `json:"note"`
	Status         string  `json:"status"`
	// RequestedBy is the asker's email, so the record outlives their Membership.
	RequestedBy string    `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
	// PayableBalanceCents is what the Organization could have asked for at the
	// moment it asked. A snapshot, never refreshed: the live figure is on the
	// detail view, and the gap between the two is the judgement the cap
	// deliberately leaves to the operator (ADR 0026).
	PayableBalanceCents int                          `json:"payable_balance_cents"`
	PayoutProfile       OperatorPayoutRequestProfile `json:"payout_profile"`
	// The answer, all null while the request is pending. They are carried here
	// because this shape also renders an Organization's request HISTORY, where
	// most rows have been answered (#177 fills them in).
	DeclineReason *string    `json:"decline_reason"`
	ResolvedBy    *string    `json:"resolved_by"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	PayoutID      *string    `json:"payout_id"`
}

// PendingPayoutRequests returns one page of every outstanding Payout Request on
// the platform, oldest first, plus the unpaginated total (ADR 0006).
//
// Page and size arrive already floored and clamped by the handler, as on every
// other list in this system.
func (s *Service) PendingPayoutRequests(ctx context.Context, page, pageSize int) ([]OperatorPayoutRequest, int, error) {
	rows, total, err := s.repo.ListPendingPayoutRequests(ctx, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	return operatorPayoutRequestViews(rows), total, nil
}

// PendingPayoutRequestCount is the backlog as one number, for the badge on the
// operator navigation. It is the same question the queue's total answers, asked
// without loading a page of rows.
func (s *Service) PendingPayoutRequestCount(ctx context.Context) (int, error) {
	return s.repo.CountPendingPayoutRequests(ctx)
}

// OrganizationPayoutRequestHistory returns one Organization's Payout Requests,
// newest first, for the operator's drill-down — where they sit beside the payout
// history and answer "has this Organization been paid recently, and are they
// asking again?" (ADR 0026).
//
// Newest first, and that is not an inconsistency with the queue above: this is a
// history, and the queue is a work queue. It is masked all the same. One
// Organization's details are still bank details, and the place to read an
// account number is the one request that is about to be paid.
func (s *Service) OrganizationPayoutRequestHistory(ctx context.Context, orgID string) ([]OperatorPayoutRequest, error) {
	rows, err := s.repo.ListPayoutRequests(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return operatorPayoutRequestViews(rows), nil
}

// PayoutRequestForOperator returns one Payout Request whole — the snapshot bank
// details included — whatever Organization it belongs to.
//
// This is the surface's one unmasked read, and it is deliberately one request at
// a time: an operator who opens a request is about to make a transfer, and the
// account number is the reason they opened it.
//
// A malformed id is answered as a missing request rather than as a database
// failure, exactly as identity answers a malformed Organization id: to the
// caller both mean "no such request at this path". A request that has already
// been answered is returned like any other — the queue drops it, and the record
// of what happened does not vanish with it.
func (s *Service) PayoutRequestForOperator(ctx context.Context, requestID string) (*PayoutRequest, error) {
	if _, err := uuid.Parse(requestID); err != nil {
		return nil, sales.ErrPayoutRequestNotFound()
	}
	row, err := s.repo.GetPayoutRequestByID(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, sales.ErrPayoutRequestNotFound()
	}
	return payoutRequestView(*row), nil
}

func operatorPayoutRequestViews(rows []repository.PayoutRequestRow) []OperatorPayoutRequest {
	out := make([]OperatorPayoutRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, operatorPayoutRequestView(row))
	}
	return out
}

// operatorPayoutRequestView renders a stored request for a list. Every list
// entry point goes through it, so the queue and an Organization's history can
// never mask a number differently — or, worse, one of them not at all.
func operatorPayoutRequestView(row repository.PayoutRequestRow) OperatorPayoutRequest {
	return OperatorPayoutRequest{
		ID:                  row.ID,
		OrganizationID:      row.OrganizationID,
		AmountCents:         row.AmountCents,
		Note:                row.Note,
		Status:              row.Status,
		RequestedBy:         row.RequestedBy,
		RequestedAt:         row.RequestedAt,
		PayableBalanceCents: row.PayableBalanceCents,
		PayoutProfile: OperatorPayoutRequestProfile{
			BankName:    row.Profile.BankName,
			AccountType: row.Profile.AccountType,
			// The masking is the sales module's own, beside the validator that
			// decides what an account number is, so the rule cannot be softened by
			// a caller that forgets to apply it (sales.MaskAccountNumber).
			AccountNumberMasked: sales.MaskAccountNumber(row.Profile.AccountNumber),
			AccountHolderName:   row.Profile.AccountHolderName,
		},
		DeclineReason: row.DeclineReason,
		ResolvedBy:    row.ResolvedBy,
		ResolvedAt:    row.ResolvedAt,
		PayoutID:      row.PayoutID,
	}
}
