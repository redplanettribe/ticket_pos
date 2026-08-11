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
	// KeptFeeCents and KeptFeeIVACents are the part of the two figures above
	// that stands on reversed sales, because an Operator Reversal stated the
	// platform kept its commission on the refunded sale (#127). They are already
	// included in PlatformFeeCents and FeeIVACents — never add them again — and
	// travel together like every fee figure here. Zero means no Operator
	// Reversal has ever kept a fee, and the dashboard then says nothing.
	KeptFeeCents    int `json:"kept_fee_cents"`
	KeptFeeIVACents int `json:"kept_fee_iva_cents"`
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

// OrganizationBalances returns one Organization's Withdrawable and Payable
// Balances, both signed, for the operator's drill-down.
//
// The operator sees both for the same reason the Organization does, and one
// reason more: an operator settling a Payout Request is the last person who can
// notice that the figure asked against has moved since the ask, and the smaller
// number is the one that says whether the money has cleared (ADR 0026). Neither
// figure ever gates the operator's write — a Payout is a record of money that
// already moved, and recording it is unconditional (ADR 0015).
func (s *Service) OrganizationBalances(ctx context.Context, orgID string) (OrganizationBalances, error) {
	balance, err := s.organizationBalance(ctx, orgID)
	if err != nil {
		return OrganizationBalances{}, err
	}
	return balances(balance), nil
}

// WithdrawableBalances returns the signed Withdrawable Balance of each given
// Organization, for the operator's Organization list. The list shows the one
// figure by design: it is a directory of what the platform owes, and the Payable
// Balance is a per-Organization question answered on the drill-down where the
// gap between the two can be explained.
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
			KeptFeeCents:     t.KeptFeeCents,
			KeptFeeIVACents:  t.KeptFeeIVACents,
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
	// OperatorReversal is the money memo an Operator Reversal left (#125), and
	// null on every sale reversed any other way. It is operator-facing only: it
	// rides this payload, which nobody but a Platform Operator can reach, and no
	// Organization-facing surface carries it.
	OperatorReversal *OperatorReversalMemo `json:"operator_reversal"`

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
		ID:               row.ID,
		OrganizationID:   row.OrganizationID,
		ConfirmationRef:  row.ConfirmationRef,
		Status:           row.Status,
		Channel:          row.Channel,
		Source:           nullableString(row.Source),
		PaymentMethod:    nullableString(row.PaymentMethod),
		SoldAt:           row.SoldAt,
		RecordedAt:       row.RecordedAt,
		ReversedAt:       nullableTime(row.ReversedAt),
		ReversedBy:       nullableString(row.ReversedBy),
		OperatorReversal: operatorReversalMemo(row),
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

// OperatorReversalMemo is what a Platform Operator asserted when they recorded
// an out-of-band refund (#125): who they are, what the buyer actually got back,
// whether the platform kept its Platform Fee and Fee IVA, and their note.
//
// The two money fields are pointers because absent and zero are different
// answers. They are absent on a free Online Sale — there was nothing to refund
// and no fee to keep — and "zero refunded" is not something this system records
// on a paid one.
type OperatorReversalMemo struct {
	// Operator is the acting operator's email, as their Staff Session knows it.
	// Never taken from a request body: who asserted a money fact must not be
	// something a caller can claim.
	Operator            string  `json:"operator"`
	RefundedAmountCents *int    `json:"refunded_amount_cents"`
	PlatformFeeKept     *bool   `json:"platform_fee_kept"`
	Note                *string `json:"note"`
}

// OperatorReversalInput is one Operator Reversal as the operator states it. The
// shape is already validated by the handler; what this layer still has to judge
// is the refunded amount against what the sale actually collected, which is a
// fact about the sale rather than about the request.
type OperatorReversalInput struct {
	Operator            string
	RefundedAmountCents *int
	PlatformFeeKept     *bool
	Note                *string
}

// OperatorReversalResult is what the operator is told after the marking: the
// sale, still under the Sale Confirmation reference the support thread quoted,
// its new provenance, and the memo as recorded.
type OperatorReversalResult struct {
	TicketSaleID    string `json:"ticket_sale_id"`
	ConfirmationRef string `json:"confirmation_ref"`
	// Status is always "reversed"; a marking that did not happen is an error,
	// never a result carrying some other word.
	Status     string `json:"status"`
	ReversedAt string `json:"reversed_at"`
	// ReversedBy is always sales.ReversalActorOperator here.
	ReversedBy       string               `json:"reversed_by"`
	OperatorReversal OperatorReversalMemo `json:"operator_reversal"`
}

// ReverseSaleAsOperator records that a Platform Operator refunded a buyer
// off-platform, marking their Ticket Sale reversed (#125, #126, #123).
//
// A free Online Sale is marked by the same call with no money in it at all: it
// collected nothing, so there was nothing to refund and no fee to keep, and the
// memo carries the operator, the moment and the note alone (#126). Everything
// else — the provenance, the capacity, the notice, the irreversibility — is
// identical, because the money was never what made a sale reversible.
//
// The Payment Provider is not called and must never be. The money left our
// account before this request was made — by hand in the provider's dashboard,
// or by bank transfer that no provider ever saw — so calling anybody here could
// only refund a second time.
//
// This path nevertheless takes the Sale Reversal advisory lock, which it did
// not need before ADR 0024. The lock never guarded this operation's own writes —
// the reversal primitive's row lock does that. It guards somebody else's: the
// Reversal Reconciler holds it while it asks the Payment Provider what became of
// an in-flight Reversal Request, and that ask can take as long as the provider
// takes to answer. Committing here inside that window is the one way this
// operation can still cost a buyer a second refund — the drain checks the sale
// is active, this path reverses it a moment later, and the drain's probe lands
// on a transaction nobody has cancelled. Taking the lock closes that window; an
// operator who finds it held waits for an answer that is seconds away.
//
// The Reversal Window is not consulted, in either direction. Being past it is
// the reason this operation exists, and being inside it is no reason to refuse
// an operator whose buyer is unreachable or whose provider path is down.
//
// The Payment stays `approved`: the checkout genuinely settled, and a reversal
// is a later event on the Sale rather than a retroactive edit to the checkout's
// outcome (ADR 0018).
func (s *Service) ReverseSaleAsOperator(ctx context.Context, confirmationRef string, in OperatorReversalInput) (*OperatorReversalResult, error) {
	row, err := s.repo.GetSaleByConfirmationRef(ctx, confirmationRef)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, sales.ErrTicketSaleRefNotFound(confirmationRef)
	}

	// Everything that can refuse without touching anything runs first, so a
	// refusal is always a no-op: the sale, its capacity and the buyer's inbox are
	// all exactly as they were.
	//
	// An imported sale is refused because no money for it ever passed through the
	// platform, so there is nothing here for anybody to assert about; its undo is
	// the batch-level Sale Import undo and stays so. An In-Person Sale is refused
	// for the same reason.
	if row.Channel != platform.OnlineSalesChannel {
		return nil, sales.ErrOperatorReversalNotAnOnlineSale(row.Channel)
	}
	if row.Status != platform.ActiveSaleStatus {
		return nil, sales.ErrSaleAlreadyReversed()
	}

	// Serialised against the Reversal Reconciler and the buyer's own undo (see
	// the doc above). Taken after the cheap refusals so a sale this operation was
	// never going to touch does not contend for a lock at all.
	//
	// A contended lock is answered differently here than on the buyer's path. A
	// buyer who double-presses has one reversal already happening and no second
	// one to make, so they are told it is done; an operator asserting a refund
	// they made by hand has made a claim that is still true a second later, and
	// telling them "already reversed" would be a lie whenever the in-flight probe
	// comes back refused. They are asked to try again instead.
	release, locked, err := s.repo.LockTicketSaleForReversal(ctx, row.ID)
	if err != nil {
		return nil, err
	}
	if !locked {
		return nil, sales.ErrSaleReversalInProgress(confirmationRef)
	}
	defer func() {
		if err := release(); err != nil {
			s.logger.Error("could not release the Sale Reversal lock after an Operator Reversal; further reversals of this Ticket Sale may be refused until the connection is recycled",
				"ticket_sale_id", row.ID,
				"confirmation_ref", confirmationRef,
				"error", err,
			)
		}
	}()

	// Re-read under the lock. The status checked above was read before anybody
	// could be excluded, so it may already be stale: the buyer's own undo, or a
	// Reconciler probe that just came back succeeded, can have reversed this sale
	// in between.
	row, err = s.repo.GetSaleByConfirmationRef(ctx, confirmationRef)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, sales.ErrTicketSaleRefNotFound(confirmationRef)
	}
	if row.Status != platform.ActiveSaleStatus {
		return nil, sales.ErrSaleAlreadyReversed()
	}
	// Whether the money memo is required is a fact about the SALE, which is why
	// it is judged here and not in the handler: the handler sees a request and
	// can only check its shape (the two facts travel together, and an amount is
	// positive), while the answer to "must there be money at all" is the amount
	// this sale collected (#126).
	//
	// A free Online Sale collected nothing and was charged no Platform Fee, so
	// both facts are claims about money that never existed and both are refused.
	// Absent, not zero: the record must keep "nothing to refund" distinguishable
	// from "zero refunded", and only the first is true of a free sale.
	if row.AmountCents == 0 {
		if in.RefundedAmountCents != nil || in.PlatformFeeKept != nil {
			return nil, sales.ErrNothingToRefund()
		}
	} else {
		// The mirror, and #125's rule unweakened: a sale that took money is never
		// marked without saying what came back.
		if in.RefundedAmountCents == nil || in.PlatformFeeKept == nil {
			return nil, sales.ErrRefundedAmountRequired(row.AmountCents, row.Currency)
		}
		// What the buyer got back cannot exceed what they paid.
		if *in.RefundedAmountCents > row.AmountCents {
			return nil, sales.ErrRefundedAmountExceedsCollected(*in.RefundedAmountCents, row.AmountCents, row.Currency)
		}
	}

	now := s.now()
	// The shared reversal primitive, the same one the Customer's own undo and the
	// Sale Import undo go through: it voids the sale and returns every line's
	// quantity to its Ticket Type's sold_count in one transaction. Capacity
	// restoration is defined once and never reimplemented per caller — and its
	// row lock is what makes a double submit, or a race against the buyer's own
	// undo, restore capacity exactly once.
	reversedSales, err := s.repo.ReverseSales(ctx, repository.ReverseSalesInput{
		EventID:        row.EventID,
		OrganizationID: row.OrganizationID,
		SaleIDs:        []string{row.ID},
		Actor:          sales.ReversalActorOperator,
		Now:            now,
		Operator: &repository.OperatorReversalMemo{
			Operator:            in.Operator,
			Note:                in.Note,
			RefundedAmountCents: in.RefundedAmountCents,
			PlatformFeeKept:     in.PlatformFeeKept,
		},
	})
	if err != nil {
		return nil, err
	}
	if len(reversedSales) == 0 {
		// Somebody got there first — a second press of the operator's own button,
		// the buyer's undo landing in between, or a Sale Import undo. Nothing was
		// written, including the memo, so the first reversal's account of what
		// happened stands.
		return nil, sales.ErrSaleAlreadyReversed()
	}

	// Only now, with the reversal committed, is the buyer told — and told by the
	// existing Sale Voided email, the same one every other reversal sends. A
	// failure to send is swallowed exactly as every other notice in this module
	// is: the tickets are gone whether or not the email lands.
	reversed := reversedSales[0]
	_ = s.email.SendSaleVoided(ctx, platform.SaleVoided{
		To:           reversed.CustomerEmail,
		CustomerName: displayName(reversed.CustomerFirstName, reversed.CustomerLastName),
		EventName:    row.EventName,
		Reference:    reversed.ConfirmationRef,
		// THE SALE'S LANGUAGE, not the operator's and not this request's (#246,
		// ADR 0033). This is the site the ordering was decided for: a Spanish
		// buyer's sale reversed three days later from the operator console, by
		// somebody reading an English page, is still written to in Spanish —
		// because the only thing consulted here is the sale.
		Locale: s.mailLocale(ctx, reversed.ID, reversed.Locale, reversed.CustomerEmail),
	})

	return &OperatorReversalResult{
		TicketSaleID:    row.ID,
		ConfirmationRef: reversed.ConfirmationRef,
		Status:          "reversed",
		ReversedAt:      now.UTC().Format(time.RFC3339),
		ReversedBy:      sales.ReversalActorOperator,
		OperatorReversal: OperatorReversalMemo{
			Operator:            in.Operator,
			RefundedAmountCents: in.RefundedAmountCents,
			PlatformFeeKept:     in.PlatformFeeKept,
			Note:                in.Note,
		},
	}, nil
}

// operatorReversalMemo lifts the Operator Reversal's memo off a looked-up sale,
// and returns nil for every sale that was not reversed by an operator. The
// columns are null together (the schema enforces it), so the operator's own
// identity is what decides whether there is a memo at all.
func operatorReversalMemo(row *repository.OperatorSaleRow) *OperatorReversalMemo {
	if !row.ReversedByOperator.Valid {
		return nil
	}
	memo := OperatorReversalMemo{
		Operator: row.ReversedByOperator.String,
		Note:     nullableString(row.ReversalNote),
	}
	if row.RefundedAmountCents.Valid {
		refunded := int(row.RefundedAmountCents.Int64)
		memo.RefundedAmountCents = &refunded
	}
	if row.PlatformFeeKept.Valid {
		feeKept := row.PlatformFeeKept.Bool
		memo.PlatformFeeKept = &feeKept
	}
	return &memo
}
