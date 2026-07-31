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
	ResolutionReason *string    `json:"resolution_reason"`
	ResolvedBy       *string    `json:"resolved_by"`
	ResolvedAt       *time.Time `json:"resolved_at"`
	PayoutID         *string    `json:"payout_id"`
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
		ResolutionReason: row.ResolutionReason,
		ResolvedBy:       row.ResolvedBy,
		ResolvedAt:       row.ResolvedAt,
		PayoutID:         row.PayoutID,
	}
}

// Answering a Payout Request: the operator transfers the money by hand and then
// records it, and that one action also ends the ask (#177, ADR 0026).
//
// There is deliberately no `approved` state between the two. An operator
// transfers and then records, exactly as they always have; an approved-but-unpaid
// request would be a debt with a state name, needing its own chasing, its own
// notifications and its own aging report.

// FulfilPayoutRequestInput is an operator answering with money.
//
// It is the SAME three facts RecordPayoutInput carries, because the Payout this
// produces is the same Payout — the request adds a target to aim at, not a
// different kind of settlement. Operator stamps both the Payout's recorded_by
// and the request's resolved_by, and comes from the Staff Session rather than
// from any request body (ADR 0015, ADR 0019).
//
// AmountCents is what MOVED and is not required to equal what was asked. An
// operator who transfers less records the smaller figure; the ask keeps the
// larger one, and the gap between them is visible on the request forever.
// Partial fulfilment needs no model of its own (ADR 0026).
type FulfilPayoutRequestInput struct {
	AmountCents int
	PaidAt      time.Time
	Note        *string
	Operator    string
}

// FulfilledPayoutRequest is what an operator is handed after answering with
// money: the Payout that is now in the ledger, and the request as it now stands.
//
// Both, because they are two different facts and the operator needs each. The
// Payout is the money — an ordinary settlement, indistinguishable from a
// directly recorded one and readable by the Organization on its own payouts page
// — while the request is the queue's answer, carrying the payout_id that is the
// single link between the two.
type FulfilledPayoutRequest struct {
	Payout  OperatorPayout `json:"payout"`
	Request PayoutRequest  `json:"request"`
}

// MarkPayoutRequestProcessingInput is an operator saying they submitted the
// transfer and cannot yet confirm it: whatever reference the bank handed back,
// and who submitted it.
//
// There is no instant here. The moment of submission is the service's injected
// clock, taken at the moment of writing, because an operator cannot be trusted
// to be the source of a timestamp the 72-hour stale flag is computed against —
// and because every other money instant in this system comes from the same clock
// (ADR 0026). Operator comes from the Staff Session and never from a body
// (ADR 0015).
//
// Reference is a pointer and nil is ordinary: PayPhone does not always hand one
// back synchronously, and a required reference an operator cannot fill is a
// field they will type "-" into.
type MarkPayoutRequestProcessingInput struct {
	Reference *string
	Operator  string
}

// MarkPayoutRequestFailedInput is an operator recording that the bank sent the
// transfer back: why, and who is saying so (#185).
//
// The reason is a plain string rather than a pointer for exactly the reason the
// decline's is, and the argument is if anything stronger. An organizer told only
// that their transfer "failed" learns nothing they can act on, while "the
// account number was rejected" is also the instruction — go and correct the
// Payout Profile, because this request's copy of it is frozen and cannot be
// edited. It is required by the handler, required here, and enforced by a CHECK
// under both (migration 046, ADR 0026 amendment).
//
// There is no instant here, and no operator identity a caller could supply. Both
// come from the same places every other money fact's do: the injected clock and
// the Staff Session (ADR 0015, ADR 0026).
type MarkPayoutRequestFailedInput struct {
	Reason   string
	Operator string
}

// DeclinePayoutRequestInput is an operator answering without money: why, and who
// said it.
//
// The reason is a plain string rather than a pointer because there is no such
// thing as a decline without one. A queue that swallows requests silently
// generates the support thread it was built to prevent, so the reason is
// required by the handler, required here, and enforced by a CHECK constraint
// under both (ADR 0026).
type DeclinePayoutRequestInput struct {
	Reason   string
	Operator string
}

// FulfilPayoutRequest records the Payout and marks the request paid, in one
// transaction, or refuses because somebody already answered it.
//
// The refusal path is a SECOND read, taken only when the compare-and-swap wrote
// nothing, so the ordinary fulfilment stays one transaction and pays nothing for
// a case that almost never happens. What that read is for is the sentence the
// operator gets: naming the current state and who reached it first is what turns
// a generic conflict into a person to go and ask, and — because the CAS stops a
// second RECORD and not a second TRANSFER — what makes it safe to tell an
// operator who genuinely wired the money to record the Payout directly
// (sales.ErrPayoutRequestAlreadyResolved).
//
// The request is read BEFORE anything is attempted as well, and for a different
// reason: the Payout needs an organization_id, and the request is where it comes
// from. An unknown id is therefore a 404 before the ledger is touched at all.
//
// Neither balance is consulted, in either direction. Recording a Payout is
// unconditional (ADR 0015), and the cap that bound the Organization was checked
// once, when it asked, and is deliberately never checked again: the operator
// standing at the bank is the party who decides what to do about a figure that
// has moved (ADR 0026).
func (s *Service) FulfilPayoutRequest(ctx context.Context, requestID string, in FulfilPayoutRequestInput) (*FulfilledPayoutRequest, error) {
	existing, err := s.lookUpPayoutRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}

	payout, request, err := s.repo.FulfilPayoutRequest(ctx, repository.FulfilPayoutRequestInput{
		RequestID:      existing.ID,
		OrganizationID: existing.OrganizationID,
		AmountCents:    in.AmountCents,
		PaidAt:         in.PaidAt,
		Note:           in.Note,
		Operator:       in.Operator,
		Now:            s.now(),
	})
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, s.payoutRequestAlreadyResolved(ctx, requestID, existing)
	}

	// Told AFTER the transaction committed, and told best-effort. The Payout is
	// in the ledger and the request is paid; a Resend outage that failed this call
	// would tell an operator who has already wired the money by hand that they had
	// not, which is the worst outcome this path can produce (#179, ADR 0026).
	//
	// What was PAID is passed beside what was asked, because they are allowed to
	// differ and this email is the only place the organizer is told so.
	s.notifyPayoutRequestPaid(ctx, request, payout.AmountCents)

	return &FulfilledPayoutRequest{
		Payout:  toOperatorPayout(*payout),
		Request: *payoutRequestView(*request),
	}, nil
}

// MarkPayoutRequestProcessing records that the transfer has been submitted and
// the bank has not confirmed it, or refuses because the request was no longer
// pending (#184, ADR 0026 amendment).
//
// NO PAYOUT IS RECORDED HERE. That is the whole reason the state exists: a
// Payout is money that MOVED (ADR 0014), and a transfer PayPhone may send back
// in 48 hours has not moved anything yet. The operator says what they actually
// did — submitted a transfer — and the ledger stays untouched until somebody
// finds out what the bank did with it. A future reader tempted to write the
// Payout here, "so the balance is right sooner", would be buying two days of
// accuracy with a voided-Payout concept every balance in the system would then
// have to understand.
//
// It is the same shape as the decline below: look the request up so an unknown
// id is a 404 before anything is attempted, compare-and-swap, and read back on
// zero rows to say who got there first. No balance is consulted — none is at any
// transition, and it matters more here than anywhere, because a request can now
// sit outstanding for three days while the Payable Balance moves under it
// (ADR 0026).
func (s *Service) MarkPayoutRequestProcessing(ctx context.Context, requestID string, in MarkPayoutRequestProcessingInput) (*PayoutRequest, error) {
	existing, err := s.lookUpPayoutRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}

	row, err := s.repo.MarkPayoutRequestProcessing(ctx, repository.MarkPayoutRequestProcessingInput{
		RequestID: existing.ID,
		Operator:  in.Operator,
		Reference: in.Reference,
		Now:       s.now(),
	})
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, s.payoutRequestAlreadyResolved(ctx, requestID, existing)
	}

	return payoutRequestView(*row), nil
}

// MarkPayoutRequestFailed records that the bank sent the transfer back, with the
// reason the organizer reads, or refuses because no transfer was submitted for
// this request (#185, ADR 0026 amendment).
//
// NO PAYOUT IS TOUCHED, CREATED OR UNDONE. There is nothing to unwind precisely
// because marking the request `processing` wrote no ledger row: the money never
// moved, and the books never said it did. This is the payoff of the decision the
// `processing` state was built around, and a reader who finds themselves wanting
// to negate a Payout here should find there is none to negate.
//
// It is the same shape as the decline below — look the request up so an unknown
// id is a 404, compare-and-swap, read back on zero rows — with one difference
// that is the whole of the ticket: THE GUARD IS `processing`, NOT `pending`, so
// the refusal a lost swap earns is its own and not the decline's.
//
// The Organization is freed to ask again by the transition itself, and by
// nothing else here: the partial unique index counts only `('pending',
// 'processing')` (migration 045), so a `failed` request stops occupying the slot
// the instant it is written. That is the whole retry story — a `failed` request
// is TERMINAL and is never reopened, because the bank details it carries are a
// frozen snapshot and the commonest failure is unfixable inside the request it
// happened to.
func (s *Service) MarkPayoutRequestFailed(ctx context.Context, requestID string, in MarkPayoutRequestFailedInput) (*PayoutRequest, error) {
	existing, err := s.lookUpPayoutRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}

	row, err := s.repo.MarkPayoutRequestFailed(ctx, existing.ID, in.Reason, in.Operator, s.now())
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, s.payoutRequestNotProcessing(ctx, requestID, existing)
	}

	return payoutRequestView(*row), nil
}

// payoutRequestNotProcessing builds the refusal a failed swap on the `failed`
// transition earns.
//
// It re-reads for the same reason payoutRequestAlreadyResolved does — the state
// it names must be the one that actually stands rather than the one read before
// the attempt — and falls back to the pre-attempt row when that read fails, so a
// database hiccup cannot swallow the sentence.
//
// TWO REFUSALS, because two different things went wrong and the operator's next
// move differs:
//
//   - The request is still `pending`. Nobody submitted a transfer, so nothing
//     bounced, and telling them it was "already resolved" would be false. What
//     they most likely meant is a decline, and the message says so.
//   - The request has ENDED — paid, declined, cancelled, or already failed. Then
//     somebody got there first and the house refusal is exactly right, naming the
//     state and who reached it. This is also what makes `failed` terminal: a
//     second failure marking lands here and is told the request already failed.
func (s *Service) payoutRequestNotProcessing(ctx context.Context, requestID string, before *repository.PayoutRequestRow) error {
	current, err := s.repo.GetPayoutRequestByID(ctx, requestID)
	if err != nil || current == nil {
		current = before
	}
	if current.Status == sales.PayoutRequestPending {
		return sales.ErrPayoutRequestTransferNotSubmitted(current.Status)
	}
	return sales.ErrPayoutRequestAlreadyResolved(current.Status, current.ResolvedBy)
}

// DeclinePayoutRequest refuses the ask with a reason its asker can read, and
// frees the Organization to ask again.
//
// A decline is a "not this" rather than a lockout: it ends this request, and the
// partial unique index that keeps `pending` singular counts only pending rows,
// so the Organization may submit a new one immediately (migration 043). Nothing
// about being declined once bears on the next ask.
func (s *Service) DeclinePayoutRequest(ctx context.Context, requestID string, in DeclinePayoutRequestInput) (*PayoutRequest, error) {
	existing, err := s.lookUpPayoutRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}

	row, err := s.repo.DeclinePayoutRequest(ctx, existing.ID, in.Reason, in.Operator, s.now())
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, s.payoutRequestAlreadyResolved(ctx, requestID, existing)
	}

	// The reason travels to the asker, best-effort like every other notice here.
	// A decline the Organization never hears about is a request that looks
	// ignored, which is the support thread requiring a reason was meant to
	// prevent — but a delivery failure still leaves the decline standing, and the
	// reason readable on the Organization's own payouts page (#179, ADR 0026).
	s.notifyPayoutRequestDeclined(ctx, row)

	return payoutRequestView(*row), nil
}

// lookUpPayoutRequest reads the request an operator is about to answer, whatever
// Organization it belongs to, and turns "no such request" into a 404 before
// anything is written.
//
// A malformed id is answered as a missing request rather than as a database
// failure, exactly as PayoutRequestForOperator does: to the caller both mean "no
// such request at this path".
func (s *Service) lookUpPayoutRequest(ctx context.Context, requestID string) (*repository.PayoutRequestRow, error) {
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
	return row, nil
}

// payoutRequestAlreadyResolved builds the refusal a lost compare-and-swap earns,
// re-reading the request so the state and the person it names are the ones that
// actually stand rather than the ones read before the attempt.
//
// The pre-attempt row is the fallback for the read failing. It is stale by
// construction — if it were current the CAS would have succeeded — but a refusal
// naming a slightly older answer is better than a database error swallowing the
// instruction to record the Payout directly, which is the one sentence on this
// path that must always be delivered.
//
// A `processing` request gets its own refusal, and the branch is not cosmetic. A
// request whose transfer is in flight HAS NOT BEEN RESOLVED — nothing has ended,
// nobody has judged it, and resolved_by is null, so the resolved-by-whom sentence
// would come out naming "another operator" for an event that did not happen.
// Worse, that sentence tells the reader to record the Payout directly, which
// against a transfer the bank has not confirmed is precisely the ledger entry
// this whole state exists to prevent. Who they need is the operator who SUBMITTED
// the transfer (ADR 0026 amendment).
//
// Fulfilment never arrives here for a `processing` request — its guard accepts
// one — so in practice this branch answers a decline, and a second operator
// marking an already-submitted request processing.
func (s *Service) payoutRequestAlreadyResolved(ctx context.Context, requestID string, before *repository.PayoutRequestRow) error {
	current, err := s.repo.GetPayoutRequestByID(ctx, requestID)
	if err != nil || current == nil {
		current = before
	}
	if current.Status == sales.PayoutRequestProcessing {
		return sales.ErrPayoutRequestTransferAlreadySubmitted(current.TransferSubmittedBy)
	}
	return sales.ErrPayoutRequestAlreadyResolved(current.Status, current.ResolvedBy)
}
