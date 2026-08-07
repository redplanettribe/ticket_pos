// Package service implements sales business rules and orchestrates transactions.
// Sales covers online, in-person, and import sales plus capacity logic.
package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/exportfile"
	"github.com/peter/ticket_pos/backend/internal/sales/importfile"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// ActorContext is the acting Member for sales operations.
type ActorContext struct {
	MemberID       string
	OrganizationID string
	// Email is the acting Member's email, and it is here for the one kind of
	// record that must outlive a Membership: a Payout Request names its asker as
	// an email, exactly as payouts.recorded_by names its recorder (ADR 0026). A
	// member id would go dangling the day that person left, taking with it the
	// answer to who asked for the money.
	Email string
}

// ImportSaleInput is one Direct Sale row to record.
type ImportSaleInput struct {
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	// CustomerTaxID is the Tax ID this row was transacted under, already
	// validated and normalised, and unset when the file supplied none — the
	// `import` channel is the one channel allowed to record a sale without one
	// (ADR 0016). It is never self-asserted: a spreadsheet is a Member's account
	// of what happened elsewhere, never the buyer proving they own the email, so
	// an import can fill or refresh a Customer's stored Tax ID but can never
	// overwrite a verified one. There is nothing to set for that — this channel
	// simply builds no self-assertion into the buyer it commits (#111).
	CustomerTaxID platform.SaleTaxID
	TicketTypeID  string
	Quantity      int
	PaymentMethod string
	SoldAt        time.Time
	// AmountCents overrides the catalog unit price snapshot; nil uses the catalog price.
	AmountCents *int
}

// CommitImportInput is a Direct Sale Import to record as one all-or-nothing batch.
type CommitImportInput struct {
	Source         string
	IdempotencyKey string
	Sales          []ImportSaleInput
}

// ImportResult is the outcome of a committed (or replayed) Sale Import.
type ImportResult struct {
	BatchID   string `json:"batch_id"`
	SaleCount int    `json:"sale_count"`
	Status    string `json:"status"`
	Replayed  bool   `json:"replayed"`
}

// CustomerService is what sales needs from Customer identity, and it is
// implemented by the customers service: cross-module calls go through services,
// never repositories, so email normalisation and Confirmation Link policy both
// live on the far side of this seam and every Sales Channel gets the same rules.
type CustomerService interface {
	// UpsertForSale creates or reuses the platform-global Customer for a Ticket
	// Sale and returns the Customer id, inside the transaction that records the
	// sale. It is handed the buyer whole (#111): sales states who bought, and
	// what the customers module then does with each fact — fill, refresh, or
	// leave a verified assertion alone — is that module's rule, not this one's
	// (ADR 0016).
	//
	// The bundle carries two things sales itself never stores. The phone is the
	// number the buyer typed at checkout, canonical E.164 and empty on every
	// channel that collects none (#107); it is deliberately absent from the
	// Ticket Sale, so this seam is the whole of its journey out of this module.
	// SelfAsserted says the checkout ran under the buyer's own Customer Session,
	// which is what the far side needs to tell a person correcting their own
	// record from a stranger typing a known email.
	UpsertForSale(ctx context.Context, tx *sql.Tx, customer platform.SaleCustomer, now time.Time) (string, error)
	// ResolveByEmail says which Customer a typed email is, without creating one:
	// the Customer id, or "" when no record exists yet. It is how the Purchase
	// Limit finds whose holdings to count (ADR 0025), and "" is an ordinary
	// answer — the Customer record is upserted only when a sale commits, so a
	// first-time buyer has none and holds nothing by definition.
	//
	// It also hands back the normalised email, because normalisation is the far
	// side's rule (ADR 0010) and this module must not restate it. Sales needs the
	// value for the one record that snapshots an email verbatim and has no
	// Customer to point at: a pending Payment, which is the Purchase Limit's
	// Capacity Hold arm.
	ResolveByEmail(ctx context.Context, email string) (customerID, normalizedEmail string, err error)
	// ConfirmationLinkURL mints the Confirmation Link for one recorded Ticket
	// Sale. eventEnd is the moment the sale's Event finishes, or the zero time
	// when it has no schedule; how long the link then lives is the customers
	// module's decision, not this one's.
	ConfirmationLinkURL(ticketSaleID string, eventEnd time.Time) (string, error)
}

// AffiliateLinkResolver is what sales needs from Affiliate Links: given the
// code a checkout arrived with, who — if anybody — the sale it produces belongs
// to. Implemented by the affiliates service, so the cross-module call goes
// through that module's service rather than its repository, exactly as
// CustomerService does above.
//
// The seam is deliberately this narrow. Sales knows nothing about codes,
// activation or the Attribution Window; it hands over what the request carried
// and stores the id it gets back. An empty id means unattributed, which is the
// ordinary case and never an error.
type AffiliateLinkResolver interface {
	ResolveLiveCode(ctx context.Context, eventID, code string) (string, error)
}

// PlatformOperators is what sales needs from identity in order to tell the
// Platform Operators that an Organization has asked to be paid (#179, ADR 0026):
// the allowlist, read as a list of addresses.
//
// The seam is one method wide on purpose. Sales knows nothing about how operator
// authority is granted — there is no role and no row to update, only presence on
// the allowlist (ADR 0015) — and asking identity for the addresses rather than
// reading `platform_operators` itself is what keeps who is NOTIFIED and who is
// AUTHORISED the same set, decided in one module.
//
// It is implemented by the identity service, so the cross-module call goes
// through a service exactly as CustomerService and AffiliateLinkResolver do.
type PlatformOperators interface {
	PlatformOperatorEmails(ctx context.Context) ([]string, error)
}

// Service implements sales business rules.
type Service struct {
	repo      *repository.Repository
	customers CustomerService
	email     platform.EmailSender
	// operators is the operator allowlist, read only to address the notice that
	// an Organization has asked to be paid. Optional: unset, the submission
	// notice is skipped and nothing else changes — which is what makes it safe
	// for any test that builds this service by hand.
	operators PlatformOperators
	// provider collects money for Online Sales behind the provider-agnostic
	// Payment Provider boundary (ADR 0012); see checkout.go.
	provider platform.PaymentProvider
	// storefrontBaseURL is the Storefront's public origin, where the Payment
	// Provider sends the Customer back after its payment page (checkout.go).
	storefrontBaseURL string
	// fees is the platform's configured Platform Fee schedule. Checkout reads it
	// once per Payment and snapshots what it computed, so a later rate change
	// never moves recorded economics (ADR 0014).
	fees sales.FeeRates
	// affiliates resolves the Affiliate Link code a checkout arrived with.
	// Optional: unset, no checkout is ever attributed and everything else is
	// unchanged — which is exactly what the channels that never carry a code do
	// anyway.
	affiliates AffiliateLinkResolver
	logger     platform.Logger
	now        func() time.Time
	// drainBatch narrows how many Reversal Requests one Reversal Reconciler run
	// pursues. Zero means the deployed bound; see WithReversalDrainBatch.
	drainBatch int
}

// New returns a sales service. The customers service is required: every Ticket
// Sale, on every Sales Channel, creates or reuses a Customer. The Payment
// Provider is equally required: the online channel cannot sell without one, and
// which implementation arrives here is server wiring's decision (ADR 0009,
// ADR 0012).
func New(repo *repository.Repository, customers CustomerService, email platform.EmailSender, provider platform.PaymentProvider, storefrontBaseURL string, fees sales.FeeRates, logger platform.Logger) *Service {
	return &Service{
		repo:              repo,
		customers:         customers,
		email:             email,
		provider:          provider,
		storefrontBaseURL: storefrontBaseURL,
		fees:              fees,
		logger:            logger,
		now:               time.Now,
	}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// WithPlatformOperators supplies the operator allowlist, so a submitted Payout
// Request reaches the people who can answer it (#179).
//
// Applied after construction rather than added to New's arguments because it
// serves one notice on one path, and a constructor that grows a parameter per
// email would be a constructor nobody can read. Without it, submission behaves
// exactly as it did before #179: the request is recorded and the operator's
// pending-count badge is the only thing that says so.
func (s *Service) WithPlatformOperators(operators PlatformOperators) *Service {
	s.operators = operators
	return s
}

// WithAffiliateLinks supplies the Affiliate Link resolver, so an Online Sale
// begun with a live code is credited to it. Applied after construction because
// the affiliates service is wired after this one, and because attribution is
// additive: without it, checkout behaves exactly as it did before #146.
func (s *Service) WithAffiliateLinks(resolver AffiliateLinkResolver) *Service {
	s.affiliates = resolver
	return s
}

// EnsureEventSellsTickets refuses, with the dedicated code, when the Event
// registers externally: it sells nothing here and never will, so no path may
// record a Ticket Sale against it (ADR 0028, issue #212).
//
// It exists as its own exported step because the refusal has to come FIRST — the
// handler calls it before it judges the request body, so a caller is told the
// one thing that is actually true about this Event rather than being sent to fix
// a Ticket Type id, a Sales Source or a spreadsheet cell that would not have
// helped. Every path that records a Ticket Sale outside online checkout belongs
// here: the Sale Import today, the In-Person Sale and the Integration Partner
// endpoints when they land.
//
// An Event that does not exist is NOT this function's business: it returns nil
// and leaves EVENT_NOT_FOUND to the path that was going to say it, so calling
// this first reorders nothing but the mode.
func (s *Service) EnsureEventSellsTickets(ctx context.Context, actor ActorContext, eventID string) error {
	event, ok, err := s.repo.GetEventImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return refuseExternalRegistration(event)
}

// refuseExternalRegistration is the guard itself, applied wherever the sales
// domain has already loaded an Event's context. Ticketed Events — and rows
// carrying a mode this binary does not recognise — pass straight through.
func refuseExternalRegistration(event *repository.EventImportContext) error {
	if catalog.RegistrationModeOrDefault(event.RegistrationMode) == catalog.RegistrationModeExternal {
		return catalog.ErrEventIsExternalRegistration()
	}
	return nil
}

// CommitImport records a Direct Sale Import for an Event: each row becomes a
// Ticket Sale with one Line, capacity decrements atomically, and each customer is
// emailed a Sale Confirmation. The batch is all-or-nothing and idempotent.
func (s *Service) CommitImport(ctx context.Context, actor ActorContext, eventID string, input CommitImportInput) (*ImportResult, error) {
	// The Event's schedule is loaded, not just its name: each Sale Confirmation
	// carries a Confirmation Link whose lifetime is derived from when the Event
	// finishes.
	event, ok, err := s.repo.GetEventImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}
	// The invariant, not just the handler's early word: an externally registered
	// Event records no Ticket Sale by any route into this service.
	if err := refuseExternalRegistration(event); err != nil {
		return nil, err
	}
	return s.commit(ctx, actor, eventID, event, input.Source, input.IdempotencyKey, input.Sales)
}

// commit records the prepared sales as one all-or-nothing, idempotent batch and
// emails a Sale Confirmation per newly-recorded sale. It is the shared core of
// the JSON and file commit paths.
func (s *Service) commit(ctx context.Context, actor ActorContext, eventID string, event *repository.EventImportContext, source, idempotencyKey string, saleRows []ImportSaleInput) (*ImportResult, error) {
	commitSales := make([]repository.CommitSale, 0, len(saleRows))
	for _, row := range saleRows {
		ref, err := generateConfirmationRef()
		if err != nil {
			return nil, err
		}
		commitSales = append(commitSales, repository.CommitSale{
			// No phone and no self-assertion: this channel collects neither. A
			// Member's account of a purchase made elsewhere never proves the buyer
			// owns the email, so the zero value here is the correct statement and
			// not a gap — see ImportSaleInput.CustomerTaxID.
			Customer: platform.SaleCustomer{
				Email:     row.CustomerEmail,
				FirstName: row.CustomerFirstName,
				LastName:  row.CustomerLastName,
				TaxID:     row.CustomerTaxID,
			},
			PaymentMethod:   row.PaymentMethod,
			SoldAt:          row.SoldAt,
			ConfirmationRef: ref,
			Lines: []repository.CommitLine{{
				TicketTypeID:   row.TicketTypeID,
				Quantity:       row.Quantity,
				UnitPriceCents: row.AmountCents,
			}},
		})
	}

	batch, err := s.repo.CommitImport(ctx, repository.CommitInput{
		EventID:           eventID,
		OrganizationID:    actor.OrganizationID,
		Source:            source,
		CreatedByMemberID: actor.MemberID,
		IdempotencyKey:    idempotencyKey,
		Sales:             commitSales,
		Now:               s.now(),
		UpsertCustomer:    s.customers.UpsertForSale,
	})
	if err != nil {
		return nil, mapCommitError(err)
	}

	result := &ImportResult{
		BatchID:   batch.ID,
		SaleCount: batch.SaleCount,
		Status:    batch.Status,
		Replayed:  batch.Replayed,
	}

	// A replay records nothing new, so it must not re-send confirmations. The
	// loop is over what was actually written rather than what was prepared,
	// because a Confirmation Link names a Ticket Sale by its database id and
	// that id does not exist until the batch commits.
	if !batch.Replayed {
		for _, rs := range batch.Recorded {
			_ = s.email.SendSaleConfirmation(ctx, platform.SaleConfirmation{
				To:               rs.CustomerEmail,
				CustomerName:     displayName(rs.CustomerFirstName, rs.CustomerLastName),
				EventName:        event.Name,
				Reference:        rs.ConfirmationRef,
				AmountCents:      rs.AmountCents,
				Currency:         event.Currency,
				ConfirmationLink: s.confirmationLink(rs.ID, event.End()),
				TaxID:            rs.CustomerTaxID,
			})
		}
	}

	return result, nil
}

// confirmationLink mints the Confirmation Link for a recorded Ticket Sale, or
// returns empty if it cannot.
//
// The only way signing fails is a service with no key, which NewApp refuses to
// build in production — so this is unreachable in a correctly deployed system.
// It degrades rather than propagates because the Ticket Sale is already recorded
// and committed by this point: a Sale Confirmation without its link is worth far
// more to the Customer than no email at all.
func (s *Service) confirmationLink(ticketSaleID string, eventEnd time.Time) string {
	link, err := s.customers.ConfirmationLinkURL(ticketSaleID, eventEnd)
	if err != nil {
		return ""
	}
	return link
}

// ImportHistoryEntry is one committed Sale Import batch in an Event's history.
type ImportHistoryEntry struct {
	BatchID     string    `json:"batch_id"`
	CreatedAt   time.Time `json:"created_at"`
	SaleCount   int       `json:"sale_count"`
	Source      string    `json:"source"`
	Status      string    `json:"status"`
	ActorMember *string   `json:"actor_member_id,omitempty"`
	ActorEmail  *string   `json:"actor_email,omitempty"`
}

// ListImportHistory returns the Event's Sale Import batches, newest first, for
// the per-event import history surface. Read-only.
func (s *Service) ListImportHistory(ctx context.Context, actor ActorContext, eventID string) ([]ImportHistoryEntry, error) {
	_, ok, err := s.repo.GetEventName(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	batches, err := s.repo.ListImportBatches(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	entries := make([]ImportHistoryEntry, 0, len(batches))
	for _, b := range batches {
		entries = append(entries, ImportHistoryEntry{
			BatchID:     b.ID,
			CreatedAt:   b.CreatedAt,
			SaleCount:   b.SaleCount,
			Source:      b.Source,
			Status:      b.Status,
			ActorMember: b.ActorMember,
			ActorEmail:  b.ActorEmail,
		})
	}
	return entries, nil
}

// SaleLine is one Ticket Type and its quantity within a Ticket Sale, rolled up
// for the Sales list.
type SaleLine struct {
	TicketTypeName string `json:"ticket_type_name"`
	Quantity       int    `json:"quantity"`
}

// SaleListItem is one Ticket Sale row on the Sales list: the Customer, the
// rolled-up Ticket Types, the amount in the Event currency, and the
// channel/source/status/reference — plus the recorded-at time and payment
// method surfaced only in the row-detail expand.
//
// TaxIDType/TaxIDNumber are the Tax ID the sale was transacted under, the same
// snapshot the unified search matches, so an organizer can confirm a match at a
// glance and copy the number for a declaration. Both are null together on sales
// recorded without one; history is never backfilled (ADR 0016).
//
// ReversedAt/ReversedBy are the Sale Reversal's provenance: when the sale was
// voided and which side caused it ("customer" or "staff"). Both are null on an
// active sale, and both stay null on a sale reversed before either was recorded
// — the same never-backfilled treatment, for the same reason (#117, ADR 0018).
type SaleListItem struct {
	ID                string     `json:"id"`
	CustomerFirstName string     `json:"customer_first_name"`
	CustomerLastName  string     `json:"customer_last_name"`
	CustomerEmail     string     `json:"customer_email"`
	TicketTypes       []SaleLine `json:"ticket_types"`
	AmountCents       int        `json:"amount_cents"`
	Currency          string     `json:"currency"`
	SoldAt            time.Time  `json:"sold_at"`
	Channel           string     `json:"channel"`
	Source            *string    `json:"source"`
	Status            string     `json:"status"`
	ConfirmationRef   string     `json:"confirmation_ref"`
	RecordedAt        time.Time  `json:"recorded_at"`
	PaymentMethod     *string    `json:"payment_method"`
	TaxIDType         *string    `json:"tax_id_type"`
	TaxIDNumber       *string    `json:"tax_id_number"`
	ReversedAt        *time.Time `json:"reversed_at"`
	ReversedBy        *string    `json:"reversed_by"`
}

// Pagination is the ADR-0006 nested pagination object: the current page and
// size, the unpaginated total match count, and the derived page count.
type Pagination struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// SalesListResult is the ADR-0006 nested envelope for the Sales list: the page
// of rows plus its pagination metadata.
//
// ReversedCount is how many of the Event's Ticket Sales are reversed, across the
// whole Event and independent of every filter on this request — including the
// status filter that decided which rows are in Data. Until a Customer could undo
// their own Online Sale, the only reversal was a Sale Import undo that staff
// performed themselves, so a row leaving the default active view was never a
// surprise; now money and capacity move with no staff action at all, and the
// count is what keeps the drop explained rather than silent (#122, ADR 0018). It
// rides the Sales list, not the sales summary, because it must reach every
// Member of the Event and the summary is refused to Event Staff.
type SalesListResult struct {
	Data          []SaleListItem `json:"data"`
	Pagination    Pagination     `json:"pagination"`
	ReversedCount int            `json:"reversed_count"`
}

// ListSalesParams is a validated, clamped Sales list request. Page and size are
// already floored/clamped by the handler per ADR-0006; the filter fields have
// been validated against their allowlists (Status, Channel, Source,
// PaymentMethod) or as calendar dates (SoldFrom/SoldTo, "YYYY-MM-DD"). An empty
// filter field means that dimension is unfiltered. Status defaults to "active".
// Sort/Dir are already resolved against the allowlists by the handler.
type ListSalesParams struct {
	Page     int
	PageSize int

	Status        string
	TicketTypeID  string
	SoldFrom      string
	SoldTo        string
	Search        string
	Channel       string
	Source        string
	PaymentMethod string

	// Sort is the resolved sort column (one of sold_at, recorded_at, customer,
	// amount) and Dir the direction ("asc"/"desc"); both default to the newest-first
	// sold_at ordering.
	Sort string
	Dir  string
}

// ListSales returns a page of the Event's Ticket Sales for the Sales list,
// newest first (sold_at descending, ADR-0006), narrowed by the supplied
// filters. It is read-only and scoped to the acting Member's Organization; a
// page beyond the last returns an empty data slice with the true total so the
// UI can still show the count. The sold-at date range is interpreted in the
// Event timezone (defaulting to UTC).
func (s *Service) ListSales(ctx context.Context, actor ActorContext, eventID string, params ListSalesParams) (*SalesListResult, error) {
	event, ok, err := s.repo.GetEventImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	status := params.Status
	if status == "" {
		status = "active"
	}
	loc := resolveEventLocation(event.Timezone)
	soldFrom, soldTo := dateRangeBounds(params.SoldFrom, params.SoldTo, loc)

	rows, total, err := s.repo.ListSales(ctx, repository.ListSalesQuery{
		OrganizationID: actor.OrganizationID,
		EventID:        eventID,
		Status:         status,
		TicketTypeID:   params.TicketTypeID,
		SoldFrom:       soldFrom,
		SoldTo:         soldTo,
		Search:         params.Search,
		Channel:        params.Channel,
		Source:         params.Source,
		PaymentMethod:  params.PaymentMethod,
		Sort:           params.Sort,
		Dir:            params.Dir,
		Limit:          params.PageSize,
		Offset:         (params.Page - 1) * params.PageSize,
	})
	if err != nil {
		return nil, err
	}

	items := make([]SaleListItem, 0, len(rows))
	for _, row := range rows {
		lines := make([]SaleLine, 0, len(row.TicketTypes))
		for _, l := range row.TicketTypes {
			lines = append(lines, SaleLine{TicketTypeName: l.TicketTypeName, Quantity: l.Quantity})
		}
		items = append(items, SaleListItem{
			ID:                row.ID,
			CustomerFirstName: row.CustomerFirstName,
			CustomerLastName:  row.CustomerLastName,
			CustomerEmail:     row.CustomerEmail,
			TicketTypes:       lines,
			AmountCents:       row.AmountCents,
			Currency:          row.Currency,
			SoldAt:            row.SoldAt,
			Channel:           row.Channel,
			Source:            row.Source,
			Status:            row.Status,
			ConfirmationRef:   row.ConfirmationRef,
			RecordedAt:        row.RecordedAt,
			PaymentMethod:     row.PaymentMethod,
			TaxIDType:         row.CustomerTaxIDType,
			TaxIDNumber:       row.CustomerTaxIDNumber,
			ReversedAt:        row.ReversedAt,
			ReversedBy:        row.ReversedBy,
		})
	}

	// Read after the page, and unfiltered: the organizer's question is whether
	// anything on this Event was reversed, not how many reversals survive the
	// filters they happen to have on.
	reversedCount, err := s.repo.ReversedSalesCount(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}

	return &SalesListResult{
		Data: items,
		Pagination: Pagination{
			Page:       params.Page,
			PageSize:   params.PageSize,
			Total:      total,
			TotalPages: totalPages(total, params.PageSize),
		},
		ReversedCount: reversedCount,
	}, nil
}

// SalesExport is a built Sales Export: the .xlsx bytes and the filename the
// download carries. The filename is decided here rather than at the HTTP edge so
// there is one answer to what an exported file is called.
type SalesExport struct {
	Data     []byte
	Filename string
}

// ExportSales builds the Event's Ticket Sales into an .xlsx, narrowed by the
// same filters as the Sales list.
//
// It takes ListSalesParams and honours every filter on it, ignoring only Page
// and PageSize: pagination is a property of a screen, and a file that stopped at
// row 50 would be a quietly wrong answer. Everything else — the status default
// of active, the sold-at range read in the Event's timezone, the sort — behaves
// exactly as it does on the list, because it is the same code path. The caller's
// role is gated at the route (Org Admin and Event Owner only): this file
// concentrates every buyer's email and Tax ID for an Event into something that
// is forwarded and kept, so it takes the Sales summary's guard rather than the
// Sales list's looser one.
func (s *Service) ExportSales(ctx context.Context, actor ActorContext, eventID string, params ListSalesParams) (*SalesExport, error) {
	event, ok, err := s.repo.GetEventImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	status := params.Status
	if status == "" {
		status = "active"
	}
	loc := resolveEventLocation(event.Timezone)
	soldFrom, soldTo := dateRangeBounds(params.SoldFrom, params.SoldTo, loc)

	// Limit 0 is the unpaginated read: the file is the whole answer.
	rows, _, err := s.repo.ListSales(ctx, repository.ListSalesQuery{
		OrganizationID: actor.OrganizationID,
		EventID:        eventID,
		Status:         status,
		TicketTypeID:   params.TicketTypeID,
		SoldFrom:       soldFrom,
		SoldTo:         soldTo,
		Search:         params.Search,
		Channel:        params.Channel,
		Source:         params.Source,
		PaymentMethod:  params.PaymentMethod,
		Sort:           params.Sort,
		Dir:            params.Dir,
	})
	if err != nil {
		return nil, err
	}

	exported := make([]exportfile.Sale, 0, len(rows))
	for _, row := range rows {
		exported = append(exported, exportfile.Sale{
			ConfirmationRef:   row.ConfirmationRef,
			SoldAt:            row.SoldAt,
			CustomerFirstName: row.CustomerFirstName,
			CustomerLastName:  row.CustomerLastName,
			CustomerEmail:     row.CustomerEmail,
			TaxIDType:         row.CustomerTaxIDType,
			TaxIDNumber:       row.CustomerTaxIDNumber,
			AmountCents:       row.AmountCents,
			NetProceedsCents:  exportedNetProceeds(row),
			Currency:          row.Currency,
			Channel:           row.Channel,
			Source:            row.Source,
			PaymentMethod:     row.PaymentMethod,
			Status:            row.Status,
		})
	}

	data, err := exportfile.Build(exported, loc)
	if err != nil {
		return nil, err
	}
	return &SalesExport{Data: data, Filename: salesExportFilename(event.Slug, s.now().In(loc))}, nil
}

// exportedNetProceeds is the Net Proceeds a Sales Export row states, or nil
// where the figure does not apply to the sale at all.
//
// The number itself is the repository's, summed off the per-line fee snapshots
// the sale froze — the same expression the Event's sales summary sums, so a sale
// and the Event it belongs to can never disagree, and so a later rate change or
// Fee Handling flip never rewrites what an old sale earned (ADR 0014). Nothing
// here recomputes a fee or branches on the Event's mode.
//
// What this function decides is only WHETHER the sale has such a figure, and it
// returns nil rather than zero in the two cases where it does not. That
// distinction is the whole of ADR 0032: a blank cell and a 0 say different
// things, and in a spreadsheet the difference becomes a SUM.
//
//   - Only an Online Sale produces Net Proceeds. On any other Sales Channel the
//     money never passed through the platform, so nothing was withheld from it —
//     and because those lines carry fee snapshots of zero, the raw sum reads back
//     as the sale's full price, which would be a plain lie about money the
//     platform never held.
//   - A reversed sale drops out of the money as it does everywhere else. It keeps
//     its row, because a Sale Reversal should be visible in the file rather than
//     a row that silently vanished, but money given back was never proceeds.
//
// Both mirror ADR-0019's treatment of a free Online Sale's figures as absent
// rather than zero. A blank here is deliberate; it is not a gap to be filled in.
func exportedNetProceeds(row repository.SaleRow) *int {
	if row.Channel != salesChannelOnline || row.Status == saleStatusReversed {
		return nil
	}
	net := row.NetProceedsCents
	return &net
}

// The two values exportedNetProceeds tests against, named so the rule above
// reads as the sentence it is. Both are stored spellings enforced by database
// CHECK constraints and shared with the Sales list's own filter allowlists.
const (
	salesChannelOnline = "online"
	saleStatusReversed = "reversed"
)

// salesExportFilename names a Sales Export after its Event and the day it was
// taken — "sales-summer-fest-2026-08-07.xlsx" — so a Downloads folder holding
// several stays navigable. The day is the Event's, drawn in the Event's own
// timezone like every other date in the file.
func salesExportFilename(slug string, generatedAt time.Time) string {
	return "sales-" + slug + "-" + generatedAt.Format("2006-01-02") + ".xlsx"
}

// SalesSummary is the Sales tab's stat strip: what the Event has left the
// Organization after the platform's withholding, and how many active Ticket
// Sales it has made. The Platform Fee and its Fee IVA are deliberately absent —
// the figure is already net, and the platform's cut is never displayed as a
// number (ADR 0014).
type SalesSummary struct {
	NetProceedsCents int    `json:"net_proceeds_cents"`
	Currency         string `json:"currency"`
	SalesCount       int    `json:"sales_count"`
}

// EventSalesSummary returns the Event's Net Proceeds and active sales count.
// It is read-only and scoped to the acting Member's Organization; the caller's
// role is gated at the route (Org Admin and Event Owner only — Event Staff see
// the Sales list without this strip).
func (s *Service) EventSalesSummary(ctx context.Context, actor ActorContext, eventID string) (*SalesSummary, error) {
	event, ok, err := s.repo.GetEventImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	row, err := s.repo.SalesSummary(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	return &SalesSummary{
		NetProceedsCents: row.NetProceedsCents,
		Currency:         event.Currency,
		SalesCount:       row.SalesCount,
	}, nil
}

// totalPages is the number of pages a total spans at the given page size (0 when
// there are no matches).
func totalPages(total, pageSize int) int {
	if total <= 0 || pageSize <= 0 {
		return 0
	}
	return (total + pageSize - 1) / pageSize
}

// UndoResult is the outcome of undoing (reversing) a Sale Import batch.
type UndoResult struct {
	BatchID   string `json:"batch_id"`
	SaleCount int    `json:"sale_count"`
	Status    string `json:"status"`
	// Notified is true when void/cancellation emails were sent to affected buyers.
	Notified bool `json:"notified"`
}

// UndoImport reverses the latest committed Sale Import batch on an Event: its
// Ticket Sales are marked reversed — each stamped with the undo time and the
// `staff` reversal actor — each affected Ticket Type's sold_count is restored,
// and the batch is marked reversed. When notifyBuyers is true, each
// affected Customer is emailed a void/cancellation notice referencing their Sale
// Confirmation — sent only after the reversal transaction commits. When false,
// nothing is sent. Only the most recent batch is reversible.
func (s *Service) UndoImport(ctx context.Context, actor ActorContext, eventID, batchID string, notifyBuyers bool) (*UndoResult, error) {
	eventName, ok, err := s.repo.GetEventName(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}

	reversed, err := s.repo.ReverseBatch(ctx, repository.ReverseInput{
		EventID:        eventID,
		OrganizationID: actor.OrganizationID,
		BatchID:        batchID,
		Now:            s.now(),
	})
	if err != nil {
		return nil, mapReverseError(err)
	}

	if notifyBuyers {
		for _, rs := range reversed.Sales {
			_ = s.email.SendSaleVoided(ctx, platform.SaleVoided{
				To:           rs.CustomerEmail,
				CustomerName: displayName(rs.CustomerFirstName, rs.CustomerLastName),
				EventName:    eventName,
				Reference:    rs.ConfirmationRef,
			})
		}
	}

	return &UndoResult{
		BatchID:   reversed.ID,
		SaleCount: len(reversed.Sales),
		Status:    "reversed",
		Notified:  notifyBuyers,
	}, nil
}

// FileCommitInput is a Sale Import to record from parsed file rows.
type FileCommitInput struct {
	Source         string
	IdempotencyKey string
	Rows           []importfile.RawRow
	// SkipRows is the set of file row numbers (RawRow.Line) the organizer chose to
	// exclude — e.g. accidental duplicates resolved as "skip". Kept rows are
	// recorded; skipped rows are dropped before validation and capacity checks.
	SkipRows []int
}

// BuildTemplate returns the per-event .xlsx Sale Import template and the Event
// name (for the download filename).
func (s *Service) BuildTemplate(ctx context.Context, actor ActorContext, eventID string) ([]byte, string, error) {
	event, types, _, err := s.loadImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, "", err
	}
	data, err := importfile.BuildTemplate(event.Name, types)
	if err != nil {
		return nil, "", err
	}
	return data, event.Name, nil
}

// PreviewImport validates parsed file rows against the Event's Ticket Types and
// returns the per-row verdicts and capacity impact, flagging rows that match an
// existing active sale as possible duplicates. It writes nothing.
func (s *Service) PreviewImport(ctx context.Context, actor ActorContext, eventID string, rows []importfile.RawRow) (*importfile.ValidateResult, error) {
	_, types, loc, err := s.loadImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	result := importfile.Validate(importfile.ValidateInput{
		Rows:     rows,
		Types:    types,
		Now:      s.now(),
		Location: loc,
	})

	existing, err := s.repo.ListActiveSaleKeys(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	flagDuplicates(&result, existing, loc)

	// After the duplicate flag, not before: the soft signal is only computed for
	// rows still valid, and a row the Purchase Limit rejects is one the organizer
	// may well fix by skipping it as the duplicate it also is.
	if err := s.refuseImportRowsOverPurchaseLimit(ctx, eventID, types, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// dupKey identifies a sale by buyer email (case-insensitive), Ticket Type, and
// sold_at date in the Event timezone — the soft duplicate signal.
type dupKey struct {
	email  string
	typeID string
	date   string
}

func makeDupKey(email, typeID string, soldAt time.Time, loc *time.Location) dupKey {
	return dupKey{
		email:  platform.NormalizeEmail(email),
		typeID: typeID,
		date:   soldAt.In(loc).Format("2006-01-02"),
	}
}

// flagDuplicates marks each valid row whose (email, ticket type, sold_at date)
// matches an existing active sale, referencing that date. Soft signal only.
func flagDuplicates(result *importfile.ValidateResult, existing []repository.ExistingSaleKey, loc *time.Location) {
	if len(existing) == 0 {
		return
	}
	seen := make(map[dupKey]struct{}, len(existing))
	for _, e := range existing {
		seen[makeDupKey(e.CustomerEmail, e.TicketTypeID, e.SoldAt, loc)] = struct{}{}
	}
	for i := range result.Rows {
		row := &result.Rows[i]
		if !row.Valid {
			continue
		}
		key := makeDupKey(row.CustomerEmail, row.TicketTypeID, row.SoldAtTime(), loc)
		if _, ok := seen[key]; ok {
			row.PossibleDuplicate = true
			row.DuplicateOfDate = key.date
		}
	}
}

// CommitImportFile validates parsed file rows and, only if every row is valid,
// records them as one all-or-nothing batch (reusing the same commit core as the
// JSON path). When some rows are invalid it returns the ValidateResult and a nil
// ImportResult without writing anything.
func (s *Service) CommitImportFile(ctx context.Context, actor ActorContext, eventID string, input FileCommitInput) (*ImportResult, *importfile.ValidateResult, error) {
	event, types, loc, err := s.loadImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, nil, err
	}

	// Drop rows the organizer chose to skip (e.g. resolved duplicates) before
	// validating: a skipped row is excluded from the batch and from capacity math,
	// and its problems must not block the kept rows.
	rows := filterSkippedRows(input.Rows, input.SkipRows)

	validated := importfile.Validate(importfile.ValidateInput{
		Rows:     rows,
		Types:    types,
		Now:      s.now(),
		Location: loc,
	})
	// The Purchase Limit is re-decided here rather than trusted from the preview.
	// The preview is the surface an organizer works on, not a gate the server can
	// enforce: this endpoint takes a file directly, so a commit that skipped the
	// check could be driven straight past it, and the tally would only ever have
	// held for organizers who happened to click Preview first.
	//
	// This is NOT the trade-off the online commit faces (ADR 0025). There, the
	// Payment Provider is holding the buyer's money by the time the sale commits,
	// so a refusal at that point is the PAYMENT_APPROVED_WITHOUT_SALE incident a
	// Platform Operator unpicks by hand — which is why begin-checkout is the only
	// place the online path checks. An import moves no money and nobody is
	// waiting on a payment page, so refusing here costs a rejected row and
	// nothing else.
	if err := s.refuseImportRowsOverPurchaseLimit(ctx, eventID, types, &validated); err != nil {
		return nil, nil, err
	}

	// Invalid kept rows block with VALIDATION_FAILED. Oversell is not decided here:
	// it falls through to the repository's under-lock capacity check, which fails
	// the whole batch with IMPORT_BATCH_FAILED / CAPACITY_EXCEEDED (and also
	// catches capacity races lost since preview).
	if !validated.Valid() {
		return nil, &validated, nil
	}

	saleRows := make([]ImportSaleInput, 0, len(validated.Rows))
	for _, row := range validated.Rows {
		saleRows = append(saleRows, ImportSaleInput{
			CustomerEmail:     row.CustomerEmail,
			CustomerFirstName: row.CustomerFirstName,
			CustomerLastName:  row.CustomerLastName,
			CustomerTaxID: platform.SaleTaxID{
				Type:   row.CustomerTaxIDType,
				Number: row.CustomerTaxIDNumber,
			},
			TicketTypeID:  row.TicketTypeID,
			Quantity:      row.Quantity,
			PaymentMethod: row.PaymentMethod,
			SoldAt:        row.SoldAtTime(),
			AmountCents:   row.AmountCents,
		})
	}

	result, err := s.commit(ctx, actor, eventID, event, input.Source, input.IdempotencyKey, saleRows)
	if err != nil {
		return nil, nil, err
	}
	return result, nil, nil
}

// filterSkippedRows returns the rows whose Line is not in skip. It preserves
// order and is a no-op when skip is empty.
func filterSkippedRows(rows []importfile.RawRow, skip []int) []importfile.RawRow {
	if len(skip) == 0 {
		return rows
	}
	skipped := make(map[int]struct{}, len(skip))
	for _, n := range skip {
		skipped[n] = struct{}{}
	}
	kept := make([]importfile.RawRow, 0, len(rows))
	for _, row := range rows {
		if _, ok := skipped[row.Line]; ok {
			continue
		}
		kept = append(kept, row)
	}
	return kept
}

// loadImportContext loads the Event and its Ticket Types for import operations,
// resolving the Event timezone (defaulting to UTC when unset or unknown).
func (s *Service) loadImportContext(ctx context.Context, orgID, eventID string) (*repository.EventImportContext, []importfile.TicketTypeRef, *time.Location, error) {
	event, ok, err := s.repo.GetEventImportContext(ctx, orgID, eventID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !ok {
		return nil, nil, nil, sales.ErrEventNotFound()
	}
	// Before the Ticket Types are read, never after: on an externally registered
	// Event the read would come back empty and every downstream surface — the
	// template's type list, the preview's per-row match, the file commit — would
	// report a missing Ticket Type instead of the mode that guarantees there is
	// none (ADR 0028, issue #212).
	if err := refuseExternalRegistration(event); err != nil {
		return nil, nil, nil, err
	}

	rows, err := s.repo.ListEventTicketTypes(ctx, orgID, eventID)
	if err != nil {
		return nil, nil, nil, err
	}
	types := make([]importfile.TicketTypeRef, 0, len(rows))
	for _, tt := range rows {
		types = append(types, importfile.TicketTypeRef{
			ID:             tt.ID,
			Name:           tt.Name,
			PriceCents:     tt.PriceCents,
			Capacity:       tt.Capacity,
			SoldCount:      tt.SoldCount,
			MaxPerCustomer: tt.MaxPerCustomer,
		})
	}

	return event, types, resolveEventLocation(event.Timezone), nil
}

// resolveEventLocation resolves an Event timezone name to a *time.Location,
// defaulting to UTC when the timezone is unset or unrecognized.
func resolveEventLocation(tz string) *time.Location {
	if tz != "" {
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
	}
	return time.UTC
}

// dateRangeBounds turns validated "YYYY-MM-DD" sold-at bounds into a half-open
// absolute-time interval [from, to) interpreted in the Event timezone: from is
// local midnight of the start date (inclusive) and to is local midnight of the
// day AFTER the end date (exclusive), so the end date's whole day is included.
// Either bound may be blank, yielding a nil (open) bound. Blank or unparseable
// values yield nil, as the handler has already validated the format.
func dateRangeBounds(from, to string, loc *time.Location) (*time.Time, *time.Time) {
	var fromT, toT *time.Time
	if d, err := time.ParseInLocation("2006-01-02", from, loc); from != "" && err == nil {
		start := d
		fromT = &start
	}
	if d, err := time.ParseInLocation("2006-01-02", to, loc); to != "" && err == nil {
		end := d.AddDate(0, 0, 1)
		toT = &end
	}
	return fromT, toT
}

func mapCommitError(err error) error {
	var capErr *repository.CapacityError
	if errors.As(err, &capErr) {
		return sales.ErrImportBatchFailed(capErr.Row, "CAPACITY_EXCEEDED", map[string]any{
			"ticket_type_id": capErr.TicketTypeID,
			"requested":      capErr.Requested,
			"available":      capErr.Available,
		})
	}
	var unknownErr *repository.UnknownTicketTypeError
	if errors.As(err, &unknownErr) {
		return sales.ErrTicketTypeNotFound(unknownErr.TicketTypeID)
	}
	return err
}

func mapReverseError(err error) error {
	var notFound *repository.BatchNotFoundError
	if errors.As(err, &notFound) {
		return sales.ErrImportBatchNotFound(notFound.BatchID)
	}
	var notLatest *repository.BatchNotLatestError
	if errors.As(err, &notLatest) {
		return sales.ErrImportNotLatestBatch(notLatest.BatchID)
	}
	var reversed *repository.BatchAlreadyReversedError
	if errors.As(err, &reversed) {
		return sales.ErrImportAlreadyReversed(reversed.BatchID)
	}
	return err
}

// displayName joins a Customer's first and last name into the single "First
// Last" form used wherever one display name is needed (Sale Confirmation and
// void notices). The Customer name is stored split; only display joins it.
func displayName(first, last string) string {
	return strings.TrimSpace(strings.TrimSpace(first) + " " + strings.TrimSpace(last))
}

// generateConfirmationRef returns a short, human-readable, collision-resistant
// reference such as "TP-J7K2QX9M".
func generateConfirmationRef() (string, error) {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "TP-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}
