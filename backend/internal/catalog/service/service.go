package service

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
	"github.com/peter/ticket_pos/backend/internal/platform/storage"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

// EventListItem is a summary row for the Events list UI.
type EventListItem struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Slug         string     `json:"slug"`
	Status       string     `json:"status"`
	StartsAt     *time.Time `json:"starts_at"`
	Timezone     *string    `json:"timezone"`
	Discoverable bool       `json:"discoverable"`
	CreatedAt    time.Time  `json:"created_at"`
}

// TicketTypeDetail is a Ticket Type with organization currency for display.
type TicketTypeDetail struct {
	ID          string  `json:"id"`
	EventID     string  `json:"event_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	PriceCents  int     `json:"price_cents"`
	Currency    string  `json:"currency"`
	Capacity    int     `json:"capacity"`
	SoldCount   int     `json:"sold_count"`
	SortOrder   int     `json:"sort_order"`
	// MaxPerCustomer is the Purchase Limit — the most of this Ticket Type one
	// Customer may hold at once — or null when the Ticket Type is unrestricted,
	// which is most of them. A count of tickets, not money: unlike PriceCents it
	// is untouched by Promotion or fee arithmetic (ADR 0025).
	MaxPerCustomer *int `json:"max_per_customer"`
	// SalesCutoffAt is the Sales Cutoff — the instant the Storefront stops
	// selling this Ticket Type — or null when it never stops, which is most of
	// them. The raw instant and never a verdict: whether the Ticket Type has
	// closed is the reader's comparison against its own clock, through
	// catalog.ClosedAt. Stated unconditionally rather than only while it is in
	// force, because nothing about the value is validated and reading it back is
	// the only catch for a typo in the year (ADR 0070).
	SalesCutoffAt *time.Time `json:"sales_cutoff_at"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
	// Promotion is the Ticket Type's one Promotion slot, or null when it is
	// empty. It travels with the Ticket Type so the editor can render the
	// Promotion's state without a second request; PriceCents above stays the
	// List Price whether or not a Promotion is live (ADR 0021).
	Promotion *PromotionView `json:"promotion"`
}

// EventDetail is the full Event record for detail views.
type EventDetail struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Slug          string     `json:"slug"`
	Status        string     `json:"status"`
	StartsAt      *time.Time `json:"starts_at"`
	EndsAt        *time.Time `json:"ends_at"`
	Timezone      *string    `json:"timezone"`
	VenueName     *string    `json:"venue_name"`
	VenueAddress  *string    `json:"venue_address"`
	Description   *string    `json:"description"`
	CoverImageKey *string    `json:"cover_image_key"`
	CoverImageURL *string    `json:"cover_image_url"`
	// CoverVideoKey and CoverVideoURL are the Event's optional Cover Video: the
	// stored object key and the public URL derived from it at read time, never
	// stored (ADR 0020).
	CoverVideoKey *string `json:"cover_video_key"`
	CoverVideoURL *string `json:"cover_video_url"`
	Discoverable  bool    `json:"discoverable"`
	// FeeHandling is the Event's Fee Handling mode, and the two rates are the
	// Platform Fee schedule it is read with. The rates travel with the Event so
	// the staff forms can show an organizer what a price means for the buyer and
	// for their own take-home using the same arithmetic checkout uses (ADR 0014).
	FeeHandling       string `json:"fee_handling"`
	FeeBasisPoints    int    `json:"fee_basis_points"`
	FeeIVABasisPoints int    `json:"fee_iva_basis_points"`
	// RegistrationMode is how this Event takes sign-ups: 'tickets' or 'external'
	// (ADR 0028). RegistrationURL is the Registration Link, null while an external
	// Event's registration page is still being built.
	RegistrationMode string  `json:"registration_mode"`
	RegistrationURL  *string `json:"registration_url"`
	// RegistrationClickCount is how many times the hand-off to the Registration
	// Link has been made. It counts clicks, never registrations or people: the
	// platform loses sight of the buyer at the link and never learns what happened
	// next. Read-only here — the redirect owns it, no Event form may write it.
	RegistrationClickCount int64 `json:"registration_click_count"`
	// TicketQuestionsEnabled is the platform's Ticket Question feature flag
	// (ADR 0045), not a property of this Event — it rides here for the reason
	// FeeBasisPoints above does: the Ticket Type editor is composed from this
	// payload, and the flag decides whether that editor offers a Ticket Question
	// surface at all. Surfacing it lets the staff app hide the section rather
	// than render one whose every request would 404, and it keeps the answer in
	// ONE place: a second environment variable on the frontend could disagree
	// with the backend about whether the feature is on.
	TicketQuestionsEnabled bool `json:"ticket_questions_enabled"`
	// TicketAssignmentEnabled is the platform's Ticket Assignment feature flag
	// (ADR 0045), riding here for TicketQuestionsEnabled's reason: the staff
	// app decides from this payload whether to offer the Holder List entry —
	// which the API serves while EITHER flag is open (#333) — and a frontend
	// environment variable would be a second copy of the answer, free to
	// disagree with the one that matters.
	TicketAssignmentEnabled bool      `json:"ticket_assignment_enabled"`
	CreatedAt               time.Time `json:"created_at"`
}

// ActorContext is the acting member for catalog operations.
type ActorContext struct {
	MemberID       string
	OrganizationID string
}

// CreateEventInput creates a draft Event.
type CreateEventInput struct {
	Name string
	Slug string
	// RegistrationMode is the submitted mode, or nil when the caller said nothing
	// about it — which is most callers, and which means the Event sells tickets.
	RegistrationMode *catalog.RegistrationMode
	// RegistrationURL is the Registration Link, or nil when there is none yet. A
	// new Event may name External Registration before its registration page
	// exists, exactly as it may be created before its start time is known.
	RegistrationURL *string
}

// CreateTicketTypeInput creates a Ticket Type on an Event.
type CreateTicketTypeInput struct {
	Name        string
	Description *string
	PriceCents  int
	Capacity    int
	// MaxPerCustomer is the Purchase Limit, nil for an unrestricted Ticket Type.
	MaxPerCustomer *int
	// SalesCutoffAt is the Sales Cutoff, nil for a Ticket Type that never stops
	// selling. Unvalidated by design: a past instant and an instant after the
	// Event's start are both ordinary values (ADR 0070).
	SalesCutoffAt *time.Time
}

// UpdateTicketTypeInput updates Ticket Type fields.
type UpdateTicketTypeInput struct {
	Name        string
	Description *string
	PriceCents  int
	Capacity    int
	SortOrder   int
	// MaxPerCustomer is the Purchase Limit. Nil clears it, because this endpoint
	// is a full restatement of the Ticket Type rather than a patch — the same
	// rule Description already follows.
	MaxPerCustomer *int
	// SalesCutoffAt is the Sales Cutoff. Nil clears it — reopening sales is one
	// edit and not a rebuild of the Ticket Type — and a cutoff that has already
	// passed may be moved like any other, which is how a closed sale is
	// extended (ADR 0070).
	SalesCutoffAt *time.Time
}

// UpdateEventInput updates Event fields on the detail form.
type UpdateEventInput struct {
	Name          string
	Slug          string
	StartsAt      *time.Time
	EndsAt        *time.Time
	Timezone      *string
	VenueName     *string
	VenueAddress  *string
	Description   *string
	CoverImageKey *string
	// CoverVideoKey attaches or clears the Cover Video: an empty string clears
	// it, nil leaves it alone, anything else must be a key under this Event's
	// videos prefix.
	CoverVideoKey *string
	// FeeHandling is the submitted Fee Handling mode, or nil when the form said
	// nothing about it — an update that omits it leaves the Event's mode alone.
	FeeHandling *sales.FeeHandling
	// RegistrationMode is the submitted registration mode, nil to leave it alone.
	RegistrationMode *catalog.RegistrationMode
	// RegistrationURL is the submitted Registration Link. Nil leaves it alone, an
	// empty string clears it, mirroring cover_image_key. The link and the mode
	// move independently: repointing a link is not a mode change, and choosing
	// External Registration does not require having the link yet.
	RegistrationURL *string
}

// CreateCoverUploadURLInput requests a presigned cover upload URL.
type CreateCoverUploadURLInput struct {
	ContentType string
	FileName    string
}

// CreateVideoUploadURLInput requests a presigned Cover Video upload URL.
type CreateVideoUploadURLInput struct {
	ContentType string
}

// CustomerHoldings is what catalog needs from sales to tell a signed-in Customer
// how much of each Ticket Type they already hold, which is the only thing the
// Storefront needs to bound its quantity picker by the Purchase Limit before the
// buyer types anything (ADR 0025, #168).
//
// It is a service seam, not a repository one: the count is a sales rule with two
// arms — active Ticket Sales and live Capacity Holds — and catalog must never
// grow a second spelling of it, or the Event page and the checkout's refusal
// would drift apart. Catalog already borrows sales.LiveHoldsSQL for the Event's
// remaining figures, and that is as far into sales' storage as this module is
// allowed to reach: that one is a shared derivation with no identity in it,
// whereas this one resolves a person.
//
// The email handed across MUST be one the caller has proven the requester owns.
// See the implementation for why nothing on this side can check that.
type CustomerHoldings interface {
	CustomerEventHoldings(ctx context.Context, eventID, email string) (map[string]int, error)
}

// Service implements catalog business rules.
type Service struct {
	repo    *repository.Repository
	storage storage.ObjectStorage
	fees    sales.FeeRates
	now     func() time.Time
	// holdings answers what a signed-in Customer already holds of an Event's
	// Ticket Types, for the public Event read only.
	holdings CustomerHoldings
	// logger is where a failed media cleanup goes to be seen. The organizer
	// never hears about it (ADR 0020), so the log line is the only record that
	// an object outlived the Event that referenced it.
	logger platform.Logger
	// ticketQuestionsEnabled is the Ticket Question feature flag (ADR 0045).
	//
	// FALSE IS THE ZERO VALUE, and that is the reason it is a plain field set by
	// a WithX rather than a constructor argument: every construction of this
	// service that has not been taught about the flag — a test, a tool, a future
	// caller — gets the dark build. A constructor argument would make an
	// unwired call site a compile error, which sounds stricter but is worse
	// here: the failure mode this flag exists to prevent is the surface being
	// OPEN when nobody decided it should be, and no arrangement of arguments
	// makes "off" easier to reach by accident than the zero value does.
	ticketQuestionsEnabled bool
	// ticketAssignmentEnabled is the Ticket Assignment feature flag (#324,
	// parent #322), and is SEPARATE from ticketQuestionsEnabled above.
	//
	// TWO FIELDS AND NOT ONE, because the two features are separable and the
	// whole operational point of a second flag is that assignment can be killed
	// without taking Ticket Questions dark. Anything in this service that reads
	// one of them to decide the other has quietly merged them.
	//
	// FALSE IS THE ZERO VALUE, for exactly the reason its neighbour's is, and
	// with more at stake: what this flag opens is the platform storing an email
	// address supplied by somebody with no authority to supply it, before any
	// published Policy Version describes that collection.
	ticketAssignmentEnabled bool
	// assignmentLinks signs and verifies Assignment Links (#325, ADR 0046).
	//
	// ITS ZERO VALUE IS UNCONFIGURED, which mints nothing and opens nothing —
	// the alternative failure mode is signing with a zero key, which anybody
	// holding a copy of this source could forge into a link accepting any
	// Ticket. A service nobody wired a secret into refuses; it does not
	// improvise one. It derives its own key under its own purpose label, so a
	// token of any other signed link cannot verify as this one however its
	// payload is spelled (ADR 0046).
	assignmentLinks catalog.AssignmentLinkSigner
	// assignmentLinkBaseURL is the Storefront origin an Assignment Link points
	// at. A Storefront URL and never this API's: the Holder must land on a page,
	// and no browser addresses the Go API directly (ADR 0008).
	assignmentLinkBaseURL string
	// mailer delivers the Assignment mail — the ONLY carrier an Assignment Link
	// ever has, since the token may never appear on a buyer surface or in an API
	// response (ADR 0046).
	//
	// Nil is a service that assigns and mails nobody, which is what every test
	// with no opinion about mail gets, and what #324 shipped.
	mailer AssignmentMailer
	// noLongerHoldingMailer delivers the one mail an accepted Holder gets when a
	// Ticket stops being theirs (#327).
	//
	// A SECOND FIELD RATHER THAN A SECOND METHOD ON mailer, because the two
	// messages have opposite risk profiles and the seams should say so: one
	// carries a credential that mints an identity and goes to a stranger, and
	// this one carries no link at all and goes to a Customer who proved their
	// address. Nil leaves the service silent — a build that reassigns and reverses
	// and tells nobody, which is what every ticket before this one was.
	noLongerHoldingMailer NoLongerHoldingMailer
	// holders writes the Customer a Holder's click mints or matches. Nil is a
	// service that accepts nothing: the accept route reports the link
	// unavailable rather than accepting a Ticket on behalf of nobody.
	holders HolderCustomers
	// assignmentMailLimits rations the Assignment mail (#332, parent #322): a
	// hard per-Ticket cap and a per-buyer rate limit.
	//
	// ITS ZERO VALUE IS "THE DEFAULTS", not "send nothing" — catalog.
	// MayMailAssignment reads the constants past any limit that is zero or
	// negative. That is the opposite posture to the flags above and is
	// deliberate: an unwired flag must fail closed because what it guards is a
	// collection nobody decided to open, while an unwired LIMIT failing closed
	// would silently disable a feature somebody did decide to open, which a flag
	// already has a proper way to say.
	assignmentMailLimits catalog.AssignmentMailLimits
	// questionReviewMail, operators and staffLocales serve the one notice a
	// Question Review submission sends (#406, ADR 0056); see
	// WithQuestionReviewNotices. All optional, all nil by default: unset, a
	// submission is recorded and nobody is mailed.
	questionReviewMail QuestionReviewMail
	operators          PlatformOperators
	staffLocales       StaffLocales
	// holderExportRowCap is how many Tickets one Holder Export may carry (#529).
	//
	// ZERO MEANS THE DEPLOYED DEFAULT, read through HolderExportRowCap, and never
	// "export nothing": a service nobody configured must produce the shipped
	// behaviour, and a zero here reaching a LIMIT would be an empty file that
	// looked like an Event with nobody coming. Only WithHolderExportRowCap sets
	// it, and only a test calls that.
	holderExportRowCap int
	// revocationMailer and revocationRecipients deliver the one mail an
	// Organization gets when a Platform Operator revokes an approved Ticket
	// Question (#410, ADR 0056): every Org Admin, each in their Staff Locale.
	// Nil leaves the service silent — the question is retired and the reason
	// readable in the staff editor either way, and only the telling is missing.
	revocationMailer     RevocationMailer
	revocationRecipients RevocationRecipients
}

// New returns a catalog service. The fee rates are the platform's configured
// Platform Fee schedule, surfaced on Event payloads so the staff forms derive
// buyer and take-home figures with the checkout arithmetic (ADR 0014).
//
// The holdings seam is a constructor argument rather than a later WithX because
// an unwired one would not degrade gracefully: a signed-in Customer would be
// told they hold nothing, which is a false statement about them rather than an
// absent one. Sales imports the catalog ROOT package for shared pricing types
// but never catalog/service, so satisfying this interface from the sales service
// closes no cycle — there is nothing to unpick, and no reason to tie the knot
// after construction.
func New(repo *repository.Repository, objectStorage storage.ObjectStorage, fees sales.FeeRates, holdings CustomerHoldings, logger platform.Logger) *Service {
	return &Service{
		repo:     repo,
		storage:  objectStorage,
		fees:     fees,
		now:      time.Now,
		holdings: holdings,
		logger:   logger,
	}
}

// WithClock overrides the clock (tests).
func (s *Service) WithClock(now func() time.Time) *Service {
	s.now = now
	return s
}

// WithTicketQuestions opens or closes the Ticket Question authoring surface
// (#309, ADR 0045). NewApp calls it with platform.Config.TicketQuestionsEnabled;
// the integration suite calls it to exercise both sides of the flag, which is
// the only way the "with the flag off, nothing differs" property can be a test
// rather than a claim.
func (s *Service) WithTicketQuestions(enabled bool) *Service {
	s.ticketQuestionsEnabled = enabled
	return s
}

// WithTicketAssignment opens or closes Ticket Assignment (#324, parent #322).
//
// A SECOND WithX BESIDE WithTicketQuestions AND NEVER A SECOND ARGUMENT TO IT.
// NewApp calls it with platform.Config.TicketAssignmentEnabled, which is read
// from TICKET_ASSIGNMENT_ENABLED; the integration suite calls it to exercise
// both sides, which is the only way "assignment can be closed while questions
// stay open" can be a test rather than a claim.
func (s *Service) WithTicketAssignment(enabled bool) *Service {
	s.ticketAssignmentEnabled = enabled
	return s
}

// WithAssignmentMailLimits lowers the Assignment mail rationing for a test
// (#332, parent #322).
//
// THE NUMBERS ARE CONFIGURATION; THE BEHAVIOUR AT THEM IS WHAT IS UNDER TEST.
// This is the OTP global ceiling's WithGlobalCeiling in another module and for
// the same reason: proving the per-buyer window binds by actually sending
// twenty mails would be a slow test that tells a reader nothing the same test at
// two does not.
//
// A ZERO OR NEGATIVE VALUE KEEPS THE DEFAULT, so a caller cannot accidentally
// mean "send nothing" — see catalog.MayMailAssignment.
//
// NO ENVIRONMENT VARIABLE READS THIS, and that is on purpose. These limits are
// the price ADR 0046 charged for writing to strangers, not a dial an operator
// turns under pressure; loosening them is a code change with a reviewer, which
// is what the ADR means by "anyone loosening the cap is spending something this
// ADR priced".
func (s *Service) WithAssignmentMailLimits(limits catalog.AssignmentMailLimits) *Service {
	s.assignmentMailLimits = limits
	return s
}

// AssignmentMailLimits reports the rationing currently in force, so a test that
// lowered it can put it back.
func (s *Service) AssignmentMailLimits() catalog.AssignmentMailLimits {
	return s.assignmentMailLimits
}

// WithAssignmentLinks gives this service the key it signs Assignment Links with
// and the Storefront origin they point at (#325, ADR 0046).
//
// The secret is the DEPLOYMENT's link secret — the same value every other signed
// link is derived from — and is turned into this purpose's own key here rather
// than used directly. Four signed links now travel in one mail flow and none may
// open what the others do; this is the fourth, and the only one that mints an
// identity.
//
// A WithX rather than a constructor argument, beside WithTicketQuestions and for
// the same reason: an unwired service is one that signs nothing, which is the
// safe way to be unwired.
func (s *Service) WithAssignmentLinks(secret []byte, storefrontBaseURL string) *Service {
	s.assignmentLinks = catalog.NewAssignmentLinkSigner(secret)
	s.assignmentLinkBaseURL = strings.TrimSuffix(strings.TrimSpace(storefrontBaseURL), "/")
	return s
}

// WithAssignmentMail gives this service the sender that delivers the Assignment
// mail (#325).
//
// A NARROW SEAM AND NOT platform.EmailSender ENTIRE, so that nothing in catalog
// can reach the Sale Confirmation, the passcode or the Follow Digest. Nil leaves
// the service silent, which is what #324 was and what every test with no opinion
// about mail wants.
func (s *Service) WithAssignmentMail(mailer AssignmentMailer) *Service {
	s.mailer = mailer
	return s
}

// WithHolderCustomers gives this service the seam that turns a click into a
// Customer (#325, ADR 0046).
//
// Satisfied by the customers service, which owns `verified_at` and every other
// statement this platform makes about who somebody is (ADR 0010). Catalog never
// imports that package; see HolderCustomers.
//
// A WithX rather than a constructor argument because the wiring is late by
// necessity — the customers service is built after this one — and because the
// unwired state is safe: a service without it accepts nothing at all.
// WithNoLongerHoldingMail gives this service the seam the No Longer Holding mail
// travels through (#327, ADR 0046).
//
// A SEPARATE SEAM FROM THE ASSIGNMENT MAIL'S, satisfied in production by the same
// split sender, so this message is structurally on the transactional identity
// (ADR 0030) — which matters because a Holder consented to nothing by accepting a
// ticket, and being told the ticket is gone must not be suppressible by a
// marketing preference.
//
// Nil leaves the service silent. That is the safe way to be unwired here: a
// Holder who is not told still stops holding the Ticket, and the Event still
// leaves their Customer Area, so the state stays consistent and only the telling
// is missing — visible in the log rather than in a failed request.
func (s *Service) WithNoLongerHoldingMail(mailer NoLongerHoldingMailer) *Service {
	s.noLongerHoldingMailer = mailer
	return s
}

// WithRevocationMail supplies the sender and the identity reads the Revocation
// notice needs (#410, ADR 0056). Nil for either leaves the service silent.
func (s *Service) WithRevocationMail(mailer RevocationMailer, recipients RevocationRecipients) *Service {
	s.revocationMailer = mailer
	s.revocationRecipients = recipients
	return s
}

func (s *Service) WithHolderCustomers(holders HolderCustomers) *Service {
	s.holders = holders
	return s
}

// ListEvents returns Events for the active Organization.
func (s *Service) ListEvents(ctx context.Context, actor ActorContext) ([]EventListItem, error) {
	events, err := s.repo.ListEventsByOrganizationID(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}
	items := make([]EventListItem, 0, len(events))
	for _, e := range events {
		items = append(items, toEventListItem(&e))
	}
	return items, nil
}

// CreateEvent creates a draft Event.
func (s *Service) CreateEvent(ctx context.Context, actor ActorContext, input CreateEventInput) (*EventDetail, error) {
	name := strings.TrimSpace(input.Name)
	slug := normalizeSlug(input.Slug)

	exists, err := s.repo.EventSlugExistsInOrganization(ctx, actor.OrganizationID, slug)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, catalog.ErrEventSlugTaken(slug)
	}

	mode := catalog.RegistrationModeTickets
	if input.RegistrationMode != nil {
		mode = *input.RegistrationMode
	}

	event, err := s.repo.CreateEvent(ctx, actor.OrganizationID, repository.CreateEventParams{
		Name:             name,
		Slug:             slug,
		RegistrationMode: string(mode),
		RegistrationURL:  nullStringFromPtr(input.RegistrationURL),
	}, s.now())
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(event)
	return &detail, nil
}

// GetEvent returns an Event by ID.
func (s *Service) GetEvent(ctx context.Context, actor ActorContext, eventID string) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}
	detail := s.toEventDetail(event)
	return &detail, nil
}

// UpdateEvent updates Event fields respecting draft slug rules.
func (s *Service) UpdateEvent(ctx context.Context, actor ActorContext, eventID string, input UpdateEventInput) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	name := strings.TrimSpace(input.Name)
	slug := normalizeSlug(input.Slug)

	if slug != event.Slug && event.Status != repository.EventStatusDraft {
		return nil, catalog.ErrEventNotDraft()
	}
	if slug != event.Slug {
		taken, err := s.repo.EventSlugExistsInOrganizationExcluding(ctx, actor.OrganizationID, slug, eventID)
		if err != nil {
			return nil, err
		}
		if taken {
			return nil, catalog.ErrEventSlugTaken(slug)
		}
	}

	params := repository.UpdateEventParams{
		Name: name,
		Slug: slug,
	}
	params.StartsAt = nullTimeFromPtr(input.StartsAt)
	params.EndsAt = nullTimeFromPtr(input.EndsAt)
	params.Timezone = nullStringFromPtr(input.Timezone)
	params.VenueName = nullStringFromPtr(input.VenueName)
	params.VenueAddress = nullStringFromPtr(input.VenueAddress)
	params.Description = nullStringFromPtr(input.Description)
	if input.CoverImageKey != nil {
		key := strings.TrimSpace(*input.CoverImageKey)
		if key == "" {
			params.CoverImageKey = sql.NullString{}
		} else if !storage.CoverKeyBelongsToEvent(key, actor.OrganizationID, eventID) {
			return nil, catalog.ErrInvalidCoverImageKey()
		} else {
			params.CoverImageKey = sql.NullString{String: key, Valid: true}
		}
	} else {
		params.CoverImageKey = event.CoverImageKey
	}
	if input.CoverVideoKey != nil {
		key := strings.TrimSpace(*input.CoverVideoKey)
		if key == "" {
			params.CoverVideoKey = sql.NullString{}
		} else if !storage.VideoKeyBelongsToEvent(key, actor.OrganizationID, eventID) {
			return nil, catalog.ErrInvalidCoverVideoKey()
		} else {
			params.CoverVideoKey = sql.NullString{String: key, Valid: true}
		}
	} else {
		params.CoverVideoKey = event.CoverVideoKey
	}

	// The poster invariant: a Cover Video requires a Cover Image. It binds on the
	// final state the update would leave behind, not on the fields the request
	// happens to carry — so setting both at once is fine, clearing both is fine,
	// and replacing the image under a video is fine, while attaching a video to an
	// imageless Event or clearing the image out from under a video is not.
	if params.CoverVideoKey.Valid && !params.CoverImageKey.Valid {
		return nil, catalog.ErrCoverVideoRequiresCoverImage()
	}

	params.Discoverable = event.Discoverable

	// Fee Handling takes effect on future checkouts only: pending Payments carry
	// their own snapshot, so a flip never rewrites money already being paid.
	params.FeeHandling = event.FeeHandling
	if input.FeeHandling != nil {
		params.FeeHandling = string(*input.FeeHandling)
	}

	// The mode and the Registration Link move independently, and a form that
	// mentions neither leaves both where they are.
	params.RegistrationMode = string(catalog.RegistrationModeOrDefault(event.RegistrationMode))
	if input.RegistrationMode != nil {
		params.RegistrationMode = string(*input.RegistrationMode)
	}
	params.RegistrationURL = event.RegistrationURL
	if input.RegistrationURL != nil {
		params.RegistrationURL = nullStringFromPtr(input.RegistrationURL)
	}

	// The published-state freezes, enforced here because the server is the only
	// place enforcement lives: a disabled button in the staff form stops a click,
	// not a request (#208).
	//
	// Both bind on the final state of the update rather than on which fields the
	// request happened to carry, like the poster rule above — the Event form
	// resubmits the whole registration pair on every save, so restating the mode
	// an Event already has is not a change and must go through.
	if event.Status != repository.EventStatusDraft {
		currentMode := string(catalog.RegistrationModeOrDefault(event.RegistrationMode))
		// The mode is settled while the Event is a draft and frozen in both
		// directions afterwards; the escape hatch is a new Event.
		if params.RegistrationMode != currentMode {
			return nil, catalog.ErrEventRegistrationModeLocked()
		}
	}
	if event.Status == repository.EventStatusPublished &&
		params.RegistrationMode == string(catalog.RegistrationModeExternal) &&
		strings.TrimSpace(params.RegistrationURL.String) == "" {
		// A published external Event has no Ticket Types behind it, so the
		// Registration Link is the only way in: it is correctable, never removable.
		return nil, catalog.ErrEventRegistrationURLRequired()
	}

	// The exclusivity invariant, seen from the Event's side: an Event may not
	// register externally while Ticket Types still sit behind it (ADR 0028). It
	// binds on the final state of the update rather than on whether the request
	// happened to name the mode, for the same reason the Cover Video poster rule
	// does — an Event that ends this request external must end it with no Ticket
	// Types, however it got there. The other side of the invariant is in
	// CreateTicketType; neither is expressible as a CHECK constraint because the
	// fact spans two tables.
	if params.RegistrationMode == string(catalog.RegistrationModeExternal) {
		ticketTypeCount, err := s.repo.CountTicketTypesByEventID(ctx, actor.OrganizationID, eventID)
		if err != nil {
			return nil, err
		}
		if ticketTypeCount > 0 {
			return nil, catalog.ErrEventHasTicketTypes()
		}
	}

	updated, err := s.repo.UpdateEvent(ctx, actor.OrganizationID, eventID, params)
	if err != nil {
		return nil, err
	}

	// The row is committed, so the old objects are unreachable from here on:
	// whatever happens next cannot make this request wrong (ADR 0020).
	s.deleteReplacedObject(ctx, event.CoverImageKey, params.CoverImageKey, "cover image", eventID)
	s.deleteReplacedObject(ctx, event.CoverVideoKey, params.CoverVideoKey, "cover video", eventID)

	detail := s.toEventDetail(updated)
	return &detail, nil
}

// deleteReplacedObject removes the object an Event has stopped pointing at, when
// the update replaced its key with a different one or cleared it. Preserved and
// unchanged keys are still in use and are left alone.
//
// Best-effort by design: the database has already committed, and the worst a
// failure can do is leave an orphan — the status quo before delete-on-replace
// existed. So it is logged and never returned (ADR 0020).
func (s *Service) deleteReplacedObject(ctx context.Context, before, after sql.NullString, kind, eventID string) {
	if s.storage == nil || !before.Valid || before.String == "" {
		return
	}
	if after.Valid && after.String == before.String {
		return
	}
	if err := s.storage.Delete(ctx, before.String); err != nil && s.logger != nil {
		s.logger.Error("media cleanup: the replaced object could not be deleted and is now an orphan",
			"kind", kind, "event_id", eventID, "object_key", before.String, "error", err)
	}
}

// SetEventDiscoverable sets whether a published Event is listed in public discovery
// surfaces. Only a published Event may change discoverability; draft and cancelled
// Events are never listed and reject the change.
func (s *Service) SetEventDiscoverable(ctx context.Context, actor ActorContext, eventID string, discoverable bool) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}
	if event.Status != repository.EventStatusPublished {
		return nil, catalog.ErrEventNotPublished()
	}

	updated, err := s.repo.SetEventDiscoverable(ctx, actor.OrganizationID, eventID, discoverable)
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(updated)
	return &detail, nil
}

// PublishEvent transitions a draft Event to published when requirements are met.
func (s *Service) PublishEvent(ctx context.Context, actor ActorContext, eventID string) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	switch event.Status {
	case repository.EventStatusPublished:
		return nil, catalog.ErrEventAlreadyPublished()
	case repository.EventStatusCancelled:
		return nil, catalog.ErrEventAlreadyCancelled()
	case repository.EventStatusDraft:
	default:
		return nil, catalog.ErrEventNotDraft()
	}

	missing := publishMissingFields(event)

	// The way in. Every Event needs one, and which one it needs is the mode: a
	// ticketed Event needs at least one Ticket Type, exactly as it always has,
	// and an externally registered one needs its Registration Link instead
	// (ADR 0028). Naming the wrong one sends the organizer to fix something the
	// Event does not use — an external Event has no Ticket Types by construction,
	// so "add a ticket type" is advice it cannot take.
	if catalog.RegistrationModeOrDefault(event.RegistrationMode) == catalog.RegistrationModeExternal {
		if !event.RegistrationURL.Valid || strings.TrimSpace(event.RegistrationURL.String) == "" {
			missing = append(missing, "registration_url")
		}
	} else {
		ticketCount, err := s.repo.CountTicketTypesByEventID(ctx, actor.OrganizationID, eventID)
		if err != nil {
			return nil, err
		}
		if ticketCount == 0 {
			missing = append(missing, "ticket_types")
		}
	}
	if len(missing) > 0 {
		return nil, catalog.ErrEventPublishRequirementsNotMet(missing)
	}

	updated, err := s.repo.UpdateEventStatus(ctx, actor.OrganizationID, eventID, repository.EventStatusPublished)
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(updated)
	return &detail, nil
}

// CancelEvent transitions a published Event to cancelled.
func (s *Service) CancelEvent(ctx context.Context, actor ActorContext, eventID string) (*EventDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	switch event.Status {
	case repository.EventStatusCancelled:
		return nil, catalog.ErrEventAlreadyCancelled()
	case repository.EventStatusDraft:
		return nil, catalog.ErrEventNotDraft()
	case repository.EventStatusPublished:
	default:
		return nil, catalog.ErrEventNotDraft()
	}

	updated, err := s.repo.UpdateEventStatus(ctx, actor.OrganizationID, eventID, repository.EventStatusCancelled)
	if err != nil {
		return nil, err
	}
	detail := s.toEventDetail(updated)
	return &detail, nil
}

func publishMissingFields(event *repository.Event) []string {
	var missing []string
	if strings.TrimSpace(event.Name) == "" {
		missing = append(missing, "name")
	}
	if strings.TrimSpace(event.Slug) == "" {
		missing = append(missing, "slug")
	}
	if !event.StartsAt.Valid {
		missing = append(missing, "starts_at")
	}
	if !event.Timezone.Valid || strings.TrimSpace(event.Timezone.String) == "" {
		missing = append(missing, "timezone")
	} else if _, err := time.LoadLocation(strings.TrimSpace(event.Timezone.String)); err != nil {
		missing = append(missing, "timezone")
	}
	return missing
}

// DeleteEvent removes a draft Event.
func (s *Service) DeleteEvent(ctx context.Context, actor ActorContext, eventID string) error {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if event == nil {
		return catalog.ErrEventNotFound()
	}
	if event.Status != repository.EventStatusDraft {
		return catalog.ErrEventDeleteForbidden()
	}

	if err := s.repo.DeleteEvent(ctx, actor.OrganizationID, eventID); err != nil {
		return err
	}
	return nil
}

// CreateCoverUploadURL returns a presigned PUT URL for an event cover image.
func (s *Service) CreateCoverUploadURL(ctx context.Context, actor ActorContext, eventID string, input CreateCoverUploadURLInput) (*storage.CoverUploadResult, error) {
	if s.storage == nil {
		return nil, catalog.ErrCoverUploadUnavailable()
	}

	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if !storage.CoverContentTypeAllowed(contentType) {
		return nil, catalog.ErrInvalidCoverImageKey()
	}

	key, err := storage.BuildCoverObjectKey(actor.OrganizationID, eventID, contentType, input.FileName)
	if err != nil {
		return nil, err
	}

	uploadURL, err := s.storage.PresignPut(ctx, key, contentType, 15*time.Minute)
	if err != nil {
		return nil, err
	}

	return &storage.CoverUploadResult{
		UploadURL: uploadURL,
		ObjectKey: key,
		PublicURL: s.storage.PublicURL(key),
	}, nil
}

// CreateVideoUploadURL returns a presigned PUT URL for an event Cover Video.
// The object is stored verbatim under the Event's videos prefix — no transcoding,
// no processing state: the video is live the moment the PUT returns (ADR 0020).
func (s *Service) CreateVideoUploadURL(ctx context.Context, actor ActorContext, eventID string, input CreateVideoUploadURLInput) (*storage.CoverUploadResult, error) {
	if s.storage == nil {
		return nil, catalog.ErrVideoUploadUnavailable()
	}

	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	contentType := strings.ToLower(strings.TrimSpace(input.ContentType))
	if !storage.VideoContentTypeAllowed(contentType) {
		return nil, catalog.ErrInvalidCoverVideoKey()
	}

	key, err := storage.BuildVideoObjectKey(actor.OrganizationID, eventID, contentType)
	if err != nil {
		return nil, err
	}

	uploadURL, err := s.storage.PresignPut(ctx, key, contentType, 15*time.Minute)
	if err != nil {
		return nil, err
	}

	return &storage.CoverUploadResult{
		UploadURL: uploadURL,
		ObjectKey: key,
		PublicURL: s.storage.PublicURL(key),
	}, nil
}

func normalizeSlug(slug string) string {
	return strings.ToLower(strings.TrimSpace(slug))
}

func nullTimeFromPtr(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func nullStringFromPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: trimmed, Valid: true}
}

// nullInt64FromPtr is nullStringFromPtr's integer sibling. Nil means the caller
// said "no value" — for a Purchase Limit, that is the unrestricted state (ADR
// 0024). It does no range checking: a non-positive value is a validation
// failure the handler has already refused, not something to silently drop.
func nullInt64FromPtr(v *int) sql.NullInt64 {
	if v == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: int64(*v), Valid: true}
}

func toEventListItem(e *repository.Event) EventListItem {
	item := EventListItem{
		ID:           e.ID,
		Name:         e.Name,
		Slug:         e.Slug,
		Status:       string(e.Status),
		Discoverable: e.Discoverable,
		CreatedAt:    e.CreatedAt,
	}
	if e.StartsAt.Valid {
		t := e.StartsAt.Time
		item.StartsAt = &t
	}
	if e.Timezone.Valid {
		tz := e.Timezone.String
		item.Timezone = &tz
	}
	return item
}

// ListTicketTypes returns Ticket Types for an Event.
func (s *Service) ListTicketTypes(ctx context.Context, actor ActorContext, eventID string) ([]TicketTypeDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	currency, err := s.repo.GetOrganizationCurrency(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}

	types, err := s.repo.ListTicketTypesByEventID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}

	promotions, err := s.repo.ListPromotionsByEventID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}

	items := make([]TicketTypeDetail, 0, len(types))
	for _, tt := range types {
		var promotion *repository.TicketTypePromotion
		if p, ok := promotions[tt.ID]; ok {
			promotion = &p
		}
		items = append(items, toTicketTypeDetail(&tt, currency, promotion))
	}
	return items, nil
}

// CreateTicketType adds a Ticket Type to an Event.
func (s *Service) CreateTicketType(ctx context.Context, actor ActorContext, eventID string, input CreateTicketTypeInput) (*TicketTypeDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	// The exclusivity invariant, seen from the Ticket Type's side (ADR 0028). The
	// refusal names the mode rather than letting the organizer discover it as a
	// generic failure, and it is the same code every sale path will raise.
	if catalog.RegistrationModeOrDefault(event.RegistrationMode) == catalog.RegistrationModeExternal {
		return nil, catalog.ErrEventIsExternalRegistration()
	}

	currency, err := s.repo.GetOrganizationCurrency(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}

	sortOrder, err := s.repo.NextTicketTypeSortOrder(ctx, eventID)
	if err != nil {
		return nil, err
	}

	created, err := s.repo.CreateTicketType(ctx, actor.OrganizationID, eventID, repository.CreateTicketTypeParams{
		Name:           strings.TrimSpace(input.Name),
		Description:    nullStringFromPtr(input.Description),
		PriceCents:     input.PriceCents,
		Capacity:       input.Capacity,
		SortOrder:      sortOrder,
		MaxPerCustomer: nullInt64FromPtr(input.MaxPerCustomer),
		SalesCutoffAt:  nullTimeFromPtr(input.SalesCutoffAt),
	}, s.now())
	if err != nil {
		return nil, err
	}

	// A brand new Ticket Type has an empty Promotion slot.
	detail := toTicketTypeDetail(created, currency, nil)
	return &detail, nil
}

// UpdateTicketType updates a Ticket Type on an Event.
func (s *Service) UpdateTicketType(ctx context.Context, actor ActorContext, eventID, ticketTypeID string, input UpdateTicketTypeInput) (*TicketTypeDetail, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	existing, err := s.repo.GetTicketTypeByID(ctx, actor.OrganizationID, eventID, ticketTypeID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, catalog.ErrTicketTypeNotFound()
	}

	currency, err := s.repo.GetOrganizationCurrency(ctx, actor.OrganizationID)
	if err != nil {
		return nil, err
	}

	// The List Price side of the Promotion invariant: an edit that would leave
	// the Promotional Price at or above the List Price is refused, and staff
	// adjust or remove the Promotion first (ADR 0021). Only a Promotion that
	// has not yet ended has that claim — an ended one is a spent record, not a
	// standing veto on every future price cut.
	promotion, err := s.repo.GetPromotionByTicketTypeID(ctx, ticketTypeID)
	if err != nil {
		return nil, err
	}
	if toPromotion(promotion).ConstrainsListPriceAt(s.now()) && input.PriceCents <= promotion.PromotionalPriceCents {
		return nil, catalog.ErrListPriceNotAbovePromotionalPrice(input.PriceCents, promotion.PromotionalPriceCents)
	}

	updated, err := s.repo.UpdateTicketType(ctx, actor.OrganizationID, eventID, ticketTypeID, repository.UpdateTicketTypeParams{
		Name:           strings.TrimSpace(input.Name),
		Description:    nullStringFromPtr(input.Description),
		PriceCents:     input.PriceCents,
		Capacity:       input.Capacity,
		SortOrder:      input.SortOrder,
		MaxPerCustomer: nullInt64FromPtr(input.MaxPerCustomer),
		SalesCutoffAt:  nullTimeFromPtr(input.SalesCutoffAt),
	}, s.now())
	if err != nil {
		return nil, err
	}
	if updated == nil {
		return nil, catalog.ErrTicketTypeNotFound()
	}

	detail := toTicketTypeDetail(updated, currency, promotion)
	return &detail, nil
}

// DeleteTicketType removes a Ticket Type when the parent Event is draft.
func (s *Service) DeleteTicketType(ctx context.Context, actor ActorContext, eventID, ticketTypeID string) error {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return err
	}
	if event == nil {
		return catalog.ErrEventNotFound()
	}
	if event.Status != repository.EventStatusDraft {
		return catalog.ErrTicketTypeDeleteForbidden()
	}

	existing, err := s.repo.GetTicketTypeByID(ctx, actor.OrganizationID, eventID, ticketTypeID)
	if err != nil {
		return err
	}
	if existing == nil {
		return catalog.ErrTicketTypeNotFound()
	}

	if err := s.repo.DeleteTicketType(ctx, actor.OrganizationID, eventID, ticketTypeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return catalog.ErrTicketTypeNotFound()
		}
		return err
	}
	return nil
}

func toTicketTypeDetail(tt *repository.TicketType, currency string, promotion *repository.TicketTypePromotion) TicketTypeDetail {
	detail := TicketTypeDetail{
		Promotion:  toPromotionView(promotion),
		ID:         tt.ID,
		EventID:    tt.EventID,
		Name:       tt.Name,
		PriceCents: tt.PriceCents,
		Currency:   currency,
		Capacity:   tt.Capacity,
		SoldCount:  tt.SoldCount,
		SortOrder:  tt.SortOrder,
		CreatedAt:  tt.CreatedAt,
		UpdatedAt:  tt.UpdatedAt,
	}
	if tt.Description.Valid {
		s := tt.Description.String
		detail.Description = &s
	}
	detail.MaxPerCustomer = nullIntPtr(tt.MaxPerCustomer)
	detail.SalesCutoffAt = nullTimeOrNil(tt.SalesCutoffAt)
	return detail
}

func (s *Service) toEventDetail(e *repository.Event) EventDetail {
	detail := EventDetail{
		ID:                e.ID,
		Name:              e.Name,
		Slug:              e.Slug,
		Status:            string(e.Status),
		Discoverable:      e.Discoverable,
		FeeHandling:       e.FeeHandling,
		FeeBasisPoints:    s.fees.FeeBasisPoints,
		FeeIVABasisPoints: s.fees.FeeIVABasisPoints,
		// A row written before migration 048 reads as an ordinary ticketed Event.
		RegistrationMode:        string(catalog.RegistrationModeOrDefault(e.RegistrationMode)),
		RegistrationClickCount:  e.RegistrationClickCount,
		TicketQuestionsEnabled:  s.ticketQuestionsEnabled,
		TicketAssignmentEnabled: s.ticketAssignmentEnabled,
		CreatedAt:               e.CreatedAt,
	}
	if e.RegistrationURL.Valid {
		v := e.RegistrationURL.String
		detail.RegistrationURL = &v
	}
	if e.StartsAt.Valid {
		t := e.StartsAt.Time
		detail.StartsAt = &t
	}
	if e.EndsAt.Valid {
		t := e.EndsAt.Time
		detail.EndsAt = &t
	}
	if e.Timezone.Valid {
		v := e.Timezone.String
		detail.Timezone = &v
	}
	if e.VenueName.Valid {
		v := e.VenueName.String
		detail.VenueName = &v
	}
	if e.VenueAddress.Valid {
		v := e.VenueAddress.String
		detail.VenueAddress = &v
	}
	if e.Description.Valid {
		v := e.Description.String
		detail.Description = &v
	}
	if e.CoverImageKey.Valid {
		key := e.CoverImageKey.String
		detail.CoverImageKey = &key
		if s.storage != nil {
			url := s.storage.PublicURL(key)
			detail.CoverImageURL = &url
		}
	}
	if e.CoverVideoKey.Valid {
		key := e.CoverVideoKey.String
		detail.CoverVideoKey = &key
		if s.storage != nil {
			url := s.storage.PublicURL(key)
			detail.CoverVideoURL = &url
		}
	}
	return detail
}
