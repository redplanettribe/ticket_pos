package service

import "context"

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

// PayoutsSummary is the Organization's Payouts section: the Withdrawable
// Balance headline and the payout history under it.
type PayoutsSummary struct {
	// WithdrawableBalanceCents is the Net Proceeds of the Organization's active
	// Online Sales minus every Payout recorded against it. It is signed on
	// purpose: a sale reversed after it was paid out leaves the Organization
	// owing the platform, and clamping that to zero would hide it.
	WithdrawableBalanceCents int      `json:"withdrawable_balance_cents"`
	Currency                 string   `json:"currency"`
	Payouts                  []Payout `json:"payouts"`
}

// OrganizationPayouts returns the acting Member's Organization's Withdrawable
// Balance and payout history. Read-only: Payouts are recorded by the platform
// operator directly in the database (ADR 0014), so this module offers no way to
// create one.
func (s *Service) OrganizationPayouts(ctx context.Context, actor ActorContext) (*PayoutsSummary, error) {
	balance, err := s.repo.GetOrganizationBalance(ctx, actor.OrganizationID)
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

	return &PayoutsSummary{
		WithdrawableBalanceCents: balance.NetProceedsCents - balance.PaidOutCents,
		Currency:                 balance.Currency,
		Payouts:                  payouts,
	}, nil
}
