package service

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/peter/ticket_pos/backend/internal/catalog"
	"github.com/peter/ticket_pos/backend/internal/catalog/repository"
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
type TagView struct {
	Name    string `json:"name"`
	Curated bool   `json:"curated"`
}

func toTagViews(tags []repository.Tag) []TagView {
	views := make([]TagView, 0, len(tags))
	for _, t := range tags {
		views = append(views, TagView{Name: t.DisplayName, Curated: t.Curated})
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
