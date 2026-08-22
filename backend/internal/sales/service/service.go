// Package service implements sales business rules and orchestrates transactions.
// Sales covers online, in-person, and import sales plus capacity logic.
package service

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/consent"
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
	// MailLocale is the language mail to this person is written in, as their
	// record remembers it from the Storefront they last signed in on, and "" when
	// the platform holds no record or no language for them (ADR 0033).
	//
	// It is asked for through this seam rather than read off a column here for
	// the reason every other Customer fact is: what a Customer is, is that
	// module's rule. Sales asks the question and hands the answer STRAIGHT to
	// platform.ResolveMailLocale, which is the only place the ordering lives —
	// this module must never decide that a remembered language beats the sale's.
	MailLocale(ctx context.Context, email string) (string, error)
	// ConfirmationLinkURL mints the Confirmation Link for one recorded Ticket
	// Sale. eventEnd is the moment the sale's Event finishes, or the zero time
	// when it has no schedule; how long the link then lives is the customers
	// module's decision, not this one's.
	ConfirmationLinkURL(ticketSaleID string, eventEnd time.Time) (string, error)
	// ConsentConfirmationLinkURL mints the link that resolves whatever optional
	// consents this Customer has sitting in Pending Confirmation, and returns ""
	// when they have none (#255, ADR 0035).
	//
	// THE EMPTY ANSWER IS THE INTERESTING ONE, because it is what keeps every
	// other receipt exactly as it was. This module asks the question for every
	// Online Sale it confirms and gets "" for the great majority — the signed-in
	// buyer, the guest who ticked nothing — and renders no line. Whether anything
	// pends, and what a link may therefore be minted for, is decided entirely on
	// the far side: sales knows that a receipt may carry a link, and nothing at
	// all about Pending Confirmation.
	//
	// It takes a CUSTOMER and not a sale, unlike the Confirmation Link above,
	// which is the honest shape of the thing. A Pending Confirmation is a fact
	// about an address rather than about a purchase — the same one may have been
	// left by an earlier checkout — so a per-sale answer would be a fiction, and a
	// receipt offering to confirm one pending while an identical one stood beside
	// it would resolve half of somebody's inbox.
	ConsentConfirmationLinkURL(ctx context.Context, customerID string) (string, error)
}

// OutstandingAnswerReporter is what sales needs from catalog for the Sale
// Confirmation's one conditional sentence (#315, ADR 0044).
//
// ONE METHOD, ANSWERING ONE YES-OR-NO QUESTION, and everything interesting is on
// the far side of it. This module does not know what a Ticket Question is, that
// `required` means outstanding rather than blocking, that a retired question
// owes nothing, or that a reversed Sale's Tickets have ceased to exist. It knows
// only that a receipt may carry one more sentence, and asks whoever owns the
// debt whether it should.
//
// It is the same shape as ConsentConfirmationLinkURL above and for the same
// reason: the receipt is assembled here, and each conditional line on it is a
// question put to the module that owns the fact. The alternative — sales
// counting unanswered rows for itself — would be a second definition of the
// Outstanding Answer, and #313 exists precisely to stop there being one.
//
// OPTIONAL, unlike the consent capturer. A deployment that never wires it sends
// the receipt it always sent, which is the correct behaviour while the feature
// is dark and the correct behaviour if somebody forgets: see
// hasOutstandingAnswers, where a nil reporter and an error are the same silence.
type OutstandingAnswerReporter interface {
	// TicketSaleHasOutstandingAnswers reports whether any Ticket of this Ticket
	// Sale still owes a required Ticket Question an Answer. It reads the Ticket
	// Question feature flag on its own side, so a dark deployment answers false
	// and every receipt renders exactly as it did before this feature existed.
	TicketSaleHasOutstandingAnswers(ctx context.Context, ticketSaleID string) (bool, error)
}

// ConsentCapturer is what sales needs from consent: the one write path every
// capture surface on the platform goes through, offered inside the transaction
// that is committing the sale (#253, parent #249).
//
// The seam is one method wide, and everything interesting is on the far side of
// it. Sales does not decide whether a tick becomes `granted` or Pending
// Confirmation, does not resolve the Policy Version, does not know that
// `digest_enabled` moves in lockstep with Marketing Consent (ADR 0034). It
// states what happened at its checkout — who, which address, which boxes,
// whether the email was proven, and the circumstances — and the consent module
// decides what that makes true. That is what keeps ADR 0035's rule a property of
// the platform rather than of two code paths that must be remembered together.
//
// Implemented by the consent service, so the cross-module call goes through a
// service exactly as CustomerService and AffiliateLinkResolver do.
//
// It gained a READ in #254, and the read is the same seam's other half: which
// boxes this buyer is still owed. Sales asks it for exactly one purpose — to
// know which of the answers in a checkout body were actually asked for — and it
// still decides nothing. Whether a Policy Version bump has re-gated somebody,
// whether a Pending Confirmation counts as an answer: all of that stays behind
// the interface, in the module that owns the question.
type ConsentCapturer interface {
	CaptureInTx(ctx context.Context, tx *sql.Tx, capture consent.Capture) (consent.Receipt, error)
	// Outstanding reports which boxes a Customer must still be shown. A checkout
	// asks it only about the Customer whose own Customer Session the request
	// carries: it is a fact about a known Customer, and nobody else may learn it.
	Outstanding(ctx context.Context, customerID string) (consent.Outstanding, error)
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

// StaffLocales is what sales needs from identity in order to write a Payout
// Request notice in the language its reader uses (#285, ADR 0041): the Staff
// Locale stored against one email address.
//
// IT TAKES AN ADDRESS AND NOT A MEMBER, and that is the whole reason these five
// notices can be localized at all. A request records its asker as an email
// deliberately untied to a member id so it still resolves after that person's
// Membership ends, which is exactly why ADR 0033 could not localize them: the
// notice was "attached to no record that could hold a language". Keying the
// Staff Locale on the address made that string such a record, and this seam is
// how sales reads it without knowing anything about how staff identity works.
//
// "" is absence rather than English, exactly as CustomerService.MailLocale's is.
// Sales hands the answer straight to platform.ResolveStaffLocale, which owns the
// floor; this module never decides what a missing preference means.
type StaffLocales interface {
	StaffLocale(ctx context.Context, email string) (string, error)
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
	// staffLocales resolves the language each Payout Request notice is written
	// in, by recipient address. Optional, and on the same terms as operators:
	// unset, every notice falls to the English floor and nothing else changes,
	// which is what any test building this service by hand gets.
	staffLocales StaffLocales
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
	// consent records what the buyer authorized at checkout, inside the sale's own
	// transaction. Required, and refused loudly when absent: a checkout that
	// captured answers and then quietly dropped them would leave the platform
	// processing personal data it cannot evidence, which is the one failure this
	// whole feature exists to prevent.
	consent ConsentCapturer
	// outstandingAnswers decides whether a Sale Confirmation carries its one
	// extra sentence (#315). Optional, and nil on any deployment that has not
	// wired it — which sends the receipt this platform always sent.
	outstandingAnswers OutstandingAnswerReporter
	// ticketQuestionsEnabled decides whether the checkout collects Answers at
	// all (ADR 0045). It is the SAME environment variable the catalog service
	// reads for the authoring surface, and one variable rather than two on
	// purpose: a deployment where an Organization can author questions the
	// checkout will not ask, or the reverse, is a deployment collecting or
	// discarding personal data by accident. Off is how the feature ships, and
	// off means a checkout identical to the one before this ticket.
	ticketQuestionsEnabled bool
	logger                 platform.Logger
	now                    func() time.Time
	// drainBatch narrows how many Reversal Requests one Reversal Reconciler run
	// pursues. Zero means the deployed bound; see WithReversalDrainBatch.
	drainBatch int
	// exportRowCap is how many Ticket Sales one Sales Export may carry. Set by
	// New to defaultExportRowCap; see WithExportRowCap.
	exportRowCap int
}

// New returns a sales service. The customers service is required: every Ticket
// Sale, on every Sales Channel, creates or reuses a Customer. The Payment
// Provider is equally required: the online channel cannot sell without one, and
// which implementation arrives here is server wiring's decision (ADR 0009,
// ADR 0012). So is the consent capturer: an Online Sale cannot complete without
// Policy Acceptance and cannot record one without somewhere to write the
// evidence, so it is a constructor argument rather than a knot tied afterwards —
// a deployment that forgot it would be a deployment selling tickets without a
// Consent Record, and that must not be reachable by omission.
func New(repo *repository.Repository, customers CustomerService, email platform.EmailSender, provider platform.PaymentProvider, storefrontBaseURL string, fees sales.FeeRates, consentCapturer ConsentCapturer, logger platform.Logger) *Service {
	return &Service{
		repo:              repo,
		customers:         customers,
		email:             email,
		consent:           consentCapturer,
		provider:          provider,
		storefrontBaseURL: storefrontBaseURL,
		fees:              fees,
		logger:            logger,
		now:               time.Now,
		exportRowCap:      defaultExportRowCap,
	}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// WithTicketQuestions opens the Sales Export's per-Ticket sheet, from the same
// TICKET_QUESTIONS_ENABLED the catalog service is handed (#309, #314, ADR 0045).
//
// It is a SECOND read of one flag rather than a second flag: the value comes
// from platform.Config in both cases, so the two modules cannot disagree about
// whether the feature is on. This module needs its own copy because the export
// is a sales artifact and catalog's service is not a dependency of it — the
// dependency runs the other way.
//
// Off — which is how it ships — the workbook is exactly the two-sheet one it has
// always been, whatever rows an Event has in ticket_questions. That is what
// makes the flag a real off switch rather than a hidden surface: an Answer given
// before the Privacy Policy describes the collection must not leave the building
// in a file either.
func (s *Service) WithTicketQuestions(enabled bool) *Service {
	s.ticketQuestionsEnabled = enabled
	return s
}

// WithOutstandingAnswers wires the seam that decides whether a Sale
// Confirmation carries its one extra sentence (#315, ADR 0044).
//
// A SETTER RATHER THAN A CONSTRUCTOR ARGUMENT, unlike the consent capturer,
// because the two failures are not comparable. A deployment that forgot the
// consent capturer would sell tickets without a Consent Record and must not be
// reachable by omission; a deployment that forgets this one sends the receipt it
// has always sent, which is exactly what a Sale owing nothing gets anyway. The
// safe direction here is silence, so omission is allowed to mean it.
//
// It also breaks a dependency cycle honestly rather than by accident: the
// catalog service is constructed with no knowledge of sales, and sales is
// constructed with no knowledge of catalog, so the two are tied together
// afterwards by whoever wires the application.
func (s *Service) WithOutstandingAnswers(reporter OutstandingAnswerReporter) *Service {
	s.outstandingAnswers = reporter
	return s
}

// WithLogger swaps the structured logger.
//
// It exists for one kind of test: the Sales Export's log line is the only record
// of who took a file of every buyer's email and Tax ID, and what it must NOT
// contain — the free-text search term — can only be asserted by reading what was
// logged. Nothing in production calls it; the deployed logger is the one New is
// handed.
func (s *Service) WithLogger(logger platform.Logger) *Service {
	s.logger = logger
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

// WithStaffLocales supplies the Staff Locale reader, so each Payout Request
// notice is written in the language its recipient reads (#285, ADR 0041).
//
// Applied after construction for the reason the allowlist is: it serves five
// emails on one path. Without it the notices are English, which is precisely
// what they were before this ticket and what a reader with no stored language
// gets anyway.
func (s *Service) WithStaffLocales(locales StaffLocales) *Service {
	s.staffLocales = locales
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
				// An IMPORTED sale's Tickets start out owing everything, because
				// nobody ever put the questions to that buyer — there is no checkout
				// form on a spreadsheet import. That is the honest state of the debt
				// (see repository.outstandingAnswerWhere, which deliberately has no
				// channel filter), and this is the one mail that can do anything
				// about it: the buyer gets their Confirmation Link and the sentence
				// telling them the Tickets behind it still need answers.
				HasOutstandingAnswers: s.hasOutstandingAnswers(ctx, rs.ID),
				TaxID:                 rs.CustomerTaxID,
				// An imported sale was produced by no page and records no Sale
				// Locale, so this resolves to whatever the recipient's own record
				// remembers, and to English for the great majority who have never
				// signed in (ADR 0033). The same helper the online path uses, for
				// the same reason: one chain, one place.
				Locale: s.mailLocale(ctx, rs.ID, rs.Locale, rs.CustomerEmail),
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

// hasOutstandingAnswers asks whether this Ticket Sale's receipt should carry the
// Outstanding Answers sentence (#315), and answers FALSE to every question it
// cannot get a clean answer to.
//
// THREE WAYS TO GET FALSE, AND ALL THREE ARE THE RECEIPT THIS PLATFORM ALWAYS
// SENT: the seam was never wired, the far side is dark because the feature flag
// is off, or the read failed. That is not defensive coding for its own sake —
// this decides one sentence on an email that has already been earned by a
// committed sale, and every possible fault here is better answered by the
// receipt as it was than by no receipt or by a wrong sentence.
//
// THE FAILURE IS LOGGED, for the same reason consentConfirmationLink's is and
// not confirmationLink's: this one reads the database. An unsigned link means a
// misconfigured deployment that fails loudly elsewhere, while a persistent
// failure here would be a feature that had quietly stopped telling buyers
// anything, with nothing anywhere to say so.
func (s *Service) hasOutstandingAnswers(ctx context.Context, ticketSaleID string) bool {
	if s.outstandingAnswers == nil {
		return false
	}
	outstanding, err := s.outstandingAnswers.TicketSaleHasOutstandingAnswers(ctx, ticketSaleID)
	if err != nil {
		s.logger.Warn("could not tell whether a Ticket Sale has Outstanding Answers; the receipt goes out without the line that would have pointed at them",
			"ticket_sale_id", ticketSaleID, "error", err)
		return false
	}
	return outstanding
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

// defaultExportRowCap is how many Ticket Sales one Sales Export may carry.
//
// It is the Sale Import's row limit, referenced rather than repeated. The same
// number twice would be two numbers the day one of them moved, and the point of
// the choice is that the system has ONE answer to how many sale rows travel in a
// file: an export can never hand back more than the importer would accept.
//
// The cap exists because generation is synchronous and the workbook is buffered
// in memory, so an unbounded Event would produce a request that hangs and then
// either times out at the proxy or takes the process down — on the busiest day
// of the Event, which is exactly when somebody reaches for this.
const defaultExportRowCap = importfile.MaxRows

// WithExportRowCap narrows how many Ticket Sales a Sales Export may carry.
//
// It exists so a test can prove the cap is a cap. Reaching the deployed ten
// thousand would mean seeding ten thousand and one Ticket Sales, which takes
// minutes and buys nothing: what has to hold is the behaviour AT the bound — the
// refusal, its count, and that exactly the cap still succeeds — and none of that
// is a property of the number. A value of zero or less keeps the default, so a
// misapplied override can never quietly mean "export nothing".
//
// Nothing in production calls it; the deployed cap is defaultExportRowCap.
func (s *Service) WithExportRowCap(rows int) *Service {
	if rows > 0 {
		s.exportRowCap = rows
	}
	return s
}

// ExportRowCap reports the export row cap currently in force.
func (s *Service) ExportRowCap() int {
	if s.exportRowCap > 0 {
		return s.exportRowCap
	}
	return defaultExportRowCap
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
//
// Above the row cap it builds nothing and returns field errors instead, which
// the handler writes as the standard VALIDATION_FAILED envelope. That is the
// same shape RequestPayout uses for a refusal the caller fixes by changing their
// input, and it is the right one here for the same reason: the answer is not
// "this failed" but "narrow your filters", and the filters are on screen beside
// the button. The refusal names the matched count because that is how the person
// knows how much narrower to go.
func (s *Service) ExportSales(ctx context.Context, actor ActorContext, eventID string, params ListSalesParams) (*SalesExport, []platform.FieldError, error) {
	event, ok, err := s.repo.GetEventImportContext(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, sales.ErrEventNotFound()
	}

	status := params.Status
	if status == "" {
		status = "active"
	}
	loc := resolveEventLocation(event.Timezone)
	soldFrom, soldTo := dateRangeBounds(params.SoldFrom, params.SoldTo, loc)

	// One row past the cap is all that is ever read. The total the query reports
	// is COUNT(*) OVER(), computed before the LIMIT, so the true matched count is
	// exact however far over the cap the Event is — the refusal can name it
	// without a second query, and an Event with a hundred thousand sales never
	// pulls a hundred thousand rows into this process to be told so.
	rowCap := s.ExportRowCap()
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
		Limit:          rowCap + 1,
	})
	if err != nil {
		return nil, nil, err
	}
	if total > rowCap {
		return nil, []platform.FieldError{exportTooManyRows(total, rowCap)}, nil
	}

	// The columns are the Event's LIVE catalog, in display order — not the Ticket
	// Types the filtered rows happen to mention. That is what keeps the shape of
	// the sheet stable: an export narrowed to one Ticket Type still carries every
	// column, and a Ticket Type nobody bought still gets one. Reading the catalog
	// cannot orphan a sale either, because ticket_sale_lines references
	// ticket_types ON DELETE RESTRICT: a Ticket Type that has ever sold cannot be
	// deleted, so the catalog is always a superset of what the rows reference.
	//
	// It is the same read the Sale Import template makes to build its dropdown,
	// and for the same reason: both files describe the Event's catalog as it
	// stands now.
	catalog, err := s.repo.ListEventTicketTypes(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, nil, err
	}
	types := make([]exportfile.TicketTypeColumn, 0, len(catalog))
	ticketTypeNames := make(map[string]string, len(catalog))
	for _, tt := range catalog {
		types = append(types, exportfile.TicketTypeColumn{ID: tt.ID, Name: tt.Name})
		ticketTypeNames[tt.ID] = tt.Name
	}

	exported := make([]exportfile.Sale, 0, len(rows))
	for _, row := range rows {
		// Keyed by Ticket Type id, so the quantity lands under the right column
		// whatever the Ticket Type is called. Summed rather than assigned: a sale
		// is free to carry more than one line of the same Ticket Type, and the
		// column states how many of it the sale was for.
		quantities := make(map[string]int, len(row.TicketTypes))
		for _, line := range row.TicketTypes {
			quantities[line.TicketTypeID] += line.Quantity
		}
		exported = append(exported, exportfile.Sale{
			ConfirmationRef:   row.ConfirmationRef,
			SoldAt:            row.SoldAt,
			CustomerFirstName: row.CustomerFirstName,
			CustomerLastName:  row.CustomerLastName,
			CustomerEmail:     row.CustomerEmail,
			TaxIDType:         row.CustomerTaxIDType,
			TaxIDNumber:       row.CustomerTaxIDNumber,
			Quantities:        quantities,
			AmountCents:       row.AmountCents,
			NetProceedsCents:  exportedNetProceeds(row),
			Currency:          row.Currency,
			Channel:           row.Channel,
			Source:            row.Source,
			PaymentMethod:     row.PaymentMethod,
			Status:            row.Status,
			ReversedAt:        row.ReversedAt,
			ReversedBy:        exportedReversalRoute(row),
		})
	}

	// Whether a free-text search was applied, computed once and read twice: the
	// Info sheet states it and the log line records it, and both must say THAT
	// one happened without ever repeating what it was.
	searched := strings.TrimSpace(params.Search) != ""

	// What the Info sheet says about the file. The status handed over is the
	// RESOLVED one, not the request's: it is the default that makes the honesty
	// necessary — a person who filtered nothing still gets a file with every
	// reversed sale missing from it, and the sheet has to say so.
	generatedAt := s.now()
	info := exportfile.Info{
		EventName:   event.Name,
		GeneratedAt: generatedAt,
		Currency:    event.Currency,
		Filters: exportfile.Filters{
			Status: status,
			// By NAME, resolved against the catalog just read. The reader never
			// saw the id, and an unrecognised id names nothing rather than being
			// printed at them.
			TicketTypeName: ticketTypeNames[params.TicketTypeID],
			SoldFrom:       params.SoldFrom,
			SoldTo:         params.SoldTo,
			Channel:        params.Channel,
			Source:         params.Source,
			PaymentMethod:  params.PaymentMethod,
			// THAT a search happened, never what it was: the term is routinely a
			// buyer's email or Tax ID, and this file is forwarded. Same call the
			// export's log line makes, from the same value.
			Searched: searched,
		},
	}

	// The per-Ticket sheet, built from the very rows the data sheet was built
	// from — which is the whole of how it respects the Sales list's filters.
	answers, err := s.exportAnswers(ctx, actor.OrganizationID, eventID, rows)
	if err != nil {
		return nil, nil, err
	}

	data, err := exportfile.Build(exported, types, answers, loc, info)
	if err != nil {
		return nil, nil, err
	}

	// The one record that a copy of this Event's buyers left the building.
	//
	// There is no audit table behind it — that implies a reading surface, a
	// retention policy and an access rule, and should be designed once across
	// Payout Profile reads and Operator actions rather than growing out of this
	// feature. So this line is the whole answer to "who pulled the customer
	// list", and it is not a question that can be answered retroactively: it is
	// written here or it is never written.
	//
	// It is logged AFTER the workbook exists, so the line claims a file that was
	// actually handed over rather than one whose build then failed.
	//
	// The free-text search is a BOOLEAN and never its value. The search matches
	// customer email and Tax ID number, so a support lookup for one buyer puts
	// that buyer's PII into the filter — and a log aggregator typically has
	// broader access and longer retention than the database it would be copied
	// out of. The structural filters below say what was asked for without saying
	// anything about any one person, which is exactly the line the Info sheet
	// draws in the file itself.
	s.logger.Info("sales export generated",
		"member_id", actor.MemberID,
		"organization_id", actor.OrganizationID,
		"event_id", eventID,
		"row_count", len(exported),
		"status", status,
		"ticket_type_id", params.TicketTypeID,
		"sold_from", params.SoldFrom,
		"sold_to", params.SoldTo,
		"channel", params.Channel,
		"source", params.Source,
		"payment_method", params.PaymentMethod,
		"search", searched,
	)

	return &SalesExport{Data: data, Filename: salesExportFilename(event.Slug, generatedAt.In(loc))}, nil, nil
}

// exportTooManyRows is the refusal a Sales Export over the row cap carries.
//
// It is a field error rather than a domain error because of what the reader is
// meant to do next: the filters that produced this request are on screen beside
// the button that sent it, and narrowing them is the fix. The staff app renders
// the message inline there, so the message is the feature — it names how many
// matched (which is how the person knows how much narrower to go), how many may
// travel at once, and the lever to reach for.
//
// The field named is `filters` and not any one parameter: no single filter is at
// fault, and blaming sold_from would be wrong for somebody whose lever is the
// Ticket Type or the channel.
func exportTooManyRows(matched, rowCap int) platform.FieldError {
	return platform.FieldError{
		Field: "filters",
		Code:  platform.CodeTooManyItems,
		Message: fmt.Sprintf(
			"This Event has %s matching sales; up to %s can be downloaded at once. Narrow the date range and try again.",
			groupDigits(matched), groupDigits(rowCap),
		),
	}
}

// groupDigits renders a count with thousands separators, because these numbers
// are read by a person deciding how much to narrow a filter and "24,318" is
// legible at a glance where "24318" is not.
func groupDigits(n int) string {
	digits := strconv.Itoa(n)
	sign := ""
	if strings.HasPrefix(digits, "-") {
		sign, digits = "-", digits[1:]
	}
	var b strings.Builder
	for i, d := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(d)
	}
	return sign + b.String()
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

// exportedReversalRoute is the route a Sales Export row names in reversed_by, or
// nil where the sale has no Sale Reversal to describe.
//
// It is a TRANSLATION, and the fact that it is one is the whole point. The
// stored column records the kind of ACTOR behind the reversal — `customer`,
// `staff`, `operator` (see the sales package's ReversalActor constants) — and on
// an Operator Reversal it sits on the same ticket_sales row as the acting
// operator's email, their free-text note, and the money memo they asserted. The
// export must state the route and only the route, so the two vocabularies are
// mapped here rather than the stored value being handed through:
//
//   - `customer` stays `customer`: the buyer undid their own Online Sale.
//   - `operator` becomes `platform`. ADR-0019 holds that an Operator Reversal is
//     invisible to the Organization beyond the sale showing as reversed by the
//     platform. The Organization is told an institution acted; which person, on
//     whose say-so, and with what note are operator-facing and stop here.
//   - `staff` becomes `import_undo`. On this side of the boundary "staff" is the
//     reader's own Organization, which tells them nothing; the Sale Import undo
//     is the lever that was actually pulled, and the only route that value has
//     ever been written by.
//
// Anything else is dropped to nil rather than emitted. A value added to the
// stored set later — a new reversal route, and the column's constraint is
// explicitly designed to be extended — would otherwise ride out to every
// Organization's spreadsheet the moment it was written, in whatever spelling the
// schema happened to use and possibly naming somebody. A blank cell is a gap the
// next reader can ask about; a leaked identity cannot be recalled from a file
// that has already been emailed. Adding a route here is one line, and it should
// be a deliberate one.
//
// nil is also the ordinary answer for every active sale, and for a sale reversed
// before the platform recorded any provenance (#117) — those rows keep their
// blank pair rather than being given a fabricated one.
func exportedReversalRoute(row repository.SaleRow) *string {
	if row.ReversedBy == nil {
		return nil
	}
	route, ok := map[string]string{
		sales.ReversalActorCustomer: exportfile.ReversedByCustomer,
		sales.ReversalActorOperator: exportfile.ReversedByPlatform,
		sales.ReversalActorStaff:    exportfile.ReversedByImportUndo,
	}[*row.ReversedBy]
	if !ok {
		return nil
	}
	return &route
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
// Organization after the platform's withholding, how many active Ticket Sales
// it has made, and how many tickets those sales moved. The Platform Fee and its
// Fee IVA are deliberately absent — the figure is already net, and the
// platform's cut is never displayed as a number (ADR 0014).
//
// The three figures do not share a scope, and cannot. NetProceedsCents is
// online money alone, because only Online Sales pass through the platform;
// SalesCount and TicketsSold count every Sales Channel, because a sale made at
// the door is still the Event's sale and still fills a seat. One Ticket Sale of
// four tickets is 1 and 4, so neither count answers for the other, and the
// tab's copy is what tells the reader which is which.
type SalesSummary struct {
	NetProceedsCents int    `json:"net_proceeds_cents"`
	Currency         string `json:"currency"`
	SalesCount       int    `json:"sales_count"`
	// TicketsSold is the quantities of the Event's active Ticket Sale Lines
	// summed — what an organizer reads to know how many people are coming,
	// which no count of checkouts can answer.
	TicketsSold int `json:"tickets_sold"`
}

// EventSalesSummary returns the Event's Net Proceeds, active sales count, and
// Tickets Sold.
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
		TicketsSold:      row.TicketsSold,
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
				// An imported sale was produced by no page and records no Sale
				// Locale, so this resolves to whatever the recipient's own record
				// remembers and to English for the great majority who have never
				// signed in (#246, ADR 0033) — which is exactly what these buyers
				// were sent before any of this shipped.
				Locale: s.mailLocale(ctx, rs.ID, rs.Locale, rs.CustomerEmail),
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

// mailLocale is the language one Ticket Sale's mail is written in, and it is
// the only way this module answers that question (ADR 0033).
//
// It resolves nothing itself: platform.ResolveMailLocale owns the ordering —
// the Sale Locale, then what the Customer's record remembers, then English —
// and every send site in this package goes through here so that no second
// spelling of that chain can appear beside it. An Online Sale made on a Spanish
// page is written in Spanish; a box office sale or an import recorded no
// language and falls through to the recipient's own.
//
// IT TAKES THREE FACTS RATHER THAN A ROW because the rows differ and the
// question does not (#246). A receipt is composed from a RecordedSale, a void
// notice from a ReversedSale and the refused-reversal notice from a
// CustomerTicketSale — three projections of one sale, loaded by three different
// queries for three different jobs. An overload per row type would be three
// places for the chain to be spelled, which is the one thing this function
// exists to prevent.
//
// saleLocale is the Sale Locale as STORED, empty when the sale recorded none,
// and is handed to the chain raw: what a language token has to be worth
// honouring is platform's rule and not this module's.
//
// A failed read of the remembered language is logged and treated as "nothing
// remembered". The sale is committed by the time any caller of this runs, so the
// worst this can cost is a message in the wrong language, and the alternative —
// no message — is far worse for the recipient.
func (s *Service) mailLocale(ctx context.Context, saleID, saleLocale, recipientEmail string) platform.Locale {
	remembered, err := s.customers.MailLocale(ctx, recipientEmail)
	if err != nil {
		s.logger.Warn("could not read the recipient's remembered language for mail about a Ticket Sale; falling back",
			"ticket_sale_id", saleID, "error", err)
	}
	return platform.ResolveMailLocale(saleLocale, remembered)
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
