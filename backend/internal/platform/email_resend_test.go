package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// noopLogger satisfies Logger without asserting on output.
type noopLogger struct{}

func (noopLogger) Info(string, ...any)                   {}
func (noopLogger) Warn(string, ...any)                   {}
func (noopLogger) Error(context.Context, string, ...any) {}

// newTestSender points a ResendEmailSender at a stub server standing in for
// Resend, so the tests exercise the real request-building and response-handling
// path without touching the network.
func newTestSender(t *testing.T, handler http.HandlerFunc) *ResendEmailSender {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	s := NewResendEmailSender("test-key", "Multiticketing <noreply@send.multiticketing.com>", noopLogger{})
	// Rewrite every request to the stub, so the real request-building path runs
	// against the httptest server instead of api.resend.com.
	s.client = &http.Client{
		Timeout:   2 * time.Second,
		Transport: rewriteHost{to: srv.URL, base: srv.Client().Transport},
	}
	return s
}

// rewriteHost sends every request to the stub server regardless of its URL.
type rewriteHost struct {
	to   string
	base http.RoundTripper
}

func (r rewriteHost) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := req.URL.Parse(r.to)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func TestResendSendOTP_Success(t *testing.T) {
	var gotAuth, gotBody string
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	})

	if err := sender.SendOTP(context.Background(), "user@example.com", "123456", DefaultLocale); err != nil {
		t.Fatalf("SendOTP returned error: %v", err)
	}

	if gotAuth != "Bearer test-key" {
		t.Errorf("Authorization = %q, want Bearer test-key", gotAuth)
	}

	var payload resendRequest
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("body was not valid JSON: %v", err)
	}
	if len(payload.To) != 1 || payload.To[0] != "user@example.com" {
		t.Errorf("To = %v, want [user@example.com]", payload.To)
	}
	if payload.From != "Multiticketing <noreply@send.multiticketing.com>" {
		t.Errorf("From = %q", payload.From)
	}
	if payload.Text == "" || payload.Subject == "" {
		t.Errorf("empty subject or text: %+v", payload)
	}
	// The code must reach the recipient; a silent template drop would be worse
	// than a send error because the caller would report success.
	if want := "123456"; !contains(payload.Text, want) {
		t.Errorf("Text %q does not contain the passcode %q", payload.Text, want)
	}
}

func TestResendSendOTP_ProviderErrorIsReturned(t *testing.T) {
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"domain not verified"}`))
	})

	err := sender.SendOTP(context.Background(), "user@example.com", "123456", DefaultLocale)
	if err == nil {
		t.Fatal("SendOTP returned nil on a 422; a failed OTP send must surface so login fails")
	}
	if !contains(err.Error(), "422") {
		t.Errorf("error %q should carry the status code for diagnosis", err.Error())
	}
}

func TestResendSaleConfirmation_Success(t *testing.T) {
	var payload resendRequest
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &payload)
		w.WriteHeader(http.StatusOK)
	})

	err := sender.SendSaleConfirmation(context.Background(), SaleConfirmation{
		To:           "buyer@example.com",
		CustomerName: "Ada",
		EventName:    "Midnight Synth",
		Reference:    "REF-1",
	})
	if err != nil {
		t.Fatalf("SendSaleConfirmation error: %v", err)
	}
	if !contains(payload.Text, "REF-1") || !contains(payload.Text, "Midnight Synth") {
		t.Errorf("confirmation missing reference or event: %q", payload.Text)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (needle == "" || indexOf(haystack, needle) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// A failed send must not put the recipient into the log: on 2026-08-23 a Resend
// 429 wrote three customer addresses to Cloud Logging through exactly this path
// (#377). The caller's neighbouring line carries the Sale or Ticket id, which is
// enough to correlate without naming anyone.
func TestResendFailureLogNamesNoRecipient(t *testing.T) {
	var buf bytes.Buffer
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"rate limited"}`))
	})
	sender.logger = NewSlogLogger(slog.New(slog.NewTextHandler(&buf, nil)), NoopReporter{})

	const to = "holder@example.com"
	ctx := context.Background()
	_ = sender.SendOTP(ctx, to, "123456", DefaultLocale)
	_ = sender.SendSaleConfirmation(ctx, SaleConfirmation{To: to, Reference: "REF-1", Locale: DefaultLocale})
	_ = sender.SendAssignmentReminder(ctx, AssignmentReminder{To: to, Locale: DefaultLocale})
	// The refusing Digest sender is a sender too, and its only line is a failure.
	_ = NewUnconfiguredDigestSender(sender.logger, "no identity").SendFollowDigest(ctx, FollowDigest{To: to})

	got := buf.String()
	if !contains(got, "resend send otp failed") || !contains(got, "resend send assignment reminder failed") {
		t.Fatalf("expected failure lines to be logged, got:\n%s", got)
	}
	if contains(got, to) || contains(got, "email=") {
		t.Errorf("failure log names the recipient:\n%s", got)
	}
	if !contains(got, "429") {
		t.Errorf("failure log should still carry the provider error:\n%s", got)
	}
}

// TestResendSendTaxDocumentDeliveryAttachesTheDocument: both attachments
// travel inline on the one POST, in order, each base64-encoded under its
// filename and media type, beside the rendered subject and text (#496).
func TestResendSendTaxDocumentDeliveryAttachesTheDocument(t *testing.T) {
	var gotBody string
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	})

	d := TaxDocumentDelivery{
		To:              "buyer@example.com",
		Kind:            TaxDocumentKindSaleInvoice,
		CustomerName:    "Ana Lopez",
		EventName:       "House Fest",
		Reference:       "REF1",
		CustomerAreaURL: "https://example.test/tickets#sale-1",
		Attachments: []EmailAttachment{
			{Filename: "clave.xml", ContentType: "application/xml; charset=utf-8", Body: []byte("<factura/>")},
			{Filename: "clave.pdf", ContentType: "application/pdf", Body: []byte("%PDF-1.3")},
		},
		Locale: LocaleEN,
	}
	if err := sender.SendTaxDocumentDelivery(context.Background(), d); err != nil {
		t.Fatalf("SendTaxDocumentDelivery returned error: %v", err)
	}

	var payload resendRequest
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("body was not valid JSON: %v", err)
	}
	if len(payload.To) != 1 || payload.To[0] != "buyer@example.com" || payload.Subject != d.Subject() || payload.Text != d.Text() {
		t.Fatalf("payload = %+v, want the rendered message to the buyer", payload)
	}
	if len(payload.Attachments) != 2 {
		t.Fatalf("attachments = %+v, want the XML and the RIDE", payload.Attachments)
	}
	a := payload.Attachments[0]
	if a.Filename != "clave.xml" || a.ContentType != "application/xml; charset=utf-8" || a.Content != base64.StdEncoding.EncodeToString([]byte("<factura/>")) {
		t.Fatalf("attachment = %+v, want the XML base64-encoded under its filename", a)
	}
	r := payload.Attachments[1]
	if r.Filename != "clave.pdf" || r.ContentType != "application/pdf" || r.Content != base64.StdEncoding.EncodeToString([]byte("%PDF-1.3")) {
		t.Fatalf("attachment = %+v, want the RIDE base64-encoded under its filename", r)
	}
}

// TestResendMessagesWithoutAttachmentsCarryNoAttachmentsKey: every earlier
// message's payload is exactly what it was before attachments existed.
func TestResendMessagesWithoutAttachmentsCarryNoAttachmentsKey(t *testing.T) {
	var gotBody string
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	})
	if err := sender.SendOTP(context.Background(), "user@example.com", "123456", DefaultLocale); err != nil {
		t.Fatalf("SendOTP returned error: %v", err)
	}
	if strings.Contains(gotBody, "attachments") {
		t.Fatalf("body = %s, want no attachments key", gotBody)
	}
}

// TestResendSendAnnulmentNoticeCarriesNoAttachments (#748): the Annulment
// Notice is the rendered words to the factura's Recipient, and nothing is
// attached - the documents it speaks of are void.
func TestResendSendAnnulmentNoticeCarriesNoAttachments(t *testing.T) {
	var gotBody string
	sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"abc"}`))
	})
	n := AnnulmentNotice{
		To: "buyer@example.com", CustomerName: "Ana Lopez", EventName: "House Fest", Reference: "REF1",
		Number: "001-001-000000001", CustomerAreaURL: "https://example.test/tickets#sale-1", Locale: LocaleES,
	}
	if err := sender.SendAnnulmentNotice(context.Background(), n); err != nil {
		t.Fatalf("SendAnnulmentNotice returned error: %v", err)
	}
	var payload resendRequest
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("body was not valid JSON: %v", err)
	}
	if len(payload.To) != 1 || payload.To[0] != "buyer@example.com" || payload.Subject != n.Subject() || payload.Text != n.Text() {
		t.Fatalf("payload = %+v, want the rendered notice to the buyer", payload)
	}
	if strings.Contains(gotBody, "attachments") {
		t.Fatalf("body = %s, want no attachments", gotBody)
	}
}

// A provider refusal is classified, not parsed (#848). The 4xx answers that say
// "this request will never be accepted as it stands" are permanent; a timeout, a
// rate limit, a 5xx and a connection that never got an answer are transient,
// because waiting is exactly what fixes them.
func TestResendRefusalIsClassifiedPermanentOrTransient(t *testing.T) {
	cases := []struct {
		status    int
		permanent bool
	}{
		{http.StatusBadRequest, true},
		{http.StatusUnauthorized, true},
		{http.StatusForbidden, true},
		{http.StatusUnprocessableEntity, true},
		{http.StatusRequestTimeout, false},
		{http.StatusTooManyRequests, false},
		{http.StatusInternalServerError, false},
		{http.StatusServiceUnavailable, false},
	}
	for _, c := range cases {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(c.status)
				_, _ = w.Write([]byte(`{"message":"refused"}`))
			})
			err := sender.SendFollowDigest(context.Background(), FollowDigest{To: "reader@example.com", Locale: DefaultLocale})
			if err == nil {
				t.Fatalf("status %d: SendFollowDigest returned nil, want an error", c.status)
			}
			if got := IsPermanentEmailRefusal(err); got != c.permanent {
				t.Fatalf("status %d: IsPermanentEmailRefusal = %v, want %v (err %v)", c.status, got, c.permanent, err)
			}
			// Wrapped by a caller, the classification survives.
			if got := IsPermanentEmailRefusal(fmt.Errorf("send follow digest: %w", err)); got != c.permanent {
				t.Fatalf("status %d: wrapped IsPermanentEmailRefusal = %v, want %v", c.status, got, c.permanent)
			}
			var refusal *EmailProviderRefusal
			if !errors.As(err, &refusal) || refusal.Status != c.status {
				t.Fatalf("status %d: error does not carry the provider status: %v", c.status, err)
			}
		})
	}

	t.Run("connection failure", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
		sender := NewResendEmailSender("test-key", "Multiticketing <noreply@send.multiticketing.com>", noopLogger{})
		sender.client = &http.Client{Timeout: 2 * time.Second, Transport: rewriteHost{to: srv.URL, base: srv.Client().Transport}}
		srv.Close() // nothing is listening any more
		err := sender.SendFollowDigest(context.Background(), FollowDigest{To: "reader@example.com", Locale: DefaultLocale})
		if err == nil {
			t.Fatal("SendFollowDigest to a closed server returned nil, want an error")
		}
		if IsPermanentEmailRefusal(err) {
			t.Fatalf("a connection failure classified as permanent: %v", err)
		}
	})
}

// A failed Follow Digest send is logged at Warn, never at Error (#848): the
// drain retries it or gives it up, and the give-up is the one reported event
// per Digest (ADR 0079), so the sender's own line must not report it too.
func TestResendFollowDigestFailureIsNotReported(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusServiceUnavailable} {
		sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		})
		var levels levelRecorder
		sender.logger = &levels
		_ = sender.SendFollowDigest(context.Background(), FollowDigest{To: "reader@example.com", Locale: DefaultLocale})
		if levels.errors != 0 || levels.warns != 1 {
			t.Fatalf("status %d: errors=%d warns=%d, want 0 and 1", status, levels.errors, levels.warns)
		}
	}
}

// levelRecorder counts lines per level.
type levelRecorder struct{ warns, errors int }

func (*levelRecorder) Info(string, ...any)                     {}
func (l *levelRecorder) Warn(string, ...any)                   { l.warns++ }
func (l *levelRecorder) Error(context.Context, string, ...any) { l.errors++ }

// Every mail kind through Resend: the shared copy as the subject and text,
// the HTML part beside it as html where the kind has one and no html key
// where it does not yet (ADR 0082), and the attachments on the Tax Document
// delivery alone.
func TestResendSendsEveryMessageWithBothParts(t *testing.T) {
	for _, tc := range everyMessageCases() {
		t.Run(tc.name, func(t *testing.T) {
			var gotBody []byte
			sender := newTestSender(t, func(w http.ResponseWriter, r *http.Request) {
				gotBody, _ = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
			})
			if err := tc.send(context.Background(), sender); err != nil {
				t.Fatalf("send: %v", err)
			}
			var payload resendRequest
			if err := json.Unmarshal(gotBody, &payload); err != nil {
				t.Fatalf("body was not valid JSON: %v", err)
			}
			if len(payload.To) != 1 || payload.To[0] != tc.to {
				t.Fatalf("To = %v, want [%s]", payload.To, tc.to)
			}
			if payload.Subject != tc.subject || payload.Text != tc.text {
				t.Fatalf("subject or text is not the shared copy: %q / %q", payload.Subject, payload.Text)
			}
			if payload.HTML != tc.html {
				t.Fatalf("html (%d bytes) is not the message's HTML part (%d bytes)", len(payload.HTML), len(tc.html))
			}
			if tc.html == "" && bytes.Contains(gotBody, []byte(`"html"`)) {
				t.Fatalf("a kind with no HTML part yet sent an html key")
			}
			if len(payload.Attachments) != len(tc.attachments) {
				t.Fatalf("request carries %d attachments, want %d", len(payload.Attachments), len(tc.attachments))
			}
			for i, want := range tc.attachments {
				got := payload.Attachments[i]
				if got.Filename != want.Filename || got.ContentType != want.ContentType {
					t.Errorf("attachment %d = %q (%q), want %q (%q)", i, got.Filename, got.ContentType, want.Filename, want.ContentType)
				}
				if body, err := base64.StdEncoding.DecodeString(got.Content); err != nil || !bytes.Equal(body, want.Body) {
					t.Errorf("attachment %d bytes = %q (%v), want %q", i, body, err, want.Body)
				}
			}
		})
	}
}
