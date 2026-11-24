package integration

import (
	"testing"
	"time"

	"github.com/peter/ticket_pos/backend/internal/platform"
)

// What the Follow Digest carries for its HTML part (#880, spec #870), read off
// the Digest the drain really sends: each entry's square rendition and its
// Organization's Logo rendition as public URLs, empty while they are not
// rendered, and the explorer for its one button. How those land in the markup
// is the platform package's (email_digest_html_test.go).

// sentDigestEntry is the one entry named name in the one Digest sent to email.
func sentDigestEntry(t *testing.T, env *testEnv, email, name string) (platform.FollowDigest, platform.FollowDigestEvent) {
	t.Helper()
	var sent []platform.FollowDigest
	for _, d := range env.email.FollowDigestsSent() {
		if d.To == email {
			sent = append(sent, d)
		}
	}
	if len(sent) != 1 {
		t.Fatalf("%s received %d Digests, want 1", email, len(sent))
	}
	for _, e := range append(append([]platform.FollowDigestEvent{}, sent[0].New...), sent[0].Happening...) {
		if e.Name == name {
			return sent[0], e
		}
	}
	t.Fatalf("the Digest does not list %q", name)
	return platform.FollowDigest{}, platform.FollowDigestEvent{}
}

func TestFollowDigestEntriesCarryTheirRenditions(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	rendered := discoverableEvent(t, env, sessionID, "Rendered Fest", "rendered-fest", env.fixedClock.Add(20*24*time.Hour))
	discoverableEvent(t, env, sessionID, "Bare Fest", "bare-fest", env.fixedClock.Add(30*24*time.Hour))
	renditions := recordMailRenditions(t, rendered)
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	digest, withImages := sentDigestEntry(t, env, "ana@example.com", "Rendered Fest")
	if want := sharedStorage.PublicURL(renditions.square); withImages.SquareURL != want {
		t.Errorf("SquareURL = %q, want %q", withImages.SquareURL, want)
	}
	if want := sharedStorage.PublicURL(renditions.logo); withImages.LogoURL != want {
		t.Errorf("LogoURL = %q, want %q", withImages.LogoURL, want)
	}

	// The Logo is the Organization's, so the other Event shows it too; its own
	// square rendition is not made yet, so it shows none.
	_, bare := sentDigestEntry(t, env, "ana@example.com", "Bare Fest")
	if bare.SquareURL != "" {
		t.Errorf("an Event with no square rendition carries SquareURL %q", bare.SquareURL)
	}
	if bare.LogoURL != withImages.LogoURL {
		t.Errorf("LogoURL = %q, want the Organization's %q", bare.LogoURL, withImages.LogoURL)
	}

	if want := "http://storefront.example/"; digest.ExploreURL != want {
		t.Errorf("ExploreURL = %q, want the explorer %q", digest.ExploreURL, want)
	}
}

// An Event with a Cover Image and no Thumbnail shows the square rendition
// cropped from its Cover Image, made by the save itself.
func TestFollowDigestEntryWithNoThumbnailShowsItsCoverImageCropped(t *testing.T) {
	env := setupTest(t)
	sessionID := orgAdminSession(t, env)
	startsAt := env.fixedClock.Add(20 * 24 * time.Hour)
	eventID := discoverableEvent(t, env, sessionID, "Cover Fest", "cover-fest", startsAt)
	cover := uploadCover(t, env, sessionID, eventID, threeBands(900, 300))
	// The save carries the Event's date again: an edit is the whole form.
	patchEventMedia(t, env, sessionID, eventID, "Cover Fest", "cover-fest", map[string]any{
		"cover_image_key": cover, "starts_at": startsAt.Format(time.RFC3339),
		"timezone": "America/New_York", "venue_name": "The Hall",
	})
	keys := readRenditionKeys(t, eventID)
	if keys.square == "" {
		t.Fatal("the Cover Image's save made no square rendition")
	}
	assertSquareIs(t, keys.square, bandGreen)
	followingCustomer(t, env, "ana@example.com")

	enqueueFollowDigests(t, env)
	drainFollowDigests(t, env)

	_, entry := sentDigestEntry(t, env, "ana@example.com", "Cover Fest")
	if want := sharedStorage.PublicURL(keys.square); entry.SquareURL != want {
		t.Fatalf("SquareURL = %q, want the Cover Image's centre crop %q", entry.SquareURL, want)
	}
	if entry.LogoURL != "" {
		t.Fatalf("an Organization with no Logo rendition carries LogoURL %q", entry.LogoURL)
	}
}
