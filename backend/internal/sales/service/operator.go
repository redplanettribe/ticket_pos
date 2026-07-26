package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// OperatorPayout is one recorded Payout as the Platform Operator sees it: the
// bare fact the Organization also sees, plus the audit trail — who entered the
// number and when the row was written. RecordedBy is null for the Payouts typed
// straight into the database before the Operator Dashboard existed.
type OperatorPayout struct {
	ID          string    `json:"id"`
	AmountCents int       `json:"amount_cents"`
	PaidAt      string    `json:"paid_at"`
	Note        *string   `json:"note"`
	RecordedBy  *string   `json:"recorded_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// CurrencyTotals is the platform's own money in one currency: what it has
// accumulated in Platform Fees and Fee IVA, and what it currently owes the
// Organizations trading in that currency. Currencies are never added together —
// there is one row per currency and no exchange rate anywhere (ADR 0015).
type CurrencyTotals struct {
	Currency         string `json:"currency"`
	PlatformFeeCents int    `json:"platform_fee_cents"`
	FeeIVACents      int    `json:"fee_iva_cents"`
	// TotalOwedCents sums only the positive Withdrawable Balances: what the
	// platform owes. An Organization in the red after a post-settlement reversal
	// owes the platform instead, and netting that off would understate the cash
	// that must stay on hand.
	TotalOwedCents int `json:"total_owed_cents"`
}

// RecordPayoutInput is a Payout an operator is recording after settling
// off-platform: how much left the bank, the day it did, an optional note, and
// the operator's own email for the audit trail.
type RecordPayoutInput struct {
	AmountCents int
	PaidAt      time.Time
	Note        *string
	RecordedBy  string
}

// WithdrawableBalance returns one Organization's Withdrawable Balance, signed.
func (s *Service) WithdrawableBalance(ctx context.Context, orgID string) (int, error) {
	balance, err := s.repo.GetOrganizationBalance(ctx, orgID)
	if err != nil {
		return 0, err
	}
	return balance.NetProceedsCents - balance.PaidOutCents, nil
}

// WithdrawableBalances returns the signed Withdrawable Balance of each given
// Organization, for the operator's Organization list.
func (s *Service) WithdrawableBalances(ctx context.Context, orgIDs []string) (map[string]int, error) {
	return s.repo.BalancesByOrganizationIDs(ctx, orgIDs)
}

// PlatformTotals returns the platform's accumulated revenue and what it owes,
// one row per currency.
func (s *Service) PlatformTotals(ctx context.Context) ([]CurrencyTotals, error) {
	rows, err := s.repo.PlatformCurrencyTotals(ctx)
	if err != nil {
		return nil, err
	}
	totals := make([]CurrencyTotals, 0, len(rows))
	for _, t := range rows {
		totals = append(totals, CurrencyTotals{
			Currency:         t.Currency,
			PlatformFeeCents: t.PlatformFeeCents,
			FeeIVACents:      t.FeeIVACents,
			TotalOwedCents:   t.TotalOwedCents,
		})
	}
	return totals, nil
}

// OperatorPayoutHistory returns one Organization's Payouts newest first, with
// their recorders — the operator's reconciliation view of the same history the
// Organization reads on its own settings page.
func (s *Service) OperatorPayoutHistory(ctx context.Context, orgID string) ([]OperatorPayout, error) {
	rows, err := s.repo.ListPayoutsWithRecorder(ctx, orgID)
	if err != nil {
		return nil, err
	}
	payouts := make([]OperatorPayout, 0, len(rows))
	for _, p := range rows {
		payouts = append(payouts, toOperatorPayout(p))
	}
	return payouts, nil
}

// RecordPayout records a settlement against an Organization and returns it.
//
// It never refuses for exceeding the Withdrawable Balance: a Payout is a record
// of money that has already moved, so the amount is accepted as stated and the
// signed balance absorbs the difference. The dashboard warns before submitting;
// this layer's job is to record what happened (ADR 0015).
func (s *Service) RecordPayout(ctx context.Context, orgID string, input RecordPayoutInput) (*OperatorPayout, error) {
	row, err := s.repo.InsertPayout(ctx, orgID, input.AmountCents, input.PaidAt, input.Note, input.RecordedBy)
	if err != nil {
		return nil, err
	}
	payout := toOperatorPayout(row)
	return &payout, nil
}

func toOperatorPayout(row repository.OperatorPayoutRow) OperatorPayout {
	return OperatorPayout{
		ID:          row.ID,
		AmountCents: row.AmountCents,
		PaidAt:      row.PaidAt.Format(payoutDateFormat),
		Note:        row.Note,
		RecordedBy:  row.RecordedBy,
		CreatedAt:   row.CreatedAt,
	}
}
