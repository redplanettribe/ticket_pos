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

	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/sales"
	"github.com/peter/ticket_pos/backend/internal/sales/importfile"
	"github.com/peter/ticket_pos/backend/internal/sales/repository"
)

// ActorContext is the acting Member for sales operations.
type ActorContext struct {
	MemberID       string
	OrganizationID string
}

// ImportSaleInput is one Direct Sale row to record.
type ImportSaleInput struct {
	CustomerEmail     string
	CustomerFirstName string
	CustomerLastName  string
	TicketTypeID      string
	Quantity          int
	PaymentMethod     string
	SoldAt            time.Time
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
	// sale. taxID is what the sale was transacted under (unset on a channel that
	// carries none); what the customers module does with it — fill, refresh, or
	// leave a verified assertion alone — is that module's rule, not this one's
	// (ADR 0016).
	UpsertForSale(ctx context.Context, tx *sql.Tx, email, firstName, lastName string, taxID platform.SaleTaxID, now time.Time) (string, error)
	// ConfirmationLinkURL mints the Confirmation Link for one recorded Ticket
	// Sale. eventEnd is the moment the sale's Event finishes, or the zero time
	// when it has no schedule; how long the link then lives is the customers
	// module's decision, not this one's.
	ConfirmationLinkURL(ticketSaleID string, eventEnd time.Time) (string, error)
}

// Service implements sales business rules.
type Service struct {
	repo      *repository.Repository
	customers CustomerService
	email     platform.EmailSender
	// provider collects money for Online Sales behind the provider-agnostic
	// Payment Provider boundary (ADR 0012); see checkout.go.
	provider platform.PaymentProvider
	// storefrontBaseURL is the Storefront's public origin, where the Payment
	// Provider sends the Customer back after its payment page (checkout.go).
	storefrontBaseURL string
	// fees is the platform's configured Platform Fee schedule. Checkout reads it
	// once per Payment and snapshots what it computed, so a later rate change
	// never moves recorded economics (ADR 0014).
	fees   sales.FeeRates
	logger platform.Logger
	now    func() time.Time
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
			CustomerEmail:     row.CustomerEmail,
			CustomerFirstName: row.CustomerFirstName,
			CustomerLastName:  row.CustomerLastName,
			PaymentMethod:     row.PaymentMethod,
			SoldAt:            row.SoldAt,
			ConfirmationRef:   ref,
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
type SalesListResult struct {
	Data       []SaleListItem `json:"data"`
	Pagination Pagination     `json:"pagination"`
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
		})
	}

	return &SalesListResult{
		Data: items,
		Pagination: Pagination{
			Page:       params.Page,
			PageSize:   params.PageSize,
			Total:      total,
			TotalPages: totalPages(total, params.PageSize),
		},
	}, nil
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
// Ticket Sales are marked reversed, each affected Ticket Type's sold_count is
// restored, and the batch is marked reversed. When notifyBuyers is true, each
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
			TicketTypeID:      row.TicketTypeID,
			Quantity:          row.Quantity,
			PaymentMethod:     row.PaymentMethod,
			SoldAt:            row.SoldAtTime(),
			AmountCents:       row.AmountCents,
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

	rows, err := s.repo.ListTicketTypesForImport(ctx, orgID, eventID)
	if err != nil {
		return nil, nil, nil, err
	}
	types := make([]importfile.TicketTypeRef, 0, len(rows))
	for _, tt := range rows {
		types = append(types, importfile.TicketTypeRef{
			ID:         tt.ID,
			Name:       tt.Name,
			PriceCents: tt.PriceCents,
			Capacity:   tt.Capacity,
			SoldCount:  tt.SoldCount,
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
