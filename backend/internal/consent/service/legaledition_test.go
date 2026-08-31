package service

import (
	"context"
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/consent/legal"
	"github.com/peter/ticket_pos/backend/internal/consent/policy"
	"github.com/peter/ticket_pos/backend/internal/consent/repository"
	"github.com/peter/ticket_pos/backend/internal/consent/terms"
	"github.com/peter/ticket_pos/backend/internal/platform"
)

// The property under test is #558's load-bearing one: TEXT AND FINGERPRINT FOR
// A (document, locale) ARE FILLED BY ONE ATOMIC READ, so an acceptance can
// never record a hash — or an edition id — for bytes that were not on screen.
//
// It is pinned HERE, on a Service whose repository would panic if it were
// touched, because that is the only way to state the interesting half: not
// "the two agree", which they do whenever nothing is published, but "there is
// no second read to disagree with the first". A test with a database can only
// ever observe the agreement; this one observes the absence of the query.

// A Service with NO REPOSITORY AT ALL. Every read below is served from the
// warm cache, and any path that went back to the database would dereference a
// nil *repository.Repository and take the test with it.
func serviceWithWarmCaches(t *testing.T, policyRead repository.PolicyEdition, termsRead repository.TermsEdition) *Service {
	t.Helper()
	s := &Service{
		logger:      discardLogger{},
		now:         time.Now,
		policyCache: newEditionCache[policyEdition](DefaultLegalTextCacheTTL),
		termsCache:  newEditionCache[termsEdition](DefaultLegalTextCacheTTL),
	}
	s.policyCache.store(s.policyEditionFrom(policyRead), time.Now())
	s.termsCache.store(s.termsEditionFrom(termsRead), time.Now())
	return s
}

type discardLogger struct{}

func (discardLogger) Info(string, ...any)  {}
func (discardLogger) Warn(string, ...any)  {}
func (discardLogger) Error(string, ...any) {}

func policyEditionRead(id, label, marker string) repository.PolicyEdition {
	slugs := []string{
		policy.SlugShortNotice,
		policy.SlugPolicyAcceptanceLabel,
		policy.SlugMarketingConsentLabel,
		policy.SlugNetworkingConsentLabel,
		policy.SlugPolicy,
	}
	artifacts := make([]legal.Artifact, 0, len(slugs)*2)
	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		for i, slug := range slugs {
			artifacts = append(artifacts, legal.Artifact{
				Locale:  locale,
				Slug:    slug,
				Ordinal: i + 1,
				Body:    slug + " " + string(locale) + " " + marker,
			})
		}
	}
	return repository.PolicyEdition{
		Version: repository.PolicyVersion{
			ID:            id,
			Label:         label,
			EffectiveDate: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
			ContentHash:   legal.ContentHash(artifacts),
		},
		Artifacts: artifacts,
	}
}

func termsEditionRead(id, label, marker string) repository.TermsEdition {
	slugs := []string{terms.SlugAcceptanceLabel, terms.SlugTerms}
	artifacts := make([]legal.Artifact, 0, len(slugs)*2)
	for _, locale := range []platform.Locale{platform.LocaleEN, platform.LocaleES} {
		for i, slug := range slugs {
			artifacts = append(artifacts, legal.Artifact{
				Locale:  locale,
				Slug:    slug,
				Ordinal: i + 1,
				Body:    slug + " " + string(locale) + " " + marker,
			})
		}
	}
	return repository.TermsEdition{
		Version: repository.TermsVersion{
			ID:            id,
			Label:         label,
			EffectiveDate: time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC),
			ContentHash:   legal.ContentHash(artifacts),
		},
		Artifacts: artifacts,
	}
}

// THE EDITION A CAPTURE WOULD RECORD IS THE ONE THE READER WAS SERVED, and it
// is resolved WITHOUT GOING BACK TO THE DATABASE. This is the midnight-rollover
// failure #558 exists to design out: the text comes from the cache, and a
// second read of the current version row would answer about the edition that
// took effect in between.
func TestTheRecordedEditionComesFromTheServedRead(t *testing.T) {
	t.Parallel()

	s := serviceWithWarmCaches(t,
		policyEditionRead("policy-edition-1", "1", "edition one"),
		termsEditionRead("terms-edition-1", "1", "edition one"))

	view, err := s.CurrentPolicy(context.Background(), "es")
	if err != nil {
		t.Fatalf("current policy: %v", err)
	}
	// currentPolicyEdition is what Capture stamps a Consent Record from. It must
	// answer from the same entry the view above came out of — with no repository
	// present, anything else panics rather than returning a plausible id.
	edition, err := s.currentPolicyEdition(context.Background())
	if err != nil {
		t.Fatalf("current policy edition: %v", err)
	}
	if edition.version.Label != view.Version || edition.version.ContentHash != view.ContentHash {
		t.Fatalf("the recorded edition and the served text disagree: %q/%q vs %q/%q",
			edition.version.Label, edition.version.ContentHash, view.Version, view.ContentHash)
	}
	if edition.version.ID != "policy-edition-1" {
		t.Fatalf("recorded policy edition id = %q", edition.version.ID)
	}

	termsView, err := s.CurrentTerms(context.Background(), "es")
	if err != nil {
		t.Fatalf("current terms: %v", err)
	}
	// The Terms' counterpart, reached the way the capture path reaches it: the
	// checkout's held edition (CurrentTermsVersionID) and the label on screen.
	held, err := s.CurrentTermsVersionID(context.Background())
	if err != nil {
		t.Fatalf("current terms version id: %v", err)
	}
	if held != "terms-edition-1" {
		t.Fatalf("held terms edition id = %q", held)
	}
	termsEdition, err := s.currentTermsEdition(context.Background())
	if err != nil {
		t.Fatalf("current terms edition: %v", err)
	}
	if termsEdition.version.ContentHash != termsView.ContentHash {
		t.Fatalf("the held Terms edition and the served text disagree: %q vs %q",
			termsEdition.version.ContentHash, termsView.ContentHash)
	}
}

// A REFILL MOVES THE ID AND THE TEXT TOGETHER. There is no window in which the
// cache serves edition one's words and edition two's id: the entry is one
// value, replaced whole.
func TestARefillMovesTheEditionIDAndItsTextTogether(t *testing.T) {
	t.Parallel()

	s := serviceWithWarmCaches(t,
		policyEditionRead("policy-edition-1", "1", "edition one"),
		termsEditionRead("terms-edition-1", "1", "edition one"))

	s.policyCache.store(s.policyEditionFrom(policyEditionRead("policy-edition-2", "2", "edition two")), time.Now())

	view, err := s.CurrentPolicy(context.Background(), "en")
	if err != nil {
		t.Fatalf("current policy: %v", err)
	}
	edition, err := s.currentPolicyEdition(context.Background())
	if err != nil {
		t.Fatalf("current policy edition: %v", err)
	}
	if edition.version.ID != "policy-edition-2" || view.Version != "2" {
		t.Fatalf("the refill was split: id %q, label %q", edition.version.ID, view.Version)
	}
	if edition.version.ContentHash != view.ContentHash {
		t.Fatalf("fingerprint after refill: %q vs served %q", edition.version.ContentHash, view.ContentHash)
	}
	if want := "policy " + string(platform.LocaleEN) + " edition two"; view.BodyMarkdown != want {
		t.Fatalf("served text after refill = %q", view.BodyMarkdown)
	}
}
