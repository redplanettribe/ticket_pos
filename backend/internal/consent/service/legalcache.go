package service

import (
	"sync"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// DefaultLegalTextCacheTTL is how long a filled legal document is served
// without going back to the database.
//
// SIXTY SECONDS, AND THE TTL IS THE LEAST IMPORTANT THING ABOUT THIS CACHE.
// What matters is that a fill is ATOMIC (see editionCache): the number only
// decides how long after a publication a reader can still be shown the previous
// edition, and a minute of that is a cost this platform can carry — a
// publication is an act somebody performs deliberately, and nobody performs it
// expecting the world to change in the same second.
//
// It is deliberately short enough that no operator ever needs to know it
// exists, and it is the ONLY invalidation there is: publishing invalidates
// NOTHING explicitly. That is not laziness, it is what keeps the scheduled
// edition free — `effective_date <= CURRENT_DATE` means an edition becomes
// current at midnight because THE QUERY MOVES, with no job to run and nothing
// to fire. A publish hook that cleared this cache would work, and it would also
// be a hook the scheduled edition does not have, so the two paths would stop
// being the same path. One expiry rule, both cases.
const DefaultLegalTextCacheTTL = 60 * time.Second

// editionCache holds one legal document's current edition, ready to serve: one
// entry per Locale, all of them filled by ONE read and replaced together.
//
// THE ATOMICITY IS THE FEATURE. Each entry is the whole answer for a
// (document, Locale) — the version label, its effective date, its content hash
// AND every string of text — captured from a single read of a single edition.
// The failure being designed out is not a slow query, it is a capture surface
// rendering cached TEXT while the acceptance path separately re-reads the
// CURRENT VERSION ROW, recording a fresh fingerprint as evidence of bytes that
// were not on screen. A cache of text alone, or a cache keyed on anything a
// reader can vary, reintroduces exactly that.
//
// It lives HERE, in the backend service, and not in the BFF and not in HTTP
// headers. The Storefront's fetch stays `cache: "no-store"` and no
// `Cache-Control` is emitted, because a CDN holding a superseded privacy policy
// is a staleness this platform cannot reach in and fix.
type editionCache[V any] struct {
	mu sync.Mutex
	// ttl of zero or less disables caching entirely — every read goes to the
	// database. That is how the integration harness runs: its clock is frozen
	// and its tests publish editions mid-run, so a cache would be a source of
	// answers from the wrong edition rather than a saved query.
	ttl       time.Duration
	filledAt  time.Time
	documents map[platform.Locale]V
}

func newEditionCache[V any](ttl time.Duration) *editionCache[V] {
	return &editionCache[V]{ttl: ttl}
}

// load returns the filled entries if the fill is still fresh.
//
// The map it returns is never mutated after being stored, so callers read it
// without holding the lock; a fill that lands while it is being read simply
// replaces the map for whoever asks next.
func (c *editionCache[V]) load(now time.Time) (map[platform.Locale]V, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.documents == nil || now.Sub(c.filledAt) >= c.ttl {
		return nil, false
	}
	return c.documents, true
}

// store replaces every entry at once. There is no per-Locale store, on purpose:
// one edition's languages are filled from one read or not at all, so two
// languages can never be served from two different editions.
func (c *editionCache[V]) store(documents map[platform.Locale]V, now time.Time) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.documents = documents
	c.filledAt = now
}
