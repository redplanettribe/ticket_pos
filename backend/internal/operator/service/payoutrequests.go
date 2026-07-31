package service

import (
	"context"
	"time"

	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// The Payout Request queue: who is waiting to be paid (#176, ADR 0026).
//
// This is the operator surface's SECOND cross-Organization view, and its
// justification is not the first's. Sale lookup crosses Organizations because a
// support thread carries a reference and nothing else, so there is no
// Organization to scope by. This one crosses them because the request IS the
// reason to open the dashboard: the per-Organization layout works everywhere
// else precisely because an operator arrives already knowing which Organization
// they care about, and a payout request inverts that.
//
// The reads come first and the two writes follow them at the foot of the file:
// seeing the backlog and deciding (#176), then answering (#177). The order is
// the operator's own — nobody fulfils a request they have not opened — and the
// writes are deliberately the thinnest things here, because everything that
// makes fulfilment safe is a transaction in the sales repository.

// PayoutRequestQueueItem is one outstanding ask with whose it is.
//
// The Organization is a separate object rather than three fields on the request,
// exactly as it is on the sale lookup: it is a different module's answer, and
// keeping the seam visible is what stops sales growing an opinion about what an
// Organization is called.
type PayoutRequestQueueItem struct {
	Request      PayoutRequestSummary `json:"request"`
	Organization Organization         `json:"organization"`
}

// PayoutRequestQueue is the ADR-0006 nested envelope for the queue.
type PayoutRequestQueue struct {
	Data       []PayoutRequestQueueItem `json:"data"`
	Pagination PageInfo                 `json:"pagination"`
}

// PendingPayoutRequestCount is the badge on the operator navigation: how many
// Organizations are waiting for an answer, across the whole platform.
//
// It is its own endpoint rather than a field on the summary because it is read
// on every page of the staff app an operator opens, and the summary aggregates
// the platform's entire revenue to answer a different question.
type PendingPayoutRequestCount struct {
	PendingCount int `json:"pending_count"`
}

// PayoutRequestDetail is everything an operator needs to execute one transfer:
// the ask, whose it is, the snapshot bank details IN FULL, and both readings of
// the money.
//
// The two Payable Balances are the point of the screen. The snapshot on the
// request says what the Organization could have asked for when it asked; the
// live figure says what it could ask for now, and the gap between them is
// exactly what tells an Organization that asked for all it had from one that
// asked for four times as much. The cap was checked once, at request time, and
// is deliberately never checked again: the operator standing at the bank is the
// party who decides what to do about a figure that has moved (ADR 0026).
//
// The Withdrawable Balance rides along because the pair only makes sense
// together — the smaller number alone provokes the question the larger one
// answers.
type PayoutRequestDetail struct {
	Request      PayoutRequestWhole `json:"request"`
	Organization Organization       `json:"organization"`
	// The LIVE figures, read now, both signed and never clamped.
	WithdrawableBalanceCents int `json:"withdrawable_balance_cents"`
	PayableBalanceCents      int `json:"payable_balance_cents"`
}

// PayoutRequestQueue returns one page of every outstanding Payout Request on the
// platform, oldest first (ADR 0006 for the envelope; the ordering deliberately
// inverts the house convention — see the repository).
//
// The Organizations are resolved in ONE batch read after the page is known,
// exactly as the Organization list resolves its Event counts and balances: fifty
// rows must not become fifty round trips on the screen an operator opens most.
//
// A request whose Organization is missing from that batch is dropped rather than
// rendered as a blank. It cannot happen — the requests table cascades with
// `organizations` — and if it ever does, a queue row nobody can pay is worse
// than a queue that is one shorter, while the total stays the database's answer
// about how many people are waiting.
func (s *Service) PayoutRequestQueue(ctx context.Context, page, pageSize int) (*PayoutRequestQueue, error) {
	requests, total, err := s.money.PendingPayoutRequests(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}

	orgIDs := make([]string, 0, len(requests))
	for _, request := range requests {
		orgIDs = append(orgIDs, request.OrganizationID)
	}
	organizations, err := s.organizations.OrganizationsForOperator(ctx, orgIDs)
	if err != nil {
		return nil, err
	}

	items := make([]PayoutRequestQueueItem, 0, len(requests))
	for _, request := range requests {
		org, ok := organizations[request.OrganizationID]
		if !ok {
			continue
		}
		items = append(items, PayoutRequestQueueItem{Request: request, Organization: org})
	}

	return &PayoutRequestQueue{
		Data: items,
		Pagination: PageInfo{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages(total, pageSize),
		},
	}, nil
}

// PendingPayoutRequestCount returns how many requests are outstanding across the
// platform, for the navigation badge.
func (s *Service) PendingPayoutRequestCount(ctx context.Context) (*PendingPayoutRequestCount, error) {
	count, err := s.money.PendingPayoutRequestCount(ctx)
	if err != nil {
		return nil, err
	}
	return &PendingPayoutRequestCount{PendingCount: count}, nil
}

// GetPayoutRequest returns one Payout Request in full, with its Organization and
// that Organization's live balances.
//
// The request is fetched first so an unknown id is a 404 before anything else is
// read, and the Organization comes from the module that owns Organizations by
// the id the request carries — the same read the rest of this surface uses, so
// an operator meets one description of an Organization everywhere.
func (s *Service) GetPayoutRequest(ctx context.Context, requestID string) (*PayoutRequestDetail, error) {
	request, err := s.money.PayoutRequestForOperator(ctx, requestID)
	if err != nil {
		return nil, err
	}
	org, err := s.organizations.GetOrganizationForOperator(ctx, request.OrganizationID)
	if err != nil {
		return nil, err
	}
	balances, err := s.money.OrganizationBalances(ctx, request.OrganizationID)
	if err != nil {
		return nil, err
	}
	return &PayoutRequestDetail{
		Request:                  *request,
		Organization:             *org,
		WithdrawableBalanceCents: balances.WithdrawableBalanceCents,
		PayableBalanceCents:      balances.PayableBalanceCents,
	}, nil
}

// Answering a request (#177, ADR 0026). Both operations are one call into sales
// and nothing else: the composition this service exists for — the ask beside its
// Organization and its live balances — belongs to the detail view that precedes
// the action, and the operator who got here came through that door.
//
// The operator's own email is not read here either. It is taken from the Staff
// Session by the handler and passed down, exactly as the direct record-payout
// path and the Operator Reversal take theirs: who asserted a money fact must not
// be something a caller can claim (ADR 0015, ADR 0019).

// FulfilPayoutRequestInput is an operator answering with money: what actually
// left the bank, the day it did, an optional note, and who is saying so.
//
// The amount is pre-filled from the request on the form and may be overwritten,
// which is a deliberate departure from ADR 0019's rule against pre-filling a
// money field. The distinction is whose number it is: a refund amount is an
// assertion only the operator can make about a transfer whose size they chose,
// while a payout amount is a figure the Organization already stated and the
// operator agreed to by transferring it. Whatever arrives here is what MOVED,
// and the request keeps what was ASKED.
type FulfilPayoutRequestInput struct {
	AmountCents int
	PaidAt      time.Time
	Note        *string
	Operator    string
}

// DeclinePayoutRequestInput is an operator answering without money: why, and who
// said it. There is no decline without a reason — the asker is shown it, and a
// queue that swallowed requests silently would generate the support thread it
// was built to prevent.
type DeclinePayoutRequestInput struct {
	Reason   string
	Operator string
}

// MarkPayoutRequestProcessingInput is an operator answering with a transfer they
// cannot yet confirm: whatever reference the bank handed back, and who submitted
// it (#184).
//
// The reference is optional and nil is ordinary. The instant is not here at all
// — it is the service's injected clock, taken when the row is written, because
// it is the figure the 72-hour stale flag is measured against and an operator
// must not be its source.
type MarkPayoutRequestProcessingInput struct {
	Reference *string
	Operator  string
}

// MarkPayoutRequestFailedInput is an operator recording the bank's rejection:
// why, and who is saying so (#185).
//
// The reason is required and is the entire content of the news. "Failed" tells
// an organizer nothing; "the account number was rejected" tells them what to go
// and fix — which is their Payout Profile, since this request's copy of it is a
// frozen snapshot and cannot be edited. The organizer's next move is a FRESH
// ask, never a retry of this one.
type MarkPayoutRequestFailedInput struct {
	Reason   string
	Operator string
}

// FulfilPayoutRequest records the Payout and marks the request paid, in one
// transaction, so there is no second step to forget.
//
// The Payout it produces is an ordinary Payout: the Organization reads it on its
// own payouts page, and its Withdrawable Balance drops by the amount, neither of
// them aware a request was involved. Nothing about the request is consulted as a
// limit — not the amount asked for, not the Payable Balance snapshot beside it,
// not the live figure — because recording a settlement is unconditional and the
// operator at the bank is the party who decides (ADR 0015, ADR 0026).
func (s *Service) FulfilPayoutRequest(ctx context.Context, requestID string, in FulfilPayoutRequestInput) (*PayoutFulfilment, error) {
	return s.money.FulfilPayoutRequest(ctx, requestID, salessvc.FulfilPayoutRequestInput{
		AmountCents: in.AmountCents,
		PaidAt:      in.PaidAt,
		Note:        in.Note,
		Operator:    in.Operator,
	})
}

// MarkPayoutRequestProcessing records that the transfer has been submitted and
// the bank has not confirmed it, leaving the request outstanding.
//
// NOTHING IS RECORDED IN THE LEDGER. This is the one write on this surface that
// answers a request without producing a Payout, and that is the point: a Payout
// is money that moved (ADR 0014), and a PayPhone transfer can take 48 hours and
// can come back rejected. The request keeps the Organization's single slot while
// the bank has it and nobody has confirmed it, so nobody can ask again for the
// same money (ADR 0026
// amendment).
func (s *Service) MarkPayoutRequestProcessing(ctx context.Context, requestID string, in MarkPayoutRequestProcessingInput) (*PayoutRequestWhole, error) {
	return s.money.MarkPayoutRequestProcessing(ctx, requestID, salessvc.MarkPayoutRequestProcessingInput{
		Reference: in.Reference,
		Operator:  in.Operator,
	})
}

// MarkPayoutRequestFailed records that the bank rejected the transfer, ending
// the request with a reason the Organization reads.
//
// NOTHING IS UNDONE IN THE LEDGER, because nothing was ever written to it: the
// transfer was recorded on the request and never as a Payout, which is the point
// of the `processing` state (ADR 0014, ADR 0026 amendment). This is the second
// write on this surface that answers a request without producing a Payout, and
// the only one that ENDS a request without one.
//
// Only a `processing` request can fail — a transfer nobody submitted cannot have
// bounced — and the failure is terminal. The Organization's answer to it is a
// fresh ask against a corrected Payout Profile, which the widened partial unique
// index permits the moment this lands.
func (s *Service) MarkPayoutRequestFailed(ctx context.Context, requestID string, in MarkPayoutRequestFailedInput) (*PayoutRequestWhole, error) {
	return s.money.MarkPayoutRequestFailed(ctx, requestID, salessvc.MarkPayoutRequestFailedInput{
		Reason:   in.Reason,
		Operator: in.Operator,
	})
}

// DeclinePayoutRequest refuses the ask with a reason the Organization reads, and
// frees it to ask again.
func (s *Service) DeclinePayoutRequest(ctx context.Context, requestID string, in DeclinePayoutRequestInput) (*PayoutRequestWhole, error) {
	return s.money.DeclinePayoutRequest(ctx, requestID, salessvc.DeclinePayoutRequestInput{
		Reason:   in.Reason,
		Operator: in.Operator,
	})
}
