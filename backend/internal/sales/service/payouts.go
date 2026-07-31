package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// payoutDateFormat renders a Payout's paid-at day as the calendar date it is.
// The column is a DATE, so serialising it as an instant would invite every
// reader to shift it into their own timezone and show the wrong day.
const payoutDateFormat = "2006-01-02"

// Payout is one recorded settlement in the Organization's payout history.
type Payout struct {
	ID          string  `json:"id"`
	AmountCents int     `json:"amount_cents"`
	PaidAt      string  `json:"paid_at"`
	Note        *string `json:"note"`
}

// OrganizationBalances is the pair of money figures every surface that shows an
// Organization's balance shows together: what the platform owes, and what the
// Organization may ask for today.
//
// They travel as a pair because they only make sense as one. An organizer shown
// the smaller figure alone asks why, and the answer is the other one; an
// operator shown only the larger one settles against money that has not cleared,
// which is the hazard the Payable Balance exists to end (ADR 0026).
type OrganizationBalances struct {
	// WithdrawableBalanceCents is the Net Proceeds of the Organization's active
	// Online Sales minus every Payout recorded against it. It is signed on
	// purpose: a sale reversed after it was paid out leaves the Organization
	// owing the platform, and clamping that to zero would hide it.
	WithdrawableBalanceCents int `json:"withdrawable_balance_cents"`
	// PayableBalanceCents is the same arithmetic over the sales that have
	// cleared, with every Payout still subtracted in full and unchanged.
	//
	// Signed and unclamped for the same reason, and it goes negative in one more
	// case than its sibling: an Organization settled in full against money that
	// had not cleared, which then sold nothing more that day, is owed a positive
	// balance and may ask for none of it. That reads oddly and is correct — it
	// is the conservative half of the ledger saying the settlement got ahead of
	// itself (ADR 0026).
	//
	// Cleared sales are a subset of all sales and the subtraction is identical,
	// so this never exceeds WithdrawableBalanceCents, including when both are
	// negative.
	PayableBalanceCents int `json:"payable_balance_cents"`
}

// PayoutsSummary is the Organization's Payouts section: the two balance
// headlines and the payout history under them.
//
// The two figures are spelled out rather than embedded so the generated OpenAPI
// schema keeps them flat, which is what every client already reads.
type PayoutsSummary struct {
	WithdrawableBalanceCents int      `json:"withdrawable_balance_cents"`
	PayableBalanceCents      int      `json:"payable_balance_cents"`
	Currency                 string   `json:"currency"`
	Payouts                  []Payout `json:"payouts"`
}

// OrganizationPayouts returns the acting Member's Organization's balances and
// payout history. Read-only from the Organization's side: a Payout is recorded
// by a Platform Operator on the operator surface (ADR 0015).
func (s *Service) OrganizationPayouts(ctx context.Context, actor ActorContext) (*PayoutsSummary, error) {
	balance, err := s.organizationBalance(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	rows, err := s.repo.ListPayouts(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}

	payouts := make([]Payout, 0, len(rows))
	for _, p := range rows {
		payouts = append(payouts, Payout{
			ID:          p.ID,
			AmountCents: p.AmountCents,
			PaidAt:      p.PaidAt.Format(payoutDateFormat),
			Note:        p.Note,
		})
	}

	pair := balances(balance)
	return &PayoutsSummary{
		WithdrawableBalanceCents: pair.WithdrawableBalanceCents,
		PayableBalanceCents:      pair.PayableBalanceCents,
		Currency:                 balance.Currency,
		Payouts:                  payouts,
	}, nil
}

// organizationBalance reads one Organization's money, with the day boundary the
// Payable Balance turns on computed HERE, from the injected clock, rather than
// asked of the database.
//
// This is the whole of the clock injection, and it is one line on purpose: every
// surface that shows a balance goes through it, so there is exactly one place
// that decides when today began and exactly one thing a test has to move to
// change the answer. See repository.GetOrganizationBalance for why the boundary
// is a query parameter (ADR 0026).
func (s *Service) organizationBalance(ctx context.Context, orgID string) (repository.OrganizationBalance, error) {
	return s.repo.GetOrganizationBalance(ctx, orgID, platform.StartOfEcuadorDay(s.now()))
}

// balances turns the repository's four sums into the two figures every surface
// shows. Both subtract every recorded Payout in full and unchanged: a Payout is
// money that has already left the bank, and there is no such thing as a Payout
// that has not cleared.
func balances(b repository.OrganizationBalance) OrganizationBalances {
	return OrganizationBalances{
		WithdrawableBalanceCents: b.NetProceedsCents - b.PaidOutCents,
		PayableBalanceCents:      b.ClearedNetProceedsCents - b.PaidOutCents,
	}
}
