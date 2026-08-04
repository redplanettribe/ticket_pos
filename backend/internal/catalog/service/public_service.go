package service

import (
	"context"
	"database/sql"
	"encoding/base64"
	"strings"
	"time"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/sales"
)

const (
	defaultPublicEventLimit = 20
	maxPublicEventLimit     = 50
)

// PublicOrganizationSummary is the trust/attribution context shown with an Event.
//
// One structure, three payloads: the Event detail, every Event card in the
// global explorer, and the Organization page. SupportWhatsApp is the one field
// that is NOT the same on all three — it is populated only on the Event detail,
// and is always absent from the two listings. Build it with publicOrgSummary
// unless you are building the detail, in which case use
// publicOrgSummaryWithSupport.
//
// That asymmetry is deliberate and is the whole of ADR 0029's exposure decision.
// Publishing an Organization's support number on the Event detail is the point
// of the feature and the organizer opted into it. Publishing it on a paginated,
// unauthenticated listing is different in kind: it would let one crawl of the
// platform harvest every Organization's support number at once, rather than
// requiring each Event detail to be discovered and fetched. An integration test
// asserts the listings stay clean — if you are here to "tidy up" by moving this
// into the shared helper, that test is why you should not.
//
// The codebase already runs this shape elsewhere: PublicTicketType.AlreadyHeld
// arrives only when the request carries a Customer Session.
type PublicOrganizationSummary struct {
	Name    string  `json:"name"`
	Slug    string  `json:"slug"`
	LogoURL *string `json:"logo_url"`
	// SupportWhatsApp is the Organization's Support WhatsApp number in canonical
	// E.164 form — a link a Customer taps to message them for support.
	//
	// DELIBERATELY PUBLIC. An Org Admin entered it in a form that states it is
	// published on every one of their Event pages and that Customers will message
	// it; it is safe to display and was not exposed by accident. Omitted entirely
	// when the Organization has none, so a client branches on the key's presence
	// rather than on an empty string.
	//
	// Served on the Event detail only. It is absent from the Event listings by
	// design, so do not read it from a card — see ADR 0029.
	SupportWhatsApp *string `json:"support_whatsapp,omitempty"`
}

// PublicEventCard is a summary row for Storefront listings (global explorer and org page).
type PublicEventCard struct {
	Slug           string                    `json:"slug"`
	Name           string                    `json:"name"`
	StartsAt       *time.Time                `json:"starts_at"`
	EndsAt         *time.Time                `json:"ends_at"`
	Timezone       *string                   `json:"timezone"`
	VenueName      *string                   `json:"venue_name"`
	CoverImageURL  *string                   `json:"cover_image_url"`
	Organization   PublicOrganizationSummary `json:"organization"`
	Currency       string                    `json:"currency"`
	PriceFromCents *int                      `json:"price_from_cents"`
	SoldOut        bool                      `json:"sold_out"`
	Tags           []TagView                 `json:"tags"`
	// RegistrationMode is how this Event takes sign-ups: 'tickets' (it sells
	// Ticket Types here) or 'external' (it hands its audience to a Registration
	// Link elsewhere). Never both (ADR 0028).
	//
	// A listing card needs it because a null price_from_cents alone cannot be
	// read: on a ticketed Event it is a data anomaly, and on an external one it
	// is the normal, permanent state — the platform does not know what the other
	// site charges and never will. The mode is what lets the card say so in
	// words instead of leaving the price slot blank and looking broken.
	//
	// The Registration Link itself is deliberately not here. A card is an
	// invitation to the Event page, and the destination is named there, next to
	// the button that goes to it; a listing that linked straight out would hand
	// a Customer to a stranger from a surface that never told them where.
	RegistrationMode string `json:"registration_mode"`
}

// PublicTicketType is a Ticket Type as shown on a Storefront event page.
// ID is exposed so the Storefront can name the Ticket Type in a begin-checkout
// request, whose lines are keyed by ticket_type_id (issue #84).
//
// PriceCents is the effective buyer price — what checkout will charge per
// ticket, already carrying the Platform Fee and Fee IVA under 'pass_on' Fee
// Handling. The Storefront never computes fees: it quotes this number, so the
// price a Customer sees can never jump at checkout (ADR 0014).
type PublicTicketType struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description *string          `json:"description"`
	PriceCents  int              `json:"price_cents"`
	Currency    string           `json:"currency"`
	Remaining   int              `json:"remaining"`
	SoldOut     bool             `json:"sold_out"`
	Promotion   *PublicPromotion `json:"promotion"`
	// MaxPerCustomer is the Purchase Limit, or null when this Ticket Type is
	// unrestricted. The Storefront bounds its quantity picker by it so a buyer is
	// never invited to choose a quantity that will be refused (ADR 0025).
	//
	// Unlike PriceCents it is a raw count, untouched by Fee Handling or Promotion
	// arithmetic — it counts tickets, not money. It states the Ticket Type's rule
	// and nothing about any Customer's holdings: an anonymous reader learns the
	// limit, never who has already used theirs up.
	MaxPerCustomer *int `json:"max_per_customer"`
	// AlreadyHeld is how many of this Ticket Type the Customer who asked for this
	// page already holds — their active Ticket Sales plus their live Capacity
	// Holds, the same count begin-checkout refuses on, and the same word its
	// PURCHASE_LIMIT_EXCEEDED details use, so a client learns one name for one
	// idea (ADR 0025, #168).
	//
	// It is NULL for an anonymous read, and that is not the same statement as 0.
	// Zero says "you hold none of these"; null says "we do not know who you are",
	// and the Storefront must not turn the second into the first — an anonymous
	// visitor learns of their allowance at submit, which is the first moment they
	// have told us who they are. Nobody ever reads anybody else's figure: it is
	// derived from the Customer Session the request carried and from nothing in
	// the URL, so there is no address a caller can ask about but their own.
	//
	// It is reported for every Ticket Type a signed-in Customer reads, including
	// unrestricted ones, so that null keeps meaning "anonymous" and never doubles
	// as "unrestricted" — max_per_customer already says that, and one field
	// answering two questions is how a picker ends up bounding the wrong thing.
	// It may legitimately exceed max_per_customer: lowering a Purchase Limit is
	// not retroactive, so remaining allowance is max(0, limit - already_held) and
	// is never asserted non-negative.
	AlreadyHeld *int `json:"already_held"`
}

// PublicPromotion is a live Promotion as the Storefront shows it (ADR 0021):
// what the ticket costs now, what it costs without the Promotion (the number
// the page strikes through), and when the Promotion ends.
// It is present only while the Promotion is live at the moment the page was
// rendered; null covers no Promotion, one not yet started, and one already
// ended alike, so the Storefront never evaluates a window itself and a page
// cached past an expiry cannot advertise a price checkout would not honor.
// Both amounts are buyer prices, carrying the Platform Fee and Fee IVA under
// 'pass_on' Fee Handling exactly as price_cents does, so the saving a Customer
// reads off the page is the saving they get at checkout. PromotionalPriceCents
// is deliberately the same number as the Ticket Type's price_cents while the
// Promotion is live — the Storefront should never have to know that the quoted
// price and the promotional price are the same thing.
type PublicPromotion struct {
	PromotionalPriceCents int       `json:"promotional_price_cents"`
	ListPriceCents        int       `json:"list_price_cents"`
	EndsAt                time.Time `json:"ends_at"`
}

// PublicEventDetail is the full Storefront event page payload.
type PublicEventDetail struct {
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	Description   *string    `json:"description"`
	StartsAt      *time.Time `json:"starts_at"`
	EndsAt        *time.Time `json:"ends_at"`
	Timezone      *string    `json:"timezone"`
	VenueName     *string    `json:"venue_name"`
	VenueAddress  *string    `json:"venue_address"`
	CoverImageURL *string    `json:"cover_image_url"`
	// CoverVideoURL is the Event's optional Cover Video, played in the hero over
	// the Cover Image poster. Only the key's derived URL is public; listings and
	// link previews stay on the Cover Image (ADR 0020).
	CoverVideoURL *string                   `json:"cover_video_url"`
	HasEnded      bool                      `json:"has_ended"`
	Organization  PublicOrganizationSummary `json:"organization"`
	Currency      string                    `json:"currency"`
	// PriceIncludesFee reports that the quoted prices carry the platform's
	// service fee — true under 'pass_on' Fee Handling. It is the whole basis of
	// the Storefront's single muted "includes service fee" note: under 'absorb'
	// the buyer pays exactly what the Organization set and no fee is mentioned
	// anywhere. No amount is exposed; the platform's cut is never a number a
	// Customer sees (ADR 0014).
	PriceIncludesFee bool               `json:"price_includes_fee"`
	TicketTypes      []PublicTicketType `json:"ticket_types"`
	Tags             []TagView          `json:"tags"`
	// Discoverable mirrors the Event's Discoverable flag so the Storefront can
	// mark a non-Discoverable Event noindex. ADR 0002 draws the line between
	// reachable and advertised: a published Event is always reachable by direct
	// link, and Discoverable only decides whether the platform's own surfaces
	// advertise it. Exposing the flag extends that same distinction to crawlers —
	// it says nothing about who may load the page, only about who should index
	// it, so reachability is unchanged.
	Discoverable bool `json:"discoverable"`
	// RegistrationMode is how this Event takes sign-ups: 'tickets' (it sells
	// Ticket Types here) or 'external' (it hands its audience to the Registration
	// Link). Never both (ADR 0028). It is what tells the Storefront whether to
	// render a ticket selector at all, so it is stated on every Event rather than
	// inferred from an empty ticket_types — an Event whose Ticket Types are all
	// sold out is not the same page as one that sells nothing here.
	RegistrationMode string `json:"registration_mode"`
	// RegistrationURL is the Registration Link, present only on an external
	// Event. The Storefront needs the URL itself and not merely the fact of it:
	// the Register panel names the destination's hostname beneath the call to
	// action, because a Customer being handed to a stranger should learn which
	// one before they click rather than after.
	//
	// It is null for a ticketed Event even if a link is stored — a link a
	// ticketed Event does not use is nothing a Customer should be offered — and
	// null for an external Event still missing one, which a published Event
	// cannot be (the publish gate requires it, #208).
	RegistrationURL *string `json:"registration_url"`
}

// PublicEventPage is one page of global explorer results.
type PublicEventPage struct {
	Events     []PublicEventCard `json:"events"`
	NextCursor *string           `json:"next_cursor"`
}

// PublicOrganizationEvents is an Organization page: its summary plus split listings.
type PublicOrganizationEvents struct {
	Organization PublicOrganizationSummary `json:"organization"`
	Upcoming     []PublicEventCard         `json:"upcoming"`
	Past         []PublicEventCard         `json:"past"`
}

// PublicEventQuery constrains the global explorer.
type PublicEventQuery struct {
	Query  string
	From   *time.Time
	To     *time.Time
	Limit  int
	Cursor string
	// Tags are raw tag names; an Event matches if it carries any of them (OR
	// within the facet). Canonicalized server-side before matching.
	Tags []string
}

// ListDiscoverableEvents returns a page of published, discoverable, not-yet-ended
// Events across all Organizations, soonest first.
func (s *Service) ListDiscoverableEvents(ctx context.Context, q PublicEventQuery) (*PublicEventPage, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = defaultPublicEventLimit
	}
	if limit > maxPublicEventLimit {
		limit = maxPublicEventLimit
	}

	cursorStartsAt, cursorID := decodeCursor(q.Cursor)

	rows, err := s.repo.ListDiscoverableEvents(ctx, repository.PublicEventFilter{
		Query:          strings.TrimSpace(q.Query),
		From:           q.From,
		To:             q.To,
		Now:            s.now(),
		Limit:          limit + 1,
		CursorStartsAt: cursorStartsAt,
		CursorID:       cursorID,
		TagKeys:        canonicalTagKeys(q.Tags),
	})
	if err != nil {
		return nil, err
	}

	pageRows := rows
	if len(rows) > limit {
		pageRows = rows[:limit]
	}
	tagViews, err := s.tagViewsByEventIDs(ctx, eventIDsOf(pageRows))
	if err != nil {
		return nil, err
	}

	page := &PublicEventPage{Events: make([]PublicEventCard, 0, limit)}
	for i := range rows {
		if i == limit {
			last := rows[limit-1]
			cursor := encodeCursor(last.StartsAt.Time, last.ID)
			page.NextCursor = &cursor
			break
		}
		page.Events = append(page.Events, s.toPublicEventCard(&rows[i], tagViewsFor(tagViews, rows[i].ID)))
	}
	return page, nil
}

// eventIDsOf collects the Event IDs from a set of public rows.
func eventIDsOf(rows []repository.PublicEventRow) []string {
	ids := make([]string, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	return ids
}

// GetOrganizationEvents returns an Organization's public profile with its
// discoverable Events split into upcoming and past.
func (s *Service) GetOrganizationEvents(ctx context.Context, orgSlug string) (*PublicOrganizationEvents, error) {
	orgSlug = strings.ToLower(strings.TrimSpace(orgSlug))
	org, err := s.repo.GetPublicOrganizationBySlug(ctx, orgSlug)
	if err != nil {
		return nil, err
	}
	if org == nil {
		return nil, catalog.ErrOrganizationNotFound()
	}

	now := s.now()
	rows, err := s.repo.ListDiscoverableEventsByOrganization(ctx, org.ID, now)
	if err != nil {
		return nil, err
	}

	tagViews, err := s.tagViewsByEventIDs(ctx, eventIDsOf(rows))
	if err != nil {
		return nil, err
	}
	result := &PublicOrganizationEvents{
		Organization: s.publicOrgSummary(org.Name, org.Slug, org.LogoImageKey),
		Upcoming:     make([]PublicEventCard, 0),
		Past:         make([]PublicEventCard, 0),
	}
	// rows are ascending by start; past is shown most-recent first.
	for i := range rows {
		card := s.toPublicEventCard(&rows[i], tagViewsFor(tagViews, rows[i].ID))
		if eventEnded(&rows[i], now) {
			result.Past = append([]PublicEventCard{card}, result.Past...)
		} else {
			result.Upcoming = append(result.Upcoming, card)
		}
	}
	return result, nil
}

// PublicEventViewer is the Customer a public Event read was made by, when the
// request proved who that is. A nil *PublicEventViewer is an anonymous read and
// is the ordinary case: the Storefront event page is public, and everything on
// it except the viewer's own holdings is the same for everybody.
//
// It carries an email rather than a Customer id because that is what identity
// rests on (ADR 0010) and because a buyer part-way through their first purchase
// has no Customer record yet while their live Capacity Holds still count.
type PublicEventViewer struct {
	// Email is an address the REQUEST PROVED the caller owns — a full Customer
	// Session's own address and nothing else. It must never be filled from a
	// query parameter, a header, or a request body: an anonymous caller able to
	// name any address would have an oracle telling them whether that address had
	// bought a given Ticket Type (ADR 0025).
	Email string
}

// GetPublicEvent returns the Storefront event page for a published Event,
// reachable by direct link regardless of discoverability.
//
// The viewer, when there is one, adds their own holdings per Ticket Type and
// changes nothing else about the page: every other figure a Customer reads is
// the one an anonymous visitor reads, so the Event page stays cacheable in the
// only shape most requests come in.
func (s *Service) GetPublicEvent(ctx context.Context, orgSlug, eventSlug string, viewer *PublicEventViewer) (*PublicEventDetail, error) {
	orgSlug = strings.ToLower(strings.TrimSpace(orgSlug))
	eventSlug = strings.ToLower(strings.TrimSpace(eventSlug))

	now := s.now()
	row, err := s.repo.GetPublishedEventBySlug(ctx, orgSlug, eventSlug, now)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, catalog.ErrEventNotFound()
	}

	types, err := s.repo.ListTicketTypesByEventID(ctx, row.OrganizationID, row.ID)
	if err != nil {
		return nil, err
	}
	// Live Capacity Holds count against what the Storefront advertises as
	// remaining (ADR 0013): tickets pending Payments speak for are not for sale
	// until those Payments settle or their holds lapse.
	held, err := s.repo.LiveCapacityHolds(ctx, row.ID, sales.HoldCutoff(now))
	if err != nil {
		return nil, err
	}
	// What THIS Customer already holds, counted by sales because the Purchase
	// Limit's count is its rule (ADR 0025). Read for every Ticket Type of the
	// Event in one query, whether or not any of them is rationed: it costs the
	// same read either way, and skipping it on an unrestricted Event would make a
	// signed-in Customer's already_held null, which this payload reserves for
	// saying we do not know who is asking.
	var ownHoldings map[string]int
	if viewer != nil {
		ownHoldings, err = s.holdings.CustomerEventHoldings(ctx, row.ID, viewer.Email)
		if err != nil {
			return nil, err
		}
	}

	tags, err := s.repo.ListEventTags(ctx, row.ID)
	if err != nil {
		return nil, err
	}

	promotions, err := s.repo.ListPromotionsByEventID(ctx, row.OrganizationID, row.ID)
	if err != nil {
		return nil, err
	}

	handling := sales.FeeHandlingOrDefault(row.FeeHandling)
	mode := catalog.RegistrationModeOrDefault(row.RegistrationMode)
	detail := &PublicEventDetail{
		Slug:             row.Slug,
		Name:             row.Name,
		Description:      nullStringPtr(row.Description),
		CoverImageURL:    s.coverURL(row.CoverImageKey),
		CoverVideoURL:    s.coverURL(row.CoverVideoKey),
		HasEnded:         eventEnded(row, now),
		Organization:     s.publicOrgSummaryWithSupport(row.OrgName, row.OrgSlug, row.OrgLogoKey, row.OrgSupportWhatsApp),
		Currency:         row.OrgCurrency,
		PriceIncludesFee: handling == sales.FeeHandlingPassOn,
		TicketTypes:      make([]PublicTicketType, 0, len(types)),
		Tags:             toTagViews(tags),
		Discoverable:     row.Discoverable,
		RegistrationMode: string(mode),
	}
	// The Registration Link travels only on the Event that actually registers
	// through it.
	if mode == catalog.RegistrationModeExternal {
		detail.RegistrationURL = nullStringPtr(row.RegistrationURL)
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time
		detail.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time
		detail.EndsAt = &t
	}
	detail.Timezone = nullStringPtr(row.Timezone)
	detail.VenueName = nullStringPtr(row.VenueName)
	detail.VenueAddress = nullStringPtr(row.VenueAddress)

	for _, tt := range types {
		remaining := tt.Capacity - tt.SoldCount - held[tt.ID]
		if remaining < 0 {
			remaining = 0
		}
		// The quoted price is the effective price at `now` — the same question
		// begin-checkout asks — so the price shown is the price charged.
		promotion := promotionFor(promotions, tt.ID)
		baseCents := catalog.EffectiveBasePriceCents(tt.PriceCents, promotion, now)
		detail.TicketTypes = append(detail.TicketTypes, PublicTicketType{
			ID:          tt.ID,
			Name:        tt.Name,
			Description: nullStringPtr(tt.Description),
			PriceCents:  s.fees.BuyerUnitPriceCents(handling, baseCents),
			Currency:    row.OrgCurrency,
			Remaining:   remaining,
			SoldOut:     remaining == 0,
			Promotion:   s.toPublicPromotion(handling, tt.PriceCents, promotion, now),

			MaxPerCustomer: nullIntPtr(tt.MaxPerCustomer),
			AlreadyHeld:    viewerHoldingOf(viewer, ownHoldings, tt.ID),
		})
	}
	return detail, nil
}

// viewerHoldingOf renders one Ticket Type's already_held: nil for an anonymous
// read, and a number — zero included — for a known Customer, since a Ticket Type
// they hold none of is absent from the count and "none" is a real answer to give
// somebody we can identify (ADR 0025).
func viewerHoldingOf(viewer *PublicEventViewer, holdings map[string]int, ticketTypeID string) *int {
	if viewer == nil {
		return nil
	}
	held := holdings[ticketTypeID]
	return &held
}

// promotionFor pulls a Ticket Type's Promotion out of an Event's batch-loaded
// set as the domain shape the effective-price rule is expressed over, or nil
// when the slot is empty.
func promotionFor(promotions map[string]repository.TicketTypePromotion, ticketTypeID string) *catalog.Promotion {
	p, ok := promotions[ticketTypeID]
	if !ok {
		return nil
	}
	return toPromotion(&p)
}

// toPublicPromotion renders a Promotion for the Storefront, but only while it
// is live: outside its window there is nothing for a Customer to know about it,
// and a Promotion the page could see but not use would invite the Storefront to
// decide for itself when it applies.
func (s *Service) toPublicPromotion(
	handling sales.FeeHandling,
	listPriceCents int,
	promotion *catalog.Promotion,
	now time.Time,
) *PublicPromotion {
	if !promotion.LiveAt(now) {
		return nil
	}
	return &PublicPromotion{
		PromotionalPriceCents: s.fees.BuyerUnitPriceCents(handling, promotion.PromotionalPriceCents),
		ListPriceCents:        s.fees.BuyerUnitPriceCents(handling, listPriceCents),
		EndsAt:                promotion.EndsAt,
	}
}

func (s *Service) toPublicEventCard(row *repository.PublicEventRow, tags []TagView) PublicEventCard {
	mode := catalog.RegistrationModeOrDefault(row.RegistrationMode)
	card := PublicEventCard{
		Slug:             row.Slug,
		Name:             row.Name,
		VenueName:        nullStringPtr(row.VenueName),
		Timezone:         nullStringPtr(row.Timezone),
		CoverImageURL:    s.coverURL(row.CoverImageKey),
		Organization:     s.publicOrgSummary(row.OrgName, row.OrgSlug, row.OrgLogoKey),
		Currency:         row.OrgCurrency,
		Tags:             tags,
		RegistrationMode: string(mode),
	}
	if row.StartsAt.Valid {
		t := row.StartsAt.Time
		card.StartsAt = &t
	}
	if row.EndsAt.Valid {
		t := row.EndsAt.Time
		card.EndsAt = &t
	}
	// An externally registered Event sells nothing here, so the two claims a card
	// makes about a sale — what it costs and whether any is left — are claims
	// this platform is not in a position to make. Both are answered here, by the
	// mode, rather than left to the Ticket Type aggregates: over an Event with no
	// Ticket Types those aggregates are NULL, and a NULL sold_out landing on
	// false is an accident that happens to be right today. Saying it explicitly
	// means an external Event stays not-sold-out even if the aggregate's shape
	// ever changes, and it can never be sold out for real: it has no capacity on
	// this platform to exhaust.
	if mode == catalog.RegistrationModeExternal {
		return card
	}
	if row.MinPriceCents.Valid {
		// The card's "from" price is a buyer price too: a Customer must never see
		// one number on a listing and a higher one on the event page. The buyer
		// price rises with the base price, so the cheapest Ticket Type is still
		// the cheapest after the fee is added.
		v := s.fees.BuyerUnitPriceCents(sales.FeeHandlingOrDefault(row.FeeHandling), int(row.MinPriceCents.Int64))
		card.PriceFromCents = &v
	}
	if row.AllSoldOut.Valid {
		card.SoldOut = row.AllSoldOut.Bool
	}
	return card
}

// tagViewsByEventIDs batch-loads Tags for a set of Events and projects them into
// TagViews keyed by Event ID, Preset Tags first. Events with no Tags are absent
// from the map; callers use tagViewsFor to get a stable empty slice.
func (s *Service) tagViewsByEventIDs(ctx context.Context, eventIDs []string) (map[string][]TagView, error) {
	tagsByEvent, err := s.repo.ListTagsByEventIDs(ctx, eventIDs)
	if err != nil {
		return nil, err
	}
	views := make(map[string][]TagView, len(tagsByEvent))
	for id, tags := range tagsByEvent {
		views[id] = toTagViews(tags)
	}
	return views, nil
}

// tagViewsFor returns the Event's TagViews, or an empty (non-nil) slice so the
// field serializes as [] rather than null.
func tagViewsFor(views map[string][]TagView, eventID string) []TagView {
	if v, ok := views[eventID]; ok {
		return v
	}
	return make([]TagView, 0)
}

func (s *Service) publicOrgSummary(name, slug string, logoKey sql.NullString) PublicOrganizationSummary {
	summary := PublicOrganizationSummary{Name: name, Slug: slug}
	if logoKey.Valid && s.storage != nil {
		url := s.storage.PublicURL(logoKey.String)
		summary.LogoURL = &url
	}
	return summary
}

// publicOrgSummaryWithSupport is publicOrgSummary plus the Organization's
// Support WhatsApp number, and exists so that adding the number is a decision
// made at one call site rather than a property of the summary everywhere.
//
// Only the Event detail calls this. The two listing payloads call
// publicOrgSummary and must keep doing so — see the note on
// PublicOrganizationSummary and ADR 0029 for why a paginated endpoint carrying
// this number is a different exposure than a detail endpoint carrying it.
func (s *Service) publicOrgSummaryWithSupport(name, slug string, logoKey, supportWhatsApp sql.NullString) PublicOrganizationSummary {
	summary := s.publicOrgSummary(name, slug, logoKey)
	if supportWhatsApp.Valid && supportWhatsApp.String != "" {
		summary.SupportWhatsApp = &supportWhatsApp.String
	}
	return summary
}

func (s *Service) coverURL(key sql.NullString) *string {
	if key.Valid && s.storage != nil {
		url := s.storage.PublicURL(key.String)
		return &url
	}
	return nil
}

// eventEnded reports whether an Event's end (or start, if no end) is in the past.
func eventEnded(row *repository.PublicEventRow, now time.Time) bool {
	switch {
	case row.EndsAt.Valid:
		return row.EndsAt.Time.Before(now)
	case row.StartsAt.Valid:
		return row.StartsAt.Time.Before(now)
	default:
		return false
	}
}

func nullStringPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

// nullIntPtr is nullStringPtr's integer sibling, so a nullable count reaches JSON
// as null rather than zero. For a Purchase Limit the difference is the whole
// meaning: null is "unrestricted", and 0 is a value the column forbids.
func nullIntPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func encodeCursor(t time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(t.UTC().Format(time.RFC3339Nano) + "|" + id))
}

// decodeCursor parses a keyset cursor. An unparseable cursor is treated as absent
// so a bad value simply starts from the first page rather than erroring.
func decodeCursor(cursor string) (*time.Time, string) {
	if strings.TrimSpace(cursor) == "" {
		return nil, ""
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, ""
	}
	parts := strings.SplitN(string(raw), "|", 2)
	if len(parts) != 2 {
		return nil, ""
	}
	t, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, ""
	}
	return &t, parts[1]
}
