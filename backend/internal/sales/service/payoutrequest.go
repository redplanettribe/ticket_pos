package service

import (
	"context"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// PayoutRequest is one ask to be paid, as its own Organization reads it back
// (ADR 0026).
//
// It carries no currency. The Organization's currency is on the payouts summary
// this history is shown beside, and a second copy here would be a second place
// for it to be wrong — a request is always in the Organization's own currency,
// which is the only currency any of its money is ever stated in.
//
// The account number is whole rather than masked, for the same reason it is
// whole on the Payout Profile: this is the Organization's own surface, and
// masking belongs to the operator queue, which shows every Organization's
// details at once and is the screen that gets screenshotted into support threads.
type PayoutRequest struct {
	ID string `json:"id"`
	// OrganizationID is whose ask it is, and is not serialised: on this
	// Organization's own surface it is the Organization already reading, and on
	// the operator's detail view (#176) the Organization travels beside the
	// request as its own object, resolved through the module that owns it.
	OrganizationID string  `json:"-"`
	AmountCents    int     `json:"amount_cents"`
	Note           *string `json:"note"`
	Status         string  `json:"status"`
	// RequestedBy is the asker's email, so the record outlives their Membership.
	RequestedBy string    `json:"requested_by"`
	RequestedAt time.Time `json:"requested_at"`
	// PayableBalanceCents is what the Organization could have asked for at the
	// moment it asked. It is a snapshot and never refreshed: the live figure
	// moves, and the difference between the two is exactly what an operator needs
	// in order to exercise the judgement the cap deliberately leaves to them.
	PayableBalanceCents int `json:"payable_balance_cents"`
	// PayoutProfile is the frozen copy of where the Organization said to pay. No
	// later edit of the profile rewrites it (ADR 0026).
	PayoutProfile PayoutRequestProfile `json:"payout_profile"`
	// The answer, all null while the request is pending. DeclineReason is filled
	// only on a decline, and PayoutID only on a payment (#177).
	DeclineReason *string    `json:"decline_reason"`
	ResolvedBy    *string    `json:"resolved_by"`
	ResolvedAt    *time.Time `json:"resolved_at"`
	PayoutID      *string    `json:"payout_id"`
}

// PayoutRequestProfile is the six-field snapshot on a request. It is a separate
// type from PayoutProfile rather than a reuse of it because the profile carries
// an UpdatedAt that has no meaning here: a snapshot was never updated, and
// offering a timestamp saying otherwise would invite a reader to trust it.
type PayoutRequestProfile struct {
	BankName          string `json:"bank_name"`
	AccountType       string `json:"account_type"`
	AccountNumber     string `json:"account_number"`
	AccountHolderName string `json:"account_holder_name"`
	TaxIDType         string `json:"tax_id_type"`
	TaxIDNumber       string `json:"tax_id_number"`
}

// RequestPayoutInput is one submitted ask.
//
// Profile is optional and its absence is meaningful: nil means "pay me where you
// already know", and a value means the organizer edited the details on the form,
// which IS an edit to the Payout Profile (ADR 0026). It arrives already
// validated and normalised by sales.PayoutProfile.Normalize — the same verdict
// the profile editor uses, never a second one.
type RequestPayoutInput struct {
	AmountCents int
	Note        *string
	Profile     *sales.PayoutProfile
}

// PayoutRequestResult is a submitted ask and whether it was newly recorded.
//
// Created=false is the ADR 0024 courtesy rather than an error: an Organization
// that already has an outstanding request is handed that request back, so an
// organizer who pressed twice learns where their earlier ask went instead of
// being told "conflict" and left to wonder. The handler turns the flag into 201
// or 200; nothing else in the system cares.
type PayoutRequestResult struct {
	Request *PayoutRequest
	Created bool
}

// RequestPayout records the acting Member's Organization's ask to be paid.
//
// The order of the four steps below is the whole of this method's design.
//
//  1. RESOLVE THE PROFILE, writing nothing. A request an operator cannot action
//     is the support thread this feature exists to remove, so a missing or
//     incomplete profile is refused with field-level errors naming what is
//     missing. Those errors come back as a second return value rather than as a
//     domain error because they are per-field and the form shows them per field,
//     exactly as the profile editor does (CommitImportFile sets the precedent for
//     a service handing validation back beside its result).
//
//  2. CHECK THE CAP, still writing nothing. Refusing an impossible claim costs
//     nothing and is kinder than letting an organizer wait three days to be told
//     no by a human. It happens BEFORE the profile is saved so that a refused ask
//     never quietly changes where the Organization is paid — an organizer whose
//     amount was rejected has not agreed to anything, including a bank change.
//
//  3. SAVE THE PROFILE, if the form carried one. An organizer correcting an
//     account number on a request means their account number changed, and having
//     to correct it in two places is worse than the alternative.
//
//  4. WRITE THE ASK, letting the partial unique index decide whether it is the
//     outstanding one.
//
// The Payable Balance is read through organizationBalance, which is the single
// place the Ecuadorian day boundary is computed from the injected clock. It is
// read ONCE and both used and snapshotted, so the figure the cap was checked
// against is provably the figure the request records.
func (s *Service) RequestPayout(ctx context.Context, actor ActorContext, input RequestPayoutInput) (*PayoutRequestResult, []platform.FieldError, error) {
	profile, fieldErrs, err := s.resolvePayoutProfile(ctx, actor.OrganizationID, input.Profile)
	if err != nil || len(fieldErrs) > 0 {
		return nil, fieldErrs, err
	}

	balance, err := s.organizationBalance(ctx, actor.OrganizationID)
	if err != nil {
		return nil, nil, err
	}
	payable := balances(balance).PayableBalanceCents
	if input.AmountCents > payable {
		return nil, nil, sales.ErrPayoutRequestExceedsPayableBalance(input.AmountCents, payable, balance.Currency)
	}

	if input.Profile != nil {
		if _, err := s.repo.SavePayoutProfile(ctx, actor.OrganizationID, profile); err != nil {
			return nil, nil, err
		}
	}

	row, created, err := s.repo.CreatePayoutRequest(ctx, repository.CreatePayoutRequestInput{
		OrganizationID:      actor.OrganizationID,
		AmountCents:         input.AmountCents,
		Note:                input.Note,
		RequestedBy:         actor.Email,
		PayableBalanceCents: payable,
		Profile:             profile,
	})
	if err != nil {
		return nil, nil, err
	}
	if row == nil {
		// The outstanding request that refused the write was resolved in the
		// same instant. Nothing was recorded and there is nothing to hand back;
		// asking again is the answer, and it will now succeed.
		return nil, nil, sales.ErrPayoutRequestNotFound()
	}

	// The operators are told only about an ask that was newly RECORDED. A repeat
	// submission is handed the outstanding request back (created=false), and an
	// organizer who pressed twice has not asked twice. The send is best-effort and
	// cannot fail the request — the notice is a courtesy over a record that
	// already stands, and the pending count on the operator navigation is the
	// backstop when it does not arrive (#179, ADR 0026).
	if created {
		s.notifyPayoutRequestSubmitted(ctx, row)
	}

	return &PayoutRequestResult{Request: payoutRequestView(*row), Created: created}, nil, nil
}

// resolvePayoutProfile decides which bank details this request is made against:
// the ones the form carried, or the ones already on file.
//
// A COMPLETE profile is required either way. When none is stored and none was
// supplied, the refusal names all six fields rather than saying "no profile",
// because the organizer's next action is to fill them in and a list of field
// names is what the form can act on. The names and codes are the profile
// editor's own — sales.PayoutProfile.Normalize produces them — so a field
// refused here is refused identically there, and the form needs no second
// mapping.
func (s *Service) resolvePayoutProfile(ctx context.Context, orgID string, supplied *sales.PayoutProfile) (sales.PayoutProfile, []platform.FieldError, error) {
	if supplied != nil {
		return *supplied, nil, nil
	}

	stored, err := s.repo.GetPayoutProfile(ctx, orgID)
	if err != nil {
		return sales.PayoutProfile{}, nil, err
	}
	if stored == nil {
		// An empty profile run through the shared validator, which is what turns
		// "there is no profile" into the six named gaps. Restating the list here
		// would be a second definition of what a complete profile is, and the two
		// would drift the first time a field was added.
		_, fieldErrs := sales.PayoutProfile{}.Normalize()
		return sales.PayoutProfile{}, fieldErrs, nil
	}

	// A stored profile was validated when it was stored and the CHECK constraints
	// have held it complete ever since, so it is used as it stands rather than
	// re-judged. Re-running the validator would mean a tightening of the rules
	// silently locking an Organization out of asking to be paid.
	return stored.Profile, nil, nil
}

// ListPayoutRequests returns the Organization's own request history, newest
// first, for the payouts section it is shown in beside the payout history.
func (s *Service) ListPayoutRequests(ctx context.Context, actor ActorContext) ([]PayoutRequest, error) {
	rows, err := s.repo.ListPayoutRequests(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	out := make([]PayoutRequest, 0, len(rows))
	for _, row := range rows {
		out = append(out, *payoutRequestView(row))
	}
	return out, nil
}

// CancelPayoutRequest withdraws the Organization's outstanding ask.
//
// Cancelling is the only change an Organization can make to a pending request:
// it cannot be edited, only cancelled and re-asked (ADR 0026). That is what keeps
// "outstanding" genuinely singular, and it means an operator is never looking at
// a figure that changed under them.
//
// The two refusals say different things and both are worth saying. A request
// that is not this Organization's — or does not exist — is not found; one that
// has already been paid, declined or cancelled names the state it reached, so
// the organizer learns what happened to it rather than being told to try again.
func (s *Service) CancelPayoutRequest(ctx context.Context, actor ActorContext, requestID string) (*PayoutRequest, error) {
	row, err := s.repo.CancelPayoutRequest(ctx, actor.OrganizationID, requestID, actor.Email, s.now())
	if err != nil {
		return nil, err
	}
	if row != nil {
		return payoutRequestView(*row), nil
	}

	// The compare-and-swap wrote nothing. Which of the two reasons that was is a
	// second read, taken only on the unhappy path so the ordinary cancellation
	// stays one statement.
	existing, err := s.repo.GetPayoutRequest(ctx, actor.OrganizationID, requestID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, sales.ErrPayoutRequestNotFound()
	}
	return nil, sales.ErrPayoutRequestNotPending(existing.Status)
}

// payoutRequestView renders a stored request for the wire. Every entry point
// goes through it, so a submission, a cancellation and the history can never
// describe the same row differently.
func payoutRequestView(row repository.PayoutRequestRow) *PayoutRequest {
	return &PayoutRequest{
		ID:                  row.ID,
		OrganizationID:      row.OrganizationID,
		AmountCents:         row.AmountCents,
		Note:                row.Note,
		Status:              row.Status,
		RequestedBy:         row.RequestedBy,
		RequestedAt:         row.RequestedAt,
		PayableBalanceCents: row.PayableBalanceCents,
		PayoutProfile: PayoutRequestProfile{
			BankName:          row.Profile.BankName,
			AccountType:       row.Profile.AccountType,
			AccountNumber:     row.Profile.AccountNumber,
			AccountHolderName: row.Profile.AccountHolderName,
			TaxIDType:         row.Profile.TaxIDType,
			TaxIDNumber:       row.Profile.TaxIDNumber,
		},
		DeclineReason: row.DeclineReason,
		ResolvedBy:    row.ResolvedBy,
		ResolvedAt:    row.ResolvedAt,
		PayoutID:      row.PayoutID,
	}
}
