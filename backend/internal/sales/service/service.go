// Package service implements sales business rules and orchestrates transactions.
// Sales covers online, in-person, and import sales plus capacity logic.
package service

import (
	"context"
	"crypto/rand"
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
	CustomerEmail string
	CustomerName  string
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

// Service implements sales business rules.
type Service struct {
	repo  *repository.Repository
	email platform.EmailSender
	now   func() time.Time
}

// New returns a sales service.
func New(repo *repository.Repository, email platform.EmailSender) *Service {
	return &Service{repo: repo, email: email, now: time.Now}
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
	eventName, ok, err := s.repo.GetEventName(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, sales.ErrEventNotFound()
	}
	return s.commit(ctx, actor, eventID, eventName, input.Source, input.IdempotencyKey, input.Sales)
}

// commit records the prepared sales as one all-or-nothing, idempotent batch and
// emails a Sale Confirmation per newly-recorded sale. It is the shared core of
// the JSON and file commit paths.
func (s *Service) commit(ctx context.Context, actor ActorContext, eventID, eventName, source, idempotencyKey string, saleRows []ImportSaleInput) (*ImportResult, error) {
	commitSales := make([]repository.CommitSale, 0, len(saleRows))
	for _, row := range saleRows {
		ref, err := generateConfirmationRef()
		if err != nil {
			return nil, err
		}
		commitSales = append(commitSales, repository.CommitSale{
			CustomerEmail:   row.CustomerEmail,
			CustomerName:    row.CustomerName,
			PaymentMethod:   row.PaymentMethod,
			SoldAt:          row.SoldAt,
			ConfirmationRef: ref,
			Line: repository.CommitLine{
				TicketTypeID:   row.TicketTypeID,
				Quantity:       row.Quantity,
				UnitPriceCents: row.AmountCents,
			},
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

	// A replay records nothing new, so it must not re-send confirmations.
	if !batch.Replayed {
		for _, cs := range commitSales {
			_ = s.email.SendSaleConfirmation(ctx, platform.SaleConfirmation{
				To:           cs.CustomerEmail,
				CustomerName: cs.CustomerName,
				EventName:    eventName,
				Reference:    cs.ConfirmationRef,
			})
		}
	}

	return result, nil
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
		email:  strings.ToLower(strings.TrimSpace(email)),
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
			CustomerEmail: row.CustomerEmail,
			CustomerName:  row.CustomerName,
			TicketTypeID:  row.TicketTypeID,
			Quantity:      row.Quantity,
			PaymentMethod: row.PaymentMethod,
			SoldAt:        row.SoldAtTime(),
			AmountCents:   row.AmountCents,
		})
	}

	result, err := s.commit(ctx, actor, eventID, event.Name, input.Source, input.IdempotencyKey, saleRows)
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

	loc := time.UTC
	if event.Timezone != "" {
		if parsed, err := time.LoadLocation(event.Timezone); err == nil {
			loc = parsed
		}
	}
	return event, types, loc, nil
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

// generateConfirmationRef returns a short, human-readable, collision-resistant
// reference such as "TP-J7K2QX9M".
func generateConfirmationRef() (string, error) {
	buf := make([]byte, 5)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "TP-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf), nil
}
