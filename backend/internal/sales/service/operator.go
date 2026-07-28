package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
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

// OperatorSaleEvent is the Event a looked-up Ticket Sale belongs to: enough to
// recognise the show a support thread is about, and the start that closes the
// Reversal Window early when the doors open first.
type OperatorSaleEvent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
	// StartsAt is null on an Event with no schedule. An Online Sale always has
	// one — publishing requires it — but this lookup reaches every Sales Channel,
	// and an imported sale can belong to a draft Event.
	StartsAt *time.Time `json:"starts_at"`
	// Timezone is the EVENT's own zone, which interprets its schedule. It is not
	// the platform's Ecuadorian clock, which is what the Reversal Window's cutoff
	// is stated in; the two are never the same thing.
	Timezone *string `json:"timezone"`
}

// OperatorSaleCustomer is the buyer as the Ticket Sale snapshotted them, which
// is who a refund would have gone to whatever the Customer record says now.
type OperatorSaleCustomer struct {
	Email     string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// OperatorSale is one Ticket Sale as the Platform Operator's lookup by Sale
// Confirmation reference returns it (#124).
//
// It answers one question: is this the sale the support thread is about? So it
// carries what identifies a sale to somebody who has never seen it — the Event,
// the buyer, the amounts, the status and the Payment Method — and the one fact
// no other surface states, whether the Reversal Window has passed.
//
// The money is split the way the platform recorded it rather than left as a
// total: AmountCents is what the buyer paid, PlatformFeeCents and FeeIVACents
// what the platform withheld, NetProceedsCents what the Organization was left
// with. All four are the snapshots the sale froze at sale time (ADR 0014), so a
// later rate change moves none of them.
type OperatorSale struct {
	ID string `json:"id"`
	// OrganizationID is the join key the operator surface resolves the
	// Organization by, and is not serialised: the Organization travels beside the
	// sale as its own object, and one id in two places is one id too many.
	OrganizationID  string `json:"-"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Status is 'active' or 'reversed'. A reversed sale is never deleted and
	// keeps its reference, so it is found here exactly as an active one is.
	Status string `json:"status"`
	// Channel is the Sales Channel: 'online', 'in_person' or 'import'.
	Channel string  `json:"channel"`
	Source  *string `json:"source"`
	// PaymentMethod is who settled the money: the Payment Provider on a paid
	// Online Sale, 'free' where the platform settled a zero total itself
	// (ADR 0017), cash or transfer on a Direct Sale, null where the channel
	// carries none.
	PaymentMethod *string   `json:"payment_method"`
	SoldAt        time.Time `json:"sold_at"`
	RecordedAt    time.Time `json:"recorded_at"`
	// ReversedAt/ReversedBy are the Sale Reversal's provenance, both null on an
	// active sale and both null on a sale reversed before either was recorded —
	// history is shown as it is, never backfilled (ADR 0018).
	ReversedAt *time.Time `json:"reversed_at"`
	ReversedBy *string    `json:"reversed_by"`

	Customer    OperatorSaleCustomer `json:"customer"`
	TicketTypes []SaleLine           `json:"ticket_types"`
	TicketCount int                  `json:"ticket_count"`

	Currency         string `json:"currency"`
	AmountCents      int    `json:"amount_cents"`
	PlatformFeeCents int    `json:"platform_fee_cents"`
	FeeIVACents      int    `json:"fee_iva_cents"`
	NetProceedsCents int    `json:"net_proceeds_cents"`

	Event OperatorSaleEvent `json:"event"`

	// ReversalWindowClosesAt is when the Reversal Window shuts for this sale:
	// the earlier of 20:00 Ecuador time on the day of purchase and the Event's
	// start. Null on a sale that never had a window at all — anything but an
	// Online Sale, or an Event with no recorded start.
	ReversalWindowClosesAt *time.Time `json:"reversal_window_closes_at"`
	// ReversalWindowPassed is the question the operator came to ask: is the
	// buyer's own undo out of reach? True once the window has shut, and true as
	// well on a sale that never had one, because either way no in-window path
	// remains. It says nothing about whether the sale may be reversed — the
	// window is deliberately irrelevant to an Operator Reversal (#123).
	ReversalWindowPassed bool `json:"reversal_window_passed"`
}

// SaleByConfirmationRef returns the one Ticket Sale carrying a Sale Confirmation
// reference, across every Organization on the platform.
//
// There is no Organization or Member scope here and that is the whole point:
// the operator is a Member of nothing and the reference names the sale
// globally. The authorization is the operator allowlist on the namespace this
// is reached through, and nowhere else (ADR 0015).
func (s *Service) SaleByConfirmationRef(ctx context.Context, confirmationRef string) (*OperatorSale, error) {
	row, err := s.repo.GetSaleByConfirmationRef(ctx, confirmationRef)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, sales.ErrTicketSaleRefNotFound(confirmationRef)
	}

	lines := make([]SaleLine, 0, len(row.TicketTypes))
	for _, l := range row.TicketTypes {
		lines = append(lines, SaleLine{TicketTypeName: l.TicketTypeName, Quantity: l.Quantity})
	}

	closesAt, passed := reversalWindowState(row, s.now())

	return &OperatorSale{
		ID:              row.ID,
		OrganizationID:  row.OrganizationID,
		ConfirmationRef: row.ConfirmationRef,
		Status:          row.Status,
		Channel:         row.Channel,
		Source:          nullableString(row.Source),
		PaymentMethod:   nullableString(row.PaymentMethod),
		SoldAt:          row.SoldAt,
		RecordedAt:      row.RecordedAt,
		ReversedAt:      nullableTime(row.ReversedAt),
		ReversedBy:      nullableString(row.ReversedBy),
		Customer: OperatorSaleCustomer{
			Email:     row.CustomerEmail,
			FirstName: row.CustomerFirstName,
			LastName:  row.CustomerLastName,
		},
		TicketTypes:      lines,
		TicketCount:      row.TicketCount,
		Currency:         row.Currency,
		AmountCents:      row.AmountCents,
		PlatformFeeCents: row.PlatformFeeCents,
		FeeIVACents:      row.FeeIVACents,
		NetProceedsCents: row.NetProceedsCents,
		Event: OperatorSaleEvent{
			ID:       row.EventID,
			Name:     row.EventName,
			Slug:     row.EventSlug,
			StartsAt: nullableTime(row.EventStartsAt),
			Timezone: nullableString(row.EventTimezone),
		},
		ReversalWindowClosesAt: closesAt,
		ReversalWindowPassed:   passed,
	}, nil
}

// reversalWindowState reports when this sale's Reversal Window closes and
// whether it already has.
//
// It asks platform.NewReversalWindow — the single definition of the window
// (ADR 0018) — rather than EligibilityAt, because the operator is being told a
// fact about the clock, not offered a button. EligibilityAt folds the window
// together with the sale's status and its provider's capabilities, so it would
// report a reversed sale's window as shut on the day it was still open, and a
// lookup that misstates a date an operator is about to act on is worse than one
// that says less.
//
// A sale with no window at all — anything but an Online Sale, or an Event with
// no recorded start — has no closing instant to publish and is reported passed:
// there is no in-window path to it and never was.
func reversalWindowState(row *repository.OperatorSaleRow, now time.Time) (*time.Time, bool) {
	if row.Channel != platform.OnlineSalesChannel || !row.EventStartsAt.Valid {
		return nil, true
	}
	window := platform.NewReversalWindow(row.SoldAt, row.EventStartsAt.Time)
	closesAt := window.ClosesAt.UTC()
	return &closesAt, !window.IsOpenAt(now)
}

func nullableString(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func nullableTime(v sql.NullTime) *time.Time {
	if !v.Valid {
		return nil
	}
	return &v.Time
}
