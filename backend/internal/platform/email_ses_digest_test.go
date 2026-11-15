package platform

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Follow Digest through SES (#696, ADR 0077), at the same seam as every
// transactional message: the sender's real request path against the fake SES
// v2 endpoint. The sender is built on the Digest's own identity, as the wiring
// builds it, and must send from that From and nothing else (ADR 0030).

const sesTestDigestFrom = "Multiticketing <digest@digest.multiticketing.com>"

// SESEmailSender is a Digest sender, so the wiring can select it for the
// Digest identity on its own.
var _ DigestEmailSender = (*SESEmailSender)(nil)

// sesTestDigest is a Spanish Digest with both sections filled and a
// mixed-case recipient, so the copy and the redaction both have something to
// bite on.
func sesTestDigest() FollowDigest {
	startsAt := time.Date(2026, 12, 5, 20, 0, 0, 0, time.UTC)
	return FollowDigest{
		To:           "Ana@Example.com",
		CustomerName: "Ana",
		Locale:       LocaleES,
		New: []FollowDigestEvent{{
			Name: "Noche de Jazz", StartsAt: startsAt, Timezone: "America/Guayaquil",
			OrganizationName: "Jazz Club", Venue: "Teatro Sucre", URL: "https://shop.example.com/e/jazz",
			MatchedOrganization: true,
		}},
		Happening: []FollowDigestEvent{{
			Name: "Feria del Libro", StartsAt: startsAt.AddDate(0, 0, 2), Timezone: "America/Guayaquil",
			OrganizationName: "Libros EC", URL: "https://shop.example.com/e/feria",
			MatchedTagNames: []string{"Literatura"},
		}},
		UnsubscribeURL: "https://shop.example.com/u/abc",
	}
}

// One Digest, one SES request: the Digest identity's From, the recipient
// alone, the shared copy as plain UTF-8 text, through the configuration set
// both identities share, with no attachments.
func TestSESSendsTheFollowDigestFromTheDigestIdentity(t *testing.T) {
	sender, fake := newTestSESSenderFrom(t, sesTestDigestFrom, noopLogger{})
	digest := sesTestDigest()

	if err := sender.SendFollowDigest(context.Background(), digest); err != nil {
		t.Fatalf("SendFollowDigest: %v", err)
	}

	sent := fake.sent()
	if len(sent) != 1 {
		t.Fatalf("fake SES saw %d requests, want 1", len(sent))
	}
	req := sent[0]
	if req.Method != http.MethodPost || req.Path != "/v2/email/outbound-emails" {
		t.Fatalf("request = %s %s, want POST /v2/email/outbound-emails", req.Method, req.Path)
	}
	body := req.Body
	if body.FromEmailAddress != sesTestDigestFrom {
		t.Fatalf("From = %q, want the Digest identity %q", body.FromEmailAddress, sesTestDigestFrom)
	}
	if got := body.Destination.ToAddresses; len(got) != 1 || got[0] != digest.To {
		t.Fatalf("To = %v, want [%s]", got, digest.To)
	}
	if body.ConfigurationSetName != "ticket-pos-transactional" {
		t.Fatalf("ConfigurationSetName = %q, want the shared configuration set", body.ConfigurationSetName)
	}
	simple := body.Content.Simple
	if simple == nil {
		t.Fatal("no Simple content in the request")
	}
	if simple.Subject.Data != digest.Subject() || simple.Subject.Charset != "UTF-8" {
		t.Fatalf("Subject = %+v, want %q in UTF-8", simple.Subject, digest.Subject())
	}
	if simple.Body.Text == nil || simple.Body.Text.Data != digest.Text() || simple.Body.Text.Charset != "UTF-8" {
		t.Fatalf("Text = %+v, want the shared copy %q in UTF-8", simple.Body.Text, digest.Text())
	}
	if !strings.Contains(simple.Body.Text.Data, "Noche de Jazz") {
		t.Fatalf("Text %q does not carry the Digest's Events", simple.Body.Text.Data)
	}
	if simple.Body.Html != nil {
		t.Fatalf("the Digest carries HTML; SES mail is plain text only: %+v", simple.Body.Html)
	}
	if len(simple.Attachments) != 0 {
		t.Fatalf("the Digest carries %d attachments, want none", len(simple.Attachments))
	}
}

// A rejected Digest reaches the drain, which retries it on its ladder. The
// error and the one failure log name the message type and the provider's
// error and never the recipient (#377), even when SES quotes it back.
func TestSESReturnsAFollowDigestFailureAndNeverLogsTheRecipient(t *testing.T) {
	logger := &lineLogger{}
	sender, fake := newTestSESSenderFrom(t, sesTestDigestFrom, logger)
	digest := sesTestDigest()
	fake.respond = func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Amzn-ErrorType", "MessageRejected")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Email address is not verified: ` + strings.ToUpper(digest.To) + `"}`))
	}

	err := sender.SendFollowDigest(context.Background(), digest)
	if err == nil {
		t.Fatal("SendFollowDigest returned nil on a rejected Digest; the drain would record it as sent")
	}
	recipient := strings.ToLower(digest.To)
	if !strings.Contains(err.Error(), "ses send follow digest") || !strings.Contains(err.Error(), "MessageRejected") {
		t.Fatalf("error %q does not name the message type and the provider's error", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), recipient) {
		t.Fatalf("error %q names the recipient", err)
	}

	lines := logger.errors()
	if len(lines) != 1 {
		t.Fatalf("logged %d error lines, want 1: %v", len(lines), lines)
	}
	line := lines[0].String()
	for _, want := range []string{"ses send follow digest failed", "MessageRejected", "locale es", "new 1", "happening 1"} {
		if !strings.Contains(line, want) {
			t.Fatalf("failure log %q does not carry %q", line, want)
		}
	}
	if strings.Contains(strings.ToLower(line), recipient) {
		t.Fatalf("failure log %q names the recipient", line)
	}
}
