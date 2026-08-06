package service

import (
	"context"

	"github.com/peter/ticket_pos/backend/internal/customers/repository"
)

// The Suggested Follow (#231, parent #229, ADR 0031): a Tag or an Organization
// the Customer does not Follow, offered to them as one they might.
//
// Ranked on ACTIVITY alone in this ticket — the count of discoverable upcoming
// Events carrying a Tag or run by an Organization. Activity measures supply and
// never audience; it counts Events, not the Customers who Follow (CONTEXT.md).
// Co-occurrence, which is what will make the panel about this reader rather than
// about the catalogue, is #232, and the Tags derived from a Customer's followed
// Organizations are #233. Until they land every suggestion carries a null
// reason, because there is no producing Tag to name.
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
// that #232's Organization reasons, which may want to name more than one Tag,
// widen this rather than replacing it.
//
// It is nil on every suggestion this ticket produces. Activity ranking has no
// producing Tag: the subject is offered because things are happening under it,
// not because of anything the Customer Follows.
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
// across both kinds; here a Tag's rank and an Organization's are computed over
// different populations, and #232 will make them different units outright — a
// normalised co-occurrence score against an Event count. Ordering them together
// would publish a comparability that does not exist, and the presentation splits
// them anyway: chips for one kind, rows for the other, so a reader knows what is
// being offered before reading a word.
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
// which is the whole of the backfill, and it is here rather than in SQL because
// it is a decision about the PANEL, spanning two queries that know nothing of
// each other.
func (s *Service) ListFollowSuggestions(ctx context.Context, token string) (*FollowSuggestionsView, error) {
	customer, err := s.fullSessionCustomer(ctx, token)
	if err != nil {
		return nil, err
	}

	// One instant for both queries. Reading the clock twice would let an Event
	// end between them and put an Organization in the panel whose only Tag had
	// just dropped out of it — a skew nobody would ever reproduce and everybody
	// would blame on the ranking.
	now := s.now().UTC()

	tagRows, err := s.repo.ListTagSuggestionsByActivity(ctx, customer.ID, now, maxSuggestedTags)
	if err != nil {
		return nil, err
	}

	organizationRows, err := s.repo.ListOrganizationSuggestionsByActivity(ctx, customer.ID, now, maxSuggestions-len(tagRows))
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
			// Null, explicitly. See SuggestionReason: Activity has no producing
			// Tag to name, and inventing one here — "because it is busy" — would
			// be the panel claiming a personalisation it has not made yet.
			Reason: nil,
		})
	}
	for _, row := range organizationRows {
		view.Organizations = append(view.Organizations, SuggestedOrganizationView{
			Organization: s.suggestedOrganizationView(row),
			Reason:       nil,
		})
	}
	return &view, nil
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
