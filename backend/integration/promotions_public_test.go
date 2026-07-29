package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// The Storefront event page shows a live Promotion (issue #140, ADR 0021): the
// quoted price is the Promotional Price, and the List Price rides along so the
// page can strike it through, both as buyer prices carrying whatever the
// Event's Fee Handling adds. Outside the window the payload says nothing about
// the Promotion at all — the Storefront never decides whether a window is open.

type publicPromotionView struct {
	PromotionalPriceCents int    `json:"promotional_price_cents"`
	ListPriceCents        int    `json:"list_price_cents"`
	EndsAt                string `json:"ends_at"`
}

// TestPublicEventPageShowsTheLivePromotionUnderPassOn: under pass-on both
// figures carry the Platform Fee and its IVA — 499¢ quotes 557¢ and the struck
// 799¢ List Price quotes 891¢ — so the strikethrough compares two prices a
// buyer would actually have paid. A Ticket Type without a Promotion carries
// none.
func TestPublicEventPageShowsTheLivePromotionUnderPassOn(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Public Promo Pass On", "public-promo-pass-on", promoListPriceCents, 20)
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", promoListPriceCents, 10)
	setFeeHandling(t, env, sessionID, eventID, "Public Promo Pass On", "public-promo-pass-on", "pass_on")
	endsAt := env.fixedClock.Add(48 * time.Hour)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, endsAt)

	types := publicTicketTypes(t, env, "test-org", "public-promo-pass-on")

	ga := types["GA"]
	if ga.PriceCents != promoAllInCents {
		t.Fatalf("quoted price = %d; want the promotional all-in %d, the price checkout charges", ga.PriceCents, promoAllInCents)
	}
	if ga.Promotion == nil {
		t.Fatalf("promoted Ticket Type carries no promotion: %+v", ga)
	}
	want := publicPromotionView{
		PromotionalPriceCents: promoAllInCents,
		ListPriceCents:        feeTestAllInCents,
		EndsAt:                endsAt.UTC().Format(time.RFC3339),
	}
	got := *ga.Promotion
	if got.PromotionalPriceCents != want.PromotionalPriceCents || got.ListPriceCents != want.ListPriceCents {
		t.Fatalf("promotion prices = %+v; want %+v", got, want)
	}
	if !sameInstant(t, got.EndsAt, want.EndsAt) {
		t.Fatalf("promotion ends_at = %q; want %q", got.EndsAt, want.EndsAt)
	}

	balcony := types["Balcony"]
	if balcony.Promotion != nil {
		t.Fatalf("unpromoted Ticket Type carries a promotion: %+v", *balcony.Promotion)
	}
	if balcony.PriceCents != feeTestAllInCents {
		t.Fatalf("unpromoted price = %d; want the List Price all-in %d", balcony.PriceCents, feeTestAllInCents)
	}
}

// TestPublicEventPageShowsTheLivePromotionUnderAbsorb: under absorb the buyer
// pays exactly what the Organization set, so both figures are the raw
// Promotional Price and List Price.
func TestPublicEventPageShowsTheLivePromotionUnderAbsorb(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Public Promo Absorb", "public-promo-absorb", promoListPriceCents, 20)
	setFeeHandling(t, env, sessionID, eventID, "Public Promo Absorb", "public-promo-absorb", "absorb")
	endsAt := env.fixedClock.Add(48 * time.Hour)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, endsAt)

	ga := publicTicketTypes(t, env, "test-org", "public-promo-absorb")["GA"]
	if ga.PriceCents != promoPriceCents {
		t.Fatalf("quoted price = %d; want the Promotional Price %d", ga.PriceCents, promoPriceCents)
	}
	if ga.Promotion == nil {
		t.Fatalf("promoted Ticket Type carries no promotion: %+v", ga)
	}
	if ga.Promotion.PromotionalPriceCents != promoPriceCents || ga.Promotion.ListPriceCents != promoListPriceCents {
		t.Fatalf("promotion prices = %+v; want %d struck from %d", *ga.Promotion, promoPriceCents, promoListPriceCents)
	}
	if !sameInstant(t, ga.Promotion.EndsAt, endsAt.UTC().Format(time.RFC3339)) {
		t.Fatalf("promotion ends_at = %q; want %q", ga.Promotion.EndsAt, endsAt.UTC().Format(time.RFC3339))
	}
}

// TestPublicEventPageOmitsThePromotionOutsideItsWindow: before the start and
// after the end the page quotes the List Price and carries no Promotion, with
// no staff action at either edge.
func TestPublicEventPageOmitsThePromotionOutsideItsWindow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Public Promo Window", "public-promo-window", promoListPriceCents, 20)
	startsAt := env.fixedClock.Add(24 * time.Hour)
	endsAt := env.fixedClock.Add(48 * time.Hour)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, &startsAt, endsAt)

	before := publicTicketTypes(t, env, "test-org", "public-promo-window")["GA"]
	if before.Promotion != nil {
		t.Fatalf("promotion shown before it starts: %+v", *before.Promotion)
	}
	if before.PriceCents != feeTestAllInCents {
		t.Fatalf("price before the window = %d; want the List Price all-in %d", before.PriceCents, feeTestAllInCents)
	}

	holdClocksAt(env.fixedClock.Add(30 * time.Hour))
	inside := publicTicketTypes(t, env, "test-org", "public-promo-window")["GA"]
	if inside.Promotion == nil || inside.PriceCents != promoAllInCents {
		t.Fatalf("inside the window = %+v; want %d with a promotion", inside, promoAllInCents)
	}

	holdClocksAt(env.fixedClock.Add(72 * time.Hour))
	after := publicTicketTypes(t, env, "test-org", "public-promo-window")["GA"]
	if after.Promotion != nil {
		t.Fatalf("promotion shown after it ended: %+v", *after.Promotion)
	}
	if after.PriceCents != feeTestAllInCents {
		t.Fatalf("price after the window = %d; want the List Price all-in %d", after.PriceCents, feeTestAllInCents)
	}
}

// A listing card's "from" price is the cheapest thing a Customer could
// actually pay right now (issue #140), so a live Promotion has to reach the
// card and not just the event page: a card quoting the List Price while the
// event page quotes a lower promotional one would tell the buyer the Promotion
// does not exist until they click through.

// makeDiscoverable puts a published Event into the global explorer, the way an
// organizer would.
func makeDiscoverable(t *testing.T, env *testEnv, sessionID, eventID string) {
	t.Helper()
	resp, body := env.put(t, "/api/v1/staff/events/"+eventID+"/discoverable", map[string]any{
		"discoverable": true,
	}, authHeader(sessionID))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("set discoverable status=%d error=%+v", resp.StatusCode, body.Error)
	}
}

// explorerCard reads the global explorer and returns the card with the given slug.
func explorerCard(t *testing.T, env *testEnv, slug string) publicEventCard {
	t.Helper()
	resp, body := env.get(t, "/api/v1/public/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public events status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var page struct {
		Events []publicEventCard `json:"events"`
	}
	if err := json.Unmarshal(body.Data, &page); err != nil {
		t.Fatalf("decode explorer page: %v", err)
	}
	for _, ev := range page.Events {
		if ev.Slug == slug {
			return ev
		}
	}
	t.Fatalf("event %s not in explorer: %+v", slug, page.Events)
	return publicEventCard{}
}

// cardPriceFrom is a card's quoted "from" price, failing rather than
// dereferencing a nil when the Event advertises no price at all.
func cardPriceFrom(t *testing.T, card publicEventCard) int {
	t.Helper()
	if card.PriceFromCents == nil {
		t.Fatalf("card %s carries no price_from_cents: %+v", card.Slug, card)
	}
	return *card.PriceFromCents
}

// TestListingCardQuotesTheLivePromotionalFromPrice: two Ticket Types share a
// List Price and one of them is promoted, so the promoted one is the cheapest
// and the card must say so — 499¢ quoting the pass-on 557¢ rather than the
// 891¢ both List Prices would quote.
func TestListingCardQuotesTheLivePromotionalFromPrice(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Card Promo Fest", "card-promo-fest", promoListPriceCents, 20)
	createTicketTypeWithCapacity(t, env, sessionID, eventID, "Balcony", promoListPriceCents, 10)
	makeDiscoverable(t, env, sessionID, eventID)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, env.fixedClock.Add(48*time.Hour))

	if got := cardPriceFrom(t, explorerCard(t, env, "card-promo-fest")); got != promoAllInCents {
		t.Fatalf("card price_from_cents = %d; want the promotional all-in %d", got, promoAllInCents)
	}

	// The event page and the card have to agree: the cheapest Ticket Type on
	// the page is the number the card promised.
	if page := publicTicketTypes(t, env, "test-org", "card-promo-fest")["GA"]; page.PriceCents != promoAllInCents {
		t.Fatalf("event page GA price = %d; want the same %d the card quotes", page.PriceCents, promoAllInCents)
	}
}

// TestListingCardQuotesTheListPriceOutsideThePromotionWindow: the card follows
// the same half-open window the rest of the system prices from — the List
// Price before the start and after the end, the Promotional Price in between,
// with no staff action at either edge.
func TestListingCardQuotesTheListPriceOutsideThePromotionWindow(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Card Window Fest", "card-window-fest", promoListPriceCents, 20)
	makeDiscoverable(t, env, sessionID, eventID)
	startsAt := env.fixedClock.Add(2 * time.Hour)
	endsAt := env.fixedClock.Add(4 * time.Hour)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, &startsAt, endsAt)

	if got := cardPriceFrom(t, explorerCard(t, env, "card-window-fest")); got != feeTestAllInCents {
		t.Fatalf("card price before the window = %d; want the List Price all-in %d", got, feeTestAllInCents)
	}

	holdClocksAt(startsAt)
	if got := cardPriceFrom(t, explorerCard(t, env, "card-window-fest")); got != promoAllInCents {
		t.Fatalf("card price at the start of the window = %d; want the promotional all-in %d", got, promoAllInCents)
	}

	holdClocksAt(endsAt)
	if got := cardPriceFrom(t, explorerCard(t, env, "card-window-fest")); got != feeTestAllInCents {
		t.Fatalf("card price at the end of the window = %d; want the List Price all-in %d", got, feeTestAllInCents)
	}
}

// TestOrganizationPageCardQuotesTheLivePromotionalFromPrice: the Organization
// page runs a second query over the same card shape, so it has to answer the
// same way the explorer does.
func TestOrganizationPageCardQuotesTheLivePromotionalFromPrice(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	eventID, gaID := publishCheckoutEvent(t, env, sessionID, "Org Card Promo", "org-card-promo", promoListPriceCents, 20)
	makeDiscoverable(t, env, sessionID, eventID)
	setPromotion(t, env, sessionID, eventID, gaID, promoPriceCents, nil, env.fixedClock.Add(48*time.Hour))

	resp, body := env.get(t, "/api/v1/public/organizations/test-org/events", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("public organization status=%d error=%+v", resp.StatusCode, body.Error)
	}
	var orgPage struct {
		Upcoming []publicEventCard `json:"upcoming"`
	}
	if err := json.Unmarshal(body.Data, &orgPage); err != nil {
		t.Fatalf("decode organization page: %v", err)
	}
	for _, card := range orgPage.Upcoming {
		if card.Slug != "org-card-promo" {
			continue
		}
		if got := cardPriceFrom(t, card); got != promoAllInCents {
			t.Fatalf("org card price_from_cents = %d; want the promotional all-in %d", got, promoAllInCents)
		}
		return
	}
	t.Fatalf("event org-card-promo not on the organization page: %+v", orgPage.Upcoming)
}
