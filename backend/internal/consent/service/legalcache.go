package service

import (
	"sync"
	"time"
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

// editionCache holds one legal document's current edition, ready to serve: ONE
// VALUE, filled by one read and replaced whole.
//
// THE ATOMICITY IS THE FEATURE. The cached value is the whole answer about a
// document — WHICH EDITION ROW it is, its label, its effective date, its
// content hash AND every string of text in every language it publishes —
// captured from a single read of a single edition. The failure being designed
// out is not a slow query, it is a capture surface rendering cached TEXT while
// the acceptance path separately re-reads the CURRENT VERSION ROW, recording a
// fresh fingerprint as evidence of bytes that were not on screen. That is why
// the version id is INSIDE the cached value and not read beside it: at a
// midnight rollover a second read answers about the next edition, and the
// record would name an edition nobody was shown. A cache of text alone, or one
// keyed on anything a reader can vary, reintroduces exactly that.
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
	ttl      time.Duration
	filledAt time.Time
	// filled distinguishes "never filled" from a fill whose value happens to be
	// the zero one. A COLD CACHE MUST BE A MISS: served as a hit it would answer
	// about a document nobody had read yet with an edition nobody published.
	filled  bool
	edition V
}

func newEditionCache[V any](ttl time.Duration) *editionCache[V] {
	return &editionCache[V]{ttl: ttl}
}

// load returns the filled edition if the fill is still fresh.
//
// The value it returns is never mutated after being stored, so callers read it
// without holding the lock; a fill that lands while it is being read simply
// replaces it for whoever asks next.
func (c *editionCache[V]) load(now time.Time) (V, bool) {
	var zero V
	if c.ttl <= 0 {
		return zero, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.filled || now.Sub(c.filledAt) >= c.ttl {
		return zero, false
	}
	return c.edition, true
}

// store replaces the whole edition at once. There is no per-Locale and no
// per-field store, on purpose: one edition's languages, its fingerprint and its
// row id are filled from one read or not at all, so no two answers about a
// document can ever come from two different editions.
func (c *editionCache[V]) store(edition V, now time.Time) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.edition = edition
	c.filled = true
	c.filledAt = now
}
