package service

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

const (
	// maxTagLength caps a Tag's display name (runes).
	maxTagLength = 30
	// maxEventTags caps how many Tags one Event may carry.
	maxEventTags = 20
	// defaultTagSearchLimit bounds the typeahead result set.
	defaultTagSearchLimit = 20
)

// tagCharset restricts Custom Tag names to letters, digits, spaces, and hyphens.
var tagCharset = regexp.MustCompile(`^[\p{L}\p{N} -]+$`)

var multiSpace = regexp.MustCompile(`\s+`)

// TagView is a Tag as shown on an Event or in pool search results.
//
// CanonicalKey is the Tag's stable machine identity, and it is here so the
// Storefront can key a Preset Tag's Locale copy on something a copy edit to
// DisplayName cannot break (ADR 0027). Name stays the English display name and
// remains what an unrecognised Tag falls back to. This is still not the Tag's
// ID: Tags are addressed by name across the API, and CanonicalKey adds no way
// to address one that DisplayName did not already give.
type TagView struct {
	CanonicalKey string `json:"canonical_key"`
	Name         string `json:"name"`
	Curated      bool   `json:"curated"`
}

func toTagViews(tags []repository.Tag) []TagView {
	views := make([]TagView, 0, len(tags))
	for _, t := range tags {
		views = append(views, TagView{
			CanonicalKey: t.CanonicalKey,
			Name:         t.DisplayName,
			Curated:      t.Curated,
		})
	}
	return views
}

// SearchTags returns pool Tags matching the query for the staff typeahead,
// Preset Tags first. An empty query returns the top Tags.
func (s *Service) SearchTags(ctx context.Context, query string) ([]TagView, error) {
	canonical := canonicalTagKey(query)
	tags, err := s.repo.SearchTags(ctx, canonical, defaultTagSearchLimit)
	if err != nil {
		return nil, err
	}
	return toTagViews(tags), nil
}

// ListAvailablePresetTags returns the Preset Tags carried by at least one
// currently discoverable, upcoming Event, for the Storefront explorer's derived
// chip bar. The set is computed against the full discoverable-upcoming pool
// (independent of any q/date facet) so the chip bar stays stable as visitors
// refine their filters, and presets with no matching Event are omitted.
func (s *Service) ListAvailablePresetTags(ctx context.Context) ([]TagView, error) {
	tags, err := s.repo.ListAvailablePresetTags(ctx, s.now())
	if err != nil {
		return nil, err
	}
	return toTagViews(tags), nil
}

// ResolveTagIDByCanonicalKey turns the canonical key a Customer's Follow names
// into the Tag's id, and is this module's implementation of the customers
// module's TagResolver (#218).
//
// It is a seam rather than an exported repository call for the reason identity's
// ResolveOrganizationIDBySlug is one: the rules are whoever owns Tags'.
// Canonicalization is the first of them — every other path into the pool
// lowercases and collapses spaces, so "  MUSIC " and "music" must be one Follow
// rather than two, and running the same function is the only way to guarantee
// that as the rule changes. The second is that an unknown key is TAG_NOT_FOUND
// rather than an empty answer, because a Follow must not coin a Tag.
//
// EVERY Tag resolves, Preset and Custom alike. There is no `curated` filter and
// adding one would undo ADR 0030: the Digest's weekly cap bounds a Follow's
// volume, so narrowing the followable pool buys nothing and costs the narrow
// interest that is the best reason to Follow a Tag at all.
//
// It returns the id and nothing else, exactly as the Organization resolver does.
// The caller stores a foreign key and renders the Tag by joining to it, which
// keeps this the smallest thing that could serve the need — and keeps the id out
// of any struct that a response is built from.
func (s *Service) ResolveTagIDByCanonicalKey(ctx context.Context, rawKey string) (string, error) {
	key := canonicalTagKey(rawKey)
	if key == "" {
		return "", catalog.ErrTagNotFound()
	}

	tag, err := s.repo.GetTagByCanonicalKey(ctx, key)
	if err != nil {
		return "", err
	}
	if tag == nil {
		return "", catalog.ErrTagNotFound()
	}
	return tag.ID, nil
}

// LocalizedTagNames resolves Tag names in one Locale, keyed by canonical key.
//
// It exists for what a Storefront page cannot do for itself: mail carries no
// address, so the Follow Digest is composed here and has to name a Tag in the
// recipient's Mail Locale (ADR 0030). Every page keeps wording its own chips
// and badges from its message catalogue, and nothing on the API's wire gains a
// language — this is called service-to-service and never rendered into a
// response.
//
// A key naming no Tag is absent from the result rather than an error, and the
// caller renders what it can.
func (s *Service) LocalizedTagNames(ctx context.Context, canonicalKeys []string, locale platform.Locale) (map[string]string, error) {
	tags, err := s.repo.ListTagsByCanonicalKeys(ctx, canonicalTagKeys(canonicalKeys))
	if err != nil {
		return nil, err
	}
	names := make(map[string]string, len(tags))
	for _, t := range tags {
		names[t.CanonicalKey] = localizedTagName(t, locale)
	}
	return names, nil
}

// localizedTagName is the whole resolution rule, in one place.
//
// Spanish is read only from a Preset Tag that has it. Everything else — every
// Custom Tag, a Preset Tag promoted by flipping curated with no commit to carry
// its words, and English itself — reads display_name, which is the Tag's one
// name for every other reader in the system. That fallback is the same one ADR
// 0027 put in the Storefront and it is not a theoretical branch: ADR 0004 grows
// the Preset tier with an UPDATE no migration accompanies.
func localizedTagName(t repository.Tag, locale platform.Locale) string {
	if locale == platform.LocaleES && t.DisplayNameES.Valid && t.DisplayNameES.String != "" {
		return t.DisplayNameES.String
	}
	return t.DisplayName
}

// canonicalTagKeys canonicalizes raw tag names into deduped canonical keys for
// matching against the pool, dropping any that canonicalize to empty.
func canonicalTagKeys(names []string) []string {
	if len(names) == 0 {
		return nil
	}
	keys := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, raw := range names {
		key := canonicalTagKey(raw)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// ListPopularCustomTags returns the most-used Custom Tags across all
// Organizations for the staff editor's browse state, so an organizer can
// discover and reuse existing custom vocabulary before typing.
func (s *Service) ListPopularCustomTags(ctx context.Context) ([]TagView, error) {
	tags, err := s.repo.ListPopularCustomTags(ctx, defaultTagSearchLimit)
	if err != nil {
		return nil, err
	}
	return toTagViews(tags), nil
}

// ListEventTags returns the Tags assigned to an Event in the active Organization.
func (s *Service) ListEventTags(ctx context.Context, actor ActorContext, eventID string) ([]TagView, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}
	tags, err := s.repo.ListEventTags(ctx, eventID)
	if err != nil {
		return nil, err
	}
	return toTagViews(tags), nil
}

// SetEventTags replaces an Event's Tags with the given names, coining Custom
// Tags where none exist. Names are normalized to a canonical key so case and
// whitespace variants collapse to one pool row; invalid names are rejected.
func (s *Service) SetEventTags(ctx context.Context, actor ActorContext, eventID string, names []string) ([]TagView, error) {
	event, err := s.repo.GetEventByID(ctx, actor.OrganizationID, eventID)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, catalog.ErrEventNotFound()
	}

	normalized, invalid := normalizeTagNames(names)
	if len(invalid) > 0 {
		return nil, catalog.ErrInvalidTag(invalid)
	}
	if len(normalized) > maxEventTags {
		return nil, catalog.ErrTooManyTags(maxEventTags)
	}

	tags, err := s.repo.SetEventTags(ctx, eventID, normalized)
	if err != nil {
		return nil, err
	}
	return toTagViews(tags), nil
}

// normalizeTagNames canonicalizes and validates a list of raw Tag names,
// deduping by canonical key (first occurrence's display casing wins). It
// returns the resolved Tags and the list of raw names that failed validation.
func normalizeTagNames(names []string) ([]repository.NormalizedTag, []string) {
	var normalized []repository.NormalizedTag
	var invalid []string
	seen := make(map[string]struct{})

	for _, raw := range names {
		display := collapseSpaces(raw)
		key := strings.ToLower(display)

		if display == "" || utf8.RuneCountInString(display) > maxTagLength || !tagCharset.MatchString(display) {
			invalid = append(invalid, raw)
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, repository.NormalizedTag{CanonicalKey: key, DisplayName: display})
	}
	return normalized, invalid
}

// canonicalTagKey trims, collapses internal whitespace, and lowercases a name
// into the key used for pool uniqueness and search.
func canonicalTagKey(raw string) string {
	return strings.ToLower(collapseSpaces(raw))
}

func collapseSpaces(raw string) string {
	return strings.TrimSpace(multiSpace.ReplaceAllString(raw, " "))
}
