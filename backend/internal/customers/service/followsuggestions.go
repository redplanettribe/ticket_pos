package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/peter/ticket_pos/backend/internal/customers/repository"
)

// The Suggested Follow (#231, #232 and #233, parent #229, ADR 0031): a Tag or an
// Organization the Customer does not Follow, offered to them as one they might.
//
// TWO RANKINGS, ONE BEHIND THE OTHER, and which one a Customer meets depends
// only on whether they Follow anything.
//
//   - CO-OCCURRENCE first, for a Customer who Follows anything at all: the Tags
//     that ride alongside their own on real Events, and the Organizations whose
//     upcoming Events carry them. This is what makes the panel about this reader
//     rather than about the catalogue, and each of these suggestions names the
//     Tag that produced it. The seeds are the Tags the Customer CHOSE and, below
//     them in weight, the DERIVED Tags carried by the upcoming Events of the
//     Organizations they Follow (#233) — so Following an Organization, which is
//     the natural first Follow for somebody who came to buy a ticket, is read as
//     the statement it is.
//   - ACTIVITY behind it — the count of discoverable upcoming Events carrying a
//     Tag or run by an Organization — filling whatever slots Co-occurrence left.
//     For a Customer who Follows NOTHING it fills all of them, which is #231's
//     ranking unchanged and is the right answer for somebody the platform knows
//     nothing about yet.
//
// The order of the two is the whole personalisation, and the fallback is not a
// second-class path: a Customer who Follows one narrow Tag has very few
// co-occurring candidates at this catalogue size, and a panel that stopped there
// would be one chip long.
//
// Activity measures supply and never audience; it counts Events, not the
// Customers who Follow (CONTEXT.md). Co-occurrence is a fact about the catalogue
// and never about other Customers — there is no collaborative filtering here in
// any disguise, and deriving Tags from a followed Organization does not make it
// one: a derived Tag is read from this Customer's own Follow joined to the
// catalogue, and no other Customer's Follows are touched by any query here.
//
// A SEPARATE READ FROM THE FOLLOWS LISTING, deliberately. That listing is not a
// page-local read: ADR 0030 built no per-subject "do I Follow this" probe, so
// the explorer and every Event and Organization page call it to decide whether
// each Follow control is drawn filled. Folding a ranking query into it would run
// this work on every render of the platform's hot public surfaces and throw the
// result away. Two reads also let the two hold different postures — the listing
// must be exact, because a stale one draws a wrong heart, while suggestions are
// advisory and a failure among them degrades to no panel at all.

// maxSuggestedTags caps the Tag group.
//
// Small because the group is a CHIP ROW and a chip row is read at a glance: past
// about half a dozen it stops being an offer and becomes a second tag bar
// competing with the explorer's, which is the feed ADR 0030 declined to build.
// The cap is also a quality floor by arithmetic — the guards below it (the Custom
// Tag floor, excluding existing Follows, discoverable-and-upcoming only) already
// thin the pool, so anything past the fifth is a Tag with almost nothing behind
// it.
const maxSuggestedTags = 5

// maxSuggestions caps the panel as a whole, and the gap between the two
// constants is what lets Organizations take the slots the Tags did not fill.
//
// The backfill runs one way only: few Tags means more Organizations, never the
// reverse. Organizations are the unbounded supply — every Organization running
// anything upcoming is a defensible suggestion, where the fifth-best Tag at this
// catalogue size may be carried by two Events and mean nothing — so the freed
// space goes to the side whose relevance is easier to defend (ADR 0031). Padding
// with weaker Tags was the alternative and is exactly what the spec's "show what
// qualifies rather than pad to a fixed count" rules out.
const maxSuggestions = 10

// SuggestionReason is why a subject is being offered: the Tag that produced it.
//
// NAMED BY CANONICAL KEY AND NEVER AS A SENTENCE. The Storefront words a Tag
// from its own message catalogues everywhere else (ADR 0027), and a reason
// composed here would be the one place a Tag's name crossed the wire in a
// language — English, from an API that has no idea which Locale the page is in,
// arriving inside a Spanish panel. A struct rather than a bare nullable string so
// that a later reason naming more than one Tag widens this rather than replacing
// it.
//
// ONE TAG AND NOT A LIST, deliberately. A candidate may co-occur with several of
// the Customer's Follows, and the Storefront has one line to say it in; naming
// the strongest contributor is a sentence a reader can check against the Event
// they land on, where three Tags joined by commas is a report on the algorithm.
//
// It is nil on everything the Activity ranking returns, which is every
// suggestion made to a Customer who Follows nothing at all. Activity has no
// producing Tag: the subject is offered because things are happening under it,
// not because of anything the Customer Follows.
//
// A DERIVED TAG NAMES ITSELF HERE, and not the Organization it was inferred
// from (#233). The reason is a claim about the CATALOGUE — the Storefront words
// it "Goes with X", meaning this candidate rides alongside X on real Events —
// which is as true of a derived Tag as of a chosen one, and is the only relation
// the ranking actually measured. Naming the Organization would turn the sentence
// into a claim about the reader, "because you Follow them", which is a second
// shape on the wire, new copy in both message catalogues, and a report on the
// inference rather than something the reader can check against the Event they
// land on.
type SuggestionReason struct {
	TagCanonicalKey string `json:"tag_canonical_key"`
}

// SuggestedTagView is one Tag being offered, with the reason it was chosen.
//
// The subject is FollowedTagView — the listing's own shape, unchanged — so the
// Storefront holds one type per followable kind and a client that can draw a
// followed Tag can draw a suggested one. `curated` travels for the reason it
// travels on the listing: a Custom Tag is rendered as its Organization coined it
// and is marked as such, which matters more here than there, because the
// platform rather than the reader put it in front of them.
type SuggestedTagView struct {
	Tag    FollowedTagView   `json:"tag"`
	Reason *SuggestionReason `json:"reason"`
}

// SuggestedOrganizationView is one Organization being offered, in
// FollowedOrganizationView's shape for the same reason.
type SuggestedOrganizationView struct {
	Organization FollowedOrganizationView `json:"organization"`
	Reason       *SuggestionReason        `json:"reason"`
}

// FollowSuggestionsView is the whole panel: TWO GROUPS, never one interleaved
// list.
//
// The opposite choice from the Follows listing, and for the reason that decided
// that one too. The listing interleaves because `followed_at` is one real scale
// across both kinds; here a Tag's rank and an Organization's are not even the
// same unit — a normalised Co-occurrence score against a count of matching
// Events. Ordering them together would publish a comparability that does not
// exist, and the presentation splits them anyway: chips for one kind, rows for
// the other, so a reader knows what is being offered before reading a word.
//
// Both slices are non-nil and empty rather than null when nothing qualifies. An
// empty panel is a success — a Customer who Follows everything worth Following is
// the feature working, not failing — and a JSON null would make every consumer
// write the guard this does not need.
type FollowSuggestionsView struct {
	Tags          []SuggestedTagView          `json:"tags"`
	Organizations []SuggestedOrganizationView `json:"organizations"`
}

// ListFollowSuggestions returns the Tags and Organizations the signed-in
// Customer does not Follow, ranked by Activity.
//
// Gated by fullSessionCustomer, the same gate every Follow route sits behind,
// and the reason is one step further on than that gate's own. What a person is
// SUGGESTED is derived from what they Follow, so this response is a reading of
// their interests with an inference laid over it. A Confirmation Link session is
// possession of an email somebody was sent; it is not authority to read what its
// owner subscribed to, and it is no more authority to read what the platform
// concluded from that.
//
// The Tags are read first and the Organizations take whatever budget is left —
// which is the whole of the backfill between the two KINDS, and it is here
// rather than in SQL because it is a decision about the PANEL, spanning queries
// that know nothing of each other. The backfill between the two RANKINGS is
// here for the same reason.
func (s *Service) ListFollowSuggestions(ctx context.Context, token string) (*FollowSuggestionsView, error) {
	customer, err := s.fullSessionCustomer(ctx, token)
	if err != nil {
		return nil, err
	}

	// One instant for every query below. Reading the clock more than once would
	// let an Event end between two of them and put an Organization in the panel
	// whose only Tag had just dropped out of it — a skew nobody would ever
	// reproduce and everybody would blame on the ranking.
	now := s.now().UTC()

	tagRows, err := s.rankedTags(ctx, customer.ID, now)
	if err != nil {
		return nil, err
	}
	organizationRows, err := s.rankedOrganizations(ctx, customer.ID, now, maxSuggestions-len(tagRows))
	if err != nil {
		return nil, err
	}

	view := FollowSuggestionsView{
		Tags:          make([]SuggestedTagView, 0, len(tagRows)),
		Organizations: make([]SuggestedOrganizationView, 0, len(organizationRows)),
	}
	for _, row := range tagRows {
		view.Tags = append(view.Tags, SuggestedTagView{
			Tag: FollowedTagView{
				CanonicalKey: row.CanonicalKey,
				Name:         row.DisplayName,
				Curated:      row.Curated,
			},
			Reason: reason(row.ReasonTagCanonicalKey),
		})
	}
	for _, row := range organizationRows {
		view.Organizations = append(view.Organizations, SuggestedOrganizationView{
			Organization: s.suggestedOrganizationView(row),
			Reason:       reason(row.ReasonTagCanonicalKey),
		})
	}
	return &view, nil
}

// rankedTags is Co-occurrence first, Activity behind it.
//
// The second read is made ONLY for the slots the first left empty, and excludes
// what it already returned so the two cannot offer the same Tag twice. That
// exclusion is passed down to the query rather than filtered here, because a
// LIMIT applied before a filter means "some of the qualifying Tags" instead of
// "the best of them" — the same reason every other rule in this feature lives in
// SQL.
//
// A Customer who Follows nothing gets nothing from the first read and everything
// from the second, with no branch to say so. That is deliberate: a conditional
// here would be a second code path to keep working, where the empty case falls
// out of the query itself — and it is why widening the seeds to derived Tags
// (#233) needed nothing here at all. Which Customers meet Co-occurrence is a
// property of what the seed set contains, and the seed set is one CTE in the
// repository.
func (s *Service) rankedTags(ctx context.Context, customerID string, now time.Time) ([]repository.SuggestedTagRow, error) {
	related, err := s.repo.ListTagSuggestionsByCoOccurrence(ctx, customerID, now, maxSuggestedTags)
	if err != nil {
		return nil, err
	}
	if len(related) >= maxSuggestedTags {
		return related, nil
	}

	offered := make([]string, 0, len(related))
	for _, row := range related {
		offered = append(offered, row.CanonicalKey)
	}
	active, err := s.repo.ListTagSuggestionsByActivity(ctx, customerID, now, maxSuggestedTags-len(related), offered)
	if err != nil {
		return nil, err
	}
	return append(related, active...), nil
}

// rankedOrganizations is the same two rankings in the same order, over the
// budget the Tag group left.
//
// The Activity backfill matters MORE here than it does for Tags. An
// Organization is related to a followed Tag only through its own upcoming
// Events, so a Customer who Follows one narrow Tag can easily match no
// Organization at all — and an Organization running things this week is a
// defensible suggestion whether or not it happens to carry that Tag, which is
// why the panel offers it rather than leaving the space blank (ADR 0031).
func (s *Service) rankedOrganizations(ctx context.Context, customerID string, now time.Time, limit int) ([]repository.SuggestedOrganizationRow, error) {
	related, err := s.repo.ListOrganizationSuggestionsByCoOccurrence(ctx, customerID, now, limit)
	if err != nil {
		return nil, err
	}
	if len(related) >= limit {
		return related, nil
	}

	offered := make([]string, 0, len(related))
	for _, row := range related {
		offered = append(offered, row.Slug)
	}
	active, err := s.repo.ListOrganizationSuggestionsByActivity(ctx, customerID, now, limit-len(related), offered)
	if err != nil {
		return nil, err
	}
	return append(related, active...), nil
}

// reason turns the producing Tag's key into the nullable field on the wire, and
// is the one place a missing one becomes JSON null.
//
// A row from the Activity ranking carries no key and must report no reason at
// all: an empty string would put `{"tag_canonical_key": ""}` on the wire, which
// every consumer would have to learn to read as "none" and one of them would
// eventually render as a blank sentence.
func reason(key sql.NullString) *SuggestionReason {
	if !key.Valid || key.String == "" {
		return nil
	}
	return &SuggestionReason{TagCanonicalKey: key.String}
}

// suggestedOrganizationView turns a logo's object key into a URL, which is the
// one thing about an Organization that needs the service. It is deliberately not
// organizationFollowView: that one also carries `followed_at`, and a suggestion
// has no such instant to report — a nonsense value there is how a Storefront
// comes to sort suggestions by a date that means nothing.
func (s *Service) suggestedOrganizationView(row repository.SuggestedOrganizationRow) FollowedOrganizationView {
	view := FollowedOrganizationView{Name: row.Name, Slug: row.Slug}
	if row.LogoImageKey.Valid && s.storage != nil {
		url := s.storage.PublicURL(row.LogoImageKey.String)
		view.LogoURL = &url
	}
	return view
}
