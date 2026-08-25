// Package service implements the Platform Operator's reads and its one write.
//
// It owns no tables. Every figure it returns is read through the module that
// owns the data — identity for Organizations and the operator allowlist,
// catalog for Events, sales for money — each exposing an operator-scoped
// operation (ADR 0015). This service is the only place those three are composed
// into the Operator Dashboard's payloads, so the operator surface can grow
// without any of them learning about the others.
package service

import (
	"context"
	"time"

	catalogsvc "github.com/peter/ticket_pos/backend/internal/catalog/service"
	identitysvc "github.com/peter/ticket_pos/backend/internal/identity/service"
	salessvc "github.com/peter/ticket_pos/backend/internal/sales/service"
)

// Organizations is what the operator surface needs from identity: the platform's
// Organizations, unscoped by Membership.
type Organizations interface {
	ListOrganizationsForOperator(ctx context.Context, page, pageSize int) ([]identitysvc.OperatorOrganization, int, error)
	// GetOrganizationForOperator returns ORGANIZATION_NOT_FOUND for an unknown
	// (or malformed) id, which the handler maps to 404.
	GetOrganizationForOperator(ctx context.Context, orgID string) (*identitysvc.OperatorOrganization, error)
	// OrganizationsForOperator resolves many Organizations at once, keyed by id,
	// for a cross-Organization list whose rows arrive from another module (#176).
	// Ids that name nothing are absent from the map rather than an error.
	OrganizationsForOperator(ctx context.Context, orgIDs []string) (map[string]identitysvc.OperatorOrganization, error)
}

// Events is what the operator surface needs from catalog: what each
// Organization is running.
type Events interface {
	ListOrganizationEventsForOperator(ctx context.Context, orgID string) ([]catalogsvc.OperatorEvent, error)
	CountEventsByOrganization(ctx context.Context, orgIDs []string) (map[string]int, error)
	// ListEventTicketQuestionsForOperator is every Ticket Question of one
	// Event, every review state and retired ones included (#410, ADR 0056).
	// EVENT_NOT_FOUND for an unknown or malformed id.
	ListEventTicketQuestionsForOperator(ctx context.Context, eventID string) ([]catalogsvc.OperatorTicketQuestion, error)
	// RevokeTicketQuestion is the Revocation: retires an approved question with
	// the reason the Organization is told, and mails its Org Admins. It answers
	// TICKET_QUESTION_NOT_FOUND, TICKET_QUESTION_NOT_APPROVED for a question
	// with no approval to take back, and TICKET_QUESTION_RETIRED for one
	// already retired.
	RevokeTicketQuestion(ctx context.Context, questionID string, input catalogsvc.RevokeTicketQuestionInput) (*catalogsvc.TicketQuestionView, error)
	// The Question Review queue and its answer (#407, ADR 0056). Every read
	// lapses Reviews whose Event has started first, so none of them ever
	// lists one. AnswerQuestionReview answers QUESTION_REVIEW_NOT_FOUND,
	// QUESTION_REVIEW_NOT_OUTSTANDING with the state reached, and the three
	// body refusals that name an item.
	ListOutstandingQuestionReviews(ctx context.Context, page, pageSize int) ([]catalogsvc.OperatorQuestionReview, int, error)
	CountOutstandingQuestionReviews(ctx context.Context) (int, error)
	GetQuestionReviewForOperator(ctx context.Context, reviewID string) (*catalogsvc.OperatorQuestionReview, error)
	AnswerQuestionReview(ctx context.Context, reviewID string, input catalogsvc.AnswerQuestionReviewInput) (*catalogsvc.OperatorQuestionReview, error)
}

// Money is what the operator surface needs from sales: the Withdrawable
// Balances, the platform's own revenue, the payout history it appends to, the
// queue of Organizations waiting to be paid, and the one Ticket Sale a support
// thread names by its Sale Confirmation reference — which is a money question
// too, since what an operator does with it next is decide what happened to
// somebody's payment.
type Money interface {
	// OrganizationBalances returns both money figures for one Organization: the
	// Withdrawable Balance and the Payable Balance, each signed (ADR 0026).
	OrganizationBalances(ctx context.Context, orgID string) (salessvc.OrganizationBalances, error)
	WithdrawableBalances(ctx context.Context, orgIDs []string) (map[string]int, error)
	PlatformTotals(ctx context.Context) ([]salessvc.CurrencyTotals, error)
	OperatorPayoutHistory(ctx context.Context, orgID string) ([]salessvc.OperatorPayout, error)
	RecordPayout(ctx context.Context, orgID string, input salessvc.RecordPayoutInput) (*salessvc.OperatorPayout, error)
	// PendingPayoutRequests returns one page of every outstanding Payout Request
	// on the platform, OLDEST FIRST, plus the unpaginated total (#176). The
	// account numbers on these rows are already masked by sales; nothing on this
	// side may unmask them, because nothing on this side has the digits.
	PendingPayoutRequests(ctx context.Context, page, pageSize int) ([]salessvc.OperatorPayoutRequest, int, error)
	// PendingPayoutRequestCount is the same backlog as one number, for the badge
	// on the operator navigation.
	PendingPayoutRequestCount(ctx context.Context) (int, error)
	// OrganizationPayoutRequestHistory is one Organization's asks, newest first
	// and masked, for the drill-down where they sit beside the payout history.
	OrganizationPayoutRequestHistory(ctx context.Context, orgID string) ([]salessvc.OperatorPayoutRequest, error)
	// PayoutRequestForOperator returns one request WHOLE — snapshot bank details
	// included — and PAYOUT_REQUEST_NOT_FOUND for an unknown (or malformed) id,
	// which the handler maps to 404.
	PayoutRequestForOperator(ctx context.Context, requestID string) (*salessvc.OperatorPayoutRequestWhole, error)
	// FulfilPayoutRequest records the Payout and marks the request paid in ONE
	// transaction (#177). It answers PAYOUT_REQUEST_NOT_FOUND for an unknown or
	// malformed id and PAYOUT_REQUEST_ALREADY_RESOLVED when somebody answered it
	// first — in which case NOTHING was written, the Payout included, because the
	// compare-and-swap rolls the whole transaction back (ADR 0026).
	FulfilPayoutRequest(ctx context.Context, requestID string, input salessvc.FulfilPayoutRequestInput) (*salessvc.FulfilledPayoutRequest, error)
	// MarkPayoutRequestProcessing records that the operator submitted the bank
	// transfer and cannot yet confirm it (#184). It writes NO Payout — a Payout
	// is money that moved, and this is money that has been sent — and answers
	// PAYOUT_REQUEST_TRANSFER_ALREADY_SUBMITTED when another operator submitted
	// one first, or PAYOUT_REQUEST_ALREADY_RESOLVED when the request had ended.
	MarkPayoutRequestProcessing(ctx context.Context, requestID string, input salessvc.MarkPayoutRequestProcessingInput) (*salessvc.OperatorPayoutRequestWhole, error)
	// MarkPayoutRequestFailed records that the bank sent the transfer back, with
	// the reason the organizer reads (#185). It writes no Payout and undoes none
	// — there is none, because marking the request processing wrote nothing to
	// the ledger. Only a `processing` request may fail: one nobody submitted a
	// transfer for answers PAYOUT_REQUEST_TRANSFER_NOT_SUBMITTED, and one that
	// had already ended answers PAYOUT_REQUEST_ALREADY_RESOLVED.
	MarkPayoutRequestFailed(ctx context.Context, requestID string, input salessvc.MarkPayoutRequestFailedInput) (*salessvc.OperatorPayoutRequestWhole, error)
	// DeclinePayoutRequest refuses the ask with a reason its asker can read, and
	// answers the same two refusals. A decline moves no money and frees the
	// Organization to ask again.
	DeclinePayoutRequest(ctx context.Context, requestID string, input salessvc.DeclinePayoutRequestInput) (*salessvc.OperatorPayoutRequestWhole, error)
	// SaleByConfirmationRef returns TICKET_SALE_NOT_FOUND when no Ticket Sale on
	// the platform carries the reference, which the handler maps to 404.
	SaleByConfirmationRef(ctx context.Context, confirmationRef string) (*salessvc.OperatorSale, error)
	// ReverseSaleAsOperator records that the operator refunded the buyer
	// off-platform and marks the Ticket Sale reversed (#125). It never calls a
	// Payment Provider. It refuses a sale that is not an Online Sale
	// (SALE_NOT_REVERSIBLE), one already reversed (SALE_ALREADY_REVERSED), and a
	// refund larger than the sale collected (REFUNDED_AMOUNT_EXCEEDS_COLLECTED).
	ReverseSaleAsOperator(ctx context.Context, confirmationRef string, input salessvc.OperatorReversalInput) (*salessvc.OperatorReversalResult, error)
	// ReAddressSaleAsOperator records a Sale Re-addressing against the Ticket
	// Sale a reference names and mails the corrected address its Re-addressing
	// Link (#420, ADR 0058). It never calls a Payment Provider. It refuses a
	// sale that is not an Online Sale (SALE_NOT_RE_ADDRESSABLE), a reversed one
	// (SALE_ALREADY_REVERSED), one whose Event has started
	// (RE_ADDRESSING_EVENT_STARTED), a correction to the address the sale
	// already carries (RE_ADDRESSING_SAME_ADDRESS), and a sale with a pending
	// re-addressing already (RE_ADDRESSING_ALREADY_PENDING).
	ReAddressSaleAsOperator(ctx context.Context, confirmationRef string, input salessvc.ReAddressSaleInput) (*salessvc.SaleReAddressing, error)
	// SaleReAddressings is the lookup's `re_addressing` block for one sale:
	// the pending record or null, and the accepted history.
	SaleReAddressings(ctx context.Context, sale *salessvc.OperatorSale) (*salessvc.SaleReAddressingBlock, error)
}

// Service implements the Operator Dashboard's operations.
type Service struct {
	organizations Organizations
	events        Events
	money         Money
	// consents is the Consent Withdrawal surface's half, and the first thing
	// this service composes that is about a person rather than about money
	// (#271). See consent.go.
	consents Consents
}

// New returns an operator service over the four owning modules.
func New(organizations Organizations, events Events, money Money, consents Consents) *Service {
	return &Service{organizations: organizations, events: events, money: money, consents: consents}
}

// PlatformSummary is the Operator Dashboard's headline: the platform's money,
// one row per currency and never summed across them.
type PlatformSummary struct {
	Totals []PlatformTotals `json:"totals"`
}

// OrganizationListItem is one row of the operator's Organization list: who they
// are, how much they run, and what the platform owes them.
type OrganizationListItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Currency string `json:"currency"`
	// EventsCount counts the Organization's Events in every status.
	EventsCount int `json:"events_count"`
	// WithdrawableBalanceCents is signed: negative means the Organization owes
	// the platform after a sale was reversed post-settlement, and the list says
	// so rather than clamping it to zero.
	WithdrawableBalanceCents int `json:"withdrawable_balance_cents"`
}

// OrganizationList is the ADR-0006 nested envelope for the Organization list.
type OrganizationList struct {
	Data       []OrganizationListItem `json:"data"`
	Pagination PageInfo               `json:"pagination"`
}

// OrganizationDetail is the operator's drill-down into one Organization: what it
// is, what the platform owes it, what of that has cleared, what it runs, and how
// it has been settled.
type OrganizationDetail struct {
	Organization             Organization `json:"organization"`
	WithdrawableBalanceCents int          `json:"withdrawable_balance_cents"`
	// PayableBalanceCents is the part of the Withdrawable Balance that has
	// cleared — sales recorded before today in Ecuador with no Reversal Request
	// still open (ADR 0026). Signed, never clamped, and never larger than the
	// figure above it. It is shown to the operator and gates nothing they do.
	PayableBalanceCents int      `json:"payable_balance_cents"`
	Events              []Event  `json:"events"`
	Payouts             []Payout `json:"payouts"`
	// PayoutRequests is this Organization's own request history, newest first and
	// with its account numbers masked (#176). It sits beside the payout history
	// because that is the question it answers: has this Organization been paid
	// recently, and are they asking again? (ADR 0026)
	PayoutRequests []PayoutRequestSummary `json:"payout_requests"`
}

// RecordPayoutInput is a validated record-payout request: the amount is already
// known positive and the date already parsed by the handler.
type RecordPayoutInput struct {
	AmountCents int
	PaidAt      time.Time
	Note        *string
	RecordedBy  string
}

// Summary returns the platform's accumulated Platform Fees and Fee IVA, and what
// it owes, grouped by currency.
func (s *Service) Summary(ctx context.Context) (*PlatformSummary, error) {
	totals, err := s.money.PlatformTotals(ctx)
	if err != nil {
		return nil, err
	}
	return &PlatformSummary{Totals: totals}, nil
}

// ListOrganizations returns one page of every Organization with its Event count
// and Withdrawable Balance.
func (s *Service) ListOrganizations(ctx context.Context, page, pageSize int) (*OrganizationList, error) {
	orgs, total, err := s.organizations.ListOrganizationsForOperator(ctx, page, pageSize)
	if err != nil {
		return nil, err
	}

	orgIDs := make([]string, 0, len(orgs))
	for _, o := range orgs {
		orgIDs = append(orgIDs, o.ID)
	}
	// Two lookups for the whole page rather than two per row: the counts and the
	// balances are fetched for the ids on the page and joined here.
	counts, err := s.events.CountEventsByOrganization(ctx, orgIDs)
	if err != nil {
		return nil, err
	}
	balances, err := s.money.WithdrawableBalances(ctx, orgIDs)
	if err != nil {
		return nil, err
	}

	items := make([]OrganizationListItem, 0, len(orgs))
	for _, o := range orgs {
		items = append(items, OrganizationListItem{
			ID:                       o.ID,
			Name:                     o.Name,
			Slug:                     o.Slug,
			Currency:                 o.Currency,
			EventsCount:              counts[o.ID],
			WithdrawableBalanceCents: balances[o.ID],
		})
	}

	return &OrganizationList{
		Data: items,
		Pagination: PageInfo{
			Page:       page,
			PageSize:   pageSize,
			Total:      total,
			TotalPages: totalPages(total, pageSize),
		},
	}, nil
}

// GetOrganization returns one Organization's operator drill-down.
func (s *Service) GetOrganization(ctx context.Context, orgID string) (*OrganizationDetail, error) {
	org, err := s.organizations.GetOrganizationForOperator(ctx, orgID)
	if err != nil {
		return nil, err
	}
	balances, err := s.money.OrganizationBalances(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	events, err := s.events.ListOrganizationEventsForOperator(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	payouts, err := s.money.OperatorPayoutHistory(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	requests, err := s.money.OrganizationPayoutRequestHistory(ctx, org.ID)
	if err != nil {
		return nil, err
	}
	return &OrganizationDetail{
		Organization:             *org,
		WithdrawableBalanceCents: balances.WithdrawableBalanceCents,
		PayableBalanceCents:      balances.PayableBalanceCents,
		Events:                   events,
		Payouts:                  payouts,
		PayoutRequests:           requests,
	}, nil
}

// RecordPayout records a settlement against an Organization, stamped with the
// operator's email.
//
// The Organization is resolved first so an unknown id is a 404 rather than a
// foreign-key failure. The amount is never checked against the Withdrawable
// Balance: the money has already moved, and the ledger records what happened
// (ADR 0015).
func (s *Service) RecordPayout(ctx context.Context, orgID string, input RecordPayoutInput) (*Payout, error) {
	org, err := s.organizations.GetOrganizationForOperator(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return s.money.RecordPayout(ctx, org.ID, salessvc.RecordPayoutInput{
		AmountCents: input.AmountCents,
		PaidAt:      input.PaidAt,
		Note:        input.Note,
		RecordedBy:  input.RecordedBy,
	})
}

func totalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}
