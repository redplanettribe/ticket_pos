package service

import "context"

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
// Everything here is read-only. Fulfilling a request and declining one are #177;
// what this file builds is the ability to see the backlog and decide.

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
	Request      PayoutRequest `json:"request"`
	Organization Organization  `json:"organization"`
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
