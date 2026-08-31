package service

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// A filled entry is served until the TTL elapses, and then it is not. The TTL
// is the uninteresting half of this cache; it is here so the arithmetic is
// pinned somewhere.
func TestAFilledEditionIsServedUntilItsTTLElapses(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	cache := newEditionCache[string](60 * time.Second)
	cache.store(map[platform.Locale]string{platform.LocaleEN: "first"}, start)

	if _, ok := cache.load(start.Add(59 * time.Second)); !ok {
		t.Error("a fill was dropped before its TTL elapsed")
	}
	if _, ok := cache.load(start.Add(60 * time.Second)); ok {
		t.Error("a fill outlived its TTL")
	}
}

// A COLD CACHE IS A MISS, not an empty answer. The difference matters: an empty
// map served as a hit would 404 every language of a document nobody had read
// yet.
func TestAColdCacheIsAMiss(t *testing.T) {
	t.Parallel()

	if _, ok := newEditionCache[string](60 * time.Second).load(time.Now()); ok {
		t.Fatal("a cache that has never been filled reported a hit")
	}
}

// EVERY LANGUAGE IS REPLACED AT ONCE. There is no per-Locale store, so two
// languages can never be served from two different editions — the property this
// cache exists for, and the one the TTL has nothing to do with.
func TestOneFillReplacesEveryLanguageTogether(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	cache := newEditionCache[string](60 * time.Second)
	cache.store(map[platform.Locale]string{
		platform.LocaleEN: "edition one, english",
		platform.LocaleES: "edition one, spanish",
	}, start)
	cache.store(map[platform.Locale]string{
		platform.LocaleEN: "edition two, english",
		platform.LocaleES: "edition two, spanish",
	}, start)

	views, ok := cache.load(start)
	if !ok {
		t.Fatal("the refill was not served")
	}
	if views[platform.LocaleEN] != "edition two, english" || views[platform.LocaleES] != "edition two, spanish" {
		t.Fatalf("the two languages came from different fills: %v", views)
	}
}

// An edition that publishes one language caches one language: the published set
// is the edition's own answer, and a missing entry is a 404 rather than a fill
// nobody performed.
func TestALanguageTheEditionDoesNotPublishIsSimplyAbsent(t *testing.T) {
	t.Parallel()

	start := time.Now()
	cache := newEditionCache[string](60 * time.Second)
	cache.store(map[platform.Locale]string{platform.LocaleES: "solo español"}, start)

	views, _ := cache.load(start)
	if _, ok := views[platform.LocaleEN]; ok {
		t.Fatal("a language the edition does not publish was cached")
	}
}

// A TTL of zero disables the cache entirely: every read goes to the database.
// That is how the integration harness runs, because it publishes editions
// mid-run against a frozen clock.
func TestAZeroTTLDisablesTheCache(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cache := newEditionCache[string](0)
	cache.store(map[platform.Locale]string{platform.LocaleEN: "cached"}, now)
	if _, ok := cache.load(now); ok {
		t.Fatal("a cache with no TTL served a fill")
	}
}
